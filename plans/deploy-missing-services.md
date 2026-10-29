# Plan: ActionHandler „deploy-missing-services“

## Context

Entwickler sollen in einer Feature-Umgebung von `foo-projekt` Services nachdeployen können, die dort noch fehlen.
Dafür kommt ein neuer ActionHandler auf Namespace-Ebene. In seiner `extra-handler-configs` steht die Liste der
erwarteten Services. Der Handler gleicht sie mit den Services im Namespace ab und bietet die fehlenden in einem
Dropdown an. Nach der Bestätigung validiert er die Auswahl und startet eine GitLab-Pipeline.

Mit dem Nutzer geklärt:
- Abgleich über den **Service-Namen**.
- **Mehrfachauswahl** (`multi-select`), **eine** Pipeline für alle gewählten Services.
- Pipeline-Parameter **frei konfigurierbar** wie bei `gitlab-pipeline` (Go-Templates). Die Auswahl steht als
  `.Services` in den Template-Daten.
- Fehlt kein Service, bleibt die Aktion **sichtbar**. Das Dropdown ist dann leer und die Ausführung wird abgelehnt.

Projektbindung an `foo-projekt` läuft wie bei allen Aktionen über `projects: [foo-projekt]` in der `config.yaml`,
nicht über Code.

## Ziel-Konfiguration

```yaml
  - action-name: deploy-missing-services
    action-title: Fehlende Services deployen
    icon: package-plus
    icon-color: green
    projects: [foo-projekt]
    extra-handler-configs:
      services:                            # Pflicht, mindestens ein Eintrag, Namen eindeutig
        - name: foo-backend
          label: Foo Backend               # optional, Standard = name (Anzeige im Dropdown)
        - name: foo-frontend
      gitlab-url: https://gitlab.mycomp.de
      project: team/foo-deploy
      ref: main                            # Go-Template erlaubt
      token-env: GITLAB_TRIGGER_TOKEN_FOO
      timeout: 15s                         # optional
      variables:
        NAMESPACE: "{{ .Namespace }}"
        SERVICES: '{{ join .Services "," }}'
```

Es gibt kein `applies-to`, der Handler gilt immer nur für Namespaces.

## Umsetzung

### 1. Trigger-Logik aus `gitlabpipeline` herauslösen (`backend/internal/actions/gitlabpipeline/`)

Damit die GitLab-Logik nicht doppelt existiert, wird sie als wiederverwendbarer Teil exportiert:
- `type TriggerConfig struct` mit `gitlab-url`, `project`, `ref`, `token-env`, `timeout`, `variables`
  (yaml-Tags). Wird in beide `settings` per `yaml:",inline"` eingebettet.
- `func NewTrigger(c TriggerConfig) (*Trigger, error)` übernimmt die heutigen Prüfungen aus `Configure`
  (URL, Projekt, Token-Env, Timeout, Template-Parsing).
- `func (t *Trigger) Run(ctx, data objecttemplate.Data) actions.Result` übernimmt die heutige `Execute`-Logik
  (Rendern, POST, `gitlabMessage`, Ergebnistext).
- `Handler` prüft danach nur noch `applies-to` und `projects` und delegiert an `Trigger`. Die bestehenden Tests in
  `gitlabpipeline_test.go` müssen unverändert grün bleiben.

### 2. Template-Daten erweitern (`backend/internal/objecttemplate/objecttemplate.go`)

- Neues Feld `Services []string` in `Data`. Es ist nur beim neuen Handler gefüllt. `NewData` setzt es auf `[]string{}`,
  damit `join .Services ","` auch sonst funktioniert.
- Das neue Feld in der Doku der Template-Daten nennen (CLAUDE.md).

### 3. Store-Lesezugriff auf Services eines Namespaces

`actions.ObjectReader` hat heute nur `Get`. Ergänzt wird:
- `backend/internal/store/read.go`: `func (s *Store) Services(namespace string) ([]model.StoredService, error)`
  (RLock, `decodeList` wie in `Namespace`, unbekannter Namespace → leere Liste).
- `backend/internal/actions/actions.go`: `ObjectReader` um `Services(namespace string) ([]model.StoredService, error)`
  erweitern.

Gelesen wird aus dem Store und nicht aus dem Cluster. `FormFields` läuft bei jedem `GET` der Namespace-Seite, ein
Cluster-Call pro Abfrage wäre dort zu teuer. Die Store-Garantie (Kopien via JSON) bleibt erhalten.

### 4. Neuer Handler `backend/internal/actions/missingservices/missingservices.go`

- `const HandlerType = "deploy-missing-services"`, `New(deps)` speichert `deps.Store`.
- `Configure`: per yaml-Roundtrip wie in `gitlabpipeline` in `settings{ Services []serviceEntry; TriggerConfig inline }`
  einlesen. Geprüft wird: `services` nicht leer, `name` nicht leer, Namen eindeutig. Danach `NewTrigger`.
  Alle Fehler werden mit `errors.Join` gesammelt.
- `IsApplicable`: `obj.Ref.Kind == model.KindNamespace`.
- `missing(namespace)`: liest `Store.Services(ns)` und liefert die konfigurierten Einträge, deren Name nicht
  vorkommt. Die Reihenfolge entspricht der Konfiguration.
- `FormFields`: ein Feld `{ID: "services", Label: "Services", Type: multi-select, IsRequired: true, Options: fehlende}`.
  Die `Description` lautet je nach Stand „Fehlende Services, die per Pipeline deployt werden“ oder „Alle
  konfigurierten Services sind bereits deployt“. Bei Store-Fehlern gibt es leere Options und eine Beschreibung mit
  dem Fehler.
- `Execute`: Die Registry hat schon gegen die aktuellen Options validiert (Pflicht, nur fehlende Werte). Der Handler
  baut die Liste trotzdem defensiv neu auf: gewählte Werte als `[]string` übernehmen, deduplizieren und erneut gegen
  `missing` prüfen. Ist nichts mehr übrig, gibt er einen Failure zurück. Sonst setzt er
  `data := objecttemplate.NewData(...)` und `data.Services = auswahl` und ruft `trigger.Run(ctx, data)` auf.
- Ist das Dropdown leer, liefert die Registry-Validierung „services: Pflichtfeld“ (400). Der Dialog zeigt die
  Beschreibung, das reicht laut Entscheidung.

### 5. Wiring und Konfiguration

- `backend/cmd/dashboard/main.go`: `registry.Register(missingservices.HandlerType, missingservices.New)`.
- `backend/config.example.yaml`: Eintrag wie oben mit `projects: [foo-projekt]`. **Achtung:** `make run` braucht dann
  `GITLAB_TRIGGER_TOKEN_FOO`, sonst startet das Backend nicht (bestehende Regel). Alternative wäre `projects: []`
  mit Kommentar. Umgesetzt wird die aktive Variante, wie gewünscht.
- `backend/deploy/gitlab-secret.example.yaml` deckt `GITLAB_TRIGGER_TOKEN_FOO` bereits ab, dort ist nichts zu ändern.
- Frontend: keine Änderung. `ActionDialog.tsx` rendert `multi-select` bereits generisch.
- `openapi.yaml`: kein Schema-Change, weil Formular-Felder schon generisch beschrieben sind.

### 6. Dokumentation

- `CLAUDE.md`: Paket-Tabelle um `internal/actions/missingservices` ergänzen, Konfig-Beispiel beim Abschnitt
  ActionHandler einfügen, Template-Daten um `.Services` erweitern.
- `README.md`: Paket-Tabelle ergänzen (Zeile ~202).

## Tests

- `missingservices_test.go`: Fake-`ObjectReader` und `httptest`-GitLab nach dem Muster von `fakeGitLab` in
  `gitlabpipeline_test.go`. Getestet wird:
  - Configure-Fehler: leere Liste, doppelter Name, fehlender Name, fehlendes Token.
  - `IsApplicable` nur für Namespaces.
  - `FormFields` liefert nur fehlende Services in Config-Reihenfolge, Labels und den Leerfall.
  - `Execute` sendet `variables[SERVICES]=a,b` und setzt `ref`.
  - `Execute` mit einem inzwischen deployten Service: Failure, kein Request.
  - Registry-Validierung lehnt einen nicht fehlenden Wert ab (über `actions.Registry` wie in `nodeportservice_test.go`).
- `store`: Test für `Services` (bekannter und unbekannter Namespace).
- `objecttemplate`: `join .Services ","` funktioniert auch mit leerer Liste.

## Verifikation

1. `make test` und `make lint` (inkl. unveränderter `gitlabpipeline`-Tests nach dem Refactoring).
2. Lokal: `GITLAB_TRIGGER_TOKEN_FOO=dummy make run`. Auf der Namespace-Seite eines `foo-projekt`-Namespaces erscheint
   die Aktion. Der Dialog zeigt nur die fehlenden Services. In Namespaces von `bar-projekt` erscheint sie nicht.
3. `PATCH /api/actions/deploy-missing-services` mit einem bereits deployten Service → 400. Mit fehlendem Service →
   Pipeline-Request (gegen Dummy-URL; erwartet wird ein Failure-Result mit „GitLab nicht erreichbar“).
