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
- Frontend: React + TypeScript, Code liegt in `frontend/`
- Keine Authentifizierung im Dashboard

## Aktueller Stand
Backend und Frontend sind implementiert (Plan Backend: `plans/backend-implementierung.md`, Umstellung auf
Server-Sent Events und eine Seite: `plans/sse-einseiten-dashboard.md`).
Das Backend liefert im Cluster auch das gebaute Frontend aus (ein Image, `Dockerfile` im Repository-Root).

Backend-Pakete (`backend/`):

| Paket                         | Aufgabe                                                                   |
|-------------------------------|---------------------------------------------------------------------------|
| `cmd/dashboard`               | Startup, Wiring, Registrierung der ActionHandler                          |
| `internal/config`             | `config.yaml` laden/validieren                                            |
| `internal/model`              | Stored-Object-Structs                                                     |
| `internal/project`            | Namespace-Labels → Projekt                                                |
| `internal/store`              | In-Memory-Store (`json.RawMessage`), Transaktionen, Timestamps            |
| `internal/converter`          | k8s-Objekt → Stored-Object, Referenzauflösung inkl. abhängiger Objekte    |
| `internal/kube`               | Informer, Event-Verarbeitung (eine Goroutine), periodischer Full-Rebuild  |
| `internal/actions`            | ActionHandler-Interface, ActionRegistry, Validierung der Formular-Felder  |
| `internal/actions/deletepod`  | ActionHandler `delete-pod`                                                |
| `internal/actions/nodeportservice` | ActionHandler `node-port-service-on-service-base` und `delete-node-port-service`; erstellte Services tragen das Label `feature-dashboard/created-by-action` und eine ownerReference auf den Basis-Service (werden mit ihm gelöscht); das Dashboard zeigt keine NodePort-Services mehr an, in den Beispiel-Configs deaktiviert |
| `internal/actions/restartservice` | ActionHandler `restart-service-replicas`: löscht alle Pods eines Services (Neustart), nur Pods mit Controller |
| `internal/actions/gitlabpipeline` | Handler-Typ `gitlab-pipeline`: startet GitLab-Pipelines per Trigger-API, beliebig viele Aktionen; `Trigger` ist wiederverwendbar |
| `internal/actions/missingservices` | Handler-Typ `deploy-missing-services`: bietet am Namespace die fehlenden konfigurierten Services (Abgleich per Name) zur Mehrfachauswahl an und startet dafür eine GitLab-Pipeline |
| `internal/objecttemplate`     | Go-Templates mit Objekt-Daten (gemeinsam für `gitlab-pipeline` und Links)  |
| `internal/links`              | Rendert die konfigurierten Links je Objekt                                |
| `internal/api`                | HTTP-API, DTOs (`dto.go`), Event-Hub und SSE (`events.go`), `openapi.yaml`, `example-api.jsonc`, Swagger-UI |

## Build, Test, Start

Alle Befehle laufen über das `Makefile` im Repository-Root:

| Befehl              | Wirkung                                                                                   |
|---------------------|-------------------------------------------------------------------------------------------|
| `make test`         | Backend- und Frontend-Tests (`go test -race`, Vitest)                                     |
| `make lint`         | `go vet` und ESLint                                                                       |
| `make build`        | Backend-Binary nach `backend/bin/dashboard`, Frontend-Build nach `frontend/dist/`         |
| `make run`          | Frontend bauen, Backend lokal starten und das Frontend mit ausliefern (Port 8080)         |
| `make docker`       | Image mit Backend + Frontend bauen (`IMAGE=...` überschreibbar); Manifeste in `backend/deploy/` |
| `make backend-run`  | Nur das Backend lokal starten                                                             |
| `make frontend-dev` | Vite-Dev-Server; `/api` wird an das lokale Backend auf `localhost:8080` weitergeleitet    |

Einzelschritte gibt es als `backend-build|test|vet|run` und `frontend-install|dev|build|test|lint`.
Lokale Starts nutzen den Cluster aus `KUBECONFIG` (Standard `~/.kube/config`) und `backend/config.example.yaml`;
andere Config mit `make run CONFIG=pfad.yaml` (relativ zum Repository-Root). Für Frontend-Entwickler bleiben die
npm-Scripts in `frontend/package.json` direkt nutzbar; das Makefile ruft nur sie auf.

Frontend-Stack: Vite, React 19, MUI, Lucide.
Frontend-Struktur: `src/api` (Typen gemäß `openapi.yaml`, `state.ts` wendet Events auf den Zustand an, Client für
Aktionen), `src/hooks/useDashboardEvents.ts` (EventSource auf `GET /api/events`), `src/components`
(`NamespaceCard`, `ServiceDialog`, generische Aktions-Buttons/-Dialoge, `ConnectionStatus` oben rechts),
`src/pages/DashboardPage.tsx` (einzige Seite).

## Fachliche Begriffe

| Begriff            | Bedeutung                                                                                            |
|--------------------|------------------------------------------------------------------------------------------------------|
| Projekt            | In der `config.yaml` definiert. Hat mehrere Feature-Umgebungen.                                      |
| Feature-Umgebung   | Genau ein Namespace. Gehört zu genau einem Projekt.                                                  |
| Anwendungs-Service | Ein Kubernetes-Service vom Typ `ClusterIP`.                                                          |
| Replica            | Ein Pod, der von einem Anwendungs-Service referenziert wird.                                         |
| Stored-Object      | Die reduzierte, konvertierte Form eines Kubernetes-Objekts im In-Memory-Store.                       |
| Aktion             | Eine vom Dashboard auslösbare Operation auf einem Objekt, umgesetzt durch genau einen ActionHandler. |

Ob ein Namespace eine Feature-Umgebung ist und zu welchem Projekt er gehört, wird ausschließlich über Labels am
Namespace bestimmt. Welche Labels für welches Projekt gelten, steht in der `config.yaml`.

## Backend

### Allgemeines zur Entwicklung

### Kubernetes-Anbindung

- Es wird die Bibliothek `client-go` verwendet
- Das Backend nutzt die Informer-API von `client-go` (`SharedInformerFactory`, Event-Handler, Lister), um stets aktuell zu sein.
- In einem definierbaren Interval wird der komplette store geleert und neu befüllt, um regelmäßig einen sauberen Stand zu haben, der nicht durch Fehler bei der Erfassung vorgekommen ist. Der Interval ist konfigurierbar über die `config.yaml`  
- Relevant sind nur vier Objekt-Typen: **Namespace, Service, Ingress, Pod**. Keine weiteren Typen beobachten.
- Von diesen Objekten werden nur wenige Kern-Informationen übernommen, nicht das vollständige Objekt.
- Es werden nur Objekte verarbeitet, die zu einem Projekt gehören (erkennbar an den Labels ihres Namespaces). Alles
  andere wird verworfen.



### Converter

Bei jedem Create/Update/Delete eines relevanten Objekts wird es in die Stored-Object-Struktur konvertiert und im Store
abgelegt bzw. daraus entfernt.

Beim Konvertieren werden Referenzen aufgelöst. Beispiel: Referenziert ein Service den Container-Port per Name
(`targetPort: http`), wird die zugehörige Port-Nummer ermittelt und zusätzlich am Stored-Service gespeichert.

Daraus folgt: Eine Änderung an einem Objekt kann ein anderes Stored-Object betreffen (z. B. ändert ein Pod-Update die
aufgelöste Port-Nummer eines Service). Das beim Implementieren von Converter-Logik berücksichtigen.

### In-Memory-Store

- Alle Stored-Objects liegen ausschließlich in-memory. Keine Datenbank, keine Persistenz.
- Gespeichert wird als `json.RawMessage`. **Das ist Absicht:** Durch Serialisierung beim Schreiben und Deserialisierung
  beim Lesen entstehen immer Kopien. Niemand hält einen Pointer auf den Zustand im Store.
- Deshalb: keine Pointer auf Structs im Store ablegen und keine Caches mit deserialisierten Objekten daneben aufbauen,
  die diese Garantie unterlaufen.
- Der Store wird nebenläufig gelesen (HTTP) und geschrieben (Informer-Events) und muss entsprechend
  abgesichert sein.
- Der Store merkt sich je Namespace, wann zuletzt der Namespace selbst oder ein Objekt darin hinzugefügt/verändert/
  entfernt wurde (`NamespaceRevisions`), und signalisiert Änderungen über `Changes()`. Darauf baut der Event-Hub auf.

#### Gespeicherte Informationen je Objekt

Je Objekt werden folgende Informationen gespeichert:

- Name
- Labels
- Annotations

Zusätzliche Informationen für Pods:

- Status
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
        - backend (inkl. aufgelöster Service-Port-Number)

### Konfiguration (`config.yaml`)

Wird beim Startup einmal eingelesen. Enthält:

1. Die Projekte und die für sie relevanten Namespace-Labels.
2. Die ActionHandler und ob sie aktiviert sind.
3. Store konfiguration
4. Links (optional)

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

Beispiel für ActionHandler:

```yaml
actionHandlers:
  - action-name: delete-pod
    action-title: Pod Löschen
    lucide-icon: trash
    lucide-icon-color: red
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
- `action-title`, `lucide-icon` und `lucide-icon-color` werden unverändert an das Frontend durchgereicht.
- `extra-handler-configs` ist handler-spezifisch und wird nur vom jeweiligen Handler interpretiert.

Beispiel für den generischen Handler-Typ `gitlab-pipeline` (startet eine Pipeline über die Pipeline-Trigger-API):

```yaml
  - action-name: deploy-feature
    handler: gitlab-pipeline
    action-title: Feature deployen
    lucide-icon: rocket
    lucide-icon-color: green
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
    lucide-icon: package-plus
    lucide-icon-color: green
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
    lucide-icon: scroll-text
    lucide-icon-color: "#00bfb3"
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
  `.Services` (nur bei `deploy-missing-services` gefüllt).
- Template-Funktionen zusätzlich zu Go-Standard (`urlquery`, `index`, `printf`, `range`, …): `rison`, `pathescape`,
  `replace`, `join`.
- Gemeinsame URL-Teile mehrerer Targets (z. B. die Kibana-Discover-Basis) in `link-variables` auslagern.
- Templates werden beim Start geparst (Fehler → Startabbruch). Lässt sich ein Link für ein Objekt nicht rendern
  (z. B. fehlende Variable) oder ist das Ergebnis keine absolute http(s)-URL, wird nur dieser Link weggelassen.

Beispiel für store-konfiguration
```yaml
store:
  recreate-store-interval: 1h
```

- `recreate-store-interval` häufigkeit in der, der komplette store erneuert wird. Möglich ist auch eine '0' einzutragen, um das Verhalten zu disablen. Ansonsten sind möglich Minuten, Stunden oder Tage anzugeben

### Aktionen

- Je aktivierter Aktion gibt es genau einen **ActionHandler**.
- Jeder ActionHandler registriert sich beim Startup in der **ActionRegistry**. Nur Aktionen, die in der `config.yaml`
  mindestens ein Projekt in `projects` haben, sind aktiv.
- Der ActionHandler entscheidet selbst:
    1. ob er für ein konkretes Objekt *applicable* ist,
    2. ob er zum Ausführen zusätzliche Eingaben vom Nutzer braucht, und wenn ja, welche Formular-Felder.
- Ein ActionHandler kann Veränderungen in Kubernetes vornehmen (Pods löschen, Service erstellen, ...). Diese werden aber
  nicht von der Aktion im Store verändert. Die Veränderung kommt nur über die Informer-Events.
- Ein ActionHandler gibt ein Result zurück, welches zeigt, ob die Aktion erfolgreich ausgeführt wurde oder nicht

Eine neue Aktion hinzufügen heißt: neuen ActionHandler schreiben, in der Registry registrieren, Eintrag in der
`config.yaml` ergänzen. Am Frontend ist dafür keine Änderung nötig.

### API zum Dashboard

Die API bietet neben management-endpunkte wie dem health-endpunkt folgende fachliche Endpunkte:

- `GET /api/events` (Server-Sent Events): zuerst ein `snapshot`-Event mit allen Objekten in der Hierarchie
  **Projekt → Namespace → Services (→ Pods) / Ingresses**, danach nur noch `upsert`- und `delete`-Events.
  Beispiel des Snapshots: `backend/internal/api/example-api.jsonc`.
- `PATCH /api/actions/{action-name}` zum Ausführen von Aktionen auf einem Objekt

- Objekte tragen nur, was das Dashboard braucht (`kind`, `name`, `parents`, Aktionen, Links und je Typ wenige
  Felder), keine Labels/Annotations. Gesendet werden nur Anwendungs-Services (ClusterIP); ihr `running` ist gesetzt,
  wenn mindestens ein Replica ready ist. Pods hängen unter jedem Service, der sie referenziert. Ingresses liefern
  fertige `https://<host><path>`-URLs (`urls`).
- Jedes Objekt kennt seine Parents bis zum Projekt (`parents.project`, `.namespace`, `.service`), damit ein Event
  ohne weitere Daten einsortiert werden kann.
- Jedes Objekt trägt die Liste der Aktionen, die auf ihm ausführbar sind. Je Aktion:
    - Name (Titel)
    - Aktion-ID (eindeutiger Name)
    - Lucide-Icon
    - Icon-Farbe
    - optional eine Liste von Formular-Feldern
- Je Formular-Feld u. a.: Feld-Typ (`text`, `select`, `multi-select`, `number`), Feld-Label, Feld-ID, `isRequired`,
  `description`.
- Die API wird in einer openapi.yaml Datei dokumentiert und stets up-to-date gehalten, wenn sich API-Änderungen ergeben.
  Es kann auch eine swagger-GUI aufgerufen werden unter /api/swagger

#### Event-Mechanismus

- Der Event-Hub (`internal/api/events.go`) wartet auf `store.Changes()`, baut jeden Namespace mit geändertem
  Zeitstempel neu als DTO-Teilbaum auf und vergleicht ihn Objekt für Objekt (JSON-Bytes) mit dem zuletzt
  verschickten Stand. So werden auch abgeleitete Änderungen (`running`, URLs, Anwendbarkeit von Aktionen) und der
  periodische Neuaufbau erfasst.
- `upsert` enthält das Objekt ohne Kind-Listen und kommt Parent vor Kind; `delete` enthält `kind`, `name`,
  `parents`, kommt Kind vor Parent und entfällt für Kinder eines ebenfalls gelöschten Parents.
- Snapshot und Abo entstehen unter demselben Lock, dazwischen geht kein Event verloren. Ein Reconnect erhält immer
  einen neuen Snapshot (`Last-Event-ID` wird ignoriert). Zu langsame Clients werden getrennt und verbinden sich neu.
- Der Hub hält nur JSON-Bytes, keine deserialisierten Objekte (Store-Garantie).

## Frontend

### Verwendete Libs / Tools

- Lucide-Icons
- MUI (Material UI) als Component-Library

### Grundsatz: maximal generisch

Das Dashboard kennt keine konkreten Aktionen. Buttons, Icons, Farben und Formulare werden vollständig aus den Daten des
Backends aufgebaut. **Kein aktionsspezifischer Code im Frontend** (kein `if (action.id === "delete-pod")`). Fehlt dem
Frontend eine Information, wird die API erweitert.

### Seite

Das Dashboard besteht aus genau einer Seite (`DashboardPage`):

- Je Projekt (Reihenfolge der `config.yaml`) ein Abschnitt mit einer Kachel je Namespace (`NamespaceCard`).
- Kachel: Namespace-Name, Aktionen und Links des Namespaces, die URLs der Ingresses (mit deren Aktionen/Links) und je
  Anwendungs-Service ein Punkt – grün, wenn mindestens ein Replica läuft, sonst rot.
- Klick auf einen Punkt öffnet den `ServiceDialog`: Aktionen und Links des Services, darunter je Replica Status,
  Container (Name, Image), Aktionen und Links. Verschwindet der Service zwischenzeitlich, zeigt der Dialog einen
  Hinweis.

### Datenaktualisierung

Beim Aufruf öffnet das Frontend `GET /api/events` (EventSource). Das erste Event (`snapshot`) liefert den kompletten
Stand, danach werden `upsert`/`delete` angewendet (`src/api/state.ts`). Nach einem Verbindungsabbruch verbindet sich
der Browser selbst neu und erhält einen frischen Snapshot. Den Verbindungszustand zeigt `ConnectionStatus` oben
rechts. Nach einer Aktion wird nichts neu geladen; die Änderung kommt über die Events.

## Leitplanken für Änderungen

- Nur Namespace, Service, Ingress und Pod beobachten; nur Kern-Informationen speichern.
- Store-Inhalte bleiben `json.RawMessage`.
- Aktionslogik gehört ins Backend in genau einen ActionHandler, nie ins Frontend.
- Projekte, Labels und aktive Aktionen kommen aus der `config.yaml`, nicht aus dem Code.
- Ändert ein ActionHandler den Cluster, muss der Store konsistent mitgeführt werden.

## Getroffene Entscheidungen

- Backend: `k8s.io/client-go` v0.33.x mit Informern, HTTP mit stdlib `net/http`, YAML mit `gopkg.in/yaml.v3`.
- Build/Test: Makefile + `go test`, Dockerfile (distroless).
- Frontend: Vite, eine Seite ohne Router; Auslieferung durch das Backend (`--static-dir`, SPA-Fallback auf
  `index.html`).
- Aktualisierung über Server-Sent Events statt Polling/ETag; der Initialstand ist das erste Event desselben Streams.
- Keine Service-Bereiche, keine Anzeige von NodePort-Services (und damit kein `cluster.dns-name`).
- Aktionen werden im Frontend immer über einen Dialog bestätigt; Formular-Felder erscheinen darin, falls vorhanden.
- Informer-Event zu einem Objekt, das bereits im Store liegt: immer überschreiben; Timestamps (und damit Events)
  ändern sich nur, wenn sich das gespeicherte JSON tatsächlich ändert.
- Periodischer Neuaufbau: frische Informer, kompletter neuer Store-Inhalt, atomarer Austausch;
  Timestamps nur für Namespaces mit abweichendem Inhalt.

## Noch nicht festgelegt

Aktuell keine offenen Punkte. Neue offene Punkte hier eintragen und vor der Umsetzung nachfragen.
