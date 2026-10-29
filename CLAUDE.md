# CLAUDE.md

Antworte und dokumentiere auf Deutsch; Code-Bezeichner (Typen, Funktionen, Variablen) bleiben englisch.

Wenn du im Plan-Mode arbeitest, dann erstelle den Plan in einer Datei unter dem Ordner `plans/`.
In dem Ordner helpers/ liegen helper Dateien, welche nicht mit der Anwendung selber in Zusammenhang stehen. 

## Projektziel

Ein Dashboard, in dem Software-Entwickler sehen, welche Feature-Environments sie in einem Kubernetes-Cluster
hochgefahren haben und welche Services darin laufen.
Über das Dashboard können außerdem Aktionen auf den angezeigten Objekten ausgelöst werden.

## Basis-Informationen

- Kubernetes-Cluster: Version 1.33.1
- Backend: Go, Code liegt in `backend/`, läuft im Cluster und ist immer auf genau ein replica skaliert
- Frontend: wird nicht mehr in diesem Repository entwickelt (siehe Abschnitt „Frontend“); hier nur das Backend
- Keine Authentifizierung im Dashboard
- Ausgehende Kommunikation läuft über einen Proxy mit eigener Company-CA. Kein eigener TLS-/Proxy-Code: HTTP-Clients
  nutzen den Standard-Transport von Go (`HTTPS_PROXY`/`NO_PROXY`, System-CAs plus `SSL_CERT_DIR`); konfiguriert wird
  über `backend/deploy/network.example.yaml`. Neue HTTP-Clients ebenso bauen (kein eigener `Transport` ohne Proxy/CA).
  `NO_PROXY` muss den Kubernetes-API-Server ausnehmen, da client-go `HTTPS_PROXY` ebenfalls beachtet.

## Aktueller Stand
Das Backend ist implementiert (Plan Backend: `plans/backend-implementierung.md`, Umstellung auf
Server-Sent Events: `plans/sse-einseiten-dashboard.md`). Das Frontend wurde aus dem Repository entfernt
(`plans/frontend-entfernen.md`). Das Image enthält nur das Backend (`Dockerfile` im Repository-Root).

Backend-Pakete (`backend/`):

| Paket                         | Aufgabe                                                                   |
|-------------------------------|---------------------------------------------------------------------------|
| `cmd/dashboard`               | Startup, Wiring, Registrierung der ActionHandler                          |
| `cmd/nightly-scale-down`      | Zweite Anwendung (CronJob): startet die Pipeline für das nächtliche Herunterskalieren |
| `internal/config`             | `config.yaml` laden/validieren                                            |
| `internal/model`              | Stored-Object-Structs, `NamespaceView`                                    |
| `internal/project`            | Namespace-Labels → Projekt                                                |
| `internal/converter`          | k8s-Objekt (aus dem Lister) → Stored-Object; `NamespaceView` löst Replicas und targetPorts beim Aufbau auf |
| `internal/kube`               | `Cluster`: Informer-Cache als einzige Datenquelle, Lister-Zugriff, Revisionen je Projekt-Namespace, Trim-Transform |
| `internal/testsource`         | In-Memory-Datenquelle nur für Tests (erfüllt `api.Source` und `actions.ObjectReader`) |
| `internal/actions`            | ActionHandler-Interface, ActionRegistry, Validierung der Formular-Felder  |
| `internal/actions/deletepod`  | ActionHandler `delete-pod`                                                |
| `internal/actions/nodeportservice` | ActionHandler `node-port-service-on-service-base` und `delete-node-port-service`; erstellte Services tragen das Label `feature-dashboard/created-by-action` und eine ownerReference auf den Basis-Service (werden mit ihm gelöscht) |
| `internal/actions/restartservice` | ActionHandler `restart-service-replicas`: löscht alle Pods eines Services (Neustart), nur Pods mit Controller |
| `internal/actions/gitlabpipeline` | Handler-Typ `gitlab-pipeline`: startet GitLab-Pipelines per Trigger-API, beliebig viele Aktionen; `Trigger` ist wiederverwendbar |
| `internal/actions/assignteam` | ActionHandler `assign-team`: ordnet einen Namespace einem Team zu (setzt/entfernt das Label `teams.label-key` per Merge-Patch) |
| `internal/actions/nightlyscaledown` | Handler-Typ `nightly-scale-down-exclusion`: nimmt einen Namespace vom nächtlichen Herunterskalieren aus, indem er das von der Skalierungs-Anwendung ausgewertete Label setzt (`mode: mark`) bzw. entfernt (`mode: unmark`); zwei Aktionen, die sich gegenseitig ablösen |
| `internal/actions/missingservices` | Handler-Typ `deploy-missing-services`: bietet am Namespace die fehlenden konfigurierten Services (Abgleich per Name) zur Mehrfachauswahl an und startet dafür eine GitLab-Pipeline |
| `internal/scaledowntrigger`   | Logik des CronJobs: Konfiguration aus Umgebungsvariablen, Namespaces mit Ausnahme-Label lesen, Pipeline mit Input `EXCLUDE_NAMESPACES` starten (`gitlabpipeline.Trigger.Start`) |
| `internal/objecttemplate`     | Go-Templates mit Objekt-Daten (gemeinsam für `gitlab-pipeline` und Links)  |
| `internal/links`              | Rendert die konfigurierten Links je Objekt                                |
| `internal/copycommands`       | Rendert die konfigurierten Kopier-Befehle je Objekt (bei Pods je Container) |
| `internal/pipelines`          | Laufende GitLab-Pipelines je Objekt und Aktion (`State`, nur im Speicher) und `Tracker` (Status-Abfrage, entfernt beendete) |
| `internal/api`                | HTTP-API, DTOs (`dto.go`), Event-Hub und SSE (`events.go`), `openapi.yaml`, `example-api.jsonc`, Swagger-UI |

## Build, Test, Start

Alle Befehle laufen über das `Makefile` im Repository-Root:

| Befehl              | Wirkung                                                                                   |
|---------------------|-------------------------------------------------------------------------------------------|
| `make test`         | Backend-Tests (`go test -race`)                                                           |
| `make lint`         | `go vet`                                                                                  |
| `make build`        | Binaries nach `backend/bin/` (`dashboard`, `nightly-scale-down`)                          |
| `make run`          | Backend lokal starten (Port 8080)                                                         |
| `make docker`       | Backend-Image bauen (`IMAGE=...`, optional `COMPANY_CA=...` hinter dem Proxy); Manifeste in `backend/deploy/` |
| `make nightly-scale-down-run` | CronJob-Anwendung einmal lokal ausführen (Konfiguration aus der Umgebung)       |

Einzelschritte gibt es als `backend-build|test|vet|run`.
Lokale Starts nutzen den Cluster aus `KUBECONFIG` (Standard `~/.kube/config`) und `backend/config.example.yaml`;
andere Config mit `make run CONFIG=pfad.yaml` (relativ zum Repository-Root).

## Fachliche Begriffe

| Begriff            | Bedeutung                                                                                            |
|--------------------|------------------------------------------------------------------------------------------------------|
| Projekt            | In der `config.yaml` definiert. Hat mehrere Feature-Umgebungen.                                      |
| Feature-Umgebung   | Genau ein Namespace. Gehört zu genau einem Projekt.                                                  |
| Anwendungs-Service | Ein Kubernetes-Service vom Typ `ClusterIP`.                                                          |
| Service-Bereich    | Je Projekt konfigurierte Gruppe von Anwendungs-Services, zugeordnet über Service-Labels; ein Reiter. |
| Replica            | Ein Pod, der von einem Anwendungs-Service referenziert wird.                                         |
| Stored-Object      | Die reduzierte, konvertierte Form eines Kubernetes-Objekts, beim Lesen aus dem Informer-Cache erzeugt. |
| Aktion             | Eine vom Dashboard auslösbare Operation auf einem Objekt, umgesetzt durch genau einen ActionHandler. |

Ob ein Namespace eine Feature-Umgebung ist und zu welchem Projekt er gehört, wird ausschließlich über Labels am
Namespace bestimmt. Welche Labels für welches Projekt gelten, steht in der `config.yaml`.

## Backend

### Allgemeines zur Entwicklung

### Kubernetes-Anbindung

- Es wird die Bibliothek `client-go` verwendet
- Das Backend nutzt die Informer-API von `client-go` (`SharedInformerFactory`, Event-Handler, Lister), um stets aktuell zu sein.
- Relevant sind nur vier Objekt-Typen: **Namespace, Service, Ingress, Pod**. Keine weiteren Typen beobachten.
- Von diesen Objekten werden nur wenige Kern-Informationen übernommen, nicht das vollständige Objekt.
- Es werden nur Objekte verarbeitet, die zu einem Projekt gehören (erkennbar an den Labels ihres Namespaces). Alles
  andere wird verworfen.



### Converter

Der Converter (`internal/converter`) wandelt die Objekte aus den Listern in Stored-Objects um. `NamespaceView`
konvertiert alle Objekte eines Namespaces und löst dabei Referenzen auf: Replicas eines Services (Pods, die der
Selector trifft) und benannte targetPorts (`targetPort: http` → Port-Nummer aus den Container-Ports der Replicas).
Da bei jedem Aufbau alles neu berechnet wird, gibt es keine inkrementell gepflegten, abhängigen Objekte.

### Datenquelle: Informer-Cache (`kube.Cluster`)

- Einzige Datenquelle ist der Informer-Cache von client-go. Es gibt **keinen eigenen Store** und keine Kopie der
  Objekte daneben, auch keinen periodischen Neuaufbau: Der Informer ist durch List/Watch mit `resourceVersion` und
  Relist nach Watch-Abbruch konsistent, und der Event-Hub vergleicht das Ergebnis.
- Lister liefern Pointer in den Cache: **Lister-Objekte nie verändern**, nur über den `converter` lesen (er klont
  Maps). Zugriff auf Lister nur in `kube` und `converter`.
- Ein Transform (`trim`) reduziert den Cache auf die genutzten Felder (bei Pods: Container mit Name/Image/Ports,
  Ready-Condition; bei allen ohne managedFields). Werden weitere Felder gebraucht, `trim` erweitern.
- Die Event-Handler merken sich nur, welcher Projekt-Namespace sich geändert hat (`NamespaceRevisions`, Signal über
  `Changes()`); Objekte in anderen Namespaces kosten nur einen Map-Lookup. Lesen: `Namespace(name)` (komplette
  `NamespaceView`), `Get(ref)`, `Services(namespace)`.
- Alle Daten liegen in-memory; es gibt keine Persistenz. Auch die laufenden Pipelines (siehe unten) gehen bei einem
  Neustart verloren.

#### Konvertierte Informationen je Objekt

Je Objekt werden folgende Informationen übernommen:

- Name
- Labels
- Annotations

Zusätzliche Informationen für Pods:

- Ready-Zustand (keine Phase)
- Containers mit jeweils:
    - Name
    - Image
    - Port-Mappings

Zusätzliche Informationen für Services:

- Service-Type
- Service-Port-Mappings mit jeweils:
    - Name
    - TargetPort (wird auch aufgelöst, wenn referenced bei Name)
    - ServicePort
    - NodePort

Zusätzliche Informationen für Ingresses:

- je rule:
    - host + je path:
        - pathType
        - path
        - backend (Service-Name)

### Konfiguration (`config.yaml`)

Wird beim Startup einmal eingelesen. Enthält:

1. Die Projekte und die für sie relevanten Namespace-Labels.
2. Die ActionHandler und ob sie aktiviert sind.
3. Cluster-Angaben (Pflicht)
4. Links (optional)
5. Kopier-Befehle (optional)
6. Abfrage laufender Pipelines (optional)
7. Entwickler-Teams (optional)

Beispiel für Projekte:

```yaml
projects:
  - project-name: foo-projekt
    namespace-labels:
      mycomp.de/is-feature-environment: true
      mycomp.de/project-name: foo-projekt
      mycomp.de/organization-unit: development
  - project-name: bar-projekt
    namespace-labels:
      mycomp.de/is-feature-environment: true
      mycomp.de/project-name: bar
      mycomp.de/organization-unit: operations
```

- `project-name` ist der eindeutige Name des Projektes
- `namespace-labels` definiert Labels, die alle am Namespace definiert sein müssen, damit der Namespace zu dem Projekt
  gehört
- `service-areas` (optional) teilt die Anwendungs-Services (ClusterIP) einer Namespace-Kachel in Reiter auf:

```yaml
    others-area-label: Andere          # optional; Standard "Andere"
    service-areas:
      - area-name: business-services   # eindeutig je Projekt, "others" ist reserviert
        area-label: Business
        label-combinations:            # ODER über die Einträge, UND innerhalb eines Eintrags; keine leeren Einträge
          - app.kubernetes.io/part-of: shop
            app.kubernetes.io/component: backend
          - app.kubernetes.io/part-of: billing
```

  Ein Service kann in mehreren Bereichen liegen; nicht zugeordnete Services gehören zu `others`. Die Zuordnung
  berechnet das Backend beim Aufbau der DTOs (`project.ServiceAreaIDs`, `areas` am Service). Gleiche Bereiche in mehreren Projekten per YAML-Anker (`&name` / `*name`) wiederverwenden.

Beispiel für ActionHandler:

```yaml
action-handlers:
  - action-name: delete-pod
    action-title: Pod Löschen
    icon: trash
    icon-color: red
    projects: [foo-projekt, bar-projekt]
    extra-handler-configs:
      force-delete: true
```

- `action-name` ist die eindeutige Aktion-ID.
- `projects` ist Pflicht: die Projekte, in denen die Aktion angeboten und ausführbar ist. `projects: []`
  deaktiviert die Aktion; ihr Handler wird dann nicht erzeugt (es wird nur geprüft, ob der Handler-Typ existiert).
  Die Prüfung übernimmt die ActionRegistry vor dem Handler;
  Handler müssen sich nicht um Projekte kümmern. Das Projekt des Objekts steht ihnen in `StoredObject.Project` zur
  Verfügung.
- `handler` (optional) ist der Handler-Typ. Fehlt er, gilt `action-name` als Handler-Typ. Ein Handler-Typ kann
  mehrere Aktionen bedienen; je Aktion entsteht eine eigene Handler-Instanz mit eigener Konfiguration.
- `action-title`, `icon` und `icon-color` werden unverändert an das Frontend durchgereicht.
- `extra-handler-configs` ist handler-spezifisch und wird nur vom jeweiligen Handler interpretiert.

Beispiel für den generischen Handler-Typ `gitlab-pipeline` (startet eine Pipeline über die Pipeline-Trigger-API):

```yaml
  - action-name: deploy-feature
    handler: gitlab-pipeline
    action-title: Feature deployen
    icon: rocket
    icon-color: green
    projects: [foo-projekt]
    extra-handler-configs:
      gitlab-url: https://gitlab.mycomp.de
      project: team/foo-deploy            # Projekt-ID oder Pfad
      ref: main                           # Go-Template erlaubt
      token-env: GITLAB_TRIGGER_TOKEN_FOO # Env-Variable mit dem Trigger-Token
      applies-to: [namespace]             # namespace | service | ingress | pod
      timeout: 15s                        # optional, Standard 10s
      variables:
        NAMESPACE: "{{ .Namespace }}"     # verfügbar: .Kind .Name .Namespace .Project .Labels .Annotations
```

Beispiel für den Handler-Typ `deploy-missing-services` (nur Namespaces; gleiche GitLab-Felder wie `gitlab-pipeline`,
ohne `applies-to`). Fehlt nichts, bleibt die Aktion sichtbar, das Dropdown ist leer und die Ausführung wird abgelehnt:

```yaml
  - action-name: deploy-missing-services
    action-title: Fehlende Services deployen
    icon: package-plus
    icon-color: green
    projects: [foo-projekt]
    extra-handler-configs:
      services:                           # Abgleich per Service-Name; label optional
        - name: foo-backend
          label: Foo Backend
      gitlab-url: https://gitlab.mycomp.de
      project: team/foo-deploy
      ref: main
      token-env: GITLAB_TRIGGER_TOKEN_FOO
      variables:
        SERVICES: '{{ join .Services "," }}'  # .Services = gewählte Services
```

Tokens stehen nie in der `config.yaml`, sondern nur in Umgebungsvariablen. Im Cluster kommen sie aus dem Secret
`feature-dashboard-gitlab` (siehe `backend/deploy/gitlab-secret.example.yaml`). Ist eine Token-Variable nicht
gesetzt, startet das Backend nicht.

**Laufende Pipelines:** Haben `gitlab-pipeline` bzw. `deploy-missing-services` zusätzlich `status-token-env` (Env-Variable
mit einem Token mit `read_api`), merkt sich das Backend jede gestartete Pipeline am Objekt der Aktion (nur die letzte
je Objekt und Aktion) und liefert sie als `pipelines` aus; das Frontend zeigt „Pipeline läuft“ mit Link nach GitLab.
Der `Tracker` fragt den Status alle `pipelines.poll-interval` ab und entfernt beendete Pipelines (success, failed,
canceled, skipped) sowie unbekannte. Ohne `status-token-env` wird nichts gemerkt.

```yaml
pipelines:                                    # optional, gezeigt ist der Standardwert
  poll-interval: 15s                          # mindestens 5s
```

- Die laufenden Pipelines liegen **nur im Speicher** (`pipelines.State`). Nach einem Neustart des Backends ist der
  Stand leer; bereits laufende Pipelines werden dann nicht mehr markiert. Das ist bewusst so entschieden.
- Zustand nie an Objekten der Feature-Umgebungen ablegen (Annotations o. Ä.): Sie werden per ArgoCD mit auto-sync
  und self-heal verwaltet, fremde Änderungen werden zurückgesetzt.
- Änderungen an `State` lösen Events aus wie Änderungen im Cluster (der Event-Hub beachtet beide Revisionen je
  Namespace).


Beispiel für Links (Absprung z. B. nach Kibana). Das Backend rendert die URLs je Objekt und liefert sie als `links`
an jedem Objekt aus; das Frontend zeigt sie als Icon mit Tooltip und öffnet sie in einem neuen Tab:

```yaml
link-variables:                       # statische Werte für .Vars
  fooUrl: https://foo.mycomp.de
projects:
  - project-name: foo-projekt
    link-variables-overrides:         # überschreibt/ergänzt link-variables für dieses Projekt
      fooUrl: https://other.mycomp.de
links:
  - link-id: foo-system               # eindeutig
    title: Absprung nach Foo
    icon: scroll-text
    icon-color: "#00bfb3"
    projects: [foo-projekt]           # Pflicht; [] = in keinem Projekt (Warnung beim Start)
    targets:                          # je Objekt-Typ eine URL; nicht aufgeführte Typen haben den Link nicht
      namespace:
        url: "{{ .Vars.fooUrl }}/{{ pathescape .Namespace }}?foo=bar&ns={{ urlquery .Namespace }}"
      pod:
        url: |-                       # Zeilenumbrüche samt Einrückung werden entfernt
          {{ .Vars.fooUrl }}/{{ pathescape .Namespace }}/{{ pathescape .Name }}
          ?foo=bar&pod={{ urlquery .Name }}
```

- Jedes Target ist genau ein URL-Template. Kodiert wird im Template selbst: `urlquery` für Query-Werte,
  `pathescape` für Pfadteile, `rison` für Kibana-Parameter; Werte ohne Funktion landen unverändert in der URL.
- Lange URLs mit dem YAML-Blockstil `|-` umbrechen, nicht mit `>-` (der macht aus Zeilenumbrüchen Leerzeichen).
- Templates, die mit `{{` beginnen, in Anführungszeichen setzen (sonst liest YAML `{` als Mapping).
- Template-Daten: `.Kind .Name .Namespace .Project .Labels .Annotations .Selector` (nur Services) `.Vars`
  `.Services` (nur bei `deploy-missing-services` gefüllt) `.Container` (nur bei Kopier-Befehlen für Pods).
- Template-Funktionen zusätzlich zu Go-Standard (`urlquery`, `index`, `printf`, `range`, …): `rison`, `pathescape`,
  `replace`, `join`, `shellquote`, `trimPrefix` (entfernt ein Präfix, z. B.
  `{{ trimPrefix (printf "%s-" .Project) .Namespace }}`: „myproject-foo-bar“ → „foo-bar“; ohne dieses Präfix
  unverändert) und `extractRenewJiraTicketNumber` (Jira-Ticketnummer aus Namespaces der Form
  `renew-<f-|if-|hf-><story|hotfix|feat|bugfix|epic|intfix><nummer>…`, sonst leer).
- Gemeinsame URL-Teile mehrerer Targets (z. B. die Kibana-Discover-Basis) in `link-variables` auslagern.
- Templates werden beim Start geparst (Fehler → Startabbruch). Lässt sich ein Link für ein Objekt nicht rendern
  (z. B. fehlende Variable) oder ist das Ergebnis keine absolute http(s)-URL, wird nur dieser Link weggelassen.

Beispiel für Kopier-Befehle (z. B. Shell im Pod). Das Backend rendert sie je Objekt und liefert sie als
`copyCommands`; das Frontend zeigt je Befehl ein Icon (Tooltip mit Titel und Befehl), ein Klick kopiert den Befehl:

```yaml
copy-commands:
  - command-id: pod-shell             # eindeutig
    title: Shell im Pod öffnen
    icon: terminal
    icon-color: "#455a64"
    projects: [foo-projekt]           # Pflicht; [] = in keinem Projekt (Warnung beim Start)
    targets:                          # je Objekt-Typ ein Befehl (namespace | service | ingress | pod)
      pod:
        command: kubectl exec -it -n {{ .Namespace }} {{ .Name }} -c {{ .Container }} -- sh
```

- Dieselben Template-Daten und -Funktionen wie bei Links (inkl. `.Vars`); dazu `.Container` und `shellquote`
  (Wert als ein Shell-Argument in einfachen Anführungszeichen).
- **Je Container:** Bei Pods wird je Container gerendert. Unterscheiden sich die Ergebnisse (das Template nutzt
  `.Container`), entsteht je Container ein Befehl mit ID `<command-id>/<container>` und Titel
  `<title> (<container>)`; sonst genau einer.
- Der Befehl wird unverändert übernommen (nur Leerraum am Anfang/Ende entfernt); mehrzeilige Befehle bleiben
  mehrzeilig. NodePort-Services bekommen die `service`-Befehle.
- Templates werden beim Start geparst (Fehler → Startabbruch); lässt sich ein Befehl für ein Objekt nicht rendern,
  wird nur dieser weggelassen.

Beispiel für Entwickler-Teams (optional):

```yaml
teams:
  label-key: mycomp.de/team           # Label am Namespace; Wert = team-id
  teams:
    - team-id: checkout               # eindeutig, gültiger Label-Wert
      team-label: Team Checkout
      team-color: "#2e7d32"           # optional, CSS-Farbe; wird als teamColor durchgereicht
```

- Ein Namespace gehört zu einem Team, wenn sein Label `label-key` die `team-id` trägt. Die Aktion `assign-team`
  (Handler-Typ gleichen Namens, keine `extra-handler-configs`) bietet am Namespace ein Single-Select mit allen Teams
  und „Kein Team“ an und setzt bzw. entfernt das Label per Merge-Patch (RBAC: `patch` auf `namespaces`).
- Die API liefert die Teams im Snapshot (`teams`: `id`, `label`, `teamColor`, leer ohne `team-color`) und am Namespace die Team-ID (`team`, nur für konfigurierte Teams).
  Das Frontend zeigt je Projekt einen Team-Filter (Chips, Mehrfachauswahl, „Ohne Team“; Auswahl in der URL als
  `?team.<projekt>=<team-id>`) und das Team an der Kachel.
- Die Namespaces werden nicht per ArgoCD self-heal verwaltet; das Label bleibt daher bestehen.

Beispiel für den Handler-Typ `nightly-scale-down-exclusion` (Namespace vom nächtlichen Herunterskalieren ausnehmen;
eine externe Anwendung skaliert nachts alle Feature-Umgebungen auf 0 und lässt Namespaces mit dem Label aus):

```yaml
  - action-name: exclude-from-nightly-scale-down
    handler: nightly-scale-down-exclusion
    action-title: Nachts nicht herunterskalieren
    icon: sun
    icon-color: "#f9a825"
    projects: [foo-projekt]
    extra-handler-configs: &skip-scale-down
      label-key: mycomp.de/skip-nightly-scale-down   # Pflicht, gültiger Label-Key
      label-value: "true"                            # optional, Standard "true"
      mode: mark                                     # mark = setzen, unmark = entfernen
  - action-name: include-in-nightly-scale-down
    handler: nightly-scale-down-exclusion
    action-title: Nächtliches Herunterskalieren wieder zulassen
    icon: moon
    icon-color: "#455a64"
    projects: [foo-projekt]
    extra-handler-configs:
      <<: *skip-scale-down                           # gleiches Label wie oben
      mode: unmark
```

- Nur für das nächtliche Herunterskalieren gedacht. Ein Handler-Typ, zwei Aktionen mit demselben Label. `mark` wird nur angeboten, solange das Label mit dem
  konfigurierten Wert fehlt, `unmark` nur, solange es gesetzt ist (`IsApplicable`). Ein anderer Wert gilt als nicht
  gesetzt. Nach der Ausführung tauscht der Event-Hub die angebotene Aktion über das `upsert` des Namespaces aus.
- `label-value` ist optional (Standard `"true"`); Key und Wert werden beim Start geprüft. RBAC: `patch` auf
  `namespaces`.

Beispiel für Cluster-Angaben (Pflicht):

```yaml
cluster:
  dns-name: k8s-nodes.mycomp.de       # DNS-Name oder IP der Nodes, ohne Schema und Port
```

- `dns-name` gilt für alle NodePort-Services. Das Backend berechnet daraus je Port die Adresse
  `<dns-name>:<node-port>` und sendet sie als `addresses` am NodePort-Service. Fehlt `dns-name`, startet das Backend
  nicht.


### Aktionen

- Je aktivierter Aktion gibt es genau einen **ActionHandler**.
- Jeder ActionHandler registriert sich beim Startup in der **ActionRegistry**. Nur Aktionen, die in der `config.yaml`
  mindestens ein Projekt in `projects` haben, sind aktiv.
- Der ActionHandler entscheidet selbst:
    1. ob er für ein konkretes Objekt *applicable* ist,
    2. ob er zum Ausführen zusätzliche Eingaben vom Nutzer braucht, und wenn ja, welche Formular-Felder.
- Ein ActionHandler kann Veränderungen in Kubernetes vornehmen (Pods löschen, Service erstellen, ...). Die Aktion
  ändert keinen Zustand im Backend; die Veränderung kommt nur über die Informer-Events.
- Ein ActionHandler gibt ein Result zurück, welches zeigt, ob die Aktion erfolgreich ausgeführt wurde oder nicht.
  Die API antwortet bei Erfolg mit `200`, bei einem Fehlschlag des Handlers mit `422` und seiner Meldung als
  `{"error": "…"}` (nie `200`). Die Meldung ist für den Nutzer formuliert.

Eine neue Aktion hinzufügen heißt: neuen ActionHandler schreiben, in der Registry registrieren, Eintrag in der
`config.yaml` ergänzen. Am (separaten) Frontend ist dafür keine Änderung nötig.

### API zum Dashboard

Die API bietet neben management-endpunkte wie dem health-endpunkt folgende fachliche Endpunkte:

- `GET /api/events` (Server-Sent Events): zuerst ein `snapshot`-Event mit allen Objekten in der Hierarchie
  **Projekt → Namespace → Services (→ Pods) / NodePort-Services / Ingresses**, danach nur noch `upsert`- und `delete`-Events.
  Beispiel des Snapshots: `backend/internal/api/example-api.jsonc`.
- `GET /api/snapshot`: derselbe Snapshot einmalig als JSON (`application/json`), danach endet die Response; keine
  weiteren Events, kein Abo am Event-Hub (`Hub.Snapshot`).
- `PATCH /api/actions/{action-name}` zum Ausführen von Aktionen auf einem Objekt

- Objekte tragen nur, was das Dashboard braucht (`kind`, `name`, `parents`, Aktionen, Links und je Typ wenige
  Felder), keine Labels/Annotations. Anwendungs-Services (ClusterIP) stehen in `services`; ihr `running` ist
  gesetzt, wenn mindestens ein Replica ready ist, `areas` nennt die IDs ihrer Service-Bereiche. Die Bereiche selbst
  (`id`, `label`, Config-Reihenfolge, `others` zuletzt) stehen im Snapshot je Projekt in `serviceAreas`. Pods hängen unter jedem Service, der sie referenziert.
  NodePort-Services stehen gesondert in `nodePortServices` (`kind: node-port-service`) und tragen nur `addresses`
  (`<dns-name>:<node-port>`, im Backend berechnet); für Aktionen sind sie Objekte vom Typ `service`. Andere
  Service-Typen werden nicht gesendet. Ingresses liefern fertige `https://<host><path>`-URLs (`urls`).
- Jedes Objekt kennt seine Parents bis zum Projekt (`parents.project`, `.namespace`, `.service`), damit ein Event
  ohne weitere Daten einsortiert werden kann.
- Der Snapshot enthält die konfigurierten Teams (`teams`), jeder Namespace seine Team-ID (`team`, optional).
- Jedes Objekt trägt `links` und `copyCommands` (je Befehl `id`, `title`, `icon`, `iconColor`, `command`)
  sowie `pipelines` (laufende Pipelines: `action`, `title`, `url`, `startedAt`).
- Jedes Objekt trägt die Liste der Aktionen, die auf ihm ausführbar sind. Je Aktion:
    - Name (Titel)
    - Aktion-ID (eindeutiger Name)
    - Icon (Lucide-Name)
    - Icon-Farbe
    - optional eine Liste von Formular-Feldern
- Je Formular-Feld u. a.: Feld-Typ (`text`, `select`, `multi-select`, `number`), Feld-Label, Feld-ID, `isRequired`,
  `description`.
- Die API wird in einer openapi.yaml Datei dokumentiert und stets up-to-date gehalten, wenn sich API-Änderungen ergeben.
  Es kann auch eine swagger-GUI aufgerufen werden unter /api/swagger

#### Event-Mechanismus

- Der Event-Hub (`internal/api/events.go`) liest über die Schnittstelle `api.Source` (im Betrieb `kube.Cluster`).
  Er wartet auf `Changes()`, baut jeden Namespace mit geänderter Revision neu als DTO-Teilbaum auf und vergleicht ihn
  Objekt für Objekt (JSON-Bytes) mit dem zuletzt verschickten Stand. So werden auch abgeleitete Änderungen
  (`running`, URLs, Anwendbarkeit von Aktionen) erfasst.
- Einmal pro Stunde baut der Hub alle Namespaces neu auf (`fullResyncInterval`), als Sicherheitsnetz gegen verpasste
  Signale; Events entstehen dabei nur für echte Unterschiede.
- `upsert` enthält das Objekt ohne Kind-Listen und kommt Parent vor Kind; `delete` enthält `kind`, `name`,
  `parents`, kommt Kind vor Parent und entfällt für Kinder eines ebenfalls gelöschten Parents.
- Snapshot und Abo entstehen unter demselben Lock, dazwischen geht kein Event verloren. Ein Reconnect erhält immer
  einen neuen Snapshot (`Last-Event-ID` wird ignoriert). Zu langsame Clients werden getrennt und verbinden sich neu.
- Der Hub hält nur JSON-Bytes der zuletzt verschickten DTOs, keine Kopie der Kubernetes-Objekte.

## CronJob `nightly-scale-down`

Zweite Anwendung im selben Go-Modul und Image (`cmd/nightly-scale-down`, Binary `/nightly-scale-down`), deployt als
CronJob (`backend/deploy/nightly-scale-down.yaml`, Plan `plans/nightly-scale-down-cronjob.md`).

- Liest einmal alle Namespaces mit dem Ausnahme-Label (`SCALE_DOWN_EXCLUSION_LABEL_KEY`/`_VALUE`, muss zu
  `label-key`/`label-value` der Aktion `nightly-scale-down-exclusion` passen). Kein Informer, keine `config.yaml`,
  Projekt-Labels spielen keine Rolle.
- Startet die GitLab-Pipeline mit dem **Pipeline-Input** (`spec:inputs`, nicht Variable) `EXCLUDE_NAMESPACES`:
  komma-getrennt, sortiert, leer, wenn keiner ausgenommen ist. Dafür gibt es `gitlabpipeline.Trigger.Start`
  (fertige Werte, `variables` und `inputs`), das auch `Trigger.Run` nutzt.
- Konfiguration nur über Umgebungsvariablen (`GITLAB_URL`, `GITLAB_PROJECT`, `GITLAB_REF`, `GITLAB_TRIGGER_TOKEN`,
  optional `GITLAB_TIMEOUT`), Token aus dem Secret `nightly-scale-down-gitlab`. Proxy und CA wie beim Dashboard.
- Fehler führen zu Exit-Code 1. Kein Retry (`backoffLimit: 0`, `concurrencyPolicy: Forbid`).
- RBAC: eigener ServiceAccount, nur `list` auf `namespaces`.

## Frontend (nicht in diesem Repository)

Das Frontend wird getrennt entwickelt und nutzt nur die HTTP-API (`GET /api/events`, `GET /api/snapshot`,
`PATCH /api/actions/{id}`).
Für dieses Repository gilt daraus:

- **API maximal generisch halten:** Das Frontend kennt keine konkreten Aktionen. Buttons, Icons, Farben und Formulare
  werden vollständig aus den API-Daten aufgebaut. Aktionsspezifische Logik gehört ins Backend; fehlt dem Frontend eine
  Information, wird die API erweitert (`openapi.yaml`, `example-api.jsonc`).
- **Kein CORS:** Das Backend setzt keine CORS-Header. Das Frontend muss unter derselben Origin wie die API laufen
  (z. B. Ingress `/api` → Backend, lokal Dev-Proxy auf `localhost:8080`).
- `--static-dir` (`internal/api/static.go`) bleibt im Backend erhalten, wird im Image aber nicht mehr genutzt.

## Leitplanken für Änderungen

- Nur Namespace, Service, Ingress und Pod beobachten; nur Kern-Informationen übernehmen.
- Einzige Datenquelle ist der Informer-Cache (`kube.Cluster`); keinen eigenen Store und keine Kopie daneben aufbauen.
  Lister-Objekte nie verändern.
- Keine Persistenz: Eigener Zustand (laufende Pipelines in `internal/pipelines`) liegt nur im Speicher. Keinen
  Zustand an ArgoCD-verwalteten Objekten ablegen.
- Aktionslogik gehört ins Backend in genau einen ActionHandler, nie ins Frontend.
- Projekte, Labels und aktive Aktionen kommen aus der `config.yaml`, nicht aus dem Code.
- Ändert ein ActionHandler den Cluster, kommt die Änderung ausschließlich über die Informer-Events ins Dashboard.

## Getroffene Entscheidungen

- Backend: `k8s.io/client-go` v0.33.x mit Informern, HTTP mit stdlib `net/http`, YAML mit `gopkg.in/yaml.v3`.
- Build/Test: Makefile + `go test`, Dockerfile (distroless).
- Das Frontend liegt nicht mehr in diesem Repository (Plan `plans/frontend-entfernen.md`); das Image enthält nur das
  Backend.
- Aktualisierung über Server-Sent Events statt Polling/ETag; der Initialstand ist das erste Event desselben Streams.
- Service-Bereiche sind Reiter in der Namespace-Kachel (kein seitenweiter Filter). NodePort-Services kommen als eigenes Attribut `nodePortServices` mit fertigen Adressen;
  `cluster.dns-name` ist dafür Pflicht.
- Kein eigener Store und kein periodischer Neuaufbau mit frischen Informern (Plan
  `plans/informer-cache-statt-store.md`): Der Informer-Cache ist konsistent, der Event-Hub baut geänderte
  Namespaces neu auf und verschickt nur echte Unterschiede; stündlich gleicht er zusätzlich alles ab.

## Noch nicht festgelegt

- Fehlgeschlagene Pipelines: Geplant ist ein Modal im Dashboard. Bis dahin werden sie wie beendete Pipelines
  entfernt.

Neue offene Punkte hier eintragen und vor der Umsetzung nachfragen.
