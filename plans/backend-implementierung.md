# Plan: Implementierung des Backends (Feature-Environment-Dashboard)

## Context

Das Repository ist leer bis auf `backend/go.mod` (Modul `feature-dashboard`, Go 1.26.3) und `CLAUDE.md`.
Ziel ist ein Go-Backend, das per Informer-API Namespaces, Services, Ingresses und Pods aus dem Cluster (1.33.1)
beobachtet, nur Objekte aus Feature-Umgebungen (über Namespace-Labels aus `config.yaml`) reduziert als
`json.RawMessage` in einem In-Memory-Store hält, Referenzen auflöst (targetPort-Namen, Ingress-Backend-Ports),
den Store periodisch komplett neu aufbaut und die Daten samt ausführbarer Aktionen über eine REST-API mit
ETag/304-Mechanismus bereitstellt.

Mit dem Nutzer geklärt:
- `k8s.io/client-go` **v0.33.x** (+ `k8s.io/api`, `k8s.io/apimachinery` v0.33.x), Informer-basiert.
- HTTP: **stdlib `net/http`** (ServeMux mit Methoden- und Pfad-Patterns, `r.PathValue`).
- Informer-Event zu bereits gespeichertem Objekt: **immer überschreiben**; Timestamps nur setzen, wenn sich das
  gespeicherte JSON tatsächlich ändert.
- Tooling: **Makefile + `go test`**, Dockerfile für den Cluster.

Schritt 0 nach Freigabe: diesen Plan nach `plans/backend-implementierung.md` kopieren (Vorgabe aus CLAUDE.md).

## Paketstruktur (`backend/`)

```
cmd/dashboard/main.go          Flags (--config, --listen, --kubeconfig), Wiring, graceful shutdown
internal/config/               config.yaml laden + validieren
internal/model/                Stored-Object-Structs (StoredNamespace/Service/Ingress/Pod), ObjectRef
internal/project/              Matcher: Namespace-Labels -> Projekt
internal/store/                Thread-sicherer Store (json.RawMessage) + Timestamps
internal/converter/            k8s-Objekt -> Stored-Object, Referenzauflösung
internal/kube/                 Informer, Event-Processor, Full-Rebuild/Recreate-Loop, Client-Erzeugung
internal/actions/              ActionHandler-Interface, Registry, FormField-/Result-Typen
internal/actions/deletepod/    erster Handler „delete-pod“ (force-delete aus extra-handler-configs)
internal/api/                  HTTP-Handler, DTOs, ETag-Logik, Health, Swagger
internal/api/openapi.yaml      API-Doku (per go:embed ausgeliefert)
Makefile, Dockerfile, config.example.yaml, deploy/ (Deployment replicas:1, SA, ClusterRole, ConfigMap)
```

## 1. Konfiguration (`internal/config`)

- YAML-Lib: `gopkg.in/yaml.v3` – dekodiert Skalare wie `true` in `map[string]string` als `"true"`
  (Label-Werte im Beispiel sind unquotiert).
- Structs: `Config{Projects []Project; ActionHandlers []ActionHandlerConfig; Store StoreConfig}`,
  `Project{Name string \`yaml:"project-name"\`; NamespaceLabels map[string]string}`,
  `ActionHandlerConfig{Name, Enabled, Title, LucideIcon, LucideIconColor, Extra map[string]any}`,
  `StoreConfig{RecreateInterval Interval}`.
- `Interval`: eigener `UnmarshalYAML`: `0` = deaktiviert, sonst `<n>m`, `<n>h`, `<n>d` (Tage selbst parsen).
- Validierung: eindeutige Projekt- und Action-Namen, nicht-leere `namespace-labels`.
- Hinweis: Das CLAUDE.md-Beispiel nutzt `projekt-name`, der Text `project-name` → `project-name` verwenden,
  CLAUDE.md-Beispiel korrigieren.

## 2. Modell (`internal/model`)

```go
type Meta struct { Name, Namespace string; Labels, Annotations map[string]string }
type StoredNamespace struct { Meta; Project string }
type StoredPod struct { Meta; Phase string; Ready bool; Containers []Container }
type Container struct { Name, Image string; Ports []ContainerPort } // ContainerPort{Name, ContainerPort, Protocol}
type StoredService struct { Meta; Type string; Selector map[string]string; Ports []ServicePort; Replicas []string }
type ServicePort struct { Name string; Port int32; TargetPort string /*Name oder Zahl roh*/; ResolvedTargetPort *int32; NodePort int32; Protocol string }
type StoredIngress struct { Meta; Rules []IngressRule }   // Rule{Host; Paths []IngressPath}
type IngressPath struct { PathType, Path string; Backend IngressBackend }
type IngressBackend struct { ServiceName, ServicePortName string; ServicePortNumber, ResolvedServicePortNumber *int32 }
type Kind string // "namespace" | "service" | "ingress" | "pod"
type ObjectRef struct { Kind Kind; Namespace, Name string }
```

Alle Services (ClusterIP, NodePort, …) werden gespeichert; Einordnung macht das Frontend über `Type`.
`Replicas` = Namen der Pods im Namespace, deren Labels den Service-Selector erfüllen.

## 3. Store (`internal/store`)

- Interne Struktur: `map[namespace]*nsBucket{ns json.RawMessage; project string; services, ingresses, pods map[name]json.RawMessage; lastUpdated time.Time}` + `projectsLastUpdated time.Time`, abgesichert durch `sync.RWMutex`.
- Ausschließlich `json.RawMessage`; Lese-API deserialisiert in frische Structs (Generics-Helfer `Get[T]`/`List[T]`).
- Schreiben über Transaktion: `Update(func(tx *Tx))` unter Write-Lock, damit zusammengehörige Änderungen
  (Objekt + abhängige Re-Resolutions) für Leser atomar sichtbar werden.
- `Tx.Put(ref, v)` serialisiert, vergleicht mit `bytes.Equal` gegen den Altwert; nur bei Änderung:
  Namespace-Timestamp setzen; bei Namespace-Objekten zusätzlich `projectsLastUpdated`.
  `Tx.Delete(ref)` analog; `Tx.DeleteNamespace(ns)` entfernt Bucket + setzt `projectsLastUpdated`.
- `ReplaceAll(snapshot)` für den Full-Rebuild: tauscht den Inhalt atomar, setzt Timestamps nur für
  Namespaces, deren Inhalt sich unterscheidet bzw. die neu/entfernt sind.
- Lese-Methoden: `Projects() (map[project][]StoredNamespace, lastUpdated)`,
  `Namespace(ns) (NamespaceSnapshot, lastUpdated, ok)`, `Get(ref)`.

## 4. Projekt-Matcher (`internal/project`)

`Match(labels) (projectName, ok)`: alle `namespace-labels` eines Projekts müssen exakt vorhanden sein; erster Treffer
in Config-Reihenfolge gewinnt (bei Mehrfachtreffer Warn-Log).

## 5. Converter (`internal/converter`)

- Reine Funktionen `Namespace/Service/Ingress/Pod(k8sObj) -> Stored*` (nur Kernfelder).
- Auflösung arbeitet **nur auf Store-Daten** (innerhalb der Tx), nicht auf dem Informer-Cache:
  - `ResolveService(svc, pods)`: Replicas per Selector; `TargetPort` als Name → erster passender
    `ContainerPort.Name` in den Replica-Pods; als Zahl → direkt; leerer TargetPort → `Port`.
  - `ResolveIngress(ing, services)`: Backend-Port per Name → `ServicePort.Port` des referenzierten Service.
- Abhängigkeiten (in derselben Tx neu berechnen, nur schreiben bei Änderung):
  - Pod create/update/delete → alle Services des Namespaces neu auflösen.
  - Service create/update/delete → alle Ingresses des Namespaces neu auflösen.

## 6. Kubernetes-Anbindung (`internal/kube`)

- Client: in-cluster, Fallback `--kubeconfig`/`KUBECONFIG` für lokale Entwicklung.
- `SharedInformerFactory` (cluster-weit) mit genau vier Informern: Namespaces, Services, `networking/v1` Ingresses, Pods.
- **Ein einziger Event-Processor-Goroutine** (Channel-basiert), damit Events und Rebuilds serialisiert sind:
  - Namespace add/update: matcht Projekt → speichern; wurde er neu zugeordnet / Projekt gewechselt → alle Objekte
    des Namespaces aus den Listern laden und konvertieren. Matcht nicht (mehr) → `DeleteNamespace`.
  - Namespace delete → `DeleteNamespace`.
  - Service/Ingress/Pod: nur wenn Namespace im Store, sonst verwerfen; konvertieren, `Put`/`Delete`, Abhängige neu auflösen.
  - `DeletedFinalStateUnknown` behandeln.
- Full-Rebuild (Startup + Recreate-Intervall): neue Informer-Factory starten, `WaitForCacheSync`, kompletten
  Snapshot aus den Listern bauen (Namespaces matchen, Objekte konvertieren, auflösen), `store.ReplaceAll`,
  alte Factory stoppen. Events tragen eine Generation-ID; Events alter Generationen werden verworfen.
  Recreate läuft als Kommando im selben Processor; Intervall `0` = aus.
- Readiness erst nach erstem erfolgreichem Rebuild.

## 7. Aktionen (`internal/actions`)

```go
type Handler interface {
    Name() string                                   // = action-name
    Configure(extra map[string]any) error           // extra-handler-configs
    IsApplicable(obj StoredObject) bool             // StoredObject: Kind + deserialisiertes Objekt
    FormFields(obj StoredObject) []FormField        // nil = keine Eingaben
    Execute(ctx context.Context, obj StoredObject, inputs map[string]any) Result
}
type FormField struct { ID, Label, Type /*text|select|multi-select|number*/, Description string; IsRequired bool; Options []Option }
type Result struct { Success bool; Message string }
```
- `Registry`: Factories werden in `main` explizit registriert (`registry.Register("delete-pod", deletepod.New)`);
  beim Startup für jeden Config-Eintrag mit `enabled: true` Factory suchen (unbekannt → Startfehler), `Configure` aufrufen.
  Titel/Icon/Farbe aus der Config werden an der Registry-Instanz gehalten und ins API-DTO übernommen.
- `ActionsFor(obj) []ActionDTO` für die API.
- Ausführung: Objekt frisch aus dem Store laden, Applicability prüfen, Pflichtfelder validieren, `Execute`.
  Handler ändern nur den Cluster, nie den Store.
- `delete-pod`: applicable für Kind `pod`; `force-delete: true` → `GracePeriodSeconds=0`. Bekommt `kubernetes.Interface` injiziert.

## 8. API (`internal/api`)

- `GET /api/projects` → `{ projects: [{ name, namespaces: [{ name, labels, annotations, actions }] }] }`
  (alle konfigurierten Projekte, auch ohne Namespaces).
- `GET /api/projects/{project}/namespaces/{namespace}` → `{ namespace, services, ingresses, pods }`, jedes Objekt
  mit `actions`. 404, wenn Namespace nicht existiert oder zu anderem Projekt gehört.
- `PATCH /api/actions/{action}` Body `{ object: {kind, namespace, name}, inputs: {...} }` →
  200 `{success, message}`; 404 unbekannte/deaktivierte Aktion oder Objekt; 400 nicht applicable/Validierungsfehler.
- ETag: `"<UnixNano des relevanten lastUpdated>"`; bei passendem `If-None-Match` → 304 ohne Body.
- Management: `GET /healthz`, `GET /readyz`.
- `GET /api/openapi.yaml` (go:embed), `GET /api/swagger` → HTML mit Swagger-UI (pinned CDN-Version), lädt die Spec.
- JSON-Felder in camelCase; Logging via `log/slog`.

## 9. Build, Deployment, Doku

- Makefile: `build`, `test` (`go test -race ./...`), `vet`, `run` (lokal mit `config.example.yaml`), `docker`.
- Dockerfile: Multi-Stage, `gcr.io/distroless/static`.
- `deploy/`: Deployment (replicas: 1), ServiceAccount, ClusterRole (`get/list/watch` auf namespaces, services, pods,
  ingresses.networking.k8s.io; `delete` auf pods), ClusterRoleBinding, ConfigMap mit `config.yaml`.
- CLAUDE.md aktualisieren: getroffene Entscheidungen, Build-/Test-/Start-Befehle, Beispiel-Key `project-name`.

## Umsetzungsreihenfolge

1. Plan nach `plans/` kopieren; Dependencies (`go get` client-go v0.33.x, yaml.v3).
2. `config` + Tests → 3. `model`, `project` + Tests → 4. `store` + Tests (Kopier-Semantik, Timestamps, `-race`)
5. `converter` + Tests (targetPort-Namensauflösung, Ingress-Port, Abhängigkeiten)
6. `kube` (Processor, Rebuild) + Tests mit `fake.NewSimpleClientset` und echten Informern
7. `actions` + `deletepod` + Tests (fake clientset) → 8. `api` + `openapi.yaml` + Tests (`httptest`, 200/304/404)
9. `main`, Makefile, Dockerfile, `deploy/`, `config.example.yaml`, CLAUDE.md.

## Verifikation

- `make vet test` (alle Unit-Tests mit Race-Detector grün).
- Lokal gegen einen Cluster (z. B. kind/minikube): `make run`, Namespace mit Projekt-Labels + Deployment/Service
  (`targetPort: http`) + Ingress anlegen; `curl /api/projects` und Namespace-Endpunkt prüfen (aufgelöste Ports,
  Replicas), zweiter Request mit `If-None-Match` → 304; Pod skalieren → neuer ETag.
- `curl -X PATCH /api/actions/delete-pod` auf einen Pod → Pod verschwindet, Store aktualisiert sich über die Informer-Events.
- `/api/swagger` im Browser öffnen; kurzes Recreate-Intervall (`1m`) setzen und Logs/ETag-Stabilität prüfen.
