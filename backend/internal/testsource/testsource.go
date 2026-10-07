// Package testsource ist eine In-Memory-Datenquelle für Tests. Sie erfüllt api.Source und actions.ObjectReader,
// damit Tests ohne Informer und Fake-Cluster auskommen. Im Betrieb ist die Quelle kube.Cluster.
package testsource

import (
	"cmp"
	"errors"
	"fmt"
	"maps"
	"slices"
	"sync"
	"time"

	"feature-dashboard/internal/model"
)

type namespace struct {
	ns        model.StoredNamespace
	services  map[string]model.StoredService
	ingresses map[string]model.StoredIngress
	pods      map[string]model.StoredPod
}

// Source hält Stored-Objects je Namespace. Replicas und aufgelöste Ports setzen Tests selbst.
type Source struct {
	mu         sync.Mutex
	namespaces map[string]*namespace
	revisions  map[string]time.Time
	last       time.Time
	changed    chan struct{}
}

// New erzeugt eine leere Quelle.
func New() *Source {
	return &Source{namespaces: map[string]*namespace{}, revisions: map[string]time.Time{}, changed: make(chan struct{}, 1)}
}

// Tx ändert die Quelle innerhalb von Update.
type Tx struct{ s *Source }

// Update führt fn unter Lock aus.
func (s *Source) Update(fn func(tx *Tx) error) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return fn(&Tx{s: s})
}

// PutNamespace legt einen Projekt-Namespace an oder ersetzt ihn (Objekte darin bleiben).
func (tx *Tx) PutNamespace(ns model.StoredNamespace) error {
	b, ok := tx.s.namespaces[ns.Name]
	if !ok {
		b = &namespace{services: map[string]model.StoredService{}, ingresses: map[string]model.StoredIngress{}, pods: map[string]model.StoredPod{}}
		tx.s.namespaces[ns.Name] = b
	}
	b.ns = ns
	tx.s.touch(ns.Name)
	return nil
}

// DeleteNamespace entfernt einen Namespace mit allen Objekten.
func (tx *Tx) DeleteNamespace(name string) {
	delete(tx.s.namespaces, name)
	delete(tx.s.revisions, name)
	tx.s.notify()
}

// Put speichert einen model.StoredService, model.StoredIngress oder model.StoredPod. Ohne Namespace passiert nichts.
func (tx *Tx) Put(kind model.Kind, namespace, name string, v any) (bool, error) {
	b, ok := tx.s.namespaces[namespace]
	if !ok {
		return false, nil
	}
	switch o := v.(type) {
	case model.StoredService:
		b.services[name] = o
	case model.StoredIngress:
		b.ingresses[name] = o
	case model.StoredPod:
		b.pods[name] = o
	default:
		return false, fmt.Errorf("ungültiger Typ %T für %s", v, kind)
	}
	tx.s.touch(namespace)
	return true, nil
}

// Delete entfernt ein Objekt.
func (tx *Tx) Delete(kind model.Kind, namespace, name string) (bool, error) {
	b, ok := tx.s.namespaces[namespace]
	if !ok {
		return false, nil
	}
	switch kind {
	case model.KindService:
		delete(b.services, name)
	case model.KindIngress:
		delete(b.ingresses, name)
	case model.KindPod:
		delete(b.pods, name)
	default:
		return false, errors.New("ungültiger Objekt-Typ")
	}
	tx.s.touch(namespace)
	return true, nil
}

func (s *Source) touch(namespace string) {
	ts := time.Now()
	if !ts.After(s.last) {
		ts = s.last.Add(time.Nanosecond)
	}
	s.last = ts
	s.revisions[namespace] = ts
	s.notify()
}

func (s *Source) notify() {
	select {
	case s.changed <- struct{}{}:
	default:
	}
}

// Changes signalisiert Änderungen.
func (s *Source) Changes() <-chan struct{} { return s.changed }

// NamespaceRevisions liefert je Namespace den Zeitpunkt der letzten Änderung.
func (s *Source) NamespaceRevisions() map[string]time.Time {
	s.mu.Lock()
	defer s.mu.Unlock()
	return maps.Clone(s.revisions)
}

// Namespace liefert alle Objekte eines Namespaces, nach Name sortiert.
func (s *Source) Namespace(name string) (model.NamespaceView, time.Time, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	b, ok := s.namespaces[name]
	if !ok {
		return model.NamespaceView{}, time.Time{}, false, nil
	}
	return model.NamespaceView{
		Namespace: b.ns,
		Services:  sorted(b.services, func(o model.StoredService) string { return o.Name }),
		Ingresses: sorted(b.ingresses, func(o model.StoredIngress) string { return o.Name }),
		Pods:      sorted(b.pods, func(o model.StoredPod) string { return o.Name }),
	}, s.revisions[name], true, nil
}

func sorted[T any](m map[string]T, name func(T) string) []T {
	list := slices.Collect(maps.Values(m))
	slices.SortFunc(list, func(a, b T) int { return cmp.Compare(name(a), name(b)) })
	if list == nil {
		list = []T{}
	}
	return list
}

// Get liest ein Objekt in out (*model.Stored*).
func (s *Source) Get(ref model.ObjectRef, out any) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	b, ok := s.namespaces[ref.Namespace]
	if !ok {
		return false, nil
	}
	switch o := out.(type) {
	case *model.StoredNamespace:
		*o = b.ns
		return ref.Name == ref.Namespace, nil
	case *model.StoredService:
		*o, ok = b.services[ref.Name]
	case *model.StoredIngress:
		*o, ok = b.ingresses[ref.Name]
	case *model.StoredPod:
		*o, ok = b.pods[ref.Name]
	default:
		return false, errors.New("ungültiger Zieltyp")
	}
	return ok, nil
}

// Services liefert alle Services eines Namespaces (leer, wenn er unbekannt ist).
func (s *Source) Services(namespace string) ([]model.StoredService, error) {
	view, _, _, err := s.Namespace(namespace)
	if view.Services == nil {
		return []model.StoredService{}, err
	}
	return view.Services, err
}
