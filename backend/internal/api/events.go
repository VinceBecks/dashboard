package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"maps"
	"slices"
	"sync"
	"time"

	"feature-dashboard/internal/config"
	"feature-dashboard/internal/model"
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

// ProjectSnapshot ist ein Projekt mit seinen Service-Bereichen und den Teilbäumen seiner Namespaces.
type ProjectSnapshot struct {
	Name string `json:"name"`
	// ServiceAreas sind die Bereiche in Config-Reihenfolge, „others“ immer zuletzt. Services verweisen über
	// ServiceDTO.Areas auf deren IDs.
	ServiceAreas []AreaDTO         `json:"serviceAreas"`
	Namespaces   []json.RawMessage `json:"namespaces"`
}

// AreaDTO ist ein Service-Bereich; im Dashboard ein Reiter der Namespace-Kachel.
type AreaDTO struct {
	ID    string `json:"id"`
	Label string `json:"label"`
}

// serviceAreas liefert die Bereiche eines Projekts für den Snapshot.
func serviceAreas(p config.Project) []AreaDTO {
	areas := make([]AreaDTO, 0, len(p.ServiceAreas)+1)
	for _, a := range p.ServiceAreas {
		areas = append(areas, AreaDTO{ID: a.Name, Label: a.Label})
	}
	return append(areas, AreaDTO{ID: config.OthersAreaName, Label: p.OthersLabel()})
}

// nsState ist der zuletzt verschickte Stand eines Namespaces. Er besteht nur aus JSON-Bytes, damit niemand
// deserialisierte Objekte neben dem Informer-Cache hält.
type nsState struct {
	revision time.Time
	// extraRevision ist die Revision der Zusatzdaten (laufende Pipelines) beim Aufbau.
	extraRevision time.Time
	project       string
	tree          json.RawMessage
	flat          []flatObject
}

// Hub vergleicht die Datenquelle nach jeder Änderung mit dem zuletzt verschickten Stand und verteilt die Unterschiede
// als upsert-/delete-Events an alle verbundenen Clients.
type Hub struct {
	source   Source
	build    func(model.NamespaceView) NamespaceDTO
	projects []config.Project
	// extra liefert Daten außerhalb des Clusters, die in die Objekte einfließen (laufende Pipelines); nil heißt keine.
	extra RevisionSource

	mu          sync.Mutex
	namespaces  map[string]*nsState
	subscribers map[*Subscription]struct{}
	closed      bool
}

// RevisionSource meldet Änderungen je Namespace, z. B. pipelines.State.
type RevisionSource interface {
	Changes() <-chan struct{}
	NamespaceRevisions() map[string]time.Time
}

// Subscription ist ein verbundener Client. Events wird geschlossen, wenn der Client zu langsam ist oder der Hub
// beendet wird.
type Subscription struct {
	Snapshot json.RawMessage
	Events   <-chan Event
	events   chan Event
	hub      *Hub
}

func newHub(source Source, projects []config.Project, build func(model.NamespaceView) NamespaceDTO) *Hub {
	return &Hub{
		source:      source,
		build:       build,
		projects:    projects,
		namespaces:  map[string]*nsState{},
		subscribers: map[*Subscription]struct{}{},
	}
}

// Run gleicht den Stand bei jeder Store-Änderung ab, bis ctx endet. Danach werden alle Clients getrennt.
func (h *Hub) Run(ctx context.Context) {
	defer h.close()
	var extraChanges <-chan struct{}
	if h.extra != nil {
		extraChanges = h.extra.Changes()
	}
	resync := time.NewTicker(fullResyncInterval)
	defer resync.Stop()
	for {
		full := false
		select {
		case <-ctx.Done():
			return
		case <-h.source.Changes():
		case <-extraChanges:
		case <-resync.C:
			full = true
		}
		h.mu.Lock()
		if full {
			h.forgetRevisionsLocked()
		}
		h.syncLocked()
		h.mu.Unlock()
	}
}

// fullResyncInterval: So oft baut der Hub alle Namespaces neu auf, unabhängig von Revisionen. Ein Sicherheitsnetz
// gegen verpasste Signale; Events entstehen dabei nur für tatsächliche Unterschiede.
const fullResyncInterval = time.Hour

// forgetRevisionsLocked sorgt dafür, dass der nächste Abgleich jeden Namespace neu aufbaut.
func (h *Hub) forgetRevisionsLocked() {
	for _, ns := range h.namespaces {
		ns.revision, ns.extraRevision = time.Time{}, time.Time{}
	}
}

func (h *Hub) extraRevisions() map[string]time.Time {
	if h.extra == nil {
		return nil
	}
	return h.extra.NamespaceRevisions()
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
		nss := byProject[p.Name]
		if nss == nil {
			nss = []json.RawMessage{}
		}
		snap.Projects = append(snap.Projects, ProjectSnapshot{Name: p.Name, ServiceAreas: serviceAreas(p), Namespaces: nss})
	}
	return snap
}

// syncLocked gleicht alle Namespaces ab, deren Zeitstempel sich geändert hat, und verteilt die Unterschiede.
func (h *Hub) syncLocked() {
	revisions := h.source.NamespaceRevisions()
	extra := h.extraRevisions()
	var events []Event
	for _, name := range slices.Sorted(maps.Keys(h.namespaces)) {
		if _, ok := revisions[name]; !ok {
			events = append(events, diff(h.namespaces[name].flat, nil)...)
			delete(h.namespaces, name)
		}
	}
	for _, name := range slices.Sorted(maps.Keys(revisions)) {
		old := h.namespaces[name]
		if old != nil && old.revision.Equal(revisions[name]) && old.extraRevision.Equal(extra[name]) {
			continue
		}
		next, ok, err := h.load(name, extra[name])
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

func (h *Hub) load(name string, extraRevision time.Time) (*nsState, bool, error) {
	view, revision, ok, err := h.source.Namespace(name)
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
	return &nsState{revision: revision, extraRevision: extraRevision, project: view.Namespace.Project, tree: raw, flat: flat}, true, nil
}

// diff liefert die Events, die old in next überführen: zuerst alle delete-, danach alle upsert-Events.
func diff(old, next []flatObject) []Event {
	return append(deleteEvents(old, next), upsertEvents(old, next)...)
}

// deleteEvents liefert ein delete-Event für jedes Objekt aus old, das in next fehlt. Die Events kommen Kind vor
// Parent; Objekte, deren Parent ebenfalls gelöscht wird, entfallen, da der Client Kinder mit dem Parent entfernt.
func deleteEvents(old, next []flatObject) []Event {
	removed := removedKeys(old, next)
	var events []Event
	for _, o := range slices.Backward(old) {
		if !removed[o.key] || removed[o.parentKey] { // objekt ist nicht entfernt worden, oder der ganze parent ist entfernt worden
			continue
		}
		data, err := json.Marshal(o.header)
		if err != nil {
			slog.Error("delete-Event nicht serialisierbar", "error", err)
			continue
		}
		events = append(events, Event{Type: EventDelete, Data: data})
	}
	return events
}

// upsertEvents liefert ein upsert-Event für jedes Objekt aus next, das neu ist oder sich gegenüber old geändert
// hat, Parent vor Kind.
func upsertEvents(old, next []flatObject) []Event {
	before := dataByKey(old)
	var events []Event
	for _, o := range next {
		if prev, ok := before[o.key]; ok && bytes.Equal(prev, o.data) {
			continue
		}
		events = append(events, Event{Type: EventUpsert, Data: o.data})
	}
	return events
}

// removedKeys liefert die Schlüssel der Objekte, die in old, aber nicht mehr in next vorkommen.
func removedKeys(old, next []flatObject) map[string]bool {
	remaining := dataByKey(next)
	removed := map[string]bool{}
	for _, o := range old {
		if _, ok := remaining[o.key]; !ok {
			removed[o.key] = true
		}
	}
	return removed
}

// dataByKey indiziert die serialisierten Objekte über ihren Schlüssel.
func dataByKey(objects []flatObject) map[string]json.RawMessage {
	result := make(map[string]json.RawMessage, len(objects))
	for _, o := range objects {
		result[o.key] = o.data
	}
	return result
}
