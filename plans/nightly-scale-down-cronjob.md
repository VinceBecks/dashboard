# Plan: CronJob `nightly-scale-down` (startet die Pipeline für das nächtliche Herunterskalieren)

## Kontext

Das Herunterskalieren aller Feature-Umgebungen übernimmt eine GitLab-Pipeline. Bisher fehlt der Auslöser. Im
Dashboard können Entwickler Namespaces über das Label der Aktion `nightly-scale-down-exclusion` davon ausnehmen.
Neu ist eine eigene kleine Anwendung, die als Kubernetes-CronJob läuft. Sie liest die markierten Namespaces und
startet die Pipeline mit dem Input `EXCLUDE_NAMESPACES`.

Mit dem Nutzer geklärt:
- `EXCLUDE_NAMESPACES` ist ein **Pipeline-Input** (`spec:inputs`). Er wird als `inputs[EXCLUDE_NAMESPACES]` an die
  Trigger-API übergeben, nicht als Variable.
- Format: **komma-getrennt, sortiert**, z. B. `foo-feat-1,foo-feat-2`. Leer, wenn kein Namespace markiert ist.
- Auswahl: **alle Namespaces mit dem Label** (Label-Selector `<key>=<value>`). Keine `config.yaml`, die
  Konfiguration kommt nur aus Umgebungsvariablen.

Gemäß CLAUDE.md wird dieser Plan bei der Umsetzung zusätzlich als `plans/nightly-scale-down-cronjob.md` abgelegt.

## Ablauf der Anwendung

1. Konfiguration aus Umgebungsvariablen lesen und prüfen. Alle Fehler gesammelt melden, dann Exit-Code 1.
2. Kubernetes-Client über `kube.NewClient("")` (`internal/kube/client.go:14`): im Cluster In-Cluster-Config, lokal
   `KUBECONFIG` bzw. `~/.kube/config`.
3. Ein Aufruf `Namespaces().List(ctx, LabelSelector: key=value)`. Kein Informer, die Namen werden sortiert.
4. Pipeline über die Trigger-API starten mit `ref`, `token` und `inputs[EXCLUDE_NAMESPACES]=<liste>`.
5. Bei Erfolg Pipeline-ID und URL loggen und mit Exit-Code 0 enden. Bei jedem Fehler Exit-Code 1, damit der Job
   als fehlgeschlagen sichtbar wird.

## Umgebungsvariablen

| Variable | Pflicht | Bedeutung |
|---|---|---|
| `SCALE_DOWN_EXCLUSION_LABEL_KEY` | ja | Label-Key; muss zum `label-key` der Dashboard-Aktion passen |
| `SCALE_DOWN_EXCLUSION_LABEL_VALUE` | nein | Label-Wert, Standard `true` (wie `label-value` der Aktion) |
| `GITLAB_URL` | ja | z. B. `https://gitlab.mycomp.de` |
| `GITLAB_PROJECT` | ja | Projekt-ID oder Pfad der Scale-down-Pipeline |
| `GITLAB_REF` | ja | Branch/Tag, z. B. `main` |
| `GITLAB_TRIGGER_TOKEN` | ja | Pipeline-Trigger-Token (aus Secret) |
| `GITLAB_TIMEOUT` | nein | HTTP-Timeout, Standard `10s` |
| `HTTPS_PROXY`/`NO_PROXY`/`SSL_CERT_DIR` | nein | wie beim Backend (Standard-Transport von Go) |

## Umsetzung

### 1. Trigger erweitern, statt GitLab-Code zu duplizieren (`backend/internal/actions/gitlabpipeline/trigger.go`)

- Neue exportierte Methode
  `func (t *Trigger) Start(ctx context.Context, ref string, variables, inputs map[string]string) (Pipeline, error)`
  mit `type Pipeline struct { ID int; WebURL string }`. Sie enthält das bisherige Absenden aus `Run`: Form-Encoding,
  POST, Statusprüfung und `gitlabMessage`. Neu ist `inputs[<key>]` in der Form.
- `Run` rendert wie bisher `ref` und `variables` und ruft dann `Start(ctx, ref, vars, nil)` auf. Sein Verhalten
  ändert sich nicht, die bestehenden Tests sichern das ab.
- `NewTrigger` bleibt wie es ist. Der CronJob übergibt `TriggerConfig{GitLabURL, Project, Ref, TokenEnv: "GITLAB_TRIGGER_TOKEN", Timeout}`.
  `Ref` ist ein Template, ein fester Wert wie `main` wird dabei unverändert übernommen. Der CronJob ruft
  `Start(ctx, ref, nil, inputs)` mit dem Ref aus der Umgebungsvariable direkt auf.

### 2. Neues Paket `backend/internal/scaledowntrigger`

- `Config` mit `FromEnv(getenv func(string) string) (Config, error)`. `getenv` ist austauschbar, damit es sich
  testen lässt. Der Label-Key wird mit `validation.IsQualifiedName` geprüft, der Wert mit `IsValidLabelValue`
  (wie in `internal/actions/nightlyscaledown`).
- `ExcludedNamespaces(ctx, client kubernetes.Interface, key, value string) ([]string, error)`: List mit
  Label-Selector (`labels.SelectorFromSet`), Namen sortiert.
- `const InputExcludeNamespaces = "EXCLUDE_NAMESPACES"`.
- `Run(ctx, cfg, client, trigger) (gitlabpipeline.Pipeline, []string, error)`: Namespaces lesen, mit `,` verbinden,
  `trigger.Start` aufrufen.

### 3. Neue main `backend/cmd/nightly-scale-down/main.go`

Lädt die Config, baut Client und Trigger, ruft `Run` auf und loggt per `slog`:
„N Namespaces ausgenommen: …“ sowie „Pipeline #id gestartet: url“. Fehler führen zu `os.Exit(1)`. Ein Context
mit Gesamt-Timeout (z. B. 2 min) verhindert, dass der Job hängen bleibt.

### 4. Tests

- `trigger_test.go` bzw. `gitlabpipeline_test.go` (bestehender `fakeGitLab`-Helfer): `Start` sendet
  `inputs[EXCLUDE_NAMESPACES]`, `ref` und `token`, liefert ID und URL. Ein Fehlerstatus wird zu einem `error` mit
  der GitLab-Meldung.
- `scaledowntrigger`:
  - `FromEnv`: Pflichtfelder, Standardwerte, ungültiger Key und Wert.
  - `ExcludedNamespaces` mit `fake.NewClientset`: nur passende Labels, ein anderer Wert zählt nicht, sortiert.
  - `Run` gegen einen httptest-GitLab: komma-getrennte Liste und leere Liste.

### 5. Build und Image

- `Makefile`: `backend-build` baut alle Binaries (`go -C backend build -o bin/ ./cmd/...`, ergibt
  `bin/dashboard` und `bin/nightly-scale-down`). Neues Target `nightly-scale-down-run` startet die Anwendung lokal
  (`go -C backend run ./cmd/nightly-scale-down`, Konfiguration aus der Umgebung).
- `Dockerfile`: zweite Binary `/nightly-scale-down` ins **selbe Image**. Der CronJob setzt `command`, der
  `ENTRYPOINT` des Dashboards bleibt.

### 6. Manifeste `backend/deploy/`

- `nightly-scale-down.yaml`:
  - ServiceAccount `nightly-scale-down` und ClusterRole mit nur `list` auf `namespaces`, dazu ein Binding.
  - `CronJob`: `schedule: "0 20 * * *"` mit `timeZone: Europe/Berlin` als Platzhalter, `concurrencyPolicy: Forbid`,
    `backoffLimit: 0`, `restartPolicy: Never`. Kein Retry, damit nie zwei Pipelines starten.
  - Container: gleiches Image, `command: ["/nightly-scale-down"]`.
  - Umgebung: `env` für Label und GitLab, `envFrom` aus dem Secret `nightly-scale-down-gitlab` und der ConfigMap
    `feature-dashboard-network` (optional). Die Company-CA wird wie im Deployment eingehängt.
- `nightly-scale-down-secret.example.yaml`: `GITLAB_TRIGGER_TOKEN`.

### 7. Doku

- `README.md`: neuer Abschnitt „Nächtliches Herunterskalieren (CronJob)“ mit Ablauf, Variablentabelle,
  Pipeline-Beispiel (`spec: inputs: EXCLUDE_NAMESPACES: {type: string, default: ""}`), Deployment und RBAC. Hinweis,
  dass der Label-Key mit der Aktion übereinstimmen muss. Entwicklungs-Tabelle um `nightly-scale-down-run` ergänzen.
- `CLAUDE.md`: Paket-Tabelle (`cmd/nightly-scale-down`, `internal/scaledowntrigger`), kurzer Abschnitt zum CronJob,
  Build-Tabelle.
- `erklaerung.md`: kurzer Absatz zur zweiten Anwendung.

## Verifikation

- `make lint`, `make test` grün.
- Lokal gegen kind, mit einem Namespace mit Label und einem ohne:
  `SCALE_DOWN_EXCLUSION_LABEL_KEY=mycomp.de/skip-nightly-scale-down GITLAB_URL=http://localhost:9999 … make nightly-scale-down-run`
  gegen einen kleinen Fake-GitLab (`python3 -m http.server` reicht nicht, also einen Mini-Handler im Scratchpad).
  Erwartet: Log mit dem markierten Namespace, der Request enthält `inputs[EXCLUDE_NAMESPACES]=<ns>`, Exit 0.
- Fehlende Variable führt zu Exit 1 mit Liste aller fehlenden Variablen.
