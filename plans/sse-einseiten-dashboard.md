# Plan: Server-Sent Events und Einseiten-Dashboard

## Context

Heute pollt das Frontend zwei Endpunkte (`GET /api/projects`, `GET /api/projects/{p}/namespaces/{ns}`) mit
ETag/304, hat zwei Seiten (Übersicht, Namespace-Seite) und bekommt pro Objekt viele Rohdaten (Labels, Annotations,
Ingress-Rules, Service-Ports, Service-Bereiche).

Gewünscht:
- **Eine Seite**: Namespaces gruppiert nach Projekt, je Namespace eine Kachel, je Anwendungs-Service ein Punkt
  (grün = mindestens ein Replica läuft, rot = keins).
- **Server-Sent Events** statt Polling: Beim Aufruf lädt das Frontend den Initialstand (komplette Hierarchie) und
  wird danach über Events aktualisiert.
- **Schlanke Objekte**: keine Labels/Annotations, keine Service-Bereiche mehr. Ingresses liefern fertige
  `https://`-URLs.
- **Jedes Objekt kennt seine Parents bis zum Projekt**, damit ein Event direkt einsortiert werden kann.
- Eine Datei `example-api.jsonc` zeigt die Struktur des Initialstands.

Mit dem Nutzer geklärt:
- Aktionen und Links bleiben an **allen** Objekten (Namespace, Service, Pod, Ingress).
- Pods hängen in der Hierarchie **unter dem Service**, der sie referenziert. Der Service trägt ein berechnetes
  `running`.
- Der Initialstand kommt als **erstes SSE-Event `snapshot`** auf demselben Stream. Es gibt keinen separaten
  GET-Endpunkt und damit kein Race zwischen Laden und Abo. Nach einem Reconnect kommt automatisch ein frischer
  Snapshot.
- Es werden **nur ClusterIP-Services** gesendet. NodePort-Services entfallen in API und UI.

Der Store, der Converter, der Watcher und die ActionHandler bleiben **unverändert** (Stored-Objects behalten Labels
usw., die Handler und Link-Templates brauchen sie). Neu ist nur die Ausgabeschicht (DTOs, Diff, SSE) und das
Frontend.

## API (neu)

| Endpunkt                       | Status                                                                 |
|--------------------------------|------------------------------------------------------------------------|
| `GET /api/events`              | **neu**, `text/event-stream`                                           |
| `PATCH /api/actions/{action}`  | bleibt unverändert                                                     |
| `GET /api/projects`, `GET /api/projects/{p}/namespaces/{ns}` | **entfallen**                            |
| `GET /api/polling`, `GET /api/cluster` | **entfallen** (kein Polling, keine NodePorts mehr)             |
| `/healthz`, `/readyz`, `/api/openapi.yaml`, `/api/swagger` | bleiben                                    |

### Event-Arten auf `GET /api/events`

```
event: snapshot      data: { "projects": [ … komplette Hierarchie … ] }   (immer zuerst)
event: upsert        data: <Objekt ohne Kind-Listen, mit kind + parents>
event: delete        data: { "kind": "...", "name": "...", "parents": { … } }
: ping               (Kommentar alle 25 s, hält Proxys offen)
```

- Jedes Event hat eine fortlaufende `id`. `Last-Event-ID` wird ignoriert, ein Reconnect bekommt immer einen neuen
  `snapshot`.
- `upsert`-Events kommen Parent-vor-Kind, `delete`-Events Kind-vor-Parent.
- Das `delete` eines Namespaces (oder Services) entfernt beim Client auch alle Kinder.
- Wechselt ein Namespace das Projekt, ergibt das `delete` (altes Projekt) plus `upsert` (neues Projekt).
- Im `upsert` fehlen die Kind-Listen (`services`, `ingresses`, `pods`). Der Client behält die vorhandenen Kinder.
- `parents`: Namespace → `{project}`, Service/Ingress → `{project, namespace}`, Pod → `{project, namespace, service}`.
  Ein Pod, den zwei Services referenzieren, erscheint unter beiden.

### Objekt-Felder (DTOs, `backend/internal/api/dto.go`)

- Alle Objekte: `kind`, `name`, `parents`, `actions`, `links`.
- Namespace: `services`, `ingresses` (nur im Snapshot).
- Service (nur `type == ClusterIP`):
  - `running`: mindestens ein Replica mit `ready == true`
  - `pods` (nur im Snapshot), sortiert nach Name
- Pod: `phase`, `ready`, `containers: [{name, image}]`.
- Ingress: `urls`.
  - Bildung je Rule mit Host und je Path: `https://<host><path>` (leerer Path → `/`).
  - Rules ohne Host werden übersprungen.
  - Die URLs werden dedupliziert und behalten die Reihenfolge der Spec.
- Projekte kommen in Config-Reihenfolge, auch ohne Namespaces. Namespaces, Services, Ingresses und Pods sind nach
  Name sortiert.

### Datei `backend/internal/api/example-api.jsonc` (liegt neben `openapi.yaml`)

```jsonc
// Initialstand: erstes Event auf GET /api/events
//   event: snapshot
//   id: 1
//   data: <dieses JSON, einzeilig>
{
  "projects": [
    {
      "name": "foo-projekt",                       // Reihenfolge wie in der config.yaml
      "namespaces": [
        {
          "kind": "namespace",
          "name": "foo-feature-123",
          "parents": { "project": "foo-projekt" },
          "actions": [
            {
              "id": "deploy-missing-services",
              "title": "Fehlende Services deployen",
              "icon": "package-plus",
              "iconColor": "green",
              "formFields": [
                {
                  "id": "services",
                  "label": "Services",
                  "type": "multi-select",
                  "isRequired": true,
                  "options": [{ "value": "foo-frontend", "label": "Foo Frontend" }]
                }
              ]
            }
          ],
          "links": [
            {
              "id": "kibana",
              "title": "Logs in Kibana",
              "icon": "scroll-text",
              "iconColor": "#00bfb3",
              "url": "https://kibana.mycomp.de/app/discover#/?_a=(query:(query:'kubernetes.namespace:foo-feature-123'))"
            }
          ],
          "services": [                             // nur ClusterIP; je Service ein Punkt auf der Kachel
            {
              "kind": "service",
              "name": "foo-backend",
              "parents": { "project": "foo-projekt", "namespace": "foo-feature-123" },
              "running": true,                      // grün: mindestens ein Replica ready
              "actions": [
                { "id": "restart-service-replicas", "title": "Neu starten", "icon": "rotate-cw", "iconColor": "orange" }
              ],
              "links": [],
              "pods": [                             // Replicas des Services
                {
                  "kind": "pod",
                  "name": "foo-backend-7d9f8c6b5-x2k4q",
                  "parents": { "project": "foo-projekt", "namespace": "foo-feature-123", "service": "foo-backend" },
                  "phase": "Running",
                  "ready": true,
                  "containers": [
                    { "name": "app", "image": "registry.mycomp.de/foo/backend:feature-123" }
                  ],
                  "actions": [
                    { "id": "delete-pod", "title": "Pod löschen", "icon": "trash", "iconColor": "red" }
                  ],
                  "links": []
                }
              ]
            },
            {
              "kind": "service",
              "name": "foo-worker",
              "parents": { "project": "foo-projekt", "namespace": "foo-feature-123" },
              "running": false,                     // rot: kein Replica ready (oder keine Pods)
              "actions": [],
              "links": [],
              "pods": []
            }
          ],
          "ingresses": [
            {
              "kind": "ingress",
              "name": "foo",
              "parents": { "project": "foo-projekt", "namespace": "foo-feature-123" },
              "urls": [                             // aus rules/paths aufgelöst
                "https://foo-feature-123.mycomp.de/",
                "https://foo-feature-123.mycomp.de/api"
              ],
              "actions": [],
              "links": []
            }
          ]
        }
      ]
    },
    {
      "name": "bar-projekt",
      "namespaces": []                             // Projekte ohne Namespaces werden mitgeliefert
    }
  ]
}

// Danach Delta-Events (Objekte ohne Kind-Listen):
//   event: upsert
//   data: {"kind":"service","name":"foo-worker","parents":{"project":"foo-projekt","namespace":"foo-feature-123"},"running":true,"actions":[],"links":[]}
//
//   event: delete
//   data: {"kind":"pod","name":"foo-backend-7d9f8c6b5-x2k4q","parents":{"project":"foo-projekt","namespace":"foo-feature-123","service":"foo-backend"}}
```

## Backend-Umsetzung

### 1. Store: Änderungs-Signal (`backend/internal/store/store.go`, `read.go`)
- Ein neuer koaleszierender Kanal `changed chan struct{}` (Puffer 1) mit `Changes() <-chan struct{}`.
- Er wird nach jedem `Update` und `ReplaceAll` nicht-blockierend signalisiert.
- Neu: `NamespaceRevisions() map[string]time.Time` (Name → `lastUpdated`), unter RLock.
- `projectsLastUpdated` und `ProjectsLastUpdated()` werden mit allem anderen entfernt, was nur für ETags da war.
  Die Namespace-Timestamps bleiben, sie sind jetzt die Diff-Grundlage.

### 2. DTO-Aufbau (`backend/internal/api/dto.go`, neu)
- `buildNamespace(view store.NamespaceView, project string) NamespaceDTO` erzeugt den kompletten Teilbaum. Dafür
  nutzt es das vorhandene `Server.extras` (Aktionen und Links aus Registry und Link-Registry, unverändert).
- Pods werden über `StoredService.Replicas` zugeordnet. Die Replicas sind schon im Converter aufgelöst
  (`converter.ResolveService`).
- `ingressURLs(model.StoredIngress) []string` baut die URLs aus den gespeicherten Rules.
- Pods ohne referenzierenden Service werden nicht gesendet.

### 3. Event-Hub (`backend/internal/api/events.go`, neu)
- Hält je Namespace:
  - den zuletzt gesehenen Timestamp,
  - den Teilbaum als `json.RawMessage` (Snapshot-Baustein),
  - eine flache Map `key → json.RawMessage` (Objekt ohne Kinder; key = kind + parents + name).

  Es sind nur Bytes, keine deserialisierten Objekte. Damit ist die Store-Garantie gewahrt.
- Eine Goroutine wartet auf `store.Changes()`:
  1. Sie vergleicht `NamespaceRevisions()` mit den bekannten Timestamps.
  2. Für jeden geänderten Namespace baut sie den Teilbaum neu und vergleicht ihn flach mit dem alten Stand.
  3. Daraus erzeugt sie `delete`- und `upsert`-Events. Weggefallene Namespaces ergeben ein `delete` für den
     Namespace.

  Damit sind auch abgeleitete Änderungen abgedeckt (`running`, aufgelöste URLs, geänderte Anwendbarkeit von
  Aktionen) sowie der Full-Rebuild.
- `Subscribe()`: Unter dem Hub-Mutex wird der Snapshot aus den Bausteinen zusammengesetzt (Projekte in
  Config-Reihenfolge) und der Subscriber registriert. So geht kein Event verloren.
- Je Subscriber gibt es einen gepufferten Kanal (z. B. 256). Ist er voll, wird der Subscriber geschlossen. Der
  Client reconnectet und bekommt einen frischen Snapshot.
- Der Hub startet erst, wenn `watcher.Ready()` gilt. Bis dahin beantwortet `/api/events` Anfragen mit `503`
  (EventSource versucht es automatisch erneut).

### 4. SSE-Handler (`backend/internal/api/server.go`)
- `GET /api/events` setzt:
  - `Content-Type: text/event-stream`
  - `Cache-Control: no-cache`
  - `X-Accel-Buffering: no`
- Danach: Snapshot schreiben, Events streamen, alle 25 s `: ping`. Flush über `http.NewResponseController`.
- Ende bei `r.Context().Done()` oder bei geschlossenem Subscriber.
- Entfernt werden: `handleProjects`, `handleNamespace`, `handlePolling`, `handleCluster`, `notModified`,
  `ProjectsResponse`/`NamespaceResponse`/`*DTO` alt, `WithCluster`, `WithFrontend`.

### 5. Shutdown (`backend/cmd/dashboard/main.go`)
- Hub mit `ctx` starten. Bei Shutdown werden alle Subscriber geschlossen (`srv.RegisterOnShutdown` →
  `hub.Close()`), sonst wartet `srv.Shutdown` auf die offenen Streams.
- Es gibt kein `WriteTimeout` am Server, und das bleibt so.

### 6. Entfernen: Service-Bereiche, Polling, Cluster-Angaben
- `backend/internal/project/areas.go` (+ Test) löschen.
- In `config.go`:
  - `ServiceAreas`, `OthersAreaLabel`, `validateServiceAreas`, `FrontendConfig` und `ClusterConfig` entfernen,
    jeweils samt Validierung und Tests.
  - Die YAML-Prüfung ist nicht strikt (`KnownFields` ist nicht gesetzt), alte Configs starten also weiter.
- `backend/config.example.yaml` und `backend/deploy/config.yaml` bereinigen.

### 7. Doku
- `backend/internal/api/openapi.yaml`: entfallene Pfade raus. `GET /api/events` mit Beschreibung der Event-Arten
  und Schemas (`Snapshot`, `NamespaceObject`, `ServiceObject`, `PodObject`, `IngressObject`, `Parents`,
  `DeleteEvent`) rein.
- `CLAUDE.md` anpassen:
  - API-Abschnitt (SSE statt 200/304, Mechanismus-Abschnitt ersetzen)
  - Frontend-Seiten (eine Seite)
  - Datenaktualisierung
  - Config (ohne `service-areas`, `frontend`, `cluster`)
  - Store (keine Projekt-Timestamps)
  - Paket-Tabelle (`internal/project` ohne Areas)
- Diesen Plan als `plans/sse-einseiten-dashboard.md` ablegen (Vorgabe aus CLAUDE.md).

## Frontend-Umsetzung

- `src/api/types.ts`: Die Typen werden nach der neuen Struktur neu geschrieben:
  - `Parents`, `NamespaceObject`, `ServiceObject`, `PodObject`, `IngressObject`, `Snapshot`, `DeleteEvent`
  - `Action`, `Link`, `FormField`, `ActionResult` bleiben.
  - `ObjectRef` für den PATCH-Body bleibt.
- `src/api/state.ts` (neu): reine Funktionen `applySnapshot`, `applyUpsert` (behält Kinder) und `applyDelete`
  (entfernt Teilbaum) auf dem Zustand `Snapshot`. Mit Vitest getestet.
- `src/hooks/useDashboardEvents.ts` (neu):
  - öffnet `EventSource('/api/events')` und wendet Events per `useReducer` an,
  - liefert `{data, connection: 'connecting'|'open'|'reconnecting'}`.
- `src/pages/DashboardPage.tsx` (neu): je Projekt eine Überschrift und ein Grid aus `NamespaceCard`.
- `src/components/NamespaceCard.tsx` (neu), von oben nach unten:
  - Namespace-Name, dazu Aktionen und Links (`ObjectButtons`)
  - Ingresses: je URL ein Link in neuem Tab, dazu die Aktionen und Links des Ingresses
  - Service-Punkte: grün/rot (`success.main`/`error.main`), Tooltip mit Service-Namen. Ein Klick öffnet den
    `ServiceDialog`.
- `src/components/ServiceDialog.tsx` (neu):
  - Service-Aktionen und -Links
  - je Pod Status, Container (Name, Image), Aktionen und Links
  - Liest live aus dem State. Verschwindet der Service, zeigt der Dialog einen Hinweis.
- `src/components/ConnectionStatus.tsx` (neu, oben rechts statt `PollingControl`): ein Chip
  „Live“/„Verbinde…“/„Getrennt“.
- `App.tsx`: kein Router mehr, nur Layout + `DashboardPage`. `react-router` aus `package.json` entfernen.
- Wiederverwendet: `ActionButtons`, `ActionDialog`, `ActionIcon`, `LinkButtons`, `ObjectButtons`, `StatusViews`.
  Nach einer Aktion ist kein Reload nötig, die Änderung kommt per Event.
- Gelöscht werden:
  - `pages/OverviewPage.tsx`, `pages/NamespacePage.tsx`
  - `components/ServiceCard.tsx`, `components/ServiceAreaFilter.tsx`, `components/PollingControl.tsx`,
    `components/CopyButton.tsx`
  - `hooks/usePolling.ts`, `hooks/PollingSettings.tsx`
  - `api/client.ts` (ETag-Teil; der Aktions-PATCH wandert nach `api/actions.ts`)
  - samt der zugehörigen Tests
- `vite.config.ts`: Der Proxy `/api` funktioniert für SSE. Falls im Dev-Modus gepuffert wird, wird ein Header
  ergänzt.

## Hinweis zu NodePort-Aktionen

Da NodePort-Services nicht mehr gesendet werden, ist `delete-node-port-service` in der UI nicht mehr erreichbar.
`node-port-service-on-service-base` erstellt dann einen Service, den niemand sieht. Die Handler bleiben im Code. In
`config.example.yaml` und `deploy/config.yaml` werden beide Aktionen auf `projects: []` gesetzt, und in `CLAUDE.md`
kommt ein Hinweis dazu.

## Verifikation

- `make test`:
  - Neue Go-Tests:
    - DTO-Aufbau: `running`, Pod-Zuordnung, Ingress-URLs, NodePort ausgefiltert
    - Hub-Diff: Pod-Update ergibt `upsert` für Pod und Service; Namespace weg ergibt `delete`; Projektwechsel;
      Snapshot vor erstem Event ohne Lücke
    - SSE-Handler mit `httptest` (Snapshot zuerst, dann `upsert`)
  - Vitest: `state.ts`-Reducer, `NamespaceCard` (Punktfarbe), `ServiceDialog`.
- `make lint`.
- `make run` gegen den kind-Cluster mit `helpers/demo-namespace` / `helpers/demo-app`:
  - `curl -N localhost:8080/api/events` zeigt `snapshot`.
  - Danach `kubectl scale deploy … --replicas=0` → `upsert` mit `running:false`, der Punkt wird rot.
  - Namespace löschen → Kachel verschwindet.
  - Aktion im Browser ausführen → Änderung erscheint ohne Reload.
