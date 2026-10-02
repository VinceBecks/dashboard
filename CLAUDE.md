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
Backend und Frontend sind implementiert (Plan Backend: `plans/backend-implementierung.md`).
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
| `internal/api`                | HTTP-API, ETag/304, `openapi.yaml`, Swagger-UI                            |

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

Frontend-Stack: Vite, React 19, MUI, React Router, Lucide.
Frontend-Struktur: `src/api` (Typen gemäß `openapi.yaml`, Client mit ETag), `src/hooks/usePolling.ts`
(Polling, Intervall in `src/config.ts`), `src/components` (generische Aktions-Buttons/-Dialoge),
`src/pages` (Übersicht, Namespace-Seite).

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
- Der store merkt sich wann es eine letzte Änderung gab von Namespaces zu Projekten (hinzugefügt/verändert/entfernt) und
  merkt sich zusätzliche, wann das letzte Mal ein Objekt innerhalb eines Namespaces hinzugefügt/verändert/entfernt wurde

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
    enabled: true
    action-title: Pod Löschen
    lucide-icon: trash
    lucide-icon-color: red
    extra-handler-configs:
      force-delete: true
```

- `action-name` ist die eindeutige Aktion-ID.
- `action-title`, `lucide-icon` und `lucide-icon-color` werden unverändert an das Frontend durchgereicht.
- `extra-handler-configs` ist handler-spezifisch und wird nur vom jeweiligen Handler interpretiert.


Beispiel für store-konfiguration
```yaml
store:
  recreate-store-interval: 1h
```

- `recreate-store-interval` häufigkeit in der, der komplette store erneuert wird. Möglich ist auch eine '0' einzutragen, um das Verhalten zu disablen. Ansonsten sind möglich Minuten, Stunden oder Tage anzugeben

### Aktionen

- Je aktivierter Aktion gibt es genau einen **ActionHandler**.
- Jeder ActionHandler registriert sich beim Startup in der **ActionRegistry**. Nur Handler, die in der `config.yaml`
  `enabled: true` haben, sind aktiv.
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

- `GET /api/projects` liefert eine Liste der Projekte und deren Namespaces
- `GET /api/projects/{project-name}/namespaces/{namespace-name}` zum Laden der Objekte des Namespaces (inkl. dem
  namespace selber)
- `PATCH /api/actions/{action-name}` zum Ausführen von Aktionen auf einem Objekt

- Objekte werden gruppiert ausgegeben:
    - **Projekt → Namespace**
    - **Namespace → Services / Ingresses / Pods**.
- Jedes Objekt trägt die Liste der Aktionen, die auf ihm ausführbar sind. Je Aktion:
    - Name (Titel)
    - Aktion-ID (eindeutiger Name)
    - Lucide-Icon
    - Icon-Farbe
    - optional eine Liste von Formular-Feldern
- Je Formular-Feld u. a.: Feld-Typ (`text`, `select`, `multi-select`, `number`), Feld-Label, Feld-ID, `isRequired`,
  `description`.
- Abfragen werden mit `200` und aktuellen Daten beantwortet oder mit `304`, wenn das Frontend bereits auf dem aktuellen
  Stand ist.
- Die API wird in einer openapi.yaml Datei dokumentiert und stets up-to-date gehalten, wenn sich API-Änderungen ergeben.
  Es kann auch eine swagger-GUI aufgerufen werden unter /api/swagger

#### Mechanismus für 200er bzw. 304er Response-Code

Ob ein 200er response-code oder 304er response-code zurückgegeben wird bestimmt sich an einem ETag Header, welches das
Frontend mitsendet.
Wenn es sich um die Abfrage der Projekte und Namespaces handelt, dann wird der last-updated-timestamp verwendet, wann
das letzte mal ein Namespace zu einem Projekt hinzugefügt/verändert/entfernt wurde.

Wenn es sich um die Abfrage der Objekte eines Namespaces handelt, dann wird der last-update-timestamp verwendet, wann
das letzte mal ein Objekte in dem Namespace hinzugefügt/verändert/entfernt wurde, oder der Namespace selber
verändert/entfernt wurde.

## Frontend

### Verwendete Libs / Tools

- Lucide-Icons
- MUI (Material UI) als Component-Library

### Grundsatz: maximal generisch

Das Dashboard kennt keine konkreten Aktionen. Buttons, Icons, Farben und Formulare werden vollständig aus den Daten des
Backends aufgebaut. **Kein aktionsspezifischer Code im Frontend** (kein `if (action.id === "delete-pod")`). Fehlt dem
Frontend eine Information, wird die API erweitert.

### Seiten

**1. Übersichtsseite**
Zeigt die Projekte und die Namespaces (Feature-Environments), die zu ihnen existieren. Klick auf einen Namespace führt
zur Namespace-Seite.

**2. Namespace-Seite**, von oben nach unten:

1. Aktionen, die zum Namespace-Objekt gehören.
2. Übersicht der Ingress-Objekte mit ihren Pfaden und den Aktionen auf Ingress-Objekten.
3. Auflistung der existierenden Node-Port-Services
4. Übersicht der Anwendungs-Services (ClusterIP), je Service eine Kachel:
    - zuerst die Aktionen des Service-Objekts,
    - danach die Replicas (Pods), die der Service referenziert.

Aktionen zu Pods werden vorerst **nicht** angezeigt, auch wenn das Backend sie liefert.

### Datenaktualisierung

Das Frontend fragt das Backend regelmäßig ab, abhängig von der Seite, auf der sich der Nutzer befindet (nur die Daten,
die diese Seite braucht). Bei `304` bleibt der bestehende Zustand unverändert, bei `200` wird er ersetzt.

## Leitplanken für Änderungen

- Nur Namespace, Service, Ingress und Pod beobachten; nur Kern-Informationen speichern.
- Store-Inhalte bleiben `json.RawMessage`.
- Aktionslogik gehört ins Backend in genau einen ActionHandler, nie ins Frontend.
- Projekte, Labels und aktive Aktionen kommen aus der `config.yaml`, nicht aus dem Code.
- Ändert ein ActionHandler den Cluster, muss der Store konsistent mitgeführt werden.

## Getroffene Entscheidungen

- Backend: `k8s.io/client-go` v0.33.x mit Informern, HTTP mit stdlib `net/http`, YAML mit `gopkg.in/yaml.v3`.
- Build/Test: Makefile + `go test`, Dockerfile (distroless).
- Frontend: Vite, React Router; Auslieferung durch das Backend (`--static-dir`, SPA-Fallback auf `index.html`);
  Polling alle 10 Sekunden.
- Aktionen werden im Frontend immer über einen Dialog bestätigt; Formular-Felder erscheinen darin, falls vorhanden.
- Informer-Event zu einem Objekt, das bereits im Store liegt: immer überschreiben; Timestamps (und damit ETags)
  ändern sich nur, wenn sich das gespeicherte JSON tatsächlich ändert.
- Periodischer Neuaufbau: frische Informer, kompletter neuer Store-Inhalt, atomarer Austausch;
  Timestamps nur für Namespaces mit abweichendem Inhalt.

## Noch nicht festgelegt

Aktuell keine offenen Punkte. Neue offene Punkte hier eintragen und vor der Umsetzung nachfragen.
