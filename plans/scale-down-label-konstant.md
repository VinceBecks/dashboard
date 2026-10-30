# Plan: Ausnahme-Label für das nächtliche Herunterskalieren fest im Code

## Kontext

Bisher wird das Label, das einen Namespace vom nächtlichen Herunterskalieren ausnimmt, zweimal konfiguriert. Im
Dashboard steht es als `label-key`/`label-value` in den `extra-handler-configs` der Aktion
`nightly-scale-down-exclusion`. Im CronJob steht es als `SCALE_DOWN_EXCLUSION_LABEL_KEY`/`_VALUE`. Passen beide
nicht zusammen, werden markierte Namespaces trotzdem herunterskaliert.

Der Nutzer möchte das Label **fest im Code**, mit dem bisherigen Platzhalter `mycomp.de/skip-nightly-scale-down` und
dem Wert `"true"`. Dashboard-Handler und CronJob nutzen dieselben Konstanten, Config und Umgebungsvariablen dafür
entfallen. `mode: mark | unmark` bleibt in der Config, denn er unterscheidet die beiden Aktionen.

Gemäß CLAUDE.md wird dieser Plan bei der Umsetzung zusätzlich als `plans/scale-down-label-konstant.md` abgelegt.

## Umsetzung

### 1. Konstanten im Handler-Paket (`backend/internal/actions/nightlyscaledown/nightlyscaledown.go`)

```go
// ExclusionLabelKey und ExclusionLabelValue markieren einen Namespace, der nachts nicht herunterskaliert wird.
// Der CronJob nightly-scale-down sucht die Namespaces mit genau diesem Label.
const (
	ExclusionLabelKey   = "mycomp.de/skip-nightly-scale-down"
	ExclusionLabelValue = "true"
)
```

- `settings` enthält nur noch `Mode`. Die Felder `key`/`value` am `Handler` entfallen, stattdessen werden überall die
  Konstanten genutzt (`IsApplicable`, `Execute`).
- `DefaultLabelValue` und die Prüfung mit `validation.IsQualifiedName`/`IsValidLabelValue` entfallen.
- `Configure`: Stehen `label-key` oder `label-value` noch in den `extra-handler-configs`, bricht der Start mit einem
  klaren Fehler ab („label-key/label-value werden nicht mehr konfiguriert, das Label ist fest: …“). So fällt eine
  alte Config auf, statt still ignoriert zu werden. Das gleiche Muster nutzt `gitlabpipeline` schon für `projects`.
  Dafür bekommt `settings` die beiden Felder nur noch zum Erkennen.
- Paket- und Funktionskommentare anpassen.

### 2. CronJob (`backend/cmd/nightly-scale-down/scaledown.go`, `main.go`)

- `EnvLabelKey`, `EnvLabelValue`, `DefaultLabelValue`, die Felder `config.LabelKey`/`LabelValue` sowie ihre Prüfung in
  `configFromEnv` entfallen.
- `excludedNamespaces(ctx, client)` nutzt `nightlyscaledown.ExclusionLabelKey`/`ExclusionLabelValue`, ein Import aus
  dem Handler-Paket. `startScaleDown` ruft es ohne Label-Parameter auf.
- Dateikommentar: Das Label kommt aus `internal/actions/nightlyscaledown`.

### 3. Tests

- `nightlyscaledown_test.go`: `extra(mode)` nur noch mit `mode`. Die Tests nutzen die Konstanten. In `TestConfigure`
  gibt es neue Fehlerfälle für `label-key` und `label-value`. Die Fälle zu ungültigem Key/Wert und zum Standardwert
  entfallen.
- `scaledown_test.go`: Die Env-Fälle für Label-Key/-Wert entfallen. Der Fake-Client nutzt die Konstanten, der
  Namespace mit `"false"` bleibt als Gegenprobe.

### 4. Configs und Manifeste

- `backend/config.example.yaml`, `backend/deploy/config.yaml`: Beide Aktionen enthalten nur noch
  `extra-handler-configs: {mode: mark}` bzw. `{mode: unmark}`. Der YAML-Anker `&skip-scale-down` entfällt.
- `backend/deploy/nightly-scale-down.yaml`: `SCALE_DOWN_EXCLUSION_LABEL_KEY`/`_VALUE` aus `env` entfernen und den
  Kommentar anpassen.

### 5. Doku

- `README.md`:
  - Abschnitt `nightly-scale-down-exclusion`: festes Label nennen und die Feldtabelle auf `mode` reduzieren.
  - Abschnitt CronJob: die beiden Variablen aus der Tabelle entfernen, Hinweis auf das feste Label. Der Hinweis
    „muss zur Aktion passen“ entfällt.
- `CLAUDE.md`: Config-Beispiel und Stichpunkte zu `nightly-scale-down-exclusion` sowie den Abschnitt CronJob anpassen.
- `erklaerung.md`: kurze Anpassung in den Handler- und CronJob-Absätzen.
- `plans/scale-down-ausnahme.md`, `plans/nightly-scale-down-cronjob.md`: Nachtrag zum festen Label.

## Verifikation

- `make lint`, `make test` grün.
- `grep -rn "SCALE_DOWN_EXCLUSION_LABEL\|label-key\|label-value" backend README.md CLAUDE.md` → keine Treffer
  (außer `teams.label-key`).
- Start des Dashboards mit `config.example.yaml` (Dummy-kubeconfig) läuft durch. Mit `label-key` in den
  `extra-handler-configs` bricht er mit der neuen Fehlermeldung ab.
- CronJob-Binary gegen `fake.py` im Scratchpad: Der Request nutzt
  `labelSelector=mycomp.de/skip-nightly-scale-down=true`, ohne die Variable `SCALE_DOWN_EXCLUSION_LABEL_KEY`.
