// Package pipelines merkt sich die vom Dashboard gestarteten, noch laufenden GitLab-Pipelines je Objekt und Aktion.
//
// Der Stand liegt im Speicher (als JSON-Bytes, damit Leser immer Kopien bekommen) und wird bei jeder Änderung in eine ConfigMap im
// Dashboard-Namespace geschrieben, damit er einen Neustart übersteht. Die ConfigMap legt das Backend selbst an; sie
// steht nicht in den Deploy-Manifesten, damit ArgoCD sie nicht zurücksetzt. Sie wird nicht per Informer beobachtet:
// Das Backend läuft mit genau einer Replica und ist ihr einziger Schreiber.
package pipelines

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"slices"
	"strings"
	"sync"
	"time"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"

	"feature-dashboard/internal/model"
)

// DataKey ist der Schlüssel in der ConfigMap, unter dem die laufenden Pipelines als JSON-Liste stehen.
const DataKey = "pipelines.json"

// Entry ist eine laufende Pipeline, die eine Aktion auf einem Objekt gestartet hat.
type Entry struct {
	Kind      model.Kind `json:"kind"`
	Namespace string     `json:"namespace"`
	Name      string     `json:"name"`
	Action    string     `json:"action"`
	ID        int        `json:"id"`
	Project   string     `json:"project"`
	WebURL    string     `json:"webUrl"`
	StartedAt time.Time  `json:"startedAt"`
}

// Ref ist das Objekt, auf dem die Pipeline gestartet wurde.
func (e Entry) Ref() model.ObjectRef {
	return model.ObjectRef{Kind: e.Kind, Namespace: e.Namespace, Name: e.Name}
}

type key struct {
	ref    model.ObjectRef
	action string
}

// State hält die laufenden Pipelines. Je Objekt und Aktion gibt es höchstens einen Eintrag.
type State struct {
	client    kubernetes.Interface
	namespace string
	name      string

	mu              sync.Mutex
	entries         map[key]json.RawMessage
	resourceVersion string
	revisions       map[string]time.Time
	lastTimestamp   time.Time
	checkers        map[string]StatusChecker
	changed         chan struct{}
}

// NewState erzeugt einen leeren State für die ConfigMap namespace/name. Vor der Nutzung muss Load laufen.
func NewState(client kubernetes.Interface, namespace, name string) *State {
	return &State{
		client: client, namespace: namespace, name: name,
		entries:   map[key]json.RawMessage{},
		revisions: map[string]time.Time{},
		checkers:  map[string]StatusChecker{},
		changed:   make(chan struct{}, 1),
	}
}

// Load liest die ConfigMap und legt sie an, falls sie fehlt.
func (s *State) Load(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	cms := s.client.CoreV1().ConfigMaps(s.namespace)
	cm, err := cms.Get(ctx, s.name, metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		cm, err = cms.Create(ctx, &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{Name: s.name, Namespace: s.namespace,
				Labels: map[string]string{"app.kubernetes.io/managed-by": "feature-dashboard"}},
			Data: map[string]string{DataKey: "[]"},
		}, metav1.CreateOptions{})
	}
	if err != nil {
		return fmt.Errorf("configmap %s/%s für laufende Pipelines: %w", s.namespace, s.name, err)
	}
	var list []Entry
	if raw := strings.TrimSpace(cm.Data[DataKey]); raw != "" {
		if err := json.Unmarshal([]byte(raw), &list); err != nil {
			return fmt.Errorf("configmap %s/%s: %s ist kein gültiges JSON: %w", s.namespace, s.name, DataKey, err)
		}
	}
	s.entries = map[key]json.RawMessage{}
	for _, e := range list {
		raw, err := json.Marshal(e)
		if err != nil {
			return err
		}
		s.entries[key{e.Ref(), e.Action}] = raw
		s.touch(e.Namespace)
	}
	s.resourceVersion = cm.ResourceVersion
	s.notify()
	return nil
}

// Set merkt sich die zuletzt gestartete Pipeline einer Aktion auf einem Objekt und ersetzt einen älteren Eintrag.
func (s *State) Set(ctx context.Context, e Entry) error {
	raw, err := json.Marshal(e)
	if err != nil {
		return err
	}
	return s.update(ctx, e.Namespace, func(entries map[key]json.RawMessage) bool {
		k := key{e.Ref(), e.Action}
		if bytes.Equal(entries[k], raw) {
			return false
		}
		entries[k] = raw
		return true
	})
}

// Remove entfernt den Eintrag einer Aktion auf einem Objekt, z. B. weil die Pipeline beendet ist.
func (s *State) Remove(ctx context.Context, ref model.ObjectRef, action string) error {
	return s.update(ctx, ref.Namespace, func(entries map[key]json.RawMessage) bool {
		k := key{ref, action}
		if _, ok := entries[k]; !ok {
			return false
		}
		delete(entries, k)
		return true
	})
}

// update ändert eine Kopie der Einträge, schreibt sie in die ConfigMap und übernimmt sie erst danach.
func (s *State) update(ctx context.Context, namespace string, change func(map[key]json.RawMessage) bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	next := maps.Clone(s.entries)
	if !change(next) {
		return nil
	}
	if err := s.persist(ctx, next); err != nil {
		return err
	}
	s.entries = next
	s.touch(namespace)
	s.notify()
	return nil
}

// persist schreibt die Einträge in die ConfigMap. Das Backend ist der einzige Schreiber; bei einem Konflikt (z. B.
// nach einer manuellen Änderung) gilt trotzdem der Stand im Speicher, geschrieben mit der aktuellen resourceVersion.
func (s *State) persist(ctx context.Context, entries map[key]json.RawMessage) error {
	data := serialize(entries)
	cms := s.client.CoreV1().ConfigMaps(s.namespace)
	for attempt := 0; ; attempt++ {
		cm := &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{Name: s.name, Namespace: s.namespace, ResourceVersion: s.resourceVersion,
				Labels: map[string]string{"app.kubernetes.io/managed-by": "feature-dashboard"}},
			Data: map[string]string{DataKey: data},
		}
		updated, err := cms.Update(ctx, cm, metav1.UpdateOptions{})
		if err == nil {
			s.resourceVersion = updated.ResourceVersion
			return nil
		}
		if !apierrors.IsConflict(err) || attempt > 0 {
			return fmt.Errorf("configmap %s/%s schreiben: %w", s.namespace, s.name, err)
		}
		current, getErr := cms.Get(ctx, s.name, metav1.GetOptions{})
		if getErr != nil {
			return fmt.Errorf("configmap %s/%s schreiben: %w", s.namespace, s.name, errors.Join(err, getErr))
		}
		s.resourceVersion = current.ResourceVersion
	}
}

// serialize liefert die Einträge als JSON-Liste, sortiert nach Namespace, Objekt und Aktion.
func serialize(entries map[key]json.RawMessage) string {
	keys := slices.SortedFunc(maps.Keys(entries), func(a, b key) int {
		return strings.Compare(a.ref.Namespace+"\x00"+string(a.ref.Kind)+"\x00"+a.ref.Name+"\x00"+a.action,
			b.ref.Namespace+"\x00"+string(b.ref.Kind)+"\x00"+b.ref.Name+"\x00"+b.action)
	})
	var buf bytes.Buffer
	buf.WriteByte('[')
	for i, k := range keys {
		if i > 0 {
			buf.WriteByte(',')
		}
		buf.Write(entries[k])
	}
	buf.WriteByte(']')
	return buf.String()
}

// For liefert die laufenden Pipelines eines Objekts als Kopie, sortiert nach Aktion (nie nil).
func (s *State) For(ref model.ObjectRef) []Entry {
	s.mu.Lock()
	defer s.mu.Unlock()
	result := []Entry{}
	for k, raw := range s.entries {
		if k.ref != ref {
			continue
		}
		var e Entry
		if json.Unmarshal(raw, &e) == nil {
			result = append(result, e)
		}
	}
	slices.SortFunc(result, func(a, b Entry) int { return strings.Compare(a.Action, b.Action) })
	return result
}

// All liefert alle laufenden Pipelines als Kopie.
func (s *State) All() []Entry {
	s.mu.Lock()
	defer s.mu.Unlock()
	result := make([]Entry, 0, len(s.entries))
	for _, raw := range s.entries {
		var e Entry
		if json.Unmarshal(raw, &e) == nil {
			result = append(result, e)
		}
	}
	return result
}

// Changes signalisiert Änderungen (koaleszierend); den aktuellen Stand liefert NamespaceRevisions.
func (s *State) Changes() <-chan struct{} { return s.changed }

// NamespaceRevisions liefert je Namespace den Zeitpunkt der letzten Änderung an seinen Pipelines.
func (s *State) NamespaceRevisions() map[string]time.Time {
	s.mu.Lock()
	defer s.mu.Unlock()
	return maps.Clone(s.revisions)
}

// touch setzt einen streng monoton steigenden Zeitstempel für den Namespace. Aufruf nur unter Lock.
func (s *State) touch(namespace string) {
	ts := time.Now()
	if !ts.After(s.lastTimestamp) {
		ts = s.lastTimestamp.Add(time.Nanosecond)
	}
	s.lastTimestamp = ts
	s.revisions[namespace] = ts
}

func (s *State) notify() {
	select {
	case s.changed <- struct{}{}:
	default:
	}
}
