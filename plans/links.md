# Plan: Konfigurierbare Links je Objekt (z. B. Absprung nach Kibana)

## Context

In der `config.yaml` sollen Links definiert werden können, die das Dashboard an Namespaces, Services, Ingresses und
Pods anzeigt. Beispiel: Absprung nach Kibana zu den Logs eines Pods, eines Services oder eines ganzen Namespaces.

Die Anforderungen:
- **Statische Parameter** wie Kibana-URL oder Index-Pattern sind konfigurierbar.
- **Variablen** werden je nach angeklicktem Objekt befüllt, z. B. Pod-Name, Namespace oder Service-Selector.
- Gemäß „maximal generisch“ in `CLAUDE.md` rendert **das Backend** die fertigen URLs. Das Frontend zeigt nur
  Icon, Titel und URL an und kennt keine Link-Typen.

Ein ähnliches Muster gibt es schon: `internal/actions/gitlabpipeline` nutzt Go-Templates mit `TemplateData`
(`Kind`, `Name`, `Namespace`, `Project`, `Labels`, `Annotations`), dazu `metaOf`, `projectOf`, `parseTemplate` und
`render`. Diese Bausteine werden in ein gemeinsames Paket gezogen und von beiden Funktionen genutzt.

## Vorschlag für die Konfiguration

```yaml
# Globale, statische Link-Variablen; je Projekt überschreibbar.
link-variables:
  kibanaUrl: https://kibana.mycomp.de
  kibanaDataView: logs-k8s
  timeRange: now-1h

projects:
  - project-name: foo-projekt
    namespace-labels: { ... }
    link-variables:                      # überschreibt/ergänzt die globalen Werte für dieses Projekt
      kibanaDataView: logs-foo

links:
  - link-id: kibana-logs                 # eindeutig
    title: Logs in Kibana                # Tooltip im Dashboard
    lucide-icon: scroll-text
    lucide-icon-color: "#00bfb3"
    projects: [foo-projekt]              # optional: nur für diese Projekte
    # Je Objekt-Typ ein eigenes Ziel. Fehlt ein Typ, gibt es den Link dort nicht.
    targets:
      namespace:
        query: 'kubernetes.namespace:"{{ .Namespace }}"'
      pod:
        query: 'kubernetes.namespace:"{{ .Namespace }}" and kubernetes.pod.name:"{{ .Name }}"'
      service:                           # Logs aller Pods, die der Service selektiert
        query: >-
          kubernetes.namespace:"{{ .Namespace }}"
          {{- range $k, $v := .Selector }} and kubernetes.labels.{{ replace $k "." "_" }}:"{{ $v }}"{{ end }}
    # URL-Template für alle Targets; .Target enthält die Werte des passenden targets-Eintrags.
    url: >-
      {{ .Vars.kibanaUrl }}/app/discover#/?_g=(time:(from:{{ .Vars.timeRange }},to:now))
      &_a=(dataSource:(dataViewId:{{ rison .Vars.kibanaDataView }},type:dataView),
      query:(language:kuery,query:{{ rison .Target.query }}))

  - link-id: grafana-pod
    title: Metriken in Grafana
    lucide-icon: chart-line
    lucide-icon-color: orange
    targets:
      pod: {}
    url: "{{ .Vars.grafanaUrl }}/d/k8s-pod"
    query-params:                        # einfache Variante: werden automatisch URL-kodiert angehängt
      var-namespace: "{{ .Namespace }}"
      var-pod: "{{ .Name }}"
```

**Begriffe:**
- **`link-variables`**: statische Werte, global und je Projekt (die Projekt-Werte gewinnen). Im Template stehen sie
  unter `.Vars`. So lassen sich unterschiedliche Kibana-Instanzen oder Data-Views je Projekt abbilden, ohne den
  Link doppelt zu definieren.
- **`targets`**: Für welche Objekt-Typen gibt es den Link, und welche typspezifischen Werte gelten dort? Erlaubt
  sind `namespace`, `service`, `ingress` und `pod`. Ein Link deckt so z. B. Pod-, Service- und Namespace-Logs ab,
  jeweils mit eigener Filter-Query. Die Werte stehen im Template unter `.Target`.
- **`url`**: Go-Template für die Ziel-URL.
- **`query-params`** (optional): Map aus Name und Template. Die Werte werden URL-kodiert angehängt. Das ist die
  einfache Variante für Tools mit klassischen Query-Parametern.
- **Template-Daten:**
  - `.Kind`, `.Name`, `.Namespace`, `.Project`, `.Labels`, `.Annotations` wie bei `gitlab-pipeline`
  - **neu** `.Selector` (nur bei Services)
  - `.Vars` und `.Target`
- **Template-Funktionen:**
  - `urlquery` (Go-Standard)
  - `pathescape`
  - `rison`: Kibana-Notation, setzt Hochkommas und escaped `'` und `!`
  - `replace`
  - `join`

  Kibana erwartet seine Parameter im URL-Fragment in Rison-Notation. Ein reines Query-Parameter-Modell reicht dafür
  nicht, deshalb gibt es das freie `url`-Template.

## Umsetzung

1. **Gemeinsame Template-Bausteine** (neues Paket `backend/internal/objecttemplate`):
   - `Data` (bisher `TemplateData`) um `Selector`, `Vars` und `Target` erweitern.
   - Dazu `NewData(obj, project)`, `Meta(obj)`, `Parse(name, text)` mit Funktionen und `missingkey=error`, sowie
     `Render`.
   - `internal/actions/gitlabpipeline` auf das Paket umstellen. Das Verhalten bleibt gleich, die vorhandenen Tests
     sichern das ab.
2. **Config** (`backend/internal/config/config.go`):
   - Neue Felder: top-level `LinkVariables map[string]string`, `Links []Link`, und `Project.LinkVariables`.
   - Neuer Typ `Link{ID, Title, LucideIcon, LucideIconColor, Projects, Targets map[model.Kind]map[string]string,
     URL, QueryParams}`.
   - Validierung: eindeutige `link-id`, `url` gesetzt, mindestens ein Target, nur gültige Kinds, bekannte Projekte.
3. **Link-Registry** (neues Paket `backend/internal/links`):
   - Parst beim Start alle Templates. Fehler führen zum Startfehler.
   - `LinksFor(obj actions.StoredObject, project string) []Link` rendert zuerst `.Target`, danach `url` und
     `query-params`.
   - Schlägt das Rendern für ein Objekt fehl, z. B. weil ein Label fehlt, wird nur dieser Link weggelassen und eine
     Warnung geloggt. Die Seite bleibt benutzbar.
   - Nur `http(s)`-URLs werden ausgeliefert. Das verhindert `javascript:`-URLs aus Templates.
4. **API** (`backend/internal/api/server.go`):
   - Jedes Objekt-DTO (`NamespaceDTO`, `ServiceDTO`, `IngressDTO`, `PodDTO`) bekommt
     `links: [{id, title, lucideIcon, lucideIconColor, url}]`, analog zu `actions`.
   - Der ETag-Mechanismus bleibt korrekt: Links hängen nur von Objekt-Daten und der statischen Config ab.
   - Die `openapi.yaml` bekommt das Schema `Link` und das Feld `links` an `ObjectBase`.
5. **Frontend**:
   - Neue Komponente `LinkButtons` mit Icon-Buttons und Tooltip, analog zu `ActionButtons`. Sie rendert
     `<a href target="_blank" rel="noopener noreferrer">`.
   - Angezeigt wird sie neben den Aktionen bei Namespace, Ingress, NodePort- und Anwendungs-Services, und **bei den
     Replicas in der Service-Kachel**. Pod-Aktionen bleiben dort ausgeblendet, Pod-Links werden gezeigt, denn gerade
     „Logs eines Pods“ ist der Hauptanwendungsfall.
   - Typen in `src/api/types.ts`, dazu Tests.
6. **Doku und Beispiele**:
   - `CLAUDE.md`: Abschnitt „Links“ unter Konfiguration, Paket-Tabelle, Hinweis zu Pod-Links im Seitenaufbau.
   - `backend/config.example.yaml` mit dem Kibana-Beispiel.
   - Plan nach `plans/links.md` kopieren.

## Verifikation

- `make lint test`. Neue Tests:
  - Config-Validierung
  - Variablen-Überschreibung global → Projekt
  - Rendern je Kind inklusive Service-Selector
  - `rison`-Escaping
  - Weglassen bei Template-Fehler
  - Ablehnen von Nicht-http-URLs
  - API-Response mit `links`
  - Frontend-Darstellung von Links an Pods
- Manuell: Das Kibana-Beispiel konfigurieren, `make run` starten und auf der Namespace-Seite die Links an
  Namespace, Service und Pod öffnen. Kibana muss mit dem passenden Filter und der passenden Data-View öffnen.
