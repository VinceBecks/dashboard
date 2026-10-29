# Plan: Endpunkt `GET /api/snapshot` (einmaliger Snapshot als JSON)

## Kontext

Bisher gibt es den Gesamtstand nur als erstes Event des SSE-Streams `GET /api/events`; danach bleibt die
Verbindung offen. Gewünscht ist ein Endpunkt, der den Snapshot **einmal** liefert und die Response dann beendet.
Mit dem Nutzer geklärt: Format **JSON** (kein SSE), Pfad `GET /api/snapshot`, Body = dasselbe `Snapshot`-Schema
wie im `snapshot`-Event. Abruf per `fetch`/`curl`, kein EventSource (der würde nach dem Ende neu verbinden).

Gemäß CLAUDE.md wird dieser Plan bei der Umsetzung zusätzlich als `plans/snapshot-endpunkt.md` abgelegt.

## Umsetzung

### 1. Hub: Snapshot ohne Abo (`backend/internal/api/events.go`)

Neue Methode, die den Teil von `Subscribe()` vor dem Anlegen des Abos wiederverwendet:

```go
// Snapshot gleicht den Stand ab und liefert ihn als JSON, ohne einen Client zu registrieren.
func (h *Hub) Snapshot() (json.RawMessage, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.snapshotJSONLocked()
}

// snapshotJSONLocked gleicht alle geänderten Namespaces ab und serialisiert den Stand.
func (h *Hub) snapshotJSONLocked() (json.RawMessage, error) {
	if h.closed {
		return nil, errHubClosed
	}
	h.syncLocked()
	return json.Marshal(h.snapshotLocked())
}
```

`Subscribe()` nutzt `snapshotJSONLocked()` statt des duplizierten Codes (Verhalten unverändert).
`syncLocked()` vor dem Serialisieren ist wichtig: Er verteilt eventuelle Unterschiede auch an bestehende
SSE-Abonnenten, der Snapshot ist damit konsistent mit dem Stream.

### 2. Handler und Route (`backend/internal/api/server.go`)

- Route: `mux.HandleFunc("GET /api/snapshot", s.handleSnapshot)` neben `/api/events`.
- `handleSnapshot`:
  - `!s.ready()` → `503` mit `{"error": "informer-Cache ist noch nicht synchronisiert"}` (wie `handleEvents`).
  - `s.hub.Snapshot()`: `errHubClosed` → `503`, anderer Fehler → `500` (`writeError`).
  - Erfolg: `Content-Type: application/json`, `Cache-Control: no-cache`, `200`, Body = Snapshot-Bytes
    (direkt schreiben, nicht über `writeJSON`, da bereits serialisiert).

### 3. Tests (`backend/internal/api/server_test.go`)

Mit bestehendem `setup(t)` und `do(...)`:
- `GET /api/snapshot` → `200`, `Content-Type: application/json`, Body lässt sich in `Snapshot` unmarshallen und
  ist identisch mit dem Snapshot aus `subscribe(t, srv)` (gleiche Bytes).
- Nicht bereit (`ready` liefert `false`, wie im bestehenden 503-Test für `/api/events`) → `503`.
- Kein Abonnent bleibt zurück (`len(srv.hub.subscribers) == 0` nach dem Aufruf).

### 4. Doku

- `backend/internal/api/openapi.yaml`: Pfad `/api/snapshot` (`get`, `200` → `$ref: '#/components/schemas/Snapshot'`,
  `503`/`500` → `Error`).
- `README.md`, Abschnitt HTTP-API: kurzer Unterabschnitt „Einmaliger Snapshot (`GET /api/snapshot`)“ mit
  `curl localhost:8080/api/snapshot` und Hinweis: gleicher Inhalt wie das `snapshot`-Event, keine weiteren Events.
- `CLAUDE.md`, „API zum Dashboard“: Endpunkt ergänzen; im Abschnitt „Frontend (nicht in diesem Repository)“ die
  Liste der genutzten Endpunkte ergänzen.
- `erklaerung.md`, Abschnitt `internal/api`: Routen-Liste in `server.go` um `/api/snapshot` ergänzen,
  `Hub.Snapshot` erwähnen.

## Verifikation

- `make lint`, `make test` grün.
- Lokal gegen kind-Cluster: `make run`, dann `curl -i localhost:8080/api/snapshot` → `200`, JSON, Verbindung endet;
  Inhalt entspricht dem ersten Event aus `curl -N localhost:8080/api/events`.
- Swagger-UI unter `/api/swagger` zeigt den neuen Pfad.
