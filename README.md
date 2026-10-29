# Feature-Environment-Dashboard

Ein Dashboard, in dem Software-Entwickler sehen, welche **Feature-Umgebungen** (Namespaces) sie in einem
Kubernetes-Cluster hochgefahren haben und welche **Services, Ingresses und Pods** darin laufen. Über das Dashboard
lassen sich außerdem **Aktionen** auf diesen Objekten auslösen, z. B. einen Pod löschen, die Replicas eines
Services neu starten oder eine GitLab-Pipeline starten. Konfigurierbare **Links** springen von Objekten in andere Systeme, etwa
zu den Logs in Kibana.

Dieses Repository enthält nur das **Backend** (Go, `client-go` v0.33 mit Informern, `net/http`, Ordner `backend/`).
Das Frontend wird getrennt entwickelt und nutzt die hier beschriebene HTTP-API (`backend/internal/api/openapi.yaml`).

Das Dashboard ist eine einzige Seite: je Projekt eine Kachel pro Feature-Umgebung, darin je Anwendungs-Service ein
grüner (mindestens ein Replica läuft) oder roter Punkt. Ein Klick auf einen Punkt zeigt die Details des Services
mit seinen Replicas. Das Frontend lädt beim Aufruf den kompletten Stand und wird danach per **Server-Sent Events**
aktuell gehalten.

Das Backend läuft im Cluster als ein Image in genau einem Deployment mit **einem** Replica.

**Hinweis für das Frontend:** Das Backend setzt keine CORS-Header. Das Frontend muss daher unter derselben Origin wie
die API erreichbar sein, z. B. per Ingress (`/api` → Backend, `/` → Frontend) oder lokal über einen Dev-Proxy, der
`/api` an `localhost:8080` weiterleitet.

---

## Inhalt

1. [Schnellstart](#schnellstart)
2. [Fachliche Begriffe](#fachliche-begriffe)
3. [Architektur des Backends](#architektur-des-backends)
4. [Kommandozeile des Backends](#kommandozeile-des-backends)
5. [Konfiguration (`config.yaml`)](#konfiguration-configyaml)
    - [Übersicht](#übersicht)
    - [`projects`](#projects)
    - [`action-handlers`](#action-handlers)
    - [Mitgelieferte Aktionen](#mitgelieferte-aktionen)
    - [`links` und `link-variables`](#links-und-link-variables)
    - [`copy-commands`](#copy-commands)
    - [Templates in der Konfiguration](#templates-in-der-konfiguration)
    - [`teams`](#teams)
    - [`cluster`](#cluster)
    - [`pipelines`](#pipelines)
    - [Prüfungen beim Start](#prüfungen-beim-start)
    - [Vollständiges Beispiel](#vollständiges-beispiel)
6. [HTTP-API](#http-api)
7. [Eine eigene Aktion entwickeln](#eine-eigene-aktion-entwickeln)
8. [Deployment im Cluster](#deployment-im-cluster)
9. [Nächtliches Herunterskalieren (CronJob)](#nächtliches-herunterskalieren-cronjob)
10. [Entwicklung](#entwicklung)
11. [Fehlersuche](#fehlersuche)

---

## Schnellstart

Voraussetzungen: Go 1.26, ein erreichbarer Kubernetes-Cluster (z. B. kind oder minikube) und eine
kubeconfig.

```sh
# Backend lokal starten (nutzt KUBECONFIG bzw. ~/.kube/config)
make run

# danach z. B. den Event-Stream ansehen oder die Swagger-UI öffnen
curl -N http://localhost:8080/api/events
# http://localhost:8080/api/swagger
```

`make run` verwendet `backend/config.example.yaml`. Eine eigene Konfiguration gibst du so an:

```sh
make run CONFIG=pfad/zur/config.yaml          # Pfad relativ zum Repository-Root
```

Damit das Dashboard etwas anzeigt, braucht ein Namespace die Labels eines konfigurierten Projekts. Für
`foo-projekt` aus der Beispiel-Config:

```sh
kubectl create namespace feat-login
kubectl label namespace feat-login \
  mycomp.de/is-feature-environment=true \
  mycomp.de/project-name=foo-projekt \
  mycomp.de/organization-unit=development
```

Ein Helm-Chart mit einer Demo-Anwendung liegt unter `helpers/demo-app`.

---

## Fachliche Begriffe

| Begriff            | Bedeutung                                                                                      |
|--------------------|------------------------------------------------------------------------------------------------|
| Projekt            | In der `config.yaml` definiert. Hat mehrere Feature-Umgebungen.                                |
| Feature-Umgebung   | Genau ein Namespace, der über seine Labels genau einem Projekt zugeordnet ist.                 |
| Anwendungs-Service | Ein Kubernetes-Service vom Typ `ClusterIP`.                                                    |
| Service-Bereich    | Je Projekt konfigurierte Gruppe von Anwendungs-Services; im Dashboard ein Reiter der Kachel.   |
| Replica            | Ein Pod, der von einem Service über dessen Selector referenziert wird.                         |
| Stored-Object      | Die reduzierte, konvertierte Form eines Kubernetes-Objekts, beim Lesen aus dem Informer-Cache erzeugt. |
| Aktion             | Eine vom Dashboard auslösbare Operation auf einem Objekt, umgesetzt durch einen ActionHandler. |
| Link               | Ein konfigurierter Absprung von einem Objekt in ein anderes System (z. B. Kibana).             |

---

## Architektur des Backends

```mermaid
flowchart LR
    K8s[(Kubernetes API)] -- List + Watch --> Inf[Informer-Cache<br/>Namespace, Service,<br/>Ingress, Pod]
    Inf -- Events: Revision je Namespace --> Hub[Event-Hub<br/>Diff je Namespace]
    Inf -- Lister + Converter --> Hub
    Pip[Laufende Pipelines<br/>im Speicher] -- Changes --> Hub
    Hub -- Server-Sent Events --> FE[Frontend]
    FE -- PATCH /api/actions --> API[HTTP-API]
    API --> Reg[ActionRegistry] --> H[ActionHandler] -- z . B . Pod löschen --> K8s
    H -- Pipeline starten --> GitLab[(GitLab)]
```

### Anbindung an Kubernetes

- Das Backend nutzt die **Informer-API** von `client-go` (`SharedInformerFactory`). Es werden genau vier
  Objekt-Typen beobachtet: **Namespace, Service, Ingress (`networking.k8s.io/v1`) und Pod**, clusterweit.
- **Der Informer-Cache ist die einzige Datenquelle** (`kube.Cluster`). Es gibt keinen eigenen Store und keine Kopie
  der Objekte. Der Cache ist durch List/Watch mit `resourceVersion` und Relist nach Watch-Abbruch konsistent.
- **Schlanker Cache:** Ein Transform reduziert die Objekte beim Einlesen auf die genutzten Felder. Bei Pods bleiben
  Container (Name, Image, Ports) und die Ready-Condition; managedFields entfallen bei allen Typen.
- **Events markieren nur:** Ein Event merkt sich lediglich, dass sich ein Projekt-Namespace geändert hat (Revision je
  Namespace). Objekte in anderen Namespaces kosten nur einen Map-Lookup.
- **Nur Feature-Umgebungen:** Ein Namespace gehört zu einem Projekt, wenn er *alle* in `namespace-labels` genannten
  Labels mit genau diesen Werten trägt. Objekte in anderen Namespaces werden nicht ausgeliefert.
    - Bekommt ein Namespace nachträglich die passenden Labels, liefert das Backend sofort alle seine Objekte aus.
    - Verliert er sie, verschwindet er samt Inhalt aus dem Dashboard.

### Converter und Referenzauflösung

Beim Aufbau eines Namespaces konvertiert der Converter die Objekte aus dem Cache. Übernommen werden nur
Kern-Informationen:

| Objekt    | Übernommen                                                                                             |
|-----------|--------------------------------------------------------------------------------------------------------|
| alle      | Name, Namespace, Labels, Annotations                                                                   |
| Namespace | zusätzlich das zugeordnete Projekt                                                                     |
| Pod       | Ready-Zustand, Container (Name, Image, Ports)                                                          |
| Service   | Typ, Selector, Ports (Name, Protokoll, Port, Target-Port, aufgelöster Target-Port, NodePort), Replicas |
| Ingress   | je Regel: Host und je Pfad: Pfad-Typ, Pfad, Backend-Service                                            |

Dabei werden **Referenzen aufgelöst**: Die *Replicas* eines Services sind die Pods im Namespace, deren Labels den
Selector erfüllen. Ein benannter `targetPort` (z. B. `http`) wird über die Container-Ports dieser Pods zur
Port-Nummer aufgelöst. Weil bei jedem Aufbau alles neu berechnet wird, gibt es keine inkrementell gepflegten,
abhängigen Objekte.

### Event-Hub

- Der Hub baut jeden Namespace, dessen Revision (im Cluster oder bei den laufenden Pipelines) sich geändert hat, neu
  auf und vergleicht das Ergebnis Objekt für Objekt mit dem zuletzt verschickten Stand. Verschickt werden nur echte
  Unterschiede.
- **Sicherheitsnetz:** Einmal pro Stunde baut er alle Namespaces neu auf. Auch dabei entstehen Events nur für
  tatsächliche Unterschiede.
- **Nebenläufigkeit:** Lister-Objekte zeigen in den Cache und werden nie verändert; nur `kube` und `converter` greifen
  auf sie zu, der Converter klont alle Maps.

### Aktionen

- **Eine Instanz je Aktion:** Jede Aktion in `action-handlers` wird beim Start einem **Handler-Typ** zugeordnet. Die
  **ActionRegistry** erzeugt daraus genau eine Handler-Instanz.
- **Sichtbarkeit:** Ob eine Aktion an einem Objekt angeboten wird, entscheiden zwei Stellen. Zuerst prüft die
  Registry das Projekt (`projects`), dann entscheidet der Handler selbst (`IsApplicable`). Benötigt der Handler
  Eingaben, beschreibt er sie als Formular-Felder, und das Frontend baut daraus generisch ein Formular.
- **Kein eigener Zustand:** Ein Handler verändert nur den Cluster oder ein externes System. Die Änderung kommt
  ausschließlich über die Informer-Events ins Dashboard.

### Links

Links werden aus Go-Templates gerendert. Die Template-Daten stammen aus dem Objekt, seinem Projekt und statischen
Variablen. Das Backend liefert fertige URLs aus, das Frontend zeigt sie nur noch an. Details stehen unter
[`links` und `link-variables`](#links-und-link-variables).

### Paketstruktur

| Paket                              | Aufgabe                                                                    |
|------------------------------------|----------------------------------------------------------------------------|
| `cmd/dashboard`                    | Start, Flags, Verdrahtung, Registrierung der Handler-Typen                 |
| `internal/config`                  | `config.yaml` laden und prüfen                                             |
| `internal/model`                   | Stored-Object-Strukturen, `NamespaceView`                                  |
| `internal/project`                 | Namespace-Labels → Projekt                                                 |
| `internal/converter`               | k8s-Objekt → Stored-Object, Referenzauflösung beim Aufbau                  |
| `internal/kube`                    | Kubernetes-Client, Informer-Cache als Datenquelle, Revisionen je Namespace |
| `internal/testsource`              | In-Memory-Datenquelle für Tests                                            |
| `internal/actions`                 | Handler-Interface, ActionRegistry, Prüfung der Formular-Eingaben           |
| `internal/actions/deletepod`       | Aktion `delete-pod`                                                        |
| `internal/actions/nodeportservice` | Aktionen `node-port-service-on-service-base`, `delete-node-port-service`   |
| `internal/actions/restartservice`  | Aktion `restart-service-replicas`                                          |
| `internal/actions/gitlabpipeline`  | Handler-Typ `gitlab-pipeline`                                              |
| `internal/actions/assignteam`      | Aktion `assign-team`                                                       |
| `internal/actions/missingservices` | Handler-Typ `deploy-missing-services`                                      |
| `internal/actions/nightlyscaledown` | Handler-Typ `nightly-scale-down-exclusion`                               |
| `internal/objecttemplate`          | Go-Templates mit Objekt-Daten (für Links und `gitlab-pipeline`)            |
| `internal/links`                   | Rendert die konfigurierten Links je Objekt                                 |
| `internal/copycommands`            | Rendert die konfigurierten Kopier-Befehle je Objekt (bei Pods je Container) |
| `internal/pipelines`               | Laufende GitLab-Pipelines im Speicher, Status-Abfrage                      |
| `internal/api`                     | HTTP-API, Event-Hub und Server-Sent Events, `openapi.yaml`, Swagger-UI, optionale Auslieferung statischer Dateien |

---

## Kommandozeile des Backends

```sh
dashboard --config config.yaml --listen :8080 [--kubeconfig PFAD] [--static-dir PFAD]
```

| Flag           | Standard      | Bedeutung                                                                                          |
|----------------|---------------|----------------------------------------------------------------------------------------------------|
| `--config`     | `config.yaml` | Pfad zur Konfiguration                                                                             |
| `--listen`     | `:8080`       | HTTP-Adresse                                                                                       |
| `--kubeconfig` | leer          | Leer: In-Cluster-Konfiguration, ersatzweise `KUBECONFIG` bzw. `~/.kube/config`. Sonst dieser Pfad. |
| `--static-dir` | leer          | Optional: Verzeichnis mit statischen Dateien (SPA-Fallback). Leer: nichts. Im Image nicht genutzt. |

Ablauf beim Start:

1. Die Konfiguration wird gelesen und vollständig geprüft. Bei einem Fehler bricht der Start ab, siehe
   [Prüfungen beim Start](#prüfungen-beim-start).
2. Die Handler aller aktiven Aktionen werden erzeugt und konfiguriert, die Link-Templates werden geparst.
3. Der HTTP-Server startet sofort. `/readyz` meldet `200`, sobald der Informer-Cache synchronisiert ist. Ist der
   Cluster nicht erreichbar, versuchen die Informer es selbstständig weiter.

Bei `SIGINT`/`SIGTERM` fährt das Backend geordnet herunter.

---

## Konfiguration (`config.yaml`)

Die Konfiguration wird **einmal beim Start** gelesen. Änderungen werden erst nach einem Neustart wirksam.

### Übersicht

| Schlüssel        | Pflicht | Inhalt                                                               |
|------------------|---------|----------------------------------------------------------------------|
| `projects`       | ja      | Projekte, ihre Namespace-Labels, Service-Bereiche, Link-Variablen    |
| `action-handlers` | nein    | Aktionen, die das Dashboard anbietet                                 |
| `links`          | nein    | Links von Objekten in andere Systeme                                 |
| `link-variables` | nein    | Statische Variablen für Link-Templates und Kopier-Befehle            |
| `copy-commands`  | nein    | Befehle (z. B. `kubectl exec`), die das Dashboard zum Kopieren anbietet |
| `cluster`        | ja      | Angaben zum Cluster: der DNS-Name der Nodes für NodePort-Adressen    |
| `teams`          | nein    | Entwickler-Teams und das Namespace-Label, das die Zuordnung trägt    |
| `pipelines`      | nein    | Ablage und Status-Abfrage laufender GitLab-Pipelines                 |

> **YAML-Hinweise**
> - Unquotierte Werte wie `true` oder `8080` werden bei Labels und Variablen als Text gelesen. `true` passt also
>   zum Label-Wert `"true"`.
> - Werte, die mit `{{` beginnen, müssen in Anführungszeichen stehen. Sonst liest YAML `{` als Beginn eines
>   Mappings.
> - Wiederkehrende Blöcke lassen sich mit YAML-Ankern (`&name` / `*name`) wiederverwenden.

### `projects`

```yaml
projects:
  - project-name: foo-projekt                      # eindeutiger Name des Projekts
    namespace-labels: # ALLE Labels müssen am Namespace gesetzt sein
      mycomp.de/is-feature-environment: true
      mycomp.de/project-name: foo-projekt
    others-area-label: Andere                      # optional
    service-areas: # optional
      - area-name: business-services
        area-label: Business
        label-combinations:
          - app.kubernetes.io/part-of: shop
            app.kubernetes.io/component: backend
          - app.kubernetes.io/part-of: billing
    link-variables-overrides: # optional
      kibanaDataView: logs-foo
```

| Feld                       | Pflicht | Bedeutung                                                                                                                   |
|----------------------------|---------|-----------------------------------------------------------------------------------------------------------------------------|
| `project-name`             | ja      | Eindeutiger Name. Er wird in `projects` von Aktionen und Links referenziert.                                                |
| `namespace-labels`         | ja      | Labels samt Werten, die ein Namespace **alle** tragen muss, um zum Projekt zu gehören. Darf nicht leer sein.                |
| `service-areas`            | nein    | Teilt die Anwendungs-Services einer Namespace-Kachel in Reiter auf.                                                         |
| `others-area-label`        | nein    | Reiter-Titel für Services ohne Bereich. Standard ist „Andere“.                                                              |
| `link-variables-overrides` | nein    | Überschreibt oder ergänzt die globalen `link-variables` für dieses Projekt.                                                 |

**Zuordnung von Namespaces:** Passt ein Namespace zu mehreren Projekten, gewinnt das erste in der Reihenfolge der
Config. Das Backend loggt dazu eine Warnung.

#### Service-Bereiche (`service-areas`)

Service-Bereiche teilen die **Anwendungs-Services (Typ `ClusterIP`)** einer Namespace-Kachel in **Reiter** auf.
Ingresses und NodePort-Services stehen außerhalb der Reiter.

| Feld                 | Pflicht | Bedeutung                                                        |
|----------------------|---------|------------------------------------------------------------------|
| `area-name`          | ja      | ID des Bereichs, eindeutig je Projekt. `others` ist reserviert.  |
| `area-label`         | ja      | Titel des Reiters                                                |
| `label-combinations` | ja      | Liste von Label-Kombinationen. Keine Kombination darf leer sein. |

Ein Service gehört zu einem Bereich, wenn er **alle Labels mindestens einer Kombination** trägt: ODER über die
Einträge, UND innerhalb eines Eintrags.

- **Mehrere Bereiche:** Ein Service kann in mehreren Bereichen und damit in mehreren Reitern stehen.
- **Rest:** Services ohne passenden Bereich stehen im Reiter `others-area-label` („Andere“), immer zuletzt.
- **Nur gefüllte Reiter:** Eine Kachel zeigt nur Bereiche, die in diesem Namespace Services haben. Bleibt nur einer
  übrig, oder hat das Projekt keine Bereiche, erscheinen die Punkte ohne Reiterleiste.
- **Wiederverwendung:** Gleiche Bereiche in mehreren Projekten lassen sich mit YAML-Ankern (`&name` / `*name`)
  teilen.

### `action-handlers`

```yaml
action-handlers:
  - action-name: delete-pod                 # eindeutige Aktion-ID
    handler: delete-pod                     # optional; Standard: action-name
    action-title: Pod löschen               # Tooltip im Dashboard
    icon: trash                             # Name eines Lucide-Icons (https://lucide.dev/icons)
    icon-color: red                         # beliebige CSS-Farbe
    projects: [ foo-projekt, bar-projekt ]    # Pflicht; [] = deaktiviert
    extra-handler-configs: # handlerspezifisch
      force-delete: true
```

| Feld                    | Pflicht | Bedeutung                                                                                                                                                                            |
|-------------------------|---------|--------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------|
| `action-name`           | ja      | Eindeutige ID der Aktion. Sie steht im Pfad von `PATCH /api/actions/{action-name}`.                                                                                                  |
| `handler`               | nein    | Handler-Typ. Fehlt er, gilt `action-name` als Handler-Typ. Ein Handler-Typ wie `gitlab-pipeline` kann so mehrere Aktionen bedienen.                                                  |
| `action-title`          | nein    | Titel, im Dashboard als Tooltip am Icon                                                                                                                                              |
| `icon`                  | nein    | Icon-Name. Unbekannte Namen zeigen ein Platzhalter-Icon.                                                                                                                             |
| `icon-color`            | nein    | Icon-Farbe als CSS-Wert, z. B. `red` oder `#00bfb3`                                                                                                                                  |
| `projects`              | ja      | Projekte, in denen die Aktion angeboten und ausführbar ist. **`[]` deaktiviert die Aktion**: Ihr Handler wird dann nicht erzeugt. Es wird nur geprüft, ob der Handler-Typ existiert. |
| `extra-handler-configs` | nein    | Handlerspezifische Einstellungen, siehe unten                                                                                                                                        |

Das Dashboard zeigt Aktionen als Icon-Buttons. Vor dem Ausführen öffnet sich immer ein Bestätigungsdialog. Hat die
Aktion Formular-Felder, stehen sie in diesem Dialog. Das Ergebnis erscheint als Meldung.

### Mitgelieferte Aktionen

| Handler-Typ                         | Objekt         | Wirkung                                                         |
|-------------------------------------|----------------|-----------------------------------------------------------------|
| `delete-pod`                        | Pod            | Löscht den Pod                                                  |
| `node-port-service-on-service-base` | Service        | Erstellt zu einem ClusterIP-Service einen NodePort-Service      |
| `delete-node-port-service`          | Service        | Löscht einen von der vorigen Aktion erstellten NodePort-Service |
| `restart-service-replicas`          | Service        | Löscht alle Pods eines Services, damit sie neu starten          |
| `gitlab-pipeline`                   | konfigurierbar | Startet eine GitLab-Pipeline, beliebig viele Aktionen möglich   |
| `deploy-missing-services`           | Namespace      | Startet eine GitLab-Pipeline für fehlende Services              |
| `assign-team`                       | Namespace      | Ordnet die Feature-Umgebung einem Team zu (Namespace-Label)     |
| `nightly-scale-down-exclusion`      | Namespace      | Nimmt den Namespace vom nächtlichen Herunterskalieren aus       |

#### `delete-pod`

Löscht den Pod, an dem die Aktion ausgelöst wird. Die Aktion erscheint in den Service-Details (Klick auf den Punkt
des Services) beim jeweiligen Replica.

| `extra-handler-configs` | Standard | Bedeutung                                                  |
|-------------------------|----------|------------------------------------------------------------|
| `force-delete`          | `false`  | `true` löscht ohne Grace-Period (`gracePeriodSeconds: 0`). |

#### `node-port-service-on-service-base`

Erstellt zu einem ClusterIP-Service einen Service vom Typ `NodePort` mit dem Namen **`<service>-node-port`**.

- **Angeboten** wird die Aktion an Services, die alle diese Bedingungen erfüllen:
    - Der Service ist vom Typ `ClusterIP`.
    - Er hat einen Selector und mindestens einen Port.
    - Im Namespace gibt es noch keinen Service `<service>-node-port`.
    - Der neue Name ist ein gültiger Service-Name, also höchstens 63 Zeichen lang.
- **Formular:** Ein Pflichtfeld „Target-Ports“ als Mehrfachauswahl, mit den Ports des Basis-Services.
- **Erstellt** wird ein Service mit:
    - demselben Selector,
    - den ausgewählten Port-Mappings (Name, Protokoll, Port, Target-Port),
    - zufällig vergebenen NodePorts,
    - einer `ownerReference` auf den Basis-Service.
- **Kennzeichnung:** Label `feature-dashboard/created-by-action: node-port-service-on-service-base`, Label
  `app.kubernetes.io/managed-by: feature-dashboard` und Annotation `feature-dashboard/base-service: <service>`.
- **Erneute Prüfung:** Vor dem Erstellen liest die Aktion den Basis-Service frisch aus dem Cluster. Fehlt er oder ist
  er nicht mehr vom Typ `ClusterIP`, schlägt die Aktion fehl.
- **Lebensdauer:** Wird der Basis-Service gelöscht, löscht Kubernetes (Garbage-Collector) über die `ownerReference`
  auch den NodePort-Service. NodePort-Services, die vor Einführung der `ownerReference` erstellt wurden, bleiben
  dabei erhalten und müssen über `delete-node-port-service` oder manuell gelöscht werden.

Keine `extra-handler-configs`.

#### `delete-node-port-service`

Löscht einen NodePort-Service, aber nur, wenn er das Label
`feature-dashboard/created-by-action: node-port-service-on-service-base` trägt. Manuell angelegte Services bleiben
unberührt.

- **Erneute Prüfung:** Vor dem Löschen liest die Aktion den Service frisch aus dem Cluster und prüft das Label noch
  einmal.
- **UID-Prüfung:** Gelöscht wird mit einer UID-Prüfung, damit nicht versehentlich ein zwischenzeitlich neu angelegter
  Service gleichen Namens erwischt wird.

Keine `extra-handler-configs`.

#### `restart-service-replicas`

Löscht alle Pods, die der Service selektiert, damit ihr Controller sie neu startet.

- **Angeboten** an Services mit Selector und mindestens einem Replica. Ausgenommen sind NodePort-Services, die das
  Dashboard selbst erstellt hat.
- **Aktueller Stand:** Die Pods werden beim Ausführen frisch aus dem Cluster gelesen.
- **Nur Pods mit Controller** wie einem ReplicaSet werden gelöscht. Pods ohne Controller würden nicht neu entstehen;
  die Aktion überspringt sie und nennt sie in der Meldung. Pods, die bereits gelöscht werden, werden übersprungen.
- **Schutz:** Ohne Selector wird nichts gelöscht, denn ein leerer Selector träfe alle Pods im Namespace.

Keine `extra-handler-configs`.

#### `gitlab-pipeline`

Generischer Handler-Typ. Er startet über die
[Pipeline-Trigger-API](https://docs.gitlab.com/ee/ci/triggers/) eine GitLab-Pipeline. Beliebig viele Aktionen
können ihn nutzen, sie unterscheiden sich nur in ihrer Konfiguration.

```yaml
  - action-name: deploy-feature
    handler: gitlab-pipeline
    action-title: Feature deployen
    icon: rocket
    icon-color: green
    projects: [ foo-projekt ]
    extra-handler-configs:
      gitlab-url: https://gitlab.mycomp.de
      project: team/foo-deploy
      ref: "feature/{{ .Namespace }}"
      token-env: GITLAB_TRIGGER_TOKEN_FOO
      status-token-env: GITLAB_READ_TOKEN_FOO   # optional: laufende Pipeline im Dashboard anzeigen
      applies-to: [ namespace ]
      timeout: 15s
      variables:
        NAMESPACE: "{{ .Namespace }}"
        ACTION: deploy
```

| `extra-handler-configs` | Pflicht | Bedeutung                                                                                                    |
|-------------------------|---------|--------------------------------------------------------------------------------------------------------------|
| `gitlab-url`            | ja      | Basis-URL von GitLab (`http`/`https`)                                                                        |
| `project`               | ja      | Projekt-ID oder Projektpfad, z. B. `team/foo-deploy`. Wird automatisch kodiert.                              |
| `ref`                   | ja      | Branch oder Tag. [Template](#templates-in-der-konfiguration) erlaubt.                                        |
| `token-env`             | ja      | Name der Umgebungsvariable mit dem Pipeline-Trigger-Token. Ist sie nicht gesetzt, startet das Backend nicht. |
| `status-token-env`      | nein    | Env-Variable mit einem Access-Token mit `read_api`. Nur dann zeigt das Dashboard die laufende Pipeline an.   |
| `applies-to`            | ja      | Objekt-Typen, an denen die Aktion erscheint: `namespace`, `service`, `ingress`, `pod`                        |
| `timeout`               | nein    | Timeout des HTTP-Aufrufs, Standard `10s`                                                                     |
| `variables`             | nein    | Pipeline-Variablen. Die Werte sind Templates.                                                                |

- **Aufruf:** `POST {gitlab-url}/api/v4/projects/{project}/trigger/pipeline` mit `token`, `ref` und
  `variables[KEY]=value`.
- **Ergebnis:** Bei Erfolg meldet das Dashboard „Pipeline #<id> gestartet: <Link>“. Bei Fehlern erscheinen der
  HTTP-Status und die Fehlermeldung von GitLab.
- **Token-Schutz:** Das Token erscheint nie in Logs oder Meldungen.
- **Fehlende Template-Werte:** Kann ein Template nicht gerendert werden, z. B. wegen eines fehlenden Werts, wird
  nichts an GitLab gesendet.

> **Tokens gehören nicht in die `config.yaml`.** Im Cluster kommen sie aus dem Secret `feature-dashboard-gitlab`
> (siehe [Deployment](#deployment-im-cluster)). Das Deployment lädt alle Schlüssel dieses Secrets als
> Umgebungsvariablen.

**Laufende Pipelines anzeigen:** Mit `status-token-env` (gilt auch für `deploy-missing-services`) merkt sich das
Backend jede gestartete Pipeline an dem Objekt, auf dem die Aktion lief. Das Dashboard zeigt dort einen Chip
„Pipeline läuft“, der zur Pipeline in GitLab springt; Service-Punkte bekommen einen pulsierenden Rand, wenn am Service
oder an einem seiner Replicas eine Pipeline läuft.

- Je Objekt und Aktion zählt nur die zuletzt gestartete Pipeline.
- Alle `pipelines.poll-interval` fragt das Backend `GET {gitlab-url}/api/v4/projects/{project}/pipelines/{id}` mit
  dem Lese-Token ab. Beendete Pipelines (`success`, `failed`, `canceled`, `skipped`) und unbekannte (404)
  verschwinden. Ist GitLab nicht erreichbar, bleibt die Markierung bis zur nächsten erfolgreichen Abfrage.
- Die laufenden Pipelines liegen nur im Speicher. Nach einem Neustart des Backends ist der Stand leer, bereits
  laufende Pipelines werden dann nicht mehr markiert.
- Das Trigger-Token kann keinen Status lesen, deshalb ist ein zweites Token nötig.

#### `assign-team`

Ordnet eine Feature-Umgebung einem Entwickler-Team zu. Der Dialog zeigt ein Single-Select mit allen Teams aus dem
Abschnitt [`teams`](#teams) und „Kein Team“, dazu die aktuelle Zuordnung. Die Aktion setzt am Namespace das Label
`teams.label-key` auf die `team-id` bzw. entfernt es (Merge-Patch, andere Labels bleiben unberührt).

```yaml
  - action-name: assign-team
    action-title: Team zuordnen
    icon: users
    projects: [ foo-projekt ]
```

Keine `extra-handler-configs`; ohne Abschnitt `teams` startet das Backend nicht. Benötigt `patch` auf `namespaces`.

#### `nightly-scale-down-exclusion`

Eine externe Anwendung skaliert jede Nacht alle Feature-Umgebungen auf 0 Replicas und lässt Namespaces mit einem
bestimmten Label aus. Dieser Handler-Typ setzt das Label (`mode: mark`, Namespace ausnehmen) oder entfernt es
(`mode: unmark`, Namespace wird wieder herunterskaliert). Er wird als **zwei Aktionen** mit demselben `label-key`
konfiguriert, die sich gegenseitig ablösen:

- `mark` wird nur angeboten, solange das Label mit dem konfigurierten Wert **fehlt**. Ein anderer Wert gilt als
  nicht gesetzt und wird überschrieben.
- `unmark` wird nur angeboten, solange das Label mit dem konfigurierten Wert **gesetzt** ist.

Nach der Ausführung kommt die Änderung über den Informer. Am Namespace steht dann automatisch die jeweils andere
Aktion. Gesetzt bzw. entfernt wird per Merge-Patch, andere Labels bleiben unberührt.

```yaml
  - action-name: exclude-from-nightly-scale-down
    handler: nightly-scale-down-exclusion
    action-title: Nachts nicht herunterskalieren
    icon: sun
    icon-color: "#f9a825"
    projects: [ foo-projekt ]
    extra-handler-configs: &skip-scale-down
      label-key: mycomp.de/skip-nightly-scale-down   # Pflicht, gültiger Label-Key
      label-value: "true"                            # optional, Standard "true"
      mode: mark                                     # mark = setzen, unmark = entfernen
  - action-name: include-in-nightly-scale-down
    handler: nightly-scale-down-exclusion
    action-title: Nächtliches Herunterskalieren wieder zulassen
    icon: moon
    icon-color: "#455a64"
    projects: [ foo-projekt ]
    extra-handler-configs:
      <<: *skip-scale-down                           # gleiches Label wie oben
      mode: unmark
```

| Feld          | Pflicht | Bedeutung                                                       |
|---------------|---------|-----------------------------------------------------------------|
| `label-key`   | ja      | Label, das die Skalierungs-Anwendung auswertet; gültiger Key    |
| `label-value` | nein    | Wert beim Setzen und für „gesetzt“, Standard `"true"`           |
| `mode`        | ja      | `mark` (Label setzen) oder `unmark` (Label entfernen)           |

Keine Formular-Felder (nur Bestätigung). Benötigt `patch` auf `namespaces`. Namespaces werden nicht per ArgoCD
self-heal verwaltet, das Label bleibt also bestehen.

#### `deploy-missing-services`

Gleicht die konfigurierten Services mit den Services im Namespace ab, verglichen wird der Service-Name. Der Dialog
bietet die fehlenden Services als Mehrfachauswahl an. Für die Auswahl startet der Handler **eine** GitLab-Pipeline,
genau wie bei [`gitlab-pipeline`](#gitlab-pipeline). Die gewählten Services stehen in den Templates als `.Services`.

```yaml
  - action-name: deploy-missing-services
    action-title: Fehlende Services deployen
    icon: package-plus
    icon-color: green
    projects: [ foo-projekt ]
    extra-handler-configs:
      services:
        - name: foo-backend
          label: Foo Backend
        - name: foo-frontend
      gitlab-url: https://gitlab.mycomp.de
      project: team/foo-deploy
      ref: main
      token-env: GITLAB_TRIGGER_TOKEN_FOO
      variables:
        NAMESPACE: "{{ .Namespace }}"
        SERVICES: '{{ join .Services "," }}'
```

| `extra-handler-configs` | Pflicht | Bedeutung                                                                       |
|-------------------------|---------|---------------------------------------------------------------------------------|
| `services`              | ja      | Erwartete Services, mindestens einer. Je Eintrag `name` (eindeutig) und optional `label` für die Anzeige, Standard ist `name`. |
| `gitlab-url` … `variables` | wie bei `gitlab-pipeline` | ohne `applies-to`, die Aktion gilt immer für Namespaces |

- **Nichts fehlt:** Die Aktion bleibt sichtbar. Das Dropdown ist dann leer und die Ausführung wird abgelehnt.
- **Prüfung:** Ausgewählt werden dürfen nur Services, die zum Zeitpunkt der Ausführung noch fehlen.

### `links` und `link-variables`

Links erscheinen im Dashboard als Icon mit Tooltip und öffnen in einem neuen Tab:

- bei Namespace und Ingresses auf der Kachel rechts neben den Aktionen, durch einen Trennstrich abgesetzt,
- bei Services und Pods in den Service-Details (Klick auf den Punkt des Services), ebenfalls neben den Aktionen.

```yaml
link-variables: # statische Werte, im Template unter .Vars
  kibanaDiscover: https://kibana.mycomp.de/app/discover#/?_g=(time:(from:now-1h,to:now))
  kibanaDataView: logs-k8s

projects:
  - project-name: foo-projekt
    # ...
    link-variables-overrides: # gewinnt gegenüber link-variables
      kibanaDataView: logs-foo

links:
  - link-id: kibana-logs                          # eindeutig
    title: Logs in Kibana                         # Tooltip
    icon: scroll-text
    icon-color: "#00bfb3"
    projects: [ foo-projekt, bar-projekt ]          # Pflicht; [] = in keinem Projekt
    targets: # je Objekt-Typ genau eine URL
      namespace:
        url: |-
          {{ .Vars.kibanaDiscover }}&_a=(dataSource:(dataViewId:{{ rison .Vars.kibanaDataView }},type:dataView),
          query:(language:kuery,query:{{ rison (printf "kubernetes.namespace:%q" .Namespace) }}))
      pod:
        url: |-
          {{ .Vars.kibanaDiscover }}&_a=(dataSource:(dataViewId:{{ rison .Vars.kibanaDataView }},type:dataView),
          query:(language:kuery,query:{{ rison (printf "kubernetes.namespace:%q and kubernetes.pod.name:%q" .Namespace .Name) }}))
      service: # Logs aller Pods, die der Service selektiert
        url: |-
          {{ .Vars.kibanaDiscover }}&_a=(dataSource:(dataViewId:{{ rison .Vars.kibanaDataView }},type:dataView),
          query:(language:kuery,query:'kubernetes.namespace:"{{ .Namespace }}"
          {{- range $k, $v := .Selector }} and kubernetes.labels.{{ replace $k "." "_" }}:"{{ $v }}"{{ end }}'))
```

| Feld                | Pflicht | Bedeutung                                                                                                                                                           |
|---------------------|---------|---------------------------------------------------------------------------------------------------------------------------------------------------------------------|
| `link-id`           | ja      | Eindeutige ID                                                                                                                                                       |
| `title`             | ja      | Tooltip im Dashboard                                                                                                                                                |
| `icon`              | nein    | Icon-Name                                                                                                                                                           |
| `icon-color`        | nein    | Icon-Farbe                                                                                                                                                          |
| `projects`          | ja      | Projekte, in denen der Link angeboten wird. `[]` heißt in keinem Projekt, beim Start gibt es dazu eine Warnung.                                                     |
| `targets`           | ja      | Je Objekt-Typ (`namespace`, `service`, `ingress`, `pod`) ein Eintrag mit `url`. Nicht aufgeführte Typen haben den Link nicht. Ein Target ohne `url` ist ein Fehler. |
| `targets.<typ>.url` | ja      | [Template](#templates-in-der-konfiguration) für die Ziel-URL                                                                                                        |

**Wichtig bei URLs:**

- **Kodierung:** Werte kodierst du im Template selbst. Für Query-Parameter nimmst du `urlquery`, für Pfadteile
  `pathescape`, für Kibana-Parameter `rison`. Werte ohne Funktion landen unverändert in der URL.
- **Umbrechen:** Lange URLs brichst du mit dem YAML-Blockstil **`|-`** um. Zeilenumbrüche samt Einrückung werden
  entfernt. **Nicht `>-` verwenden**, denn das macht aus Zeilenumbrüchen Leerzeichen, und die landen in der URL.
- **Gemeinsame Teile:** Was mehrere Targets gemeinsam haben, gehört in `link-variables`, etwa die
  Kibana-Discover-Basis.
- **Absolute URLs:** Nur absolute `http(s)`-URLs werden ausgeliefert. Alles andere, z. B. `javascript:`, wird
  verworfen.
- **Fehler beim Rendern:** Lässt sich ein Link für ein Objekt nicht rendern, wird nur dieser Link weggelassen und
  eine Warnung geloggt. Typische Ursachen sind eine fehlende Variable oder eine ungültige URL.

### `copy-commands`

Kopier-Befehle erscheinen im Dashboard als Icon neben den Links. Der Tooltip zeigt Titel und Befehl, ein Klick legt den
Befehl in die Zwischenablage. Pod-Befehle stehen in den Service-Details beim jeweiligen Replica.

```yaml
copy-commands:
  - command-id: pod-shell                   # eindeutige ID
    title: Shell im Pod öffnen              # Tooltip
    icon: terminal                          # Name eines Lucide-Icons
    icon-color: "#455a64"                   # beliebige CSS-Farbe
    projects: [ foo-projekt ]               # Pflicht; [] = in keinem Projekt
    targets:                                # je Objekt-Typ ein Befehl; fehlende Typen haben den Befehl nicht
      pod:
        command: kubectl exec -it -n {{ .Namespace }} {{ .Name }} -c {{ .Container }} -- sh
      namespace:
        command: kubectl config set-context --current --namespace={{ shellquote .Namespace }}
```

| Feld                | Pflicht | Bedeutung                                                                                  |
|---------------------|---------|--------------------------------------------------------------------------------------------|
| `command-id`        | ja      | Eindeutige ID                                                                              |
| `title`             | ja      | Tooltip im Dashboard                                                                       |
| `icon`              | nein    | Icon des Buttons                                                                           |
| `icon-color`        | nein    | Farbe des Icons                                                                            |
| `projects`          | ja      | Projekte, in denen der Befehl angeboten wird. `[]` heißt: in keinem (Warnung beim Start).  |
| `targets.<typ>`     | ja      | `command` je Objekt-Typ (`namespace`, `service`, `ingress`, `pod`) als Template            |

- **Je Container:** Bei Pods rendert das Backend den Befehl je Container (`.Container`). Unterscheiden sich die
  Ergebnisse, gibt es je Container ein Icon („Shell im Pod öffnen (app)“, „… (sidecar)“). Nutzt das Template
  `.Container` nicht, bleibt es bei einem Befehl.
- **Text:** Der Befehl wird unverändert übernommen, nur Leerraum am Anfang und Ende wird entfernt. Mehrzeilige Befehle
  (YAML `|`) bleiben mehrzeilig.
- **NodePort-Services** bekommen die `service`-Befehle.
- **Fehler:** Lässt sich ein Befehl für ein Objekt nicht rendern oder ist er leer, fehlt nur dieser Befehl; das Backend
  loggt eine Warnung.

### Templates in der Konfiguration

Templates in Links, Kopier-Befehlen und bei `gitlab-pipeline` bzw. `deploy-missing-services` (`ref`, `variables`) sind
[Go-Templates](https://pkg.go.dev/text/template).

**Verfügbare Daten:**

| Feld           | Inhalt                                                                                    |
|----------------|-------------------------------------------------------------------------------------------|
| `.Kind`        | `namespace`, `service`, `ingress` oder `pod`                                              |
| `.Name`        | Name des Objekts, bei einem Namespace dessen Name                                         |
| `.Namespace`   | Namespace des Objekts                                                                     |
| `.Project`     | Dashboard-Projekt des Namespaces                                                          |
| `.Labels`      | Labels des Objekts                                                                        |
| `.Annotations` | Annotations des Objekts                                                                   |
| `.Selector`    | Selector eines Services, bei anderen Objekten leer                                        |
| `.Vars`        | `link-variables`, überschrieben durch `link-variables-overrides`. In Links und Kopier-Befehlen. |
| `.Services`    | Gewählte Services, nur bei `deploy-missing-services` gefüllt, sonst leer                  |
| `.Container`   | Name des Containers, nur bei Kopier-Befehlen für Pods gefüllt (je Container ein Befehl)   |

**Zusätzliche Funktionen** neben den Go-Standardfunktionen wie `urlquery`, `index`, `printf`, `range` und `if`:

| Funktion     | Beispiel                                    | Ergebnis                         |
|--------------|---------------------------------------------|----------------------------------|
| `rison`      | `{{ rison "it's" }}`                        | `'it!'s'` (Kibana-Notation)      |
| `pathescape` | `{{ pathescape "team/foo" }}`               | `team%2Ffoo`                     |
| `replace`    | `{{ replace "app.kubernetes.io" "." "_" }}` | `app_kubernetes_io`              |
| `join`       | `{{ join .Liste ", " }}`                    | verbindet eine Liste von Strings |
| `shellquote` | `{{ shellquote "it's" }}`                   | `'it'\''s'` (ein Shell-Argument)  |
| `trimPrefix` | `{{ trimPrefix (printf "%s-" .Project) .Namespace }}` | entfernt ein Präfix: `myproject-foo-bar` → `foo-bar` (Projekt `myproject`); ohne dieses Präfix unverändert |
| `extractRenewJiraTicketNumber` | `{{ extractRenewJiraTicketNumber .Namespace }}` | Jira-Ticketnummer aus `renew-<f-\|if-\|hf-><story\|hotfix\|feat\|bugfix\|epic\|intfix><nummer>…`, z. B. `renew-f-feat1234-login` → `1234`; sonst leer |

**Fallstricke:**

- **Fehlende Map-Schlüssel:** Ein Zugriff wie `.Vars.gibtsNicht` ist ein **Fehler** und liefert keinen leeren Text.
- **Labels mit `.` oder `/` im Namen:** Hier nimmst du `index`, z. B. `{{ index .Labels "app.kubernetes.io/name" }}`.
  Ein fehlendes Label liefert dabei einen leeren Text.
- **Unbekannte Felder:** Ein Feld wie `.Service` gibt es nicht. Der Name des Objekts steht immer in `.Name`.
- **Wann Fehler auffallen:** Syntaxfehler fallen beim Start auf. Unbekannte Felder und fehlende Werte fallen erst auf,
  wenn das Template für ein konkretes Objekt gerendert wird. Bei Links wird dann nur der betroffene Link weggelassen,
  bei `gitlab-pipeline` schlägt die Aktion mit einer Meldung fehl.
- **Leerzeichen als `+`:** `urlquery` kodiert Leerzeichen als `+`. Das ist für Query-Parameter korrekt.

### `teams`

```yaml
teams:
  label-key: mycomp.de/team
  teams:
    - team-id: checkout
      team-label: Team Checkout
      team-color: "#2e7d32"
    - team-id: search
      team-label: Team Suche
```

| Feld                 | Pflicht | Bedeutung                                                                         |
|----------------------|---------|-----------------------------------------------------------------------------------|
| `label-key`          | ja      | Label am Namespace, das die Team-Zuordnung trägt                                  |
| `teams[].team-id`    | ja      | Eindeutige ID und zugleich Wert des Labels; muss ein gültiger Label-Wert sein     |
| `teams[].team-label` | ja      | Anzeigename                                                                       |
| `teams[].team-color` | nein    | Farbe des Teams als CSS-Wert; in der API `teamColor` (leer, wenn nicht gesetzt)   |

Sind Teams konfiguriert, zeigt das Dashboard je Projekt einen **Team-Filter**: je Team ein Chip plus „Ohne Team“,
mehrere gleichzeitig wählbar, ohne Auswahl werden alle Namespaces gezeigt. Die Auswahl steht in der URL
(`?team.<projekt>=<team-id>`), Reload und geteilte Links behalten sie. An der Kachel steht das zugeordnete Team.
Namespaces mit einem Label-Wert, der kein konfiguriertes Team ist, gelten als „Ohne Team“.

### `cluster`

```yaml
cluster:
  dns-name: k8s-nodes.mycomp.de     # DNS-Name oder IP der Nodes, ohne Schema und Port
```

`dns-name` ist Pflicht und gilt für alle NodePort-Services. Das Backend berechnet daraus je Port die Adresse
`<dns-name>:<nodePort>`; das Dashboard zeigt sie auf der Kachel des Namespaces mit einem Kopieren-Button an.

### `pipelines`

```yaml
pipelines:                                    # optional, gezeigt ist der Standardwert
  poll-interval: 15s
```

| Feld            | Standard | Bedeutung                                               |
|-----------------|----------|---------------------------------------------------------|
| `poll-interval` | `15s`    | Abstand der Status-Abfragen bei GitLab, mindestens `5s` |

Die laufenden Pipelines liegen nur im Speicher des Backends. Nach einem Neustart ist der Stand leer.

### Prüfungen beim Start

Das Backend prüft die Konfiguration vollständig und **bricht bei Fehlern mit einer Liste aller Probleme ab**:

- **Projekte:** `project-name` ist gesetzt und eindeutig, `namespace-labels` sind nicht leer.
- **Service-Bereiche:**
    - `area-name` ist gesetzt, eindeutig je Projekt und nicht `others`.
    - `area-label` ist gesetzt.
    - Es gibt mindestens eine Label-Kombination, und keine Kombination ist leer.
- **Aktionen:**
    - `action-name` ist gesetzt und eindeutig.
    - `projects` ist vorhanden und nennt nur bekannte Projekte.
    - Der Handler-Typ ist registriert.
    - Die `extra-handler-configs` sind gültig, nur bei aktiven Aktionen. Bei `gitlab-pipeline` gehört dazu auch, dass
      die Token-Variable gesetzt ist.
- **Links:**
    - `link-id` ist gesetzt und eindeutig, `title` ist gesetzt.
    - `projects` ist vorhanden und nennt nur bekannte Projekte.
    - Es gibt mindestens ein Target, nur gültige Objekt-Typen, und jedes Target hat eine `url`.
    - Alle Templates sind syntaktisch korrekt.
- **Kopier-Befehle:**
    - `command-id` ist gesetzt und eindeutig, `title` ist gesetzt.
    - `projects` ist vorhanden und nennt nur bekannte Projekte.
    - Es gibt mindestens ein Target, nur gültige Objekt-Typen, und jedes Target hat ein nicht leeres `command`.
    - Alle Templates sind syntaktisch korrekt.
- **Cluster:** `dns-name` ist gesetzt und ein gültiger DNS-Name oder eine IP.
- **Teams:** `label-key` ist ein gültiger Label-Schlüssel; `team-id` ist gesetzt, eindeutig und ein gültiger
  Label-Wert; `team-label` ist gesetzt.
- **Pipelines:** `poll-interval` ist eine Go-Dauer von mindestens `5s`; Env-Variablen aus `status-token-env` sind
  gesetzt.

### Vollständiges Beispiel

Eine kommentierte Beispiel-Konfiguration liegt in [`backend/config.example.yaml`](backend/config.example.yaml).
Die Konfiguration fürs Deployment steht als ConfigMap in
[`backend/deploy/config.yaml`](backend/deploy/config.yaml).

---

## HTTP-API

Die API ist in [`backend/internal/api/openapi.yaml`](backend/internal/api/openapi.yaml) beschrieben. Eine
Swagger-UI gibt es unter **`/api/swagger`**. Sie lädt ihre Skripte von unpkg.com, der Browser braucht dafür
Internetzugang.

| Methode | Pfad                                | Zweck                                                                   |
|---------|-------------------------------------|-------------------------------------------------------------------------|
| GET     | `/api/events`                       | Server-Sent Events: zuerst der komplette Stand, danach nur Änderungen   |
| PATCH   | `/api/actions/{action-name}`        | Aktion auf einem Objekt ausführen                                       |
| GET     | `/api/openapi.yaml`, `/api/swagger` | API-Beschreibung                                                        |
| GET     | `/healthz`                          | Liveness, immer `200`                                                   |
| GET     | `/readyz`                           | Readiness: `200`, sobald der Informer-Cache synchronisiert ist, vorher `503` |

Ältere Konfigurationen mit `frontend` starten weiterhin; der Schlüssel wird ignoriert. `cluster.dns-name` ist dagegen
Pflicht.

### Server-Sent Events (`GET /api/events`)

| Event      | `data`                                         | Bedeutung                                         |
|------------|------------------------------------------------|---------------------------------------------------|
| `snapshot` | `{"projects": [...]}`                          | Immer das erste Event: der komplette Stand        |
| `upsert`   | ein Objekt ohne Kind-Listen                    | Objekt neu oder geändert; vorhandene Kinder bleiben |
| `delete`   | `{"kind": …, "name": …, "parents": {…}}`       | Objekt samt aller Kinder entfernen                |

- **Teams:** Der Snapshot enthält `teams` (`id`, `label`), jeder Namespace optional `team` (ID des Teams).
- **Hierarchie:** Projekt → Namespace → Services (→ Pods), NodePort-Services und Ingresses. Ein Beispiel des Snapshots mit allen
  Feldern steht in [`backend/internal/api/example-api.jsonc`](backend/internal/api/example-api.jsonc).
- **Schlanke Objekte:** Jedes Objekt trägt `kind`, `name`, `parents`, `actions` (ID, Titel, Icon, Farbe, optional
  `formFields`), `links` (ID, Titel, Icon, Farbe, `url`), `copyCommands` (ID, Titel, Icon, Farbe, `command`) und
  `pipelines` (laufende GitLab-Pipelines: `action`, `title`, `url`, `startedAt`), dazu
  je Typ:
    - Service (Typ `ClusterIP`, in `services`): `running`, also mindestens ein Replica ready, und `areas`, die IDs
      seiner Service-Bereiche. Die Bereiche selbst (`id`, `label`) stehen im Snapshot je Projekt in `serviceAreas`,
      `others` immer zuletzt.
    - NodePort-Service (Typ `NodePort`, in `nodePortServices`, `kind: node-port-service`): `addresses`, je Port
      `<cluster.dns-name>:<nodePort>`. Für `PATCH /api/actions` ist er ein Objekt vom Typ `service`.
    - Pod: `ready`, `containers` (Name, Image). Pods hängen unter jedem Service, der sie referenziert.
    - Ingress: `urls`, je Rule mit Host und je Pfad `https://<host><pfad>`.
- **Parents:** Über `parents` (`project`, `namespace`, `service`) lässt sich jedes Event ohne weitere Daten
  einsortieren.
- **Reihenfolge:** `upsert` kommt Parent vor Kind, `delete` Kind vor Parent. Wird ein Parent gelöscht, kommt für
  seine Kinder kein eigenes `delete`.
- **Reconnect:** Bricht die Verbindung ab, verbindet sich der Browser selbst neu und erhält wieder einen `snapshot`.
  Ist ein Client zu langsam, beendet das Backend seinen Stream; auch dann folgt ein Reconnect.
- Alle 25 Sekunden kommt ein Kommentar `: ping`, damit Proxys die Verbindung offen halten.

```sh
curl -N localhost:8080/api/events
```

### Einmaliger Snapshot (`GET /api/snapshot`)

Liefert den aktuellen Stand einmalig als JSON und beendet die Response. Der Inhalt ist derselbe wie im
`snapshot`-Event von `GET /api/events`, es folgen aber keine weiteren Events. Gedacht für Clients, die nur den
aktuellen Stand brauchen (z. B. Skripte). Solange der Informer-Cache synchronisiert, antwortet der Endpunkt mit `503`.

```sh
curl localhost:8080/api/snapshot
```

### Aktion ausführen

```sh
curl -X PATCH localhost:8080/api/actions/delete-pod \
  -H 'Content-Type: application/json' \
  -d '{"object": {"kind": "pod", "namespace": "feat-login", "name": "web-5d9c-abcde"}, "inputs": {}}'
```

| Status | Bedeutung                                                                                                                       |
|--------|---------------------------------------------------------------------------------------------------------------------------------|
| `200`  | Die Aktion wurde erfolgreich ausgeführt: `{"success": true, "message": "…"}`.                                                   |
| `400`  | Ungültiger Body, die Aktion ist auf dem Objekt bzw. in diesem Projekt nicht anwendbar, oder ungültige Eingaben (mit `problems`) |
| `404`  | Unbekannte oder deaktivierte Aktion, oder das Objekt gibt es nicht                                                              |
| `422`  | Der Handler konnte die Aktion nicht ausführen; `{"error": "…"}` enthält seine Meldung für den Nutzer                            |
| `500`  | Interner Fehler im Backend                                                                                                      |

Alle Fehlerantworten haben die Form `{"error": "…"}`. Ein Fehlschlag des Handlers kommt nie als `200`.

Bei Namespaces darf `object.namespace` fehlen. `inputs` enthält die Werte der Formular-Felder mit der Feld-ID als
Schlüssel: `text` als String, `number` als Zahl, `select` als String, `multi-select` als Liste von Strings.

---

## Eine eigene Aktion entwickeln

Eine neue Aktion braucht **keine Änderung am Frontend** (das Frontend ist generisch). Buttons, Icons und Formulare
entstehen aus den Backend-Daten.

1. **Handler schreiben.** Lege ein neues Paket unter `backend/internal/actions/<name>` an und implementiere
   `actions.Handler`:

   ```go
   type Handler interface {
       // extra-handler-configs übernehmen; Fehler brechen den Start ab
       Configure(extra map[string]any) error
       // Ist die Aktion auf diesem Objekt anwendbar? (Projekte prüft bereits die Registry)
       IsApplicable(obj actions.StoredObject) bool
       // Benötigte Eingaben; nil, wenn keine
       FormFields(obj actions.StoredObject) []actions.FormField
       // Ausführen; inputs sind bereits gegen FormFields geprüft
       Execute(ctx context.Context, obj actions.StoredObject, inputs map[string]any) actions.Result
   }
   ```

    - **Objekt:** `obj.Object` ist eine Kopie vom Typ `model.StoredNamespace`, `StoredService`, `StoredIngress` oder
      `StoredPod`. `obj.Project` enthält das Projekt.
    - **Abhängigkeiten:** Über `actions.Deps` bekommt der Handler den Kubernetes-Client (`Client`) und einen
      Lesezugriff auf den Informer-Cache (`Store`, Schnittstelle `actions.ObjectReader`).
    - **Kein eigener Zustand:** Der Handler ändert nur den Cluster bzw. das externe System. Das Dashboard
      aktualisiert sich über die Informer-Events.
    - **Formular-Felder:** Mögliche Typen sind `text`, `number`, `select` und `multi-select`, mit `ID`, `Label`,
      `IsRequired`, `Description` und bei Auswahlfeldern `Options`.
2. **Registrieren** in `backend/cmd/dashboard/main.go`:
   `registry.Register("mein-handler-typ", meinhandler.New)`.
3. **Konfigurieren** in der `config.yaml`: Füge einen Eintrag in `action-handlers` hinzu, mit `projects`.
4. **Rechte prüfen:** Braucht der Handler weitere Kubernetes-Rechte, ergänzt du die ClusterRole in
   `backend/deploy/rbac.yaml`.

---

## Deployment im Cluster

Das Image enthält nur das Backend. Gebaut wird es aus dem `Dockerfile` im Repository-Root:

```sh
make docker IMAGE=feature-dashboard-backend:1.0.0
```

Bei einem lokalen Cluster ohne Registry lädst du das Image danach in den Cluster, z. B. mit
`kind load docker-image …` oder `minikube image load …`.

Die Manifeste liegen in `backend/deploy/`:

| Datei                        | Inhalt                                                              |
|------------------------------|---------------------------------------------------------------------|
| `config.yaml`                | ConfigMap `feature-dashboard` mit der `config.yaml`                 |
| `rbac.yaml`                  | ServiceAccount, ClusterRole, ClusterRoleBinding                     |
| `deployment.yaml`            | Deployment (1 Replica, Strategie `Recreate`) und Service            |
| `gitlab-secret.example.yaml` | Muster für das Secret `feature-dashboard-gitlab` mit Trigger-Tokens |
| `network.example.yaml`       | Muster für Proxy und Company-CA (ConfigMaps `feature-dashboard-network`, `company-ca`) |

```sh
kubectl create namespace feature-dashboard
kubectl apply -f backend/deploy/config.yaml -f backend/deploy/rbac.yaml -f backend/deploy/deployment.yaml
# optional, für gitlab-pipeline-Aktionen (vorher echte Tokens eintragen, nicht committen):
kubectl apply -f backend/deploy/gitlab-secret.example.yaml
```

- **Startbefehl:** Der Container startet mit `--config /etc/dashboard/config.yaml`. Die
  ConfigMap wird unter `/etc/dashboard` eingehängt.
- **Secret:** Das Secret `feature-dashboard-gitlab` wird per `envFrom` als Umgebungsvariablen geladen. Es ist
  optional, das Backend startet also auch ohne dieses Secret.
- **Proben:** Liveness prüft `/healthz`, Readiness prüft `/readyz`.
- **Ein Replica:** Das Backend muss **immer auf genau ein Replica** laufen: Laufende Pipelines liegen im Speicher, und
  jede Replica hielte einen eigenen Informer-Cache und eigene Event-Streams.

### Proxy und Company-CA

Läuft die ausgehende Kommunikation über einen Proxy mit eigener Company-CA, braucht das Backend keinen Code und
keine Config-Änderung, nur zwei optionale ConfigMaps (Muster: `backend/deploy/network.example.yaml`):

```sh
kubectl -n feature-dashboard create configmap company-ca --from-file=company-ca.crt=/pfad/zur/company-ca.pem
kubectl apply -f backend/deploy/network.example.yaml   # vorher Proxy-Adresse und NO_PROXY anpassen
kubectl -n feature-dashboard rollout restart deployment/feature-dashboard-backend
```

- **Proxy:** Die ConfigMap `feature-dashboard-network` wird per `envFrom` geladen und setzt `HTTPS_PROXY`,
  `HTTP_PROXY` und `NO_PROXY`. Alle ausgehenden HTTP-Aufrufe (GitLab-Trigger und Pipeline-Status) laufen darüber.
- **Company-CA:** Die ConfigMap `company-ca` wird unter `/etc/company-ca` eingehängt. `SSL_CERT_DIR=/etc/ssl/certs:/etc/company-ca`
  lässt das Backend den System-CAs des Images **und** der Company-CA vertrauen.
- **`NO_PROXY` nicht vergessen:** Auch der Kubernetes-Client beachtet `HTTPS_PROXY`. Ohne Ausnahme liefen Informer und
  Aktionen über den Proxy. In `NO_PROXY` gehören mindestens die IP des API-Servers (`KUBERNETES_SERVICE_HOST`, bzw.
  der Service-CIDR) sowie `.svc` und `.cluster.local`. CIDR-Angaben sind erlaubt.
- **Image bauen hinter dem Proxy:** `make docker COMPANY_CA=/pfad/zur/company-ca.pem` übergibt die CA als
  BuildKit-Secret (sie landet in keinem Layer) für `go mod download`. `HTTP_PROXY`, `HTTPS_PROXY` und
  `NO_PROXY` aus der Umgebung werden als Build-Args durchgereicht.
- **Lokal (`make run`):** Unter macOS und Windows nutzt Go den System-Zertifikatsspeicher; ist die Company-CA dort
  installiert, ist nichts zu tun. Unter Linux z. B. `SSL_CERT_DIR=/etc/ssl/certs:/pfad/zum/ca-verzeichnis make run`.
- **Browser:** Die Swagger-UI unter `/api/swagger` lädt ihre Skripte von
  unpkg.com; dafür muss der Browser selbst der Company-CA vertrauen.

### Benötigte Rechte (ClusterRole)

| Ressource                         | Verben                           | Wofür                                              |
|-----------------------------------|----------------------------------|----------------------------------------------------|
| `namespaces`                      | get, list, watch, patch          | Informer, `assign-team`, `nightly-scale-down-exclusion` |
| `services`                        | get, list, watch, create, delete | Informer, NodePort-Service erstellen und löschen   |
| `pods`                            | get, list, watch, delete         | Informer, `delete-pod`, `restart-service-replicas` |
| `ingresses` (`networking.k8s.io`) | get, list, watch                 | Informer                                           |

---

## Nächtliches Herunterskalieren (CronJob)

Eine GitLab-Pipeline skaliert jede Nacht alle Feature-Umgebungen auf 0 Replicas. Ausgelöst wird sie von einer
zweiten, kleinen Anwendung im selben Repository (`backend/cmd/nightly-scale-down`). Sie läuft als Kubernetes-CronJob.
Namespaces, die Entwickler im Dashboard mit der Aktion
[`nightly-scale-down-exclusion`](#nightly-scale-down-exclusion) ausgenommen haben, bekommt die Pipeline als Liste.

**Ablauf je Lauf:**

1. Konfiguration aus Umgebungsvariablen lesen. Fehlt etwas, endet die Anwendung mit Exit-Code 1 und nennt alle
   fehlenden bzw. ungültigen Variablen.
2. Alle Namespaces mit dem Label `<SCALE_DOWN_EXCLUSION_LABEL_KEY>=<SCALE_DOWN_EXCLUSION_LABEL_VALUE>` lesen (ein
   List-Aufruf mit Label-Selector, kein Informer). Projekt-Labels spielen dabei keine Rolle.
3. Die Pipeline über die Trigger-API starten, mit dem Pipeline-Input **`EXCLUDE_NAMESPACES`**: die Namen
   komma-getrennt und sortiert, z. B. `foo-feat-1,foo-feat-2`. Ist kein Namespace ausgenommen, ist der Wert leer,
   die Pipeline startet trotzdem.
4. Pipeline-ID und URL loggen und mit Exit-Code 0 enden. Jeder Fehler führt zu Exit-Code 1, der Job gilt dann als
   fehlgeschlagen.

**Umgebungsvariablen:**

| Variable                           | Pflicht | Bedeutung                                                                  |
|------------------------------------|---------|----------------------------------------------------------------------------|
| `SCALE_DOWN_EXCLUSION_LABEL_KEY`   | ja      | Ausnahme-Label; muss zum `label-key` der Aktion passen                     |
| `SCALE_DOWN_EXCLUSION_LABEL_VALUE` | nein    | Wert des Labels, Standard `true` (wie `label-value` der Aktion)            |
| `GITLAB_URL`                       | ja      | z. B. `https://gitlab.mycomp.de`                                           |
| `GITLAB_PROJECT`                   | ja      | Projekt-ID oder Pfad des Projekts mit der Scale-down-Pipeline              |
| `GITLAB_REF`                       | ja      | Branch oder Tag, z. B. `main`                                              |
| `GITLAB_TRIGGER_TOKEN`             | ja      | Pipeline-Trigger-Token (im Cluster aus dem Secret)                         |
| `GITLAB_TIMEOUT`                   | nein    | HTTP-Timeout, Standard `10s`                                               |
| `HTTPS_PROXY`, `NO_PROXY`, `SSL_CERT_DIR` | nein | Proxy und Company-CA wie beim Dashboard                           |
| `KUBECONFIG`                       | nein    | Nur lokal; im Cluster gilt die In-Cluster-Konfiguration                    |

**Pipeline:** `EXCLUDE_NAMESPACES` ist ein Pipeline-Input (`spec:inputs`), keine CI/CD-Variable. Er wird als
`inputs[EXCLUDE_NAMESPACES]` an die Trigger-API übergeben, dafür braucht es eine GitLab-Version, die Inputs über
die Trigger-API annimmt. Beispiel für den Kopf der `.gitlab-ci.yml`:

```yaml
spec:
  inputs:
    EXCLUDE_NAMESPACES:
      type: string
      default: ""
      description: Komma-getrennte Namespaces, die nicht herunterskaliert werden
---
scale-down:
  script:
    - ./scale-down.sh --exclude "$[[ inputs.EXCLUDE_NAMESPACES ]]"
```

**Deployment:** Der CronJob nutzt dasselbe Image wie das Dashboard und startet mit `command: ["/nightly-scale-down"]`.

```sh
kubectl apply -f backend/deploy/nightly-scale-down-secret.example.yaml   # vorher echtes Trigger-Token eintragen
kubectl apply -f backend/deploy/nightly-scale-down.yaml                  # vorher Schedule, Label und GitLab-Werte anpassen
```

- **Zeitplan:** `schedule: "0 20 * * *"` mit `timeZone: Europe/Berlin` ist ein Platzhalter.
- **Kein Retry:** `concurrencyPolicy: Forbid` und `backoffLimit: 0` verhindern, dass in einer Nacht zwei
  Pipelines starten. Ein fehlgeschlagener Lauf wird nicht wiederholt.
- **Rechte:** eigener ServiceAccount `nightly-scale-down` mit ClusterRole, nur `list` auf `namespaces`.
- **Proxy und Company-CA:** wie beim Dashboard über die ConfigMaps `feature-dashboard-network` und `company-ca`.
- **Label:** `SCALE_DOWN_EXCLUSION_LABEL_KEY` und `_VALUE` müssen zu `label-key` und `label-value` der Aktion in
  der `config.yaml` des Dashboards passen. Sonst werden markierte Namespaces trotzdem herunterskaliert.

**Lokal ausführen** (startet wirklich eine Pipeline):

```sh
SCALE_DOWN_EXCLUSION_LABEL_KEY=mycomp.de/skip-nightly-scale-down GITLAB_URL=https://gitlab.mycomp.de \
  GITLAB_PROJECT=team/nightly-scale-down GITLAB_REF=main GITLAB_TRIGGER_TOKEN=… make nightly-scale-down-run
```

---

## Entwicklung

Alle Befehle laufen über das `Makefile` im Repository-Root:

| Befehl              | Wirkung                                                                             |
|---------------------|-------------------------------------------------------------------------------------|
| `make test`         | Backend-Tests (`go test -race ./...`)                                               |
| `make lint`         | `go vet`                                                                            |
| `make build`        | Binaries nach `backend/bin/` (`dashboard`, `nightly-scale-down`)                    |
| `make run`          | Backend lokal starten (`CONFIG=…`, `STATIC_DIR=…`, `KUBECONFIG=…` überschreibbar)   |
| `make nightly-scale-down-run` | CronJob-Anwendung einmal lokal ausführen (Konfiguration aus der Umgebung) |
| `make docker`       | Image bauen (`IMAGE=…` überschreibbar, `COMPANY_CA=…` für den Build hinter dem Proxy) |
| `make clean`        | Build-Artefakte löschen                                                             |

Einzelschritte gibt es als `backend-build|test|vet|run`.

Die Backend-Tests laufen ohne Cluster:

- Die Kubernetes-Anbindung wird mit dem fake-Clientset von `client-go` getestet.
- GitLab wird durch einen Test-HTTP-Server ersetzt.

Weitere Hinweise zu Architektur und Konventionen stehen in [`CLAUDE.md`](CLAUDE.md), Entwurfs- und
Umsetzungspläne in `plans/`.

---

## Fehlersuche

| Meldung / Symptom                                                         | Ursache und Lösung                                                                                                                                                     |
|---------------------------------------------------------------------------|------------------------------------------------------------------------------------------------------------------------------------------------------------------------|
| `action-handlers[n]: projects fehlt …`                                     | `projects` ist Pflicht. Zum Deaktivieren `projects: []` setzen.                                                                                                        |
| `aktion "…": umgebungsvariable … mit dem Trigger-Token ist nicht gesetzt` | Token-Variable setzen bzw. Secret `feature-dashboard-gitlab` anlegen, oder die Aktion mit `projects: []` deaktivieren.                                                 |
| `projects gehört nicht mehr in extra-handler-configs …`                   | `projects` bei `gitlab-pipeline` direkt an die Aktion verschieben.                                                                                                     |
| `WARN Link konnte nicht erzeugt werden … can't evaluate field X`          | Das Template nutzt ein Feld, das es nicht gibt, siehe [Template-Daten](#templates-in-der-konfiguration).                                                               |
| `WARN Link konnte nicht erzeugt werden … map has no entry for key`        | Die Variable fehlt in `link-variables` bzw. `link-variables-overrides`.                                                                                                |
| Link-URL enthält Leerzeichen bzw. ist kaputt                              | Blockstil `>-` statt `\|-` verwendet, oder ein Wert wurde nicht mit `urlquery`/`pathescape` kodiert.                                                                   |
| Namespace erscheint nicht im Dashboard                                    | Labels prüfen: Der Namespace muss **alle** `namespace-labels` eines Projekts mit genau diesen Werten tragen.                                                           |
| `/readyz` liefert dauerhaft `503`                                         | Der Cluster ist nicht erreichbar, oder Rechte fehlen. Log prüfen: `Informer-Cache nicht synchronisiert` bzw. Fehler der Informer (Rechte, Erreichbarkeit).                                                        |
| Aktion fehlt an einem Objekt                                              | Das Projekt steht nicht in `projects` der Aktion, oder der Handler hält sie nicht für anwendbar (Bedingungen siehe [Mitgelieferte Aktionen](#mitgelieferte-aktionen)). |
| Kopieren-Button tut nichts                                                | Ohne HTTPS greift ein Ausweichweg über ein Textfeld. Manche Browser blockieren auch diesen.                                                                            |
