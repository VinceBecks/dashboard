# Feature-Dashboard – Spring-Boot-Anwendung

Diese Anwendung ist der Nachbau des Go-Dashboards (`backend/cmd/dashboard`) mit Spring Boot. Sie bietet dieselben
Features und dieselbe HTTP-API (`backend/internal/api/openapi.yaml`): Snapshot, Server-Sent Events, Aktionen, Links,
Kopier-Befehle, laufende Pipelines, Teams und Service-Bereiche. Das separate Frontend und der Go-CronJob
`nightly-scale-down` funktionieren unverändert mit ihr.

Die Go-Anwendung unter `backend/` bleibt vorerst unverändert bestehen. Der CronJob `nightly-scale-down` bleibt in Go.

| Thema          | Umsetzung                                                                          |
|----------------|------------------------------------------------------------------------------------|
| Laufzeit       | Java 21, Spring Boot 4.1 (Spring MVC, virtuelle Threads), Gradle                   |
| Kubernetes     | fabric8 `kubernetes-client` 8 mit Informern (Namespace, Service, Pod, Ingress)    |
| Templates      | Pebble (statt Go-Templates)                                                         |
| Konfiguration  | `application.yml` unter `dashboard:` (statt eigener `config.yaml`)                 |
| API-Doku       | dieselbe `openapi.yaml` wie das Go-Backend, Swagger-UI unter `/api/swagger`         |

Der Plan zur Umstellung steht in `plans/spring-boot-migration.md`.

---

## Schnellstart

Voraussetzungen: JDK 21 und eine kubeconfig mit Zugriff auf einen Cluster (z. B. kind).

```sh
./gradlew test                                   # alle Tests (ohne Cluster)
GITLAB_TRIGGER_TOKEN_FOO=dummy ./gradlew bootRun # lokal starten, Port 8080
```

`bootRun` lädt die Beispiel-Konfiguration `config-example/application.yml`. Sie enthält GitLab-Aktionen, die das
Token aus `GITLAB_TRIGGER_TOKEN_FOO` brauchen; ohne die Variable startet die Anwendung nicht. Den Cluster wählt
fabric8 wie `kubectl`: im Cluster die In-Cluster-Konfiguration, lokal `KUBECONFIG` bzw. `~/.kube/config`.

Eine eigene Konfiguration startest du so:

```sh
./gradlew bootJar
java -jar build/libs/feature-dashboard-0.0.1-SNAPSHOT.jar --spring.config.additional-location=file:meine-config.yml
```

---

## Konfiguration

Die fachliche Konfiguration steht unter `dashboard:` in einer `application.yml`. Inhaltlich entspricht sie der
`config.yaml` des Go-Backends (siehe `README.md`, Abschnitt Konfiguration). Vollständiges Beispiel:
`config-example/application.yml`. Technische Standardwerte (Port, Swagger, virtuelle Threads) stehen in
`src/main/resources/application.yml`.

Die Konfiguration wird beim Start vollständig geprüft (dieselben Regeln wie im Go-Backend, `DashboardConfigValidator`).
Fehler in Projekten, Aktionen, `extra-handler-configs`, Templates oder fehlende Tokens brechen den Start mit einer
Liste aller Fehler ab.

### Unterschiede zur `config.yaml`

1. **Alles unter `dashboard:`.**
2. **Label-Schlüssel mit `.` oder `/` in eckigen Klammern**, sonst verfälscht Spring sie (Relaxed Binding). Das
   betrifft `namespace-labels`, `label-combinations`, `link-variables`, `link-variables-overrides` und die
   `variables` von GitLab-Aktionen. Werte am besten als String in Anführungszeichen:

   ```yaml
   dashboard:
     projects:
       - project-name: foo-projekt
         namespace-labels:
           "[mycomp.de/is-feature-environment]": "true"
           "[mycomp.de/project-name]": foo-projekt
   ```

   Schlüssel ohne Sonderzeichen (z. B. `NAMESPACE` oder `demoUrl`) behalten Groß-/Kleinschreibung auch ohne Klammern;
   mit Klammern ist es eindeutig.
3. **Tokens als Spring-Platzhalter** statt `token-env`/`status-token-env`:

   ```yaml
   extra-handler-configs:
     token: ${GITLAB_TRIGGER_TOKEN_FOO}          # Pflicht
     status-token: ${GITLAB_READ_TOKEN_FOO}      # optional, laufende Pipelines verfolgen
   ```

   Ist die Umgebungsvariable nicht gesetzt, bricht der Start ab. `token-env` und `status-token-env` werden mit einem
   Hinweis abgelehnt.
4. **Templates in Pebble-Syntax** (siehe unten).
5. **Dauern** im Spring-Format: `15s`, `2m`, `500ms` oder ISO-8601 (`PT15S`). Zusammengesetzte Go-Dauern wie `1m30s`
   gehen nicht, dafür `90s`.
6. **`--static-dir`** heißt `dashboard.static-dir` (optional, mit SPA-Fallback auf `index.html`), **`--listen`** heißt
   `server.port`.
7. **`${…}`** in Werten löst Spring auf. Soll ein Template (z. B. ein Kopier-Befehl) wörtlich `${HOME}` enthalten,
   `\${HOME}` schreiben.

Unverändert bleiben alle anderen Schlüssel: `projects`, `service-areas`, `others-area-label`, `action-handlers`
(`action-name`, `handler`, `action-title`, `icon`, `icon-color`, `projects`, `extra-handler-configs`), `links`,
`copy-commands`, `teams`, `cluster`, `pipelines`. Das Label der Aktion `nightly-scale-down-exclusion` ist wie im
Go-Backend fest: `mycomp.de/skip-nightly-scale-down=true`.

### Templates (Pebble)

Templates gibt es in Links (`url`), Kopier-Befehlen (`command`) und GitLab-Aktionen (`ref`, `variables`).

**Daten:** `kind`, `name`, `namespace`, `project`, `labels`, `annotations`, `selector` (nur Services), `vars`
(`link-variables` mit Projekt-Overrides), `services` (nur `deploy-missing-services`), `container` (nur Kopier-Befehle
für Pods).

| Go-Template                                         | Pebble                                                   |
|-----------------------------------------------------|----------------------------------------------------------|
| `{{ .Namespace }}`                                  | `{{ namespace }}`                                        |
| `{{ .Vars.kibana }}`                                | `{{ vars.kibana }}`                                      |
| `{{ index .Labels "app.kubernetes.io/name" }}`      | `{{ labels['app.kubernetes.io/name'] }}`                 |
| `{{ urlquery .Name }}`                              | `{{ name \| urlencode }}` (Leerzeichen als `+`)           |
| `{{ pathescape .Namespace }}`                       | `{{ namespace \| pathescape }}`                          |
| `{{ rison "x" }}`, `{{ shellquote .Name }}`         | `{{ 'x' \| rison }}`, `{{ name \| shellquote }}`         |
| `{{ replace .Name "." "_" }}`                       | `{{ name \| replace({'.': '_'}) }}`                      |
| `{{ join .Services "," }}`                          | `{{ services \| join(',') }}`                            |
| `{{ trimPrefix (printf "%s-" .Project) .Namespace }}` | `{{ namespace \| trimPrefix(project ~ '-') }}`          |
| `{{ extractRenewJiraTicketNumber .Namespace }}`     | `{{ namespace \| extractRenewJiraTicketNumber }}`        |
| `{{ printf "a %s" .Name }}`                         | `{{ 'a ' ~ name }}`                                      |
| `{{ range $k, $v := .Selector }}…{{ end }}`         | `{% for e in selector %}{{ e.key }}={{ e.value }}{% endfor %}` (sortiert nach Schlüssel) |

- **Fehlende Werte:** Unbekannte Variablen und fehlende `vars`-Schlüssel sind ein Fehler (der Link bzw. Befehl
  entfällt, eine Aktion schlägt fehl). Fehlende Labels, Annotations und Selector-Einträge liefern dagegen `""`, wie
  `index` in Go.
- Kein HTML-Escaping. Syntaxfehler fallen beim Start auf.
- Lange Link-URLs mit `|-` umbrechen; Zeilenumbrüche samt Einrückung werden entfernt.

---

## HTTP-API

Identisch mit dem Go-Backend (`openapi.yaml`): `GET /api/events`, `GET /api/snapshot`,
`PATCH /api/actions/{action}`, `GET /healthz`, `GET /readyz`, `GET /api/openapi.yaml`, `GET /api/swagger`.
JSON-Feldnamen, Feldreihenfolge, Statuscodes (200/400/404/422/500/503) und Fehler-Bodies (`{"error": …}`, bei
Validierung mit `problems`) entsprechen dem Go-Backend; die Vertragstests (`EventHubTest`, `ActionsControllerTest`)
prüfen dieselben JSON-Strings wie `backend/internal/api/server_test.go`.

Kleine, fachlich gleichwertige Abweichungen:

| Thema                       | Go                          | Spring Boot                                                           |
|-----------------------------|-----------------------------|-----------------------------------------------------------------------|
| SSE-Zeilen                  | `id: 1`, `data: {…}`        | `id:1`, `data:{…}` (das Leerzeichen ist laut SSE-Spezifikation optional) |
| Sonderzeichen in JSON       | `&` als `&` usw.       | unverändert (`&`); beim Parsen identisch                              |
| `/api/swagger`              | HTML-Seite                  | Weiterleitung (302) auf die Swagger-UI, die `/api/openapi.yaml` lädt  |
| Pods beim Neustart löschen  | mit UID-Precondition        | ohne Precondition (fabric8 kennt keine UID-Precondition; Pods werden direkt vorher gelesen) |
| NodePort-Service löschen    | mit UID-Precondition        | mit Resource-Version-Precondition (strenger)                          |
| Zusätzlich                  | –                           | Actuator unter `/actuator` (Health, Info)                             |

---

## Aufbau (`src/main/java/its/me/feature_dashboard/`)

Jedes Paket entspricht dem gleichnamigen Go-Paket; der Go-Code und seine Tests waren die Spezifikation.

| Paket                     | Go-Vorlage                      | Inhalt                                                                  |
|---------------------------|---------------------------------|-------------------------------------------------------------------------|
| `config`                  | `internal/config`               | `DashboardProperties` (Records), `DashboardConfigValidator`, `DashboardConfiguration` (Verdrahtung), statische Dateien |
| `model`                   | `internal/model`                | `Kind`, `ObjectRef`, Stored-Records, `NamespaceView`, `ClusterSource`, `RevisionSource` |
| `project`                 | `internal/project`              | `ProjectMatcher`, `ServiceAreas`                                        |
| `converter`               | `internal/converter`            | fabric8-Objekt → Stored-Record, Replicas und benannte targetPorts       |
| `kube`                    | `internal/kube`                 | `Cluster` (Informer-Cache, Revisionen je Projekt-Namespace), `TrimmingItemStore` (reduziert den Cache wie `trim`) |
| `template`                | `internal/objecttemplate`       | Pebble-Engine, Template-Daten, eigene Filter                            |
| `links`, `copycommands`   | gleichnamig                     | Rendern je Objekt                                                       |
| `pipelines`               | `internal/pipelines`            | `PipelineState` (nur im Speicher), `PipelineTracker`                    |
| `actions` (+ Unterpakete) | `internal/actions`              | `ActionHandler`, `ActionHandlerType` (Spring-Bean je Typ), `ActionRegistry`, `ExtraConfig`, alle Handler-Typen, `gitlabpipeline.Trigger` |
| `api`                     | `internal/api`                  | DTOs, `NamespaceDtoBuilder`, `EventHub`, Controller                     |

Eine neue Aktion: Handler-Typ als `@Component` implementieren (`ActionHandlerType` + `ActionHandler`), Settings als
Record über `ExtraConfig.bind` lesen, Eintrag in `dashboard.action-handlers`. Am Frontend ist nichts zu tun.

Die Leitplanken der `CLAUDE.md` gelten unverändert: nur die vier Objekt-Typen beobachten, Informer-Cache als einzige
Datenquelle, Lister-Objekte nie verändern, kein Zustand an ArgoCD-verwalteten Objekten, Aktionslogik nur im Backend.

---

## Tests

`./gradlew test` läuft ohne Cluster:

- Kubernetes über den fabric8-Mock-Server (`@EnableKubernetesMockClient(crud = true)`), inklusive echter Informer
  (`ClusterTest`) und der Handler, die den Cluster ändern (`KubernetesHandlersTest`).
- GitLab über einen kleinen HTTP-Server aus dem JDK (`FakeGitLab`).
- `TestSource` ist die In-Memory-Datenquelle für Hub und Handler (wie `internal/testsource`).
- `FeatureDashboardApplicationTests` startet die ganze Anwendung gegen den Mock-Server und prüft Health, Snapshot,
  SSE-Stream, eine Aktion und den daraus folgenden `upsert`.

**Gegenprobe gegen das Go-Backend:** Beide gegen denselben Cluster starten (Go auf 8080, Spring Boot auf 8081, mit
gleichwertiger Konfiguration) und die Snapshots vergleichen:

```sh
diff <(curl -s localhost:8080/api/snapshot | jq -S .) <(curl -s localhost:8081/api/snapshot | jq -S .)
```

---

## Image und Deployment

```sh
docker build -f Dockerfile.spring-boot -t feature-dashboard:1.0.0 .
# mit Company-CA im Truststore:
docker build -f Dockerfile.spring-boot --secret id=company_ca,src=/pfad/zur/company-ca.pem -t feature-dashboard:1.0.0 .
```

- Das Image enthält nur das Jar (Laufzeit `eclipse-temurin:21-jre`, Benutzer 10001). Die Company-CA wird beim Build
  optional per `keytool` in den Truststore importiert.
- Die Konfiguration wird als `/config/application.yml` eingehängt (z. B. ConfigMap); das Image setzt
  `SPRING_CONFIG_ADDITIONAL_LOCATION=optional:file:/config/application.yml`.
- Tokens kommen als Umgebungsvariablen (z. B. per `envFrom` aus dem Secret `feature-dashboard-gitlab`) und werden in
  der Konfiguration als `${…}` referenziert.
- Proben: Liveness `/healthz`, Readiness `/readyz`. RBAC wie beim Go-Backend (`backend/deploy/rbac.yaml`).
- **Genau ein Replica**, wie beim Go-Backend: Laufende Pipelines liegen nur im Speicher, und jede Replica hätte
  eigene Event-Streams.
