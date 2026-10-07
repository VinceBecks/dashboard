// Package pipelines merkt sich die vom Dashboard gestarteten, noch laufenden GitLab-Pipelines je Objekt und Aktion.
//
// Der Stand liegt nur im Speicher. Nach einem Neustart des Backends ist er leer; bereits laufende Pipelines werden
// dann nicht mehr angezeigt. Das ist bewusst so: Es gibt keine Persistenz außerhalb des Informer-Caches.
package pipelines

import (
	"cmp"
	"maps"
	"slices"
	"sync"
	"time"

	"feature-dashboard/internal/model"
)

// Entry ist eine laufende Pipeline, die eine Aktion auf einem Objekt gestartet hat.
type Entry struct {
	Kind      model.Kind
	Namespace string
	Name      string
	Action    string
	ID        int
	Project   string
	WebURL    string
	StartedAt time.Time
}

// Ref ist das Objekt, auf dem die Pipeline gestartet wurde.
func (e Entry) Ref() model.ObjectRef {
	return model.ObjectRef{Kind: e.Kind, Namespace: e.Namespace, Name: e.Name}
}

type key struct {
	ref    model.ObjectRef
	action string
}

// State hält die laufenden Pipelines. Je Objekt und Aktion gibt es höchstens einen Eintrag. Entry enthält nur
// Werttypen; Leser bekommen daher immer Kopien.
type State struct {
	mu            sync.Mutex
	entries       map[key]Entry
	revisions     map[string]time.Time
	lastTimestamp time.Time
	checkers      map[string]StatusChecker
	changed       chan struct{}
}

// NewState erzeugt einen leeren State.
func NewState() *State {
	return &State{
		entries:   map[key]Entry{},
		revisions: map[string]time.Time{},
		checkers:  map[string]StatusChecker{},
		changed:   make(chan struct{}, 1),
	}
}

// Set merkt sich die zuletzt gestartete Pipeline einer Aktion auf einem Objekt und ersetzt einen älteren Eintrag.
func (s *State) Set(e Entry) {
	s.mu.Lock()
	defer s.mu.Unlock()
	k := key{e.Ref(), e.Action}
	if old, ok := s.entries[k]; ok && old == e {
		return
	}
	s.entries[k] = e
	s.touch(e.Namespace)
}

// Remove entfernt den Eintrag einer Aktion auf einem Objekt, z. B. weil die Pipeline beendet ist.
func (s *State) Remove(ref model.ObjectRef, action string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	k := key{ref, action}
	if _, ok := s.entries[k]; !ok {
		return
	}
	delete(s.entries, k)
	s.touch(ref.Namespace)
}

// For liefert die laufenden Pipelines eines Objekts, sortiert nach Aktion (nie nil).
func (s *State) For(ref model.ObjectRef) []Entry {
	s.mu.Lock()
	defer s.mu.Unlock()
	result := []Entry{}
	for k, e := range s.entries {
		if k.ref == ref {
			result = append(result, e)
		}
	}
	slices.SortFunc(result, func(a, b Entry) int { return cmp.Compare(a.Action, b.Action) })
	return result
}

// All liefert alle laufenden Pipelines.
func (s *State) All() []Entry {
	s.mu.Lock()
	defer s.mu.Unlock()
	return slices.Collect(maps.Values(s.entries))
}

// Changes signalisiert Änderungen (koaleszierend); den aktuellen Stand liefert NamespaceRevisions.
func (s *State) Changes() <-chan struct{} { return s.changed }

// NamespaceRevisions liefert je Namespace den Zeitpunkt der letzten Änderung an seinen Pipelines.
func (s *State) NamespaceRevisions() map[string]time.Time {
	s.mu.Lock()
	defer s.mu.Unlock()
	return maps.Clone(s.revisions)
}

// touch setzt einen streng monoton steigenden Zeitstempel für den Namespace und signalisiert. Aufruf nur unter Lock.
func (s *State) touch(namespace string) {
	ts := time.Now()
	if !ts.After(s.lastTimestamp) {
		ts = s.lastTimestamp.Add(time.Nanosecond)
	}
	s.lastTimestamp = ts
	s.revisions[namespace] = ts
	select {
	case s.changed <- struct{}{}:
	default:
	}
}
