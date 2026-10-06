package api

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"maps"
	"slices"
	"sync"
	"time"

	"feature-dashboard/internal/store"
)

// Event-Typen auf GET /api/events.
const (
	EventSnapshot = "snapshot"
	EventUpsert   = "upsert"
	EventDelete   = "delete"
)

// subscriberBuffer ist die Anzahl Events, die ein Client im Rückstand sein darf. Läuft der Puffer voll, wird die
// Verbindung beendet; der Client verbindet sich neu und erhält einen frischen Snapshot.
const subscriberBuffer = 256

var errHubClosed = errors.New("event-hub ist beendet")

// Event ist ein Server-Sent Event. Data ist einzeiliges JSON.
type Event struct {
	Type string
	Data json.RawMessage
}

// Snapshot ist der Inhalt des snapshot-Events: alle Projekte (Reihenfolge der config.yaml) mit ihren Namespaces.
type Snapshot struct {
	Projects []ProjectSnapshot `json:"projects"`
}

// ProjectSnapshot ist ein Projekt mit den Teilbäumen seiner Namespaces.
type ProjectSnapshot struct {
	Name       string            `json:"name"`
	Namespaces []json.RawMessage `json:"namespaces"`
}

// nsState ist der zuletzt verschickte Stand eines Namespaces. Er besteht nur aus JSON-Bytes, damit niemand
// deserialisierte Objekte neben dem Store hält.
type nsState struct {
	revision time.Time
	project  string
	tree     json.RawMessage
	flat     []flatObject
}

// Hub vergleicht den Store nach jeder Änderung mit dem zuletzt verschickten Stand und verteilt die Unterschiede
// als upsert-/delete-Events an alle verbundenen Clients.
type Hub struct {
	store    *store.Store
	build    func(store.NamespaceView) NamespaceDTO
	projects []string

	mu          sync.Mutex
	namespaces  map[string]*nsState
	subscribers map[*Subscription]struct{}
	closed      bool
}

// Subscription ist ein verbundener Client. Events wird geschlossen, wenn der Client zu langsam ist oder der Hub
// beendet wird.
type Subscription struct {
	Snapshot json.RawMessage
	Events   <-chan Event
	events   chan Event
	hub      *Hub
}

func newHub(st *store.Store, projects []string, build func(store.NamespaceView) NamespaceDTO) *Hub {
	return &Hub{
		store:       st,
		build:       build,
		projects:    projects,
		namespaces:  map[string]*nsState{},
		subscribers: map[*Subscription]struct{}{},
	}
}

// Run gleicht den Stand bei jeder Store-Änderung ab, bis ctx endet. Danach werden alle Clients getrennt.
func (h *Hub) Run(ctx context.Context) {
	defer h.close()
	for {
		select {
		case <-ctx.Done():
			return
		case <-h.store.Changes():
			h.mu.Lock()
			h.syncLocked()
			h.mu.Unlock()
		}
	}
}

// Subscribe registriert einen Client und liefert den aktuellen Snapshot. Beides geschieht unter demselben Lock
// wie das Verteilen der Events, daher geht zwischen Snapshot und erstem Event nichts verloren.
func (h *Hub) Subscribe() (*Subscription, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.closed {
		return nil, errHubClosed
	}
	h.syncLocked()
	snapshot, err := json.Marshal(h.snapshotLocked())
	if err != nil {
		return nil, err
	}
	events := make(chan Event, subscriberBuffer)
	sub := &Subscription{Snapshot: snapshot, Events: events, events: events, hub: h}
	h.subscribers[sub] = struct{}{}
	return sub, nil
}

// Cancel meldet den Client ab.
func (s *Subscription) Cancel() {
	s.hub.mu.Lock()
	defer s.hub.mu.Unlock()
	s.hub.removeLocked(s)
}

func (h *Hub) removeLocked(sub *Subscription) {
	if _, ok := h.subscribers[sub]; ok {
		delete(h.subscribers, sub)
		close(sub.events)
	}
}

func (h *Hub) close() {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.closed = true
	for sub := range h.subscribers {
		h.removeLocked(sub)
	}
}

func (h *Hub) snapshotLocked() Snapshot {
	byProject := map[string][]json.RawMessage{}
	for _, name := range slices.Sorted(maps.Keys(h.namespaces)) {
		ns := h.namespaces[name]
		byProject[ns.project] = append(byProject[ns.project], ns.tree)
	}
	snap := Snapshot{Projects: make([]ProjectSnapshot, 0, len(h.projects))}
	for _, p := range h.projects {
		nss := byProject[p]
		if nss == nil {
			nss = []json.RawMessage{}
		}
		snap.Projects = append(snap.Projects, ProjectSnapshot{Name: p, Namespaces: nss})
	}
	return snap
}

// syncLocked gleicht alle Namespaces ab, deren Zeitstempel sich geändert hat, und verteilt die Unterschiede.
func (h *Hub) syncLocked() {
	revisions := h.store.NamespaceRevisions()
	var events []Event
	for _, name := range slices.Sorted(maps.Keys(h.namespaces)) {
		if _, ok := revisions[name]; !ok {
			events = append(events, diff(h.namespaces[name].flat, nil)...)
			delete(h.namespaces, name)
		}
	}
	for _, name := range slices.Sorted(maps.Keys(revisions)) {
		old := h.namespaces[name]
		if old != nil && old.revision.Equal(revisions[name]) {
			continue
		}
		next, ok, err := h.load(name)
		if err != nil {
			slog.Error("Namespace für Events nicht aufbereitet", "namespace", name, "error", err)
			continue
		}
		if !ok {
			// Zwischen NamespaceRevisions und dem Lesen entfernt; der nächste Abgleich bestätigt das.
			continue
		}
		var oldFlat []flatObject
		if old != nil {
			oldFlat = old.flat
		}
		events = append(events, diff(oldFlat, next.flat)...)
		h.namespaces[name] = next
	}
	for _, ev := range events {
		for sub := range h.subscribers {
			select {
			case sub.events <- ev:
			default:
				slog.Warn("Client zu langsam, Event-Stream wird beendet")
				h.removeLocked(sub)
			}
		}
	}
}

func (h *Hub) load(name string) (*nsState, bool, error) {
	view, revision, ok, err := h.store.Namespace(name)
	if err != nil || !ok {
		return nil, ok, err
	}
	tree := h.build(view)
	raw, err := json.Marshal(tree)
	if err != nil {
		return nil, false, err
	}
	flat, err := flatten(tree)
	if err != nil {
		return nil, false, err
	}
	return &nsState{revision: revision, project: view.Namespace.Project, tree: raw, flat: flat}, true, nil
}

// diff liefert die Events, die old in next überführen: zuerst delete (Kind vor Parent, ohne Objekte, deren
// Parent ohnehin gelöscht wird), dann upsert (Parent vor Kind).
func diff(old, next []flatObject) []Event {
	nextData := make(map[string]json.RawMessage, len(next))
	for _, o := range next {
		nextData[o.key] = o.data
	}
	deleted := map[string]bool{}
	for _, o := range old {
		if _, ok := nextData[o.key]; !ok {
			deleted[o.key] = true
		}
	}
	var events []Event
	for _, o := range slices.Backward(old) {
		if !deleted[o.key] || deleted[o.parentKey] {
			continue
		}
		data, err := json.Marshal(o.header)
		if err != nil {
			slog.Error("delete-Event nicht serialisierbar", "error", err)
			continue
		}
		events = append(events, Event{Type: EventDelete, Data: data})
	}
	oldData := make(map[string]json.RawMessage, len(old))
	for _, o := range old {
		oldData[o.key] = o.data
	}
	for _, o := range next {
		if prev, ok := oldData[o.key]; ok && string(prev) == string(o.data) {
			continue
		}
		events = append(events, Event{Type: EventUpsert, Data: o.data})
	}
	return events
}
