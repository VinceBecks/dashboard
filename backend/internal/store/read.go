package store

import (
	"encoding/json"
	"maps"
	"slices"
	"time"

	"feature-dashboard/internal/model"
)

// NamespaceView ist eine deserialisierte Kopie aller Objekte eines Namespaces.
type NamespaceView struct {
	Namespace model.StoredNamespace
	Services  []model.StoredService
	Ingresses []model.StoredIngress
	Pods      []model.StoredPod
}

// ProjectsLastUpdated liefert den Zeitpunkt der letzten Änderung an Namespaces von Projekten.
func (s *Store) ProjectsLastUpdated() time.Time {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.projectsLastUpdated
}

// Namespaces liefert alle gespeicherten Namespaces (sortiert nach Name) und den Projects-Timestamp.
func (s *Store) Namespaces() ([]model.StoredNamespace, time.Time, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	result := make([]model.StoredNamespace, 0, len(s.namespaces))
	for _, name := range slices.Sorted(maps.Keys(s.namespaces)) {
		var ns model.StoredNamespace
		if err := json.Unmarshal(s.namespaces[name].namespace, &ns); err != nil {
			return nil, time.Time{}, err
		}
		result = append(result, ns)
	}
	return result, s.projectsLastUpdated, nil
}

// NamespaceLastUpdated liefert den Zeitpunkt der letzten Änderung im Namespace.
func (s *Store) NamespaceLastUpdated(name string) (time.Time, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	b, ok := s.namespaces[name]
	if !ok {
		return time.Time{}, false
	}
	return b.lastUpdated, true
}

// Namespace liefert eine Kopie aller Objekte des Namespaces und den Zeitpunkt der letzten Änderung.
func (s *Store) Namespace(name string) (NamespaceView, time.Time, bool, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	b, ok := s.namespaces[name]
	if !ok {
		return NamespaceView{}, time.Time{}, false, nil
	}
	var v NamespaceView
	if err := json.Unmarshal(b.namespace, &v.Namespace); err != nil {
		return v, time.Time{}, false, err
	}
	if err := decodeList(b.objects[model.KindService], &v.Services); err != nil {
		return v, time.Time{}, false, err
	}
	if err := decodeList(b.objects[model.KindIngress], &v.Ingresses); err != nil {
		return v, time.Time{}, false, err
	}
	if err := decodeList(b.objects[model.KindPod], &v.Pods); err != nil {
		return v, time.Time{}, false, err
	}
	return v, b.lastUpdated, true, nil
}

// Get dekodiert ein einzelnes Objekt in out. Der Rückgabewert meldet, ob es existiert.
func (s *Store) Get(ref model.ObjectRef, out any) (bool, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	b, ok := s.namespaces[ref.Namespace]
	if !ok {
		return false, nil
	}
	var raw json.RawMessage
	if ref.Kind == model.KindNamespace {
		if ref.Name != ref.Namespace {
			return false, nil
		}
		raw = b.namespace
	} else if raw, ok = b.objects[ref.Kind][ref.Name]; !ok {
		return false, nil
	}
	return true, json.Unmarshal(raw, out)
}
