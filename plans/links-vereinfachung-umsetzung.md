# Plan: Links vereinfachen – Antworten zu `plans/links-vereinfachung.md` und Umsetzung „nur url je Target“

## Context

Der Nutzer möchte die Link-Konfiguration vereinfachen. Er hat dazu `plans/links-vereinfachung.md` geschrieben:
Jedes Target definiert seine URL direkt, mit `query-params` (roh) und `query-params-url-encoded`.
`link-variables` heißt je Projekt `link-variables-overrides`, und `projects` wird Pflicht.

Er fragt, ob dadurch Funktionalität verloren geht, und wünscht alternativ einen eigenen Vereinfachungsplan. Gewählt
ist die noch einfachere Alternative: **Jedes Target ist nur ein URL-Template.** Die Kodierung geschieht je Wert im
Template.

## Antworten auf die Fragen im Entwurf

**Verliert die Logik durch „URL je Target“ an Funktionalität?** Nein, alles bleibt abbildbar. Im Einzelnen:
- **Wegfall von `.Target`:** Bisher teilten sich alle Typen eines Links ein URL-Template, nur `.Target`-Werte wie
  die Kibana-Query unterschieden sich.
  - Neu steht die URL je Typ komplett da. Das ist kein Funktionsverlust, aber **Wiederholung**: Eine lange
    Kibana-URL steht dann z. B. dreimal da (Namespace, Service, Pod).
  - Gegenmittel: Gemeinsame Teile in `link-variables` auslagern, etwa
    `kibanaDiscover: https://kibana…/app/discover#/?_g=(…)`.
- **Kodierung je Parameter statt je Link:** Das ist sogar **flexibler** als heute (`encode-query-params` gilt
  bisher für alle Parameter eines Links).
- **Listen mit key/value statt Map:** Auch das ist **flexibler**, weil Reihenfolge und doppelte Keys wie
  `filter=a&filter=b` möglich werden.

**Probleme im Beispiel des Entwurfs:**
- **`{{ .Pod }}` gibt es nicht.** Der Name des Objekts steht immer in `{{ .Name }}`.
- **`value: {{ .Namespace }}` ohne Anführungszeichen ist ungültiges YAML.** `{` beginnt dort ein Mapping. Es muss
  `value: "{{ .Namespace }}"` heißen. Das gilt für jedes Template, das mit `{{` beginnt.
- **`service: {}` als „nicht verfügbar“ ist doppeldeutig.** Heute bedeutet `pod: {}` „verfügbar, ohne
  Zusatzwerte“. Klarer ist es, den Typ einfach wegzulassen. Ein Target ohne `url` wird daher beim Start als
  Config-Fehler gemeldet.
- **`projects` als Pflichtfeld mit „leer = nirgends“ ist in Ordnung.** Ein Link mit `projects: []` ist dann aber
  wirkungslos und leicht zu übersehen. Deshalb gilt:
  - Fehlt das Feld ganz, ist das ein Fehler.
  - `projects: []` ist erlaubt, beim Start gibt es dazu eine Warnung im Log.
- **Rohe Query-Parameter** brauchen weiterhin die Prüfung auf `#`, Leerzeichen und Zeilenumbrüche, sonst bricht
  die URL. Bei der gewählten Alternative entfällt diese Sonderlogik ganz, siehe unten.

## Gewählte Vereinfachung: nur `url` je Target

```yaml
link-variables:                          # global; je Projekt per link-variables-overrides überschreibbar
  fooUrl: https://foo.mycomp.de

projects:
  - project-name: foo-projekt
    namespace-labels: { ... }
    link-variables-overrides:            # umbenannt (bisher link-variables)
      fooUrl: https://other.mycomp.de

links:
  - link-id: foo-system
    title: Absprung nach Foo
    lucide-icon: scroll-text
    lucide-icon-color: "#00bfb3"
    projects: [foo-projekt]              # Pflicht; [] = in keinem Projekt (Warnung beim Start)
    targets:                             # nur aufgeführte Typen haben den Link
      namespace:
        url: "{{ .Vars.fooUrl }}/{{ .Namespace }}?foo=bar&foobar={{ urlquery .Namespace }}"
      pod:
        url: |-                          # Zeilenumbrüche samt Einrückung werden entfernt
          {{ .Vars.fooUrl }}/{{ pathescape .Namespace }}/{{ pathescape .Name }}
          ?foo=bar&foobar={{ urlquery .Name }}
```

**Warum das einfacher ist:**
- Je Target gibt es genau ein Feld. Es gibt kein `.Target`, keine `query-params`, kein `encode-query-params` und
  keine zweite Encoding-Logik.
- Wo kodiert wird, steht sichtbar im Template: `urlquery` für Query-Werte, `pathescape` für Pfadteile, `rison` für
  Kibana, oder bewusst roh.

**Was man beachten muss:** Werte kodiert man jetzt selbst. Ohne `urlquery` landet z. B. ein `&` im Wert roh in der
URL. Doku und Beispiele zeigen das deshalb überall.

**Hinweis zu Zeilenumbrüchen:** In der YAML `|-` verwenden, nicht `>-`. Bei `>-` macht YAML aus Zeilenumbrüchen
Leerzeichen, und die landen in der URL.

## Umsetzung

1. **Config** (`backend/internal/config/config.go`):
   - `Link`: Die Felder `URL`, `QueryParams` und `EncodeQueryParams` sowie die Methode `EncodesQueryParams` fallen
     weg.
   - `Targets` wird zu `map[model.Kind]LinkTarget`, mit `LinkTarget{URL string \`yaml:"url"\`}`.
   - `Project.LinkVariables` hat jetzt den YAML-Key `link-variables-overrides`. Der Go-Name wird
     `LinkVariablesOverrides`.
   - `Projects` wird Pflicht: `nil`, also Feld fehlt, ergibt einen Fehler.
   - Validierung: Jedes Target braucht eine `url`. Die übrigen Prüfungen bleiben.
   - Tests in `config_test.go` anpassen: `TestLinks`, `TestLinksValidation`; `TestEncodesQueryParams` entfällt.
2. **`backend/internal/objecttemplate`:** Das Feld `Target` und seine Initialisierung entfallen, ebenso der
   Kommentar dazu.
3. **`backend/internal/links/links.go`:**
   - `compiled` hält nur noch `targets map[model.Kind]*template.Template`.
   - `compile` parst je Target die `url`, mit dem bestehenden `lineBreaks`-Stripping.
   - `For` und `render` rendern nur die URL des Typs, dann `url.Parse` und die http(s)-Prüfung wie bisher.
   - `appendQueryParams` entfällt.
   - `NewRegistry` loggt eine Warnung für Links mit `projects: []`. Die Projektprüfung in `For` wird: Ist das
     Projekt nicht in `projects`, wird übersprungen.
   - Tests in `links_test.go` umstellen:
     - Kibana-Beispiel mit URL je Typ
     - Overrides je Projekt
     - Kodierung über `urlquery` und `pathescape`
     - weggelassene Links bei Fehlern
     - `projects: []` ergibt keine Links
4. **API, `openapi.yaml`, Frontend:** Keine Änderung. Die Ausgabe `links: [{id, title, …, url}]` bleibt gleich.
   Nur der API-Test `TestLinks` in `server_test.go` bekommt die neue Config-Form.
5. **Doku und Beispiele:**
   - Den Link-Abschnitt in `CLAUDE.md` neu schreiben: neue Form, Template-Daten ohne `.Target`, Kodierungshinweis
     zu `urlquery`/`pathescape`/`rison`, `|-` statt `>-`, `projects` Pflicht.
   - `backend/config.example.yaml`: Kibana-Beispiel auf URL je Typ umstellen. Dabei steht die gemeinsame
     Discover-Basis in `link-variables`.
   - Diesen Plan als `plans/links-vereinfachung-umsetzung.md` ablegen. Den Entwurf des Nutzers
     `plans/links-vereinfachung.md` nicht überschreiben.

## Verifikation

- `make lint test`. Alle Backend- und Frontend-Tests sind grün.
- Das umgestellte Beispiel aus `config.example.yaml` einkommentiert mit `make run` starten. Der Start muss ohne
  Template-Fehler durchlaufen, und `/api/projects/.../namespaces/...` muss die erwarteten Link-URLs liefern.
