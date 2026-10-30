# Plan: Namespace vom nächtlichen Herunterskalieren ausnehmen (Handler-Typ `namespace-label`)

> **Nachtrag:** Nach der Umsetzung umbenannt in Handler-Typ `nightly-scale-down-exclusion`, Paket
> `internal/actions/nightlyscaledown`; Meldungen fachlich formuliert. Der Handler ist nur für das nächtliche
> Herunterskalieren gedacht, nicht als generischer Label-Handler.

> **Nachtrag (festes Label):** Das Ausnahme-Label ist inzwischen fest im Code
> (`nightlyscaledown.ExclusionLabelKey = "mycomp.de/skip-nightly-scale-down"`, `ExclusionLabelValue = "true"`).
> `label-key`/`label-value` und `SCALE_DOWN_EXCLUSION_LABEL_KEY`/`_VALUE` gibt es nicht mehr, siehe
> `plans/scale-down-label-konstant.md`.

## Kontext

Eine externe Anwendung skaliert jede Nacht alle Feature-Umgebungen auf 0 Replicas. Ein Label am Namespace soll
sagen: diesen Namespace auslassen. Entwickler sollen das Label im Dashboard setzen („markieren“) und wieder
entfernen („Markierung aufheben“) können.

Vorgaben des Nutzers: **ein** ActionHandler, in der `config.yaml` als **zwei Aktionen** konfiguriert. Markieren wird
nur angeboten, wenn das Label fehlt; Aufheben nur, wenn es gesetzt ist.

Gemäß CLAUDE.md wird dieser Plan bei der Umsetzung zusätzlich als `plans/scale-down-ausnahme.md` abgelegt.

## Entscheidungen

- **„Enabled“ = angeboten.** Die API kennt keine deaktivierten Aktionen. `IsApplicable` entscheidet, ob die Aktion
  am Namespace erscheint. Nach der Ausführung kommt die Label-Änderung über den Informer. Der Event-Hub baut den
  Namespace neu und schickt ein `upsert`, in dem statt „Markieren“ die Aktion „Aufheben“ steht (und umgekehrt).
  Kein Frontend-Code nötig.
- **Handler-Typ generisch: `namespace-label`** (Paket `internal/actions/namespacelabel`). Er setzt oder entfernt
  ein konfiguriertes Label. Fachlich ist nichts fest im Code, Label und Bedeutung kommen aus der Config. So ist er
  später für andere Markierungen wiederverwendbar.
- **Aktionen (Namen/Titel):**

  | `action-name`                     | `action-title`                                   | Icon | Farbe     | `mode`   |
  |-----------------------------------|--------------------------------------------------|------|-----------|----------|
  | `exclude-from-nightly-scale-down` | Nachts nicht herunterskalieren                   | `sun`  | `#f9a825` | `mark`   |
  | `include-in-nightly-scale-down`   | Nächtliches Herunterskalieren wieder zulassen    | `moon` | `#455a64` | `unmark` |

- **„Gesetzt“ heißt:** Label-Key vorhanden **mit dem konfigurierten Wert**. Andere Werte gelten als „nicht
  gesetzt“: Markieren wird angeboten und überschreibt den Wert.
- Namespaces werden nicht per ArgoCD self-heal verwaltet (wie bei `assign-team`), das Label bleibt bestehen.
  RBAC `patch` auf `namespaces` gibt es schon.

## Konfiguration

```yaml
action-handlers:
  - action-name: exclude-from-nightly-scale-down
    handler: namespace-label
    action-title: Nachts nicht herunterskalieren
    icon: sun
    icon-color: "#f9a825"
    projects: [foo-projekt]
    extra-handler-configs: &skip-scale-down
      label-key: mycomp.de/skip-nightly-scale-down   # Label, das die Skalierungs-Anwendung auswertet
      label-value: "true"                            # optional, Standard "true"
      mode: mark
  - action-name: include-in-nightly-scale-down
    handler: namespace-label
    action-title: Nächtliches Herunterskalieren wieder zulassen
    icon: moon
    icon-color: "#455a64"
    projects: [foo-projekt]
    extra-handler-configs:
      <<: *skip-scale-down
      mode: unmark
```

Der Label-Key ist ein Platzhalter. Er muss zu dem passen, was die nächtliche Skalierungs-Anwendung auswertet.

## Umsetzung

### 1. Handler `backend/internal/actions/namespacelabel/namespacelabel.go`

Aufbau wie `internal/actions/assignteam/assignteam.go`:

- `const HandlerType = "namespace-label"`, `ModeMark = "mark"`, `ModeUnmark = "unmark"`.
- `settings` mit yaml-Tags `label-key`, `label-value`, `mode`. Auslesen über `gitlabpipeline.DecodeExtra`
  (`internal/actions/gitlabpipeline/gitlabpipeline.go:73`), so wie es `missingservices` schon tut.
- `Configure`: `label-key` Pflicht und gültig (`validation.IsQualifiedName` aus `k8s.io/apimachinery/pkg/util/validation`).
  `label-value` Standard `"true"`, gültig (`validation.IsValidLabelValue`). `mode` Pflicht, `mark` oder `unmark`.
  Fehler werden mit `errors.Join` gesammelt und brechen den Start ab.
- `IsApplicable`: nur `model.KindNamespace`. `marked := ns.Labels[key] == value`. Bei `mark` gilt `!marked`,
  bei `unmark` gilt `marked`.
- `FormFields`: `nil` (nur Bestätigungsdialog).
- `Execute`: JSON-Merge-Patch `{"metadata":{"labels":{key: value}}}` bzw. `{key: null}` auf `Namespaces().Patch`
  (wie `assignteam`). Andere Labels bleiben unberührt.
  - Erfolg: generische Meldung, da der Handler die Bedeutung des Labels nicht kennt: „Label k=v am Namespace X
    gesetzt“ bzw. „Label k am Namespace X entfernt“.
  - Fehler: `Success: false` mit Meldung, die API antwortet dann mit 422.

### 2. Registrierung `backend/cmd/dashboard/main.go`

`registry.Register(namespacelabel.HandlerType, namespacelabel.New)`

### 3. Tests `backend/internal/actions/namespacelabel/namespacelabel_test.go`

Fake-Clientset und Registry wie in `assignteam_test.go`:
- Anwendbarkeit: ohne Label nur `mark`, mit Label nur `unmark`, mit anderem Wert nur `mark`, nie an Pods/Services.
- Ausführen `mark`: Label gesetzt, fremde Labels bleiben. `unmark`: Label entfernt, fremde Labels bleiben.
- Über `Registry.Execute`: `unmark` auf ungelabeltem Namespace ergibt `ErrNotApplicable`.
- `Configure`-Fehler: fehlender/ungültiger Key, ungültiger Wert, fehlender/unbekannter `mode`. Standardwert `"true"`.

### 4. Configs und Doku

- `backend/config.example.yaml`, `backend/deploy/config.yaml`: beide Aktionen wie oben (YAML-Anker).
- `README.md`, „Mitgelieferte Aktionen“: Abschnitt `namespace-label` mit Beispiel, Feldtabelle, Hinweis auf
  Anwendbarkeit, Merge-Patch und RBAC.
- `CLAUDE.md`: Paket-Tabelle (`internal/actions/namespacelabel`) und kurzer Konfig-Abschnitt nach `teams`.
- `erklaerung.md`: Handler-Liste um `namespacelabel` ergänzen.

## Verifikation

- `make lint`, `make test` grün.
- Start mit `config.example.yaml` (Dummy-kubeconfig) läuft durch die Aktivierung der Aktionen.
- Gegen kind: Namespace eines Projekts. `curl localhost:8080/api/snapshot` zeigt am Namespace nur
  `exclude-from-nightly-scale-down`. Dann
  `curl -X PATCH localhost:8080/api/actions/exclude-from-nightly-scale-down -d '{"object":{"kind":"namespace","name":"<ns>"}}'`
  ausführen. Danach gilt: `kubectl get ns <ns> --show-labels` zeigt das Label, und im Stream `/api/events` kommt
  ein `upsert` mit `include-in-nightly-scale-down`. Gegenprobe mit `unmark`.
