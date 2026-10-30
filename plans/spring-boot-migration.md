# Plan: Feature-Dashboard als Spring-Boot-Anwendung (Feature-Parität mit dem Go-Backend)

## Kontext

Das Team arbeitet überwiegend mit Java/Spring Boot. Das Dashboard-Backend (Go, `backend/cmd/dashboard`) wird
deshalb als Spring-Boot-Anwendung nachgebaut. Grundlage ist das im letzten Commit (`c5f6b79`) erzeugte
Gradle-Projekt im Repository-Root: Spring Boot 4.1.2, Java 21, `spring-boot-starter-webmvc`, Actuator, springdoc 3.2.0,
Lombok, Paket `its.me.feature_dashboard`.

Vorgaben des Nutzers:
- Gleiche Features wie das Go-Dashboard. **Die Go-Anwendung wird nicht verändert**, auch nicht `backend/`,
  `Makefile`, `Dockerfile`, `README.md`, `CLAUDE.md` und `.dockerignore`.
- Der CronJob `nightly-scale-down` bleibt in Go.
- Template-Engine: **Pebble**.
- Kein Proxy-Code. Die Company-CA kommt im Dockerfile in den Truststore.
- Die Anwendung bleibt im Root. Neben dem Code entstehen nur `README-spring-boot.md` und `Dockerfile.spring-boot`.
  `README.md` und `Dockerfile` im Root gehören zur Go-Anwendung.

Ziel: Die API bleibt **byte-kompatibel** zu `backend/internal/api/openapi.yaml`, ebenso SSE-Events, Snapshot,
Statuscodes und Fehler-Bodies. Das separate Frontend und der Go-CronJob (festes Label
`mycomp.de/skip-nightly-scale-down=true`) funktionieren dann unverändert weiter.

Gemäß CLAUDE.md wird dieser Plan bei der Umsetzung zusätzlich als `plans/spring-boot-migration.md` abgelegt.

## Technische Entscheidungen

| Thema | Entscheidung |
|---|---|
| Kubernetes | fabric8 `kubernetes-client` 7.x: `SharedInformerFactory`/`SharedIndexInformer`, `Lister`, `ResourceEventHandler`. Kubeconfig über die Standard-Auflösung von fabric8 (In-Cluster, sonst `KUBECONFIG` bzw. `~/.kube/config`). |
| Trim des Caches | Eigener `ItemStore` (Wrapper um `BasicItemStore`) über `informer.itemStore(...)` vor dem Start. Er entfernt `managedFields` und reduziert Pods auf Container (Name/Image/Ports), `phase` und die Ready-Condition, analog zu `trim` in `backend/internal/kube/cluster.go:255`. |
| Web/SSE | Spring MVC mit `SseEmitter` (Timeout 0). Virtuelle Threads (`spring.threads.virtual.enabled=true`). |
| JSON | Jackson 3, aus Spring Boot 4. Records mit `@JsonPropertyOrder` in der Feldreihenfolge der Go-Structs. `@JsonInclude(NON_NULL)` dort, wo Go `omitempty`/`omitzero` nutzt (`team`, `formFields`, Kind-Listen in `upsert`). |
| Konfiguration | Die `config.yaml` entfällt, alles liegt unter `dashboard:` in der `application.yml` (`@ConfigurationProperties`, Records). |
| Templates | Pebble mit `strictVariables(true)` und `autoEscaping(false)`. Templates werden beim Start geparst (`getLiteralTemplate`). |
| GitLab | `RestClient` mit Timeout je Aktion (`JdkClientHttpRequestFactory`). |
| OpenAPI/Swagger | `openapi.yaml` und `example-api.jsonc` werden **nicht kopiert**: `processResources` übernimmt sie aus `backend/internal/api/`, sodass es nur eine Quelle gibt. springdoc zeigt die statische Datei: `springdoc.api-docs.enabled=false`, `springdoc.swagger-ui.url=/api/openapi.yaml`, `springdoc.swagger-ui.path=/api/swagger`. |
| Health | Eigene Endpunkte `GET /healthz` und `GET /readyz` (`503`, bis der Informer-Cache synchron ist), wie im Go-Backend. Actuator bleibt unter `/actuator`. |
| Tests | JUnit 5, fabric8 `kubernetes-server-mock` (`@EnableKubernetesMockClient(crud = true)`) statt Fake-Clientset, okhttp `mockwebserver` als Fake-GitLab. |

## Konfiguration (`application.yml`)

Die Struktur entspricht der `config.yaml`, alles liegt unter `dashboard:`. Unterschiede:

- **Label-Schlüssel mit `.`/`/` in eckigen Klammern** (Spring Relaxed Binding), Werte als String:
  `"[mycomp.de/project-name]": "foo-projekt"`. Das betrifft `namespace-labels`, `label-combinations` und Schlüssel mit
  Punkt in `link-variables`.
- **Tokens als Spring-Platzhalter** statt `token-env`/`status-token-env`: `token: ${GITLAB_TRIGGER_TOKEN_FOO}` und
  optional `status-token: ${GITLAB_READ_TOKEN_FOO}`. Ist die Variable nicht gesetzt, scheitert der Start, wie bisher.
- **Pebble-Syntax** in allen Templates (Links, Kopier-Befehle, `ref`, `variables`).
- `extra-handler-configs` bindet jeder Handler typisiert, z. B. über
  `Binder.bind("dashboard.action-handlers[i].extra-handler-configs", Settings.class)`, statt `DecodeExtra`.
- `--static-dir` wird zur optionalen Property `dashboard.static-dir` (mit SPA-Fallback auf `index.html`).
  `--listen` wird zu `server.port`.

Template-Daten (Pebble-Variablen): `kind`, `name`, `namespace`, `project`, `labels`, `annotations`, `selector`, `vars`,
`services`, `container`. Funktionen bzw. Filter:

| Go | Pebble |
|---|---|
| `urlquery` | Filter `urlencode` (eingebaut, Leerzeichen als `+`) |
| `pathescape`, `rison`, `shellquote`, `extractRenewJiraTicketNumber` | eigene Filter (`DashboardPebbleExtension`) |
| `trimPrefix prefix value` | Filter `value \| trimPrefix(prefix)` |
| `replace s old new`, `join list sep` | eingebaute Filter `replace({'old':'new'})`, `join(sep)` |
| `index .Labels "a/b"` | `labels['a/b']`. Ein fehlendes Label ist leer, ein fehlender `vars`-Schlüssel ein Fehler. Das Verhalten von Pebble im Strict-Mode legen Tests fest, notfalls mit einem eigenen `Map`-Wrapper. |

Links: Zeilenumbrüche samt Einrückung entfernen, nur absolute http(s)-URLs zulassen. Kopier-Befehle: Leerraum
trimmen, bei Pods je Container rendern (`<id>/<container>`, `<title> (<container>)`), wie in `copycommands.go`.

## Paketstruktur (`src/main/java/its/me/feature_dashboard/…`)

Jedes Paket portiert das gleichnamige Go-Paket. **Der Go-Code und seine Tests sind die Spezifikation.**

| Paket | Go-Vorlage | Inhalt |
|---|---|---|
| `config` | `internal/config` | `DashboardProperties` (Records), `DashboardConfigValidator`: alle Regeln aus `config.go` (eindeutige IDs, bekannte Projekte, Service-Bereiche, Teams, Cluster, `poll-interval` ≥ 5s, Warnungen bei `projects: []`). Fehler gesammelt, Startabbruch. |
| `model` | `internal/model` | `Kind`, `ObjectRef`, `Stored*`-Records, `NamespaceView` |
| `project` | `internal/project` | `ProjectMatcher` (erstes Projekt in Config-Reihenfolge), `ServiceAreas.areaIds` |
| `kube` | `internal/kube` | `Cluster`: Informer für Namespace, Service, Pod und Ingress, Revisionen je Projekt-Namespace (streng monoton), Signal `changes()`, `namespace(name)`, `get(ref)`, `services(ns)`, `ready()`, `TrimmingItemStore` |
| `converter` | `internal/converter` | fabric8-Objekt → Stored-Record, `namespaceView` mit Replicas (Selector-Match) und Auflösung benannter targetPorts |
| `template` | `internal/objecttemplate` | `ObjectTemplates` (Pebble-Engine, Daten, Filter) |
| `links`, `copycommands` | gleichnamig | Rendern je Objekt, Weglassen bei Fehlern |
| `pipelines` | `internal/pipelines` | `PipelineState` (letzte Pipeline je Objekt und Aktion, Revisionen, Signal), `PipelineTracker` (Polling im konfigurierten Intervall, entfernt beendete und unbekannte) |
| `actions` | `internal/actions` | `ActionHandler` (configure/isApplicable/formFields/execute), `ActionHandlerType` (Spring-Bean je Typ, erzeugt Instanzen), `ActionRegistry` (Aktivierung, Projektprüfung, Validierung von `text`/`number`/`select`/`multi-select`), `Result`, `FormField`, Exceptions |
| `actions.*` | je Handler-Paket | `deletepod`, `nodeportservice` (create/delete, Label `feature-dashboard/created-by-action`, ownerReference), `restartservice`, `gitlabpipeline` (`Trigger` mit `start(ref, variables, inputs)`), `missingservices`, `assignteam`, `nightlyscaledown` (gleiche Konstanten) |
| `api` | `internal/api` | DTO-Records, `NamespaceDtoBuilder`, `Flattener`, `EventHub`, Controller |

## API (`api`)

- `EventHub`, Port von `events.go`:
  - Lock, `syncLocked` über die Revisionen von `Cluster` und `PipelineState`, Aufbau geänderter Namespaces, Diff
    über JSON-Bytes.
  - `upsert` Parent vor Kind, `delete` Kind vor Parent, kein `delete` für Kinder eines gelöschten Parents.
  - Snapshot und Abo unter demselben Lock. Puffer je Abonnent 256, zu langsame Clients werden getrennt.
  - Stündlicher Voll-Abgleich. `snapshot()` ohne Abo.
- `EventsController`: `GET /api/events` mit `id`/`event`/`data` je Event und alle 25 s `: ping`. Header
  `Cache-Control: no-cache` und `X-Accel-Buffering: no`. `503`, solange nicht bereit. `Last-Event-ID` wird ignoriert.
- `SnapshotController`: `GET /api/snapshot`.
- `ActionsController`: `PATCH /api/actions/{action}` mit `200`/`400` (inkl. `problems`)/`404`/`422`/`500`, alle
  Fehler als `{"error": …}`. Bei Namespaces darf `object.namespace` fehlen.
- `HealthController` (`/healthz`, `/readyz`), Auslieferung von OpenAPI und Swagger, optional statische Dateien.

## Build und Dockerfile

- `build.gradle.kts` bekommt diese Abhängigkeiten:
  - `io.fabric8:kubernetes-client`, `io.pebbletemplates:pebble`, `spring-boot-starter-validation`
  - Test: `io.fabric8:kubernetes-server-mock`, `com.squareup.okhttp3:mockwebserver`
  - `processResources` kopiert `backend/internal/api/openapi.yaml` nach `static/api/`.
  - `bootRun` lädt `config-example/application.yml` über `spring.config.additional-location`.
- `config-example/application.yml`: `backend/config.example.yaml` umgesetzt in Spring-Syntax, Klammer-Keys und
  Pebble. Nicht unter `./config/`, damit Spring sie nicht automatisch lädt, auch nicht in Tests.
- `src/main/resources/application.yml`: technische Defaults (Port 8080, virtuelle Threads, springdoc,
  Actuator-Basispfad). Keine Fachkonfiguration. `application.properties` entfällt.
- `Dockerfile.spring-boot`:
  - Build mit `eclipse-temurin:21-jdk` und `./gradlew bootJar`.
  - Laufzeit `eclipse-temurin:21-jre`, nonroot.
  - Company-CA optional als BuildKit-Secret `company_ca`, importiert mit `keytool -importcert -cacerts`.
  - `ENTRYPOINT java -jar app.jar`. Die Konfiguration kommt per ConfigMap nach `/config/application.yml`, mit
    `SPRING_CONFIG_ADDITIONAL_LOCATION`.
  - Build: `docker build -f Dockerfile.spring-boot .`

## Reihenfolge der Umsetzung (jede Etappe mit Tests)

1. Build, Abhängigkeiten, `DashboardProperties` und Validator, `config-example/application.yml`. Tests nach
   `config_test.go`.
2. `model`, `project`, `converter`. Tests nach `converter_test.go` und `project`-Tests.
3. `kube.Cluster` mit Trim-Store. Tests mit dem Mock-Server: Revisionen, Projektwechsel, Löschen, Trim.
4. `template`, `links`, `copycommands`. Tests nach den Go-Tests (Kibana/rison, je Container, Fehlerfälle).
5. `actions`-Framework und alle Handler-Typen, `gitlabpipeline.Trigger`. Tests nach den Go-Handler-Tests.
6. `pipelines` (State und Tracker).
7. `api`: DTOs, Flatten, `EventHub`, Controller. **Vertragstests** mit den erwarteten JSON-Strings aus
   `backend/internal/api/server_test.go` und dem Beispiel `example-api.jsonc`.
8. `Dockerfile.spring-boot`, `README-spring-boot.md` (Start, Konfiguration inkl. Unterschiede zur `config.yaml`,
   Pebble-Syntax, Tests, Image), `plans/spring-boot-migration.md`.

## Verifikation

- In dieser Umgebung gibt es kein Java und kein Docker. Für Build und Tests wird ein Temurin-JDK 21 ins Scratchpad
  geladen und `./gradlew test` damit ausgeführt (`JAVA_HOME` auf das Scratchpad).
- `./gradlew test` grün, inklusive der Vertragstests gegen die JSON-Strings des Go-Backends.
- Start mit `./gradlew bootRun` gegen den Fake-API-Server aus dem Scratchpad bzw. eine Dummy-kubeconfig: Die Config
  wird geprüft (fehlerhafte Config bricht den Start ab), `/healthz` antwortet, `/api/swagger` zeigt die Spec.
- **Gegenprobe durch den Nutzer** gegen einen kind-Cluster: Go- und Java-Dashboard parallel starten (Ports 8080 und
  8081) und `curl -s …/api/snapshot | jq -S` vergleichen. Die Ausgaben müssen identisch sein. Dazu eine Aktion über
  beide auslösen und die `upsert`-Events im Stream vergleichen.

## Umsetzungsnotizen (Abweichungen vom Plan)

- **SSE über `SseEmitter`** statt `StreamingResponseBody`: Mit `StreamingResponseBody` kamen die Daten in Spring
  Boot 4.1 nicht beim Client an (Antwort-Header blieben aus). `SseEmitter` schreibt `data:{…}` statt `data: {…}`;
  laut SSE-Spezifikation gleichwertig.
- **Fake-GitLab** in Tests mit dem `HttpServer` des JDK statt okhttp `mockwebserver` (keine zusätzliche Abhängigkeit).
- **Versionen:** fabric8 8.0.0, Pebble 4.1.2 (aktuelle Releases; Plan nannte fabric8 7.x).
- **Pebble-Strict-Mode** meldet auch fehlende Map-Schlüssel als Fehler. Labels, Annotations und Selector stehen deshalb
  in einer toleranten Map (fehlender Schlüssel → ""), `vars` bleibt streng – wie in Go.
- **Nicht auflösbare Platzhalter** (`token: ${FEHLT}`) lässt Spring beim Binden stehen; der Trigger prüft das und
  bricht den Start ab.
- **Löschen mit Precondition:** fabric8 kennt keine UID-Precondition. NodePort-Services werden mit
  Resource-Version-Precondition gelöscht, Pods (Neustart) ohne.
- **Maps in Templates** durchläuft man in Pebble als Einträge: `{% for e in selector %}{{ e.key }}={{ e.value }}{% endfor %}`.
