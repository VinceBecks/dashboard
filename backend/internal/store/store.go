// Package store hält alle Stored-Objects in-memory als json.RawMessage.
//
// Durch Serialisierung beim Schreiben und Deserialisierung beim Lesen entstehen immer Kopien;
// niemand hält einen Pointer auf den Zustand im Store.
package store

import (
	"bytes"
	"encoding/json"
	"fmt"
	"maps"
	"slices"
	"sync"
	"time"

	"feature-dashboard/internal/model"
)

// Store ist der nebenläufig nutzbare In-Memory-Store.
type Store struct {
	mu                  sync.RWMutex
	namespaces          map[string]*bucket
	projectsLastUpdated time.Time
	lastTimestamp       time.Time
	now                 func() time.Time
}

type bucket struct {
	project     string
	namespace   json.RawMessage
	objects     map[model.Kind]map[string]json.RawMessage
	lastUpdated time.Time
}

func newBucket() *bucket {
	return &bucket{objects: map[model.Kind]map[string]json.RawMessage{
		model.KindService: {},
		model.KindIngress: {},
		model.KindPod:     {},
	}}
}

func (b *bucket) equal(o *bucket) bool {
	if b.project != o.project || !bytes.Equal(b.namespace, o.namespace) {
		return false
	}
	for kind, objs := range b.objects {
		if !maps.EqualFunc(objs, o.objects[kind], func(a, b json.RawMessage) bool { return bytes.Equal(a, b) }) {
			return false
		}
	}
	return true
}

// New erzeugt einen leeren Store.
func New() *Store {
	s := &Store{namespaces: map[string]*bucket{}, now: time.Now}
	s.projectsLastUpdated = s.nextTimestamp()
	return s
}

// nextTimestamp liefert einen streng monoton steigenden Zeitstempel, damit ETags eindeutig sind.
func (s *Store) nextTimestamp() time.Time {
	ts := s.now()
	if !ts.After(s.lastTimestamp) {
		ts = s.lastTimestamp.Add(time.Nanosecond)
	}
	s.lastTimestamp = ts
	return ts
}

// Update führt fn unter Write-Lock aus. Alle Änderungen einer Transaktion werden für Leser atomar sichtbar.
func (s *Store) Update(fn func(tx *Tx) error) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return fn(&Tx{s: s})
}

// ReplaceAll ersetzt den kompletten Inhalt durch den von other (Full-Rebuild).
// Timestamps werden nur für Namespaces gesetzt, deren Inhalt sich unterscheidet.
func (s *Store) ReplaceAll(other *Store) {
	other.mu.RLock()
	defer other.mu.RUnlock()
	s.mu.Lock()
	defer s.mu.Unlock()

	projectsChanged := false
	next := make(map[string]*bucket, len(other.namespaces))
	for name, nb := range other.namespaces {
		cp := &bucket{project: nb.project, namespace: nb.namespace, objects: map[model.Kind]map[string]json.RawMessage{}}
		for kind, objs := range nb.objects {
			cp.objects[kind] = maps.Clone(objs)
		}
		old, ok := s.namespaces[name]
		switch {
		case !ok:
			projectsChanged = true
			cp.lastUpdated = s.nextTimestamp()
		case !old.equal(cp):
			if old.project != cp.project || !bytes.Equal(old.namespace, cp.namespace) {
				projectsChanged = true
			}
			cp.lastUpdated = s.nextTimestamp()
		default:
			cp.lastUpdated = old.lastUpdated
		}
		next[name] = cp
	}
	for name := range s.namespaces {
		if _, ok := next[name]; !ok {
			projectsChanged = true
		}
	}
	s.namespaces = next
	if projectsChanged {
		s.projectsLastUpdated = s.nextTimestamp()
	}
}

// Tx ist eine Schreib-Transaktion. Sie ist nur innerhalb von Store.Update gültig.
type Tx struct {
	s *Store
}

// PutNamespace legt einen Namespace an oder aktualisiert ihn.
func (tx *Tx) PutNamespace(ns model.StoredNamespace) error {
	raw, err := json.Marshal(ns)
	if err != nil {
		return err
	}
	b, ok := tx.s.namespaces[ns.Name]
	if ok && b.project == ns.Project && bytes.Equal(b.namespace, raw) {
		return nil
	}
	if !ok {
		b = newBucket()
		tx.s.namespaces[ns.Name] = b
	}
	b.project = ns.Project
	b.namespace = raw
	b.lastUpdated = tx.s.nextTimestamp()
	tx.s.projectsLastUpdated = b.lastUpdated
	return nil
}

// DeleteNamespace entfernt einen Namespace mit allen seinen Objekten.
func (tx *Tx) DeleteNamespace(name string) {
	if _, ok := tx.s.namespaces[name]; !ok {
		return
	}
	delete(tx.s.namespaces, name)
	tx.s.projectsLastUpdated = tx.s.nextTimestamp()
}

// HasNamespace meldet, ob der Namespace im Store liegt.
func (tx *Tx) HasNamespace(name string) bool {
	_, ok := tx.s.namespaces[name]
	return ok
}

// NamespaceProject liefert das Projekt eines gespeicherten Namespaces.
func (tx *Tx) NamespaceProject(name string) (string, bool) {
	b, ok := tx.s.namespaces[name]
	if !ok {
		return "", false
	}
	return b.project, true
}

// Put speichert ein Objekt (Service, Ingress, Pod). Liegt der Namespace nicht im Store, wird nichts gespeichert.
// Der Rückgabewert meldet, ob sich der gespeicherte Inhalt geändert hat.
func (tx *Tx) Put(kind model.Kind, namespace, name string, v any) (bool, error) {
	b, objs, err := tx.objects(kind, namespace)
	if err != nil || b == nil {
		return false, err
	}
	raw, err := json.Marshal(v)
	if err != nil {
		return false, err
	}
	if old, ok := objs[name]; ok && bytes.Equal(old, raw) {
		return false, nil
	}
	objs[name] = raw
	b.lastUpdated = tx.s.nextTimestamp()
	return true, nil
}

// Delete entfernt ein Objekt (Service, Ingress, Pod).
func (tx *Tx) Delete(kind model.Kind, namespace, name string) (bool, error) {
	b, objs, err := tx.objects(kind, namespace)
	if err != nil || b == nil {
		return false, err
	}
	if _, ok := objs[name]; !ok {
		return false, nil
	}
	delete(objs, name)
	b.lastUpdated = tx.s.nextTimestamp()
	return true, nil
}

// List dekodiert alle Objekte eines Typs im Namespace in out (Pointer auf Slice), sortiert nach Name.
func (tx *Tx) List(kind model.Kind, namespace string, out any) error {
	b, objs, err := tx.objects(kind, namespace)
	if err != nil || b == nil {
		return err
	}
	return decodeList(objs, out)
}

func (tx *Tx) objects(kind model.Kind, namespace string) (*bucket, map[string]json.RawMessage, error) {
	if kind == model.KindNamespace || !kind.Valid() {
		return nil, nil, fmt.Errorf("ungültiger Objekt-Typ %q", kind)
	}
	b, ok := tx.s.namespaces[namespace]
	if !ok {
		return nil, nil, nil
	}
	return b, b.objects[kind], nil
}

func decodeList(objs map[string]json.RawMessage, out any) error {
	names := slices.Sorted(maps.Keys(objs))
	buf := bytes.NewBufferString("[")
	for i, n := range names {
		if i > 0 {
			buf.WriteByte(',')
		}
		buf.Write(objs[n])
	}
	buf.WriteByte(']')
	return json.Unmarshal(buf.Bytes(), out)
}
