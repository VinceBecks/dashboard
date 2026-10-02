# Plan: Generischer ActionHandler „gitlab-pipeline“

## Context

Es soll mehrere Aktionen geben, die jeweils per HTTP-Call eine GitLab-Pipeline starten. Sie unterscheiden sich
nur in Titel, Icon, Icon-Farbe, GitLab-Projekt bzw. -Pfad und Variablen. Statt für jede Aktion einen eigenen
Handler zu schreiben, soll ein generischer Handler beliebig viele solcher Aktionen aus der `config.yaml` bedienen.

Heute ist die Registry 1:1 an den `action-name` gebunden. `Registry.Activate` in
`backend/internal/actions/registry.go` sucht die Factory über `c.Name`. Derselbe Handler-Typ kann also nicht für
mehrere Aktionen genutzt werden.

Mit dem Nutzer geklärt:
- Der Handler ist **GitLab-spezifisch** und kein freier HTTP-Call.
- Er nutzt die **Pipeline-Trigger-API** mit Trigger-Token (`POST /api/v4/projects/:id/trigger/pipeline`).
- Variablen dürfen **Templates mit Objekt-Daten** enthalten.
- Tokens kommen aus **Umgebungsvariablen**. Die `config.yaml` nennt nur deren Namen, im Cluster stammen die Werte
  aus einem Kubernetes-Secret.

## Ziel-Konfiguration

```yaml
actionHandlers:
  - action-name: deploy-feature          # eindeutige Aktion-ID (wie bisher)
    handler: gitlab-pipeline             # NEU: Handler-Typ; fehlt er, gilt action-name (abwärtskompatibel)
    enabled: true
    action-title: Feature deployen
    lucide-icon: rocket
    lucide-icon-color: green
    extra-handler-configs:
      gitlab-url: https://gitlab.mycomp.de
      project: team/foo-deploy           # Projekt-ID oder Pfad
      ref: "main"                        # Template erlaubt, z. B. "feature/{{ .Namespace }}"
      token-env: GITLAB_TRIGGER_TOKEN_FOO
      applies-to: [namespace]            # namespace | service | ingress | pod
      projects: [foo-projekt]            # optional: nur Objekte dieser Dashboard-Projekte
      timeout: 15s                       # optional, Standard 10s
      variables:
        NAMESPACE: "{{ .Namespace }}"
        SERVICE: "{{ .Name }}"
        ACTION: deploy
  - action-name: destroy-feature
    handler: gitlab-pipeline
    # … gleiche Struktur, anderes Projekt/andere Variablen
```

## Änderungen

### 1. Handler-Typ von der Aktion trennen

- `backend/internal/config/config.go`: In `ActionHandlerConfig` kommt das Feld `Handler string \`yaml:"handler"\``
  hinzu, außerdem die Methode `HandlerType()`. Sie liefert `Handler`, oder `Name`, wenn `Handler` leer ist.
- `backend/internal/actions/registry.go`: `Activate` sucht die Factory über `c.HandlerType()` und erzeugt **je
  Config-Eintrag eine eigene Handler-Instanz**. Die Fehlermeldung nennt Aktion und Handler-Typ.
  `delete-pod` und die NodePort-Aktionen bleiben unverändert, weil bei ihnen `handler` fehlt.
- `backend/cmd/dashboard/main.go`: `registry.Register(gitlabpipeline.HandlerType, gitlabpipeline.New)`.
- Tests: In `registry_test.go` und `config_test.go` gibt es einen Fall mit zwei Aktionen auf demselben Handler-Typ.
  Ein weiterer Fall prüft, dass ein fehlendes `handler`-Feld auf `action-name` zurückfällt.

### 2. Neues Paket `backend/internal/actions/gitlabpipeline`

- **`Configure(extra)`** parst und validiert beim Start. Fehler führen zum Startfehler wie bisher:
  - Pflichtfelder: `gitlab-url` (absolute URL), `project`, `ref`, `token-env`, `applies-to` (gültige Kinds über
    `model.Kind.Valid`).
  - `os.Getenv(token-env)` darf nicht leer sein. Ein fehlendes Token fällt so schon beim Start auf.
  - `ref` und alle `variables` werden mit `text/template` und `Option("missingkey=error")` vorab geparst.
  - Für `timeout` gibt es einen eigenen `http.Client` mit diesem Timeout.
- **`IsApplicable(obj)`**: Der Kind steht in `applies-to`. Ist `projects` gesetzt, muss zusätzlich das Projekt
  des Objekts enthalten sein. Das Projekt kommt bei Namespaces direkt aus `StoredNamespace.Project`, bei anderen
  Objekten aus dem Namespace im Store über `Deps.Store`. Diese Lesemöglichkeit wurde für die NodePort-Aktion
  eingeführt.
- **`FormFields`**: `nil`. Das Frontend zeigt wie gewohnt nur den Bestätigungsdialog.
- **Template-Daten** (`templateData`):
  - `Kind`, `Name`, `Namespace`, `Project`, `Labels`, `Annotations` des Objekts.
  - Gelesen über eine kleine Hilfsfunktion, die aus `actions.StoredObject` die `model.Meta` holt. Dazu gibt es einen
    Type-Switch über `StoredNamespace`, `StoredService`, `StoredIngress` und `StoredPod`.
- **`Execute`**:
  1. Templates ausführen. Ein Fehler führt zu `Result{Success: false}`.
  2. `POST {gitlab-url}/api/v4/projects/{url.PathEscape(project)}/trigger/pipeline` als
     `application/x-www-form-urlencoded` mit `token`, `ref` und `variables[KEY]=value`. Der Request nutzt den
     Request-Context.
  3. Bei 201 wird `{id, web_url}` gelesen. Ergebnis ist z. B. „Pipeline #123 gestartet: https://…“.
  4. Bei anderem Status kommt eine Fehlermeldung mit Status und GitLab-`message`, gekürzt.
  5. **Das Token erscheint nie in Log oder Result.** Das gilt auch für URL-Fehler von `net/http`, die den Body nicht
     enthalten.
- **Tests (`gitlabpipeline_test.go`)** mit `httptest.NewServer` als GitLab-Ersatz und `t.Setenv` für das Token:
  - Pfad mit escaptem Projektpfad.
  - Form-Werte inklusive gerenderter Variablen und `ref`-Template.
  - Erfolg mit Pipeline-ID und URL in der Meldung.
  - Fehlerstatus 400/401 mit GitLab-Message.
  - Applicability nach `applies-to` und `projects`.
  - Config-Fehler: fehlendes Token, ungültiger Kind, kaputtes Template.
  - Prüfung, dass das Token nicht in der Result-Message auftaucht.

### 3. Deployment und Config

- `backend/deploy/deployment.yaml`: Am Container kommt
  `envFrom: [{secretRef: {name: feature-dashboard-gitlab, optional: true}}]` hinzu. Ein neues Manifest
  `backend/deploy/gitlab-secret.example.yaml` enthält ein Beispiel-Secret mit Platzhaltern, ohne echte Tokens.
- `backend/config.example.yaml`: Ein auskommentiertes Beispiel für eine `gitlab-pipeline`-Aktion. Auskommentiert
  bleibt es, damit `make run` ohne Token-Variable weiter startet. `deploy/config.yaml` bleibt unverändert, bis
  echte Pipelines feststehen.
- Kein Frontend-Umbau. Titel, Icon, Farbe und Result-Message kommen wie bei allen Aktionen aus dem Backend.
  `openapi.yaml` bleibt ebenfalls unverändert, die API ändert sich nicht.

### 4. Doku (`CLAUDE.md`)

- Im Abschnitt zur Konfiguration der ActionHandler kommt das Feld `handler` dazu: Es ist ein Handler-Typ, der
  mehrere Aktionen bedienen kann. „Je aktivierter Aktion genau ein ActionHandler“ bleibt wahr, weil es je Aktion eine
  eigene Instanz gibt.
- Die Paket-Tabelle bekommt den Eintrag `internal/actions/gitlabpipeline`, dazu ein kurzes Config-Beispiel und den
  Hinweis, dass Tokens nur über Env-Variablen bzw. das Secret kommen.

## Verifikation

- `make lint test` im Repository-Root.
- Lokal mit einem echten GitLab:
  - Ein Trigger-Token im Projekt anlegen (Settings → CI/CD → Pipeline trigger tokens).
  - `export GITLAB_TRIGGER_TOKEN_FOO=…`, das Beispiel in einer eigenen Config aktivieren und
    `make run CONFIG=…` starten.
  - In der Namespace-Seite die Aktion auslösen und prüfen, dass die Pipeline mit den Variablen in GitLab erscheint
    und die Meldung den Pipeline-Link zeigt.
- Ohne gesetzte Env-Variable muss das Backend mit einer klaren Fehlermeldung beim Start abbrechen.
