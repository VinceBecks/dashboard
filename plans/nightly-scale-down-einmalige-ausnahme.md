# Plan: Ausnahme vom nächtlichen Herunterskalieren gilt nur für eine Nacht + Umzug nach `cmd/renew/`

## Context

Der CronJob `nightly-scale-down` liest bisher die Namespaces mit dem Ausnahme-Label
(`nightlyscaledown.ExclusionLabelKey`=`ExclusionLabelValue`) und übergibt sie der Pipeline als `EXCLUDE_NAMESPACES`.
Das Label bleibt danach stehen, die Ausnahme gilt also dauerhaft. Neu: Die Ausnahme gilt nur für den nächsten Lauf.
Nach erfolgreichem Pipeline-Start entfernt der CronJob das Label an allen gelesenen Namespaces, sodass sie beim
nächsten Lauf wieder herunterskaliert werden (außer jemand setzt die Ausnahme im Dashboard erneut).
Außerdem zieht der CronJob-Code nach `backend/cmd/renew/nightly-scale-down/`.

Entscheidungen (mit Nutzer abgestimmt):
- Zielordner: `backend/cmd/renew/nightly-scale-down/` (komplettes Paket `main`: `main.go`, `scaledown.go`,
  `scaledown_test.go`). Binary bleibt `/nightly-scale-down`.
- Reihenfolge: Label erst **nach** erfolgreichem Pipeline-Start entfernen. Schlägt der Start fehl, bleibt die
  Ausnahme bestehen.

## Schritte

### 0. Plan ablegen
Diesen Plan nach `plans/nightly-scale-down-einmalige-ausnahme.md` kopieren (CLAUDE.md-Vorgabe).

### 1. Umzug
`git mv backend/cmd/nightly-scale-down backend/cmd/renew/nightly-scale-down` (Paket bleibt `main`, Importe
unverändert). Pfade anpassen:
- `Dockerfile:21` → `./cmd/renew/nightly-scale-down`
- `Makefile:55` → `./cmd/renew/nightly-scale-down`

### 2. Label entfernen (`backend/cmd/renew/nightly-scale-down/scaledown.go`)
- Neue Funktion `removeExclusions(ctx, client kubernetes.Interface, names []string) error`:
  je Namespace JSON-Merge-Patch `{"metadata":{"labels":{ExclusionLabelKey: null}}}` – gleiches Muster wie
  `nightlyscaledown.Handler.Execute` (`backend/internal/actions/nightlyscaledown/nightlyscaledown.go:87ff`).
  Alle Namespaces versuchen, Fehler mit `errors.Join` sammeln (je Fehler Namespace-Name nennen).
  `NotFound` (Namespace inzwischen gelöscht) ignorieren (`apierrors.IsNotFound`).
- `startScaleDown`: nach erfolgreichem `st.Start` → `removeExclusions(ctx, client, excluded)`. Fehler daraus
  zurückgeben (zusammen mit der bereits gestarteten Pipeline, damit `main` sie trotzdem loggt).
- Hinweis zur Race: Setzt jemand das Label zwischen Lesen und Entfernen neu, wird es mit entfernt — akzeptiert
  (Lauf dauert Sekunden). Kein Optimistic Locking.
- Kommentare im Datei-/Funktionskopf auf das neue Verhalten anpassen.

### 3. `main.go`
- Pipeline-Start auch loggen, wenn danach das Entfernen fehlschlägt; dann Exit-Code 1 mit eigener Meldung
  („Ausnahme-Label konnte nicht entfernt werden“). Kein Retry (bleibt bei `backoffLimit: 0`).
- Bei Erfolg zusätzlich loggen: „Ausnahme-Labels entfernt“, `count`, `namespaces`.
- Paket-Kommentar ergänzen.

### 4. Tests (`scaledown_test.go`)
- `TestStartScaleDown`: nach dem Lauf haben `foo-feat-1`/`foo-feat-2` kein `key`-Label mehr, andere Labels
  (`"a": "b"`) bleiben; `foo-feat-3` (`false`) bleibt unverändert.
- Start-Fehler → Labels bleiben erhalten.
- Patch-Fehler (Reactor auf `patch namespaces` am Fake-Client) → Fehler wird gemeldet, Pipeline trotzdem
  zurückgegeben, übrige Namespaces werden dennoch bearbeitet.
- NotFound beim Patch → kein Fehler.

### 5. Deployment (`backend/deploy/nightly-scale-down.yaml`)
ClusterRole: Verben `[list, patch]` auf `namespaces` (Kommentar: Label nach dem Start entfernen).
Kopfkommentar ergänzen.

### 6. Doku
- `CLAUDE.md`: Paket-Tabelle (`cmd/renew/nightly-scale-down`, Logik inkl. Label entfernen), Abschnitt
  „CronJob `nightly-scale-down`“ (Pfad, Ausnahme gilt nur für einen Lauf, RBAC `list` + `patch`), Abschnitt zu
  `nightly-scale-down-exclusion` (Ausnahme gilt nur für die nächste Nacht).
- `README.md` (~Z. 990ff): Pfad, neuer Ablaufschritt „Label entfernen“, RBAC; Abschnitt
  `nightly-scale-down-exclusion` (~Z. 482) um „nur für die nächste Nacht“ ergänzen.
- `erklaerung.md:271`: Pfad.
- Optional die Beispiel-`action-title` in `backend/deploy/config.yaml` / CLAUDE.md auf „Heute Nacht nicht
  herunterskalieren“ anpassen.
- Alte Pläne unter `plans/` bleiben unverändert (historisch).

Spring-Boot-Implementierung (`src/`) hat keinen CronJob und ist nicht betroffen.

## Verifikation
- `make test` und `make lint` (inkl. neuer Tests im verschobenen Paket).
- `make build` → `backend/bin/nightly-scale-down` entsteht weiterhin.
- `make docker IMAGE=…` baut mit neuem Pfad.
- Optional manuell gegen Testcluster: Namespace labeln, `make nightly-scale-down-run` mit Test-Pipeline,
  danach `kubectl get ns -L mycomp.de/skip-nightly-scale-down` → Label weg.
