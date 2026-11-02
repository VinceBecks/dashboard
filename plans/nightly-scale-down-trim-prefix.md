# Plan: Präfix der ausgenommenen Namespaces per `TRIM_NAMESPACE_PREFIX` entfernen

## Context

Der CronJob `nightly-scale-down` (`backend/cmd/renew/nightly-scale-down/`) übergibt der GitLab-Pipeline die
ausgenommenen Namespaces als Input `EXCLUDE_NAMESPACES` mit ihren vollen Namen. Die Pipeline erwartet sie
(teilweise) ohne gemeinsames Präfix. Neu: Die optionale Umgebungsvariable `TRIM_NAMESPACE_PREFIX` definiert ein
Präfix, das vor der Übergabe entfernt wird, z. B. `foo-` → `foo-feat-1` wird zu `feat-1`.

## Verhalten

- Nur der Pipeline-Input wird gekürzt. Das Entfernen des Ausnahme-Labels (`removeExclusions`) arbeitet weiter mit den
  echten Namespace-Namen.
- Kürzen mit `strings.TrimPrefix`: Namespaces ohne das Präfix bleiben unverändert. Leer/nicht gesetzt = kein Kürzen.
- Danach sortieren und Duplikate entfernen (`slices.Sort` + `slices.Compact`), damit der Input sortiert bleibt.
  Ergibt das Kürzen einen leeren Namen (Namespace heißt genau wie das Präfix), wird er weggelassen.

## Schritte (`backend/cmd/renew/nightly-scale-down/`)

### 1. `scaledown.go`
- Konstante `EnvTrimNamespacePrefix = "TRIM_NAMESPACE_PREFIX"` bei den Env-Namen.
- `config` um `TrimNamespacePrefix string` ergänzen; `configFromEnv` liest es (optional, keine Validierung).
- Neue Funktion `pipelineNamespaces(names []string, prefix string) []string` (kürzen, leere weglassen, sortieren,
  Duplikate entfernen).
- `startScaleDown`: Input `EXCLUDE_NAMESPACES` = `strings.Join(pipelineNamespaces(excluded, cfg.TrimNamespacePrefix), ",")`.
  Rückgabe `excluded` bleibt die echten Namen (für Logging und Label-Entfernung).
- Dateikopf-Kommentar ergänzen.

### 2. `main.go`
- Beim Pipeline-Start-Log zusätzlich den übergebenen Input loggen (`input`), damit gekürzte Werte im Job-Log
  sichtbar sind. Dafür liefert `startScaleDown` den Input-Wert nicht extra zurück, sondern `main` ruft
  `pipelineNamespaces` mit `cfg.TrimNamespacePrefix` selbst auf (reine Funktion).

### 3. Tests (`scaledown_test.go`)
- `TestConfigFromEnv`: `TRIM_NAMESPACE_PREFIX` wird übernommen; fehlt sie, ist das Präfix leer, kein Fehler.
- `TestPipelineNamespaces` (tabellarisch): leeres Präfix unverändert; `foo-` kürzt `foo-feat-1`; Namen ohne Präfix
  bleiben; Name == Präfix fällt weg; Duplikate nach dem Kürzen nur einmal; Ergebnis sortiert.
- `TestStartScaleDown`: mit `TrimNamespacePrefix: "foo-"` ist der Input `feat-1,feat-2`, und die Labels an
  `foo-feat-1`/`foo-feat-2` sind trotzdem entfernt.

### 4. Deployment `backend/deploy/nightly-scale-down.yaml`
Auskommentiertes Beispiel im `env`-Block:
```yaml
                # Optional: Präfix, das von den Namen in EXCLUDE_NAMESPACES entfernt wird.
                # - name: TRIM_NAMESPACE_PREFIX
                #   value: foo-
```

### 5. Doku
- `README.md` (CronJob-Abschnitt): Zeile in der Env-Tabelle (`TRIM_NAMESPACE_PREFIX`, nein, Präfix wird von den
  Namen in `EXCLUDE_NAMESPACES` entfernt; Label-Entfernung nutzt volle Namen), Ablaufschritt 3 ergänzen.
- `CLAUDE.md` (Abschnitt „CronJob `nightly-scale-down`“): optionale Variable `TRIM_NAMESPACE_PREFIX` bei der
  Konfiguration nennen, inkl. Hinweis „nur der Pipeline-Input wird gekürzt“.
- Plan als `plans/nightly-scale-down-trim-prefix.md` ablegen.

## Verifikation
- `make test`, `make lint`, `make build`.
- Optional lokal: `TRIM_NAMESPACE_PREFIX=foo- … make nightly-scale-down-run` gegen Testcluster/Test-Pipeline;
  im Log steht `input=feat-1,...`, Labels an den vollen Namespace-Namen sind entfernt.
