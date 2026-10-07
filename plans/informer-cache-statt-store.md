# Plan: Informer-Cache statt eigenem Store

## Context

Der In-Memory-Store (`internal/store`) stammt aus der Polling-Zeit. Er hielt eine reduzierte Kopie aller Objekte
und lieferte die Zeitstempel für ETag/304. Seit der SSE-Umstellung gilt:

- Den Event-Hub (`internal/api/events.go`) interessiert nur noch, **welcher Namespace sich geändert hat**. Er baut den
  Namespace dann komplett neu auf und vergleicht das Ergebnis Objekt für Objekt als JSON.
- Die inkrementelle Referenzauflösung in `converter/apply.go` ist überflüssig. Abhängige Objekte (Pod → Service →
  Ingress) werden beim Aufbau ohnehin vollständig neu berechnet.
- Einige aufgelöste Felder werden nicht mehr gebraucht. `IngressBackend.ResolvedServicePortNumber` liest niemand.
  `ResolvedTargetPort` dient nur der Beschriftung in `node-port-service-on-service-base`.
- Der periodische Full-Rebuild sollte Fehler im inkrementell gepflegten Zustand ausbügeln. Diesen Zustand gibt es
  dann nicht mehr. Der Informer-Cache selbst ist durch List und Watch mit `resourceVersion` und Relist nach
  Watch-Abbruch konsistent.
- Die Objekte liegen doppelt im Speicher: im Informer-Cache und im Store.

**Ziel:** Der Informer-Cache ist die einzige Quelle. Der Hub, die API und die Aktionen lesen über die Lister und
konvertieren bei Bedarf in die vorhandenen `model.Stored*`-Typen. Store, inkrementeller Converter, Generationen und
Full-Rebuild entfallen. Die API (SSE-Events, `PATCH /api/actions`) bleibt unverändert.

## Zielbild

```
Informer (Namespace, Service, Ingress, Pod; WithTransform)
  ├─ Lister ──────────────► kube.Cluster.Namespace(name) → model.NamespaceView (Konvertierung + Resolve bei jedem Aufruf)
  └─ Event-Handler ──────► Revision je Projekt-Namespace + Changes()-Signal
Hub: Changes() → geänderte Namespaces über Cluster.Namespace neu aufbauen → Diff → SSE   (Logik unverändert)
Aktionen: actions.ObjectReader → kube.Cluster (Get, Services)                            (Handler unverändert)
pipelines.State bleibt (eigener Zustand in der ConfigMap)
```

## Schritte

### 1. `model.NamespaceView` und Converter aufräumen
- `NamespaceView` wird von `store/read.go` nach `internal/model` verschoben.
- `converter/apply.go` wird gelöscht.
- In `converter/convert.go` bleiben die reinen Funktionen `Namespace`, `Pod`, `Service`, `Ingress` und
  `ResolveService`. Neu ist `NamespaceView(ns, project, pods, services, ingresses) model.NamespaceView`. Sie
  konvertiert, löst bei allen Services Replicas und `targetPort` auf und sortiert nach Name.
- **Entfällt:** `ResolveIngress`, `IngressBackend.ResolvedServicePortNumber` und `ServicePortNumber/Name` am
  Ingress-Backend (nur `ServiceName` bleibt, falls noch benötigt).
- Converter-Tests: Die Store-basierten Tests werden zu Tabellen-Tests auf `NamespaceView`, z. B.:
  - Pod-Update ändert den aufgelösten Port
  - Replicas folgen den Labels
  - ein Pod ohne Selector-Treffer taucht nicht als Replica auf

### 2. `kube.Cluster` ersetzt Store und Watcher
`watcher.go` wird zu `cluster.go`, etwa 150 statt 311 Zeilen.

- **Aufbau:** `NewCluster(client, matcher)` erzeugt eine `SharedInformerFactory` mit den vier Informern.
  - Der Transform `stripManagedFields` bleibt.
  - Optional kommt ein Transform hinzu, der nur die genutzten Felder behält: Pod-Container mit Name, Image und
    Ports, die Ready-Condition sowie Labels und Annotations.
- **`Run(ctx)`:** startet die Informer, wartet auf `WaitForCacheSync`, setzt dann `Ready()`.
- **Event-Handler:** Sie markieren nur Revisionen, ohne Store-Schreibzugriff.
  - **Namespace-Event:** Gehört der Namespace zu einem Projekt (`matcher.Match`), bekommt er eine neue Revision
    (`touch`), sonst wird er aus der Revisions-Map entfernt. Ein Projektwechsel ergibt einfach eine neue Revision.
  - **Pod-, Service- oder Ingress-Event:** `touch(namespace)`, aber nur, wenn der Namespace in der Revisions-Map
    steht. Events aus fremden Namespaces kosten nur einen Map-Lookup.
  - `touch` vergibt streng monotone Zeitstempel (Logik aus `store.nextTimestamp`) und signalisiert über einen
    koaleszierenden `Changes()`-Kanal.
- **Lesefunktionen** für Hub, API und Aktionen; alle konvertieren aus den Listern, Objekte daraus werden nie
  verändert:
  - `NamespaceRevisions() map[string]time.Time`
  - `Namespace(name) (model.NamespaceView, time.Time, bool, error)`
  - `Get(ref, out) (bool, error)` und `Services(namespace)`, beide von `actions.ObjectReader`
- **Entfällt:** Generationen, Event-Queue, `rebuild`, `ReplaceAll`, `handleNamespace`, `loadNamespaceObjects`.
- **Tests:** `watcher_test.go` wird zu `cluster_test.go` (Fake-Client), mit denselben Szenarien:
  - Port-Auflösung nach Pod-Update
  - Namespace bekommt nachträglich Labels
  - Label entfernt, Namespace verschwindet
  - Revision ändert sich nur für betroffene Namespaces

### 3. API und Hub auf eine Schnittstelle umstellen
- In `internal/api` gibt es die Schnittstelle
  `Source { Changes(); NamespaceRevisions(); Namespace(name); Get(ref, out) }`. `kube.Cluster` erfüllt sie.
- `NewServer` und `newHub` nehmen `Source` statt `*store.Store`. `buildNamespace` nimmt `model.NamespaceView`.
- Die Hub-Logik bleibt unverändert. Sie arbeitet schon heute nur mit Revisionen und `Namespace(name)`; die
  Pipeline-Revisionen (`RevisionSource`) bleiben.
- **Tests (`server_test.go`):** Es gibt etwa 14 Stellen mit `st.Update(…)`. Ein kleiner `fakeSource` im Testpaket
  ersetzt sie: Map plus `put/delete`, die die Revision setzen. Der Aufbau läuft direkt über
  `converter.NamespaceView`. Die Testfälle selbst bleiben.

### 4. Aktionen
- `actions.ObjectReader` bleibt. In `main.go` wird `kube.Cluster` übergeben statt des Stores.
- Die Handler-Tests (`missingservices`, `nodeportservice`) nutzen heute `store.New()`. Sie bekommen einen kleinen
  Map-basierten Reader im jeweiligen Test oder einen gemeinsamen in `internal/actions/actionstest`.

### 5. Store und Config entfernen
- `internal/store` wird gelöscht (Code und Tests).
- `config.StoreConfig`, `Interval` und `ParseInterval` werden entfernt (samt Tests). Ein vorhandener Abschnitt `store:`
  wird ignoriert, weil die YAML-Prüfung nicht strikt ist. Aus den Beispiel-Configs wird er entfernt.
- **Optional als Sicherheitsnetz:** Der Hub gleicht einmal pro Stunde alle Namespaces ab (`syncLocked` mit erzwungenem
  Neuaufbau). Das ist billig, weil nur echte Unterschiede Events erzeugen, und braucht keine Config.

### 6. `main.go`
- `kube.NewCluster(client, matcher)` erzeugen, `go cluster.Run(ctx)` starten.
- `actions.Deps{Store: cluster, …}`, `api.NewServer(cluster, …)`, `cluster.Ready` für `/readyz`.

### 7. Doku
- **`CLAUDE.md`:**
  - **Paket-Tabelle:** `store` entfällt, `kube` wird „Informer, Lister-Zugriff, Revisionen je Namespace“,
    `converter` wird „reine Konvertierung + Referenzauflösung beim Aufbau“.
  - **Abschnitt „In-Memory-Store“** wird durch „Datenquelle: Informer-Cache“ ersetzt:
    - Lister-Objekte nie verändern, nur über `converter` lesen.
    - Keine Kopie daneben aufbauen.
    - Revisionen je Projekt-Namespace treiben den Event-Hub.
  - **„Gespeicherte Informationen je Objekt“** wird zu „Konvertierte Informationen je Objekt“; die
    Ingress-Port-Auflösung entfällt.
  - **Config:** `store.recreate-store-interval` entfällt.
  - **Leitplanken:**
    - „Store-Inhalte bleiben `json.RawMessage`“ wird ersetzt durch „Einzige Datenquelle ist der Informer-Cache;
      Lister-Objekte nicht verändern“.
    - „Store konsistent mitführen“ wird ersetzt durch „Änderungen kommen ausschließlich über Informer-Events“.
  - **Getroffene Entscheidungen:** Die Einträge zu Store-Überschreiben und periodischem Neuaufbau werden ersetzt durch
    „Kein eigener Store, kein Full-Rebuild: Der Informer ist konsistent, der Hub vergleicht das Ergebnis“.
- **`README.md`:**
  - Architektur-Diagramm und Abschnitte „In-Memory-Store“ und „Converter“
  - Config-Abschnitt `store`
  - Prüfungen beim Start
- **`openapi.yaml`** bleibt unverändert, denn an der API ändert sich nichts.

## Umfang (Schätzung)

| Bereich                         | heute (Zeilen, ohne Tests) | danach |
|---------------------------------|----------------------------|--------|
| `store`                         | 357                        | 0      |
| `converter` (`apply` + Ingress-Resolve) | ~190               | ~0     |
| `kube/watcher` → `kube/cluster` | 311                        | ~170   |
| `config` (Store/Interval)       | ~45                        | 0      |
| Summe                           | ~900                       | ~170   |

Tests:
- `store_test.go` und die Store-basierten Converter-Tests entfallen.
- `server_test.go` und zwei Handler-Tests werden mechanisch auf Fakes umgestellt.
- `watcher_test.go` wird zu `cluster_test.go`.

## Risiken

- **Konsistenz:** Der Aufbau liest vier Lister nacheinander. Ein kurzzeitig inkonsistenter Stand (z. B. ein neuer Pod
  schon da, ein Service-Update noch nicht) wird durch das nächste Event korrigiert. Das ist gleichwertig zum heutigen
  Verhalten.
- **Mutationen:** Lister liefern Pointer in den Cache. Absicherung: Nur `converter` und `kube.Cluster` greifen auf
  Lister-Objekte zu, und `converter` klont Maps (heute schon `maps.Clone`).
- **Last:** Ein Pod-Statuswechsel löst wie bisher den Neuaufbau eines Namespaces aus. Der Aufbau liest jetzt aus den
  Listern statt aus dem Store, das ist vergleichbar. Die Coalescing-Logik bleibt.

## Verifikation

- `make lint`, `make test`.
- Lokales Backend gegen kind (Test-kubeconfig mit `host.docker.internal`):
  1. Den Snapshot mit dem Stand vor der Umstellung vergleichen; er muss identisch sein (gleiche Objekte, `running`,
     `areas`, URLs, Aktionen).
  2. Pod löschen. Die Events entsprechen dem bisherigen Verhalten (vier Events beim Neustart eines Pods).
  3. Namespace-Label entfernen und wieder setzen. Erwartet wird ein `delete` und danach ein `upsert` mit vollem Inhalt.
  4. Eine Aktion ausführen, z. B. „NodePort-Service erstellen“ und wieder löschen. Die Events kommen wie bisher.
