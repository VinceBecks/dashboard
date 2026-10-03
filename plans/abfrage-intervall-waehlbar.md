# Plan: Abfrage-Intervall im Frontend wählbar (Liste und Default aus dem Backend)

## Kontext

Das Frontend fragt das Backend fest alle 10 s ab (`POLL_INTERVAL_MS` in `frontend/src/config.ts`, genutzt von
`usePolling`). Künftig liefert das Backend die wählbaren Intervalle, einen Default und eine Sperrzeit für die
manuelle Aktualisierung. Oben rechts in der AppBar wählt der User das Intervall per Dropdown. Der Wert `0s` bedeutet:
kein automatisches Abfragen, stattdessen ein „Aktualisieren“-Icon; nur ein Klick darauf lädt neu. Damit niemand
schnell hintereinander klickt, ist der Button nach einem Klick für die Sperrzeit aus dem Backend gesperrt.

Entscheidungen mit dem User: Die Auswahl wird **nicht gespeichert** (nach dem Neuladen gilt der Default). Die
**Sperrzeit kommt aus der `config.yaml`**.

## Backend

### `config.yaml` – neuer optionaler Abschnitt `frontend`

```yaml
frontend:
  polling-intervals: [0s, 5s, 10s, 30s, 1m]   # Go-Dauern (time.ParseDuration); 0s = nur manuell
  default-polling-interval: 10s               # muss in polling-intervals stehen
  manual-refresh-cooldown: 3s                 # Sperrzeit des Aktualisieren-Buttons
```

`backend/internal/config/config.go`:
- `FrontendConfig{PollingIntervals []Duration; DefaultPollingInterval Duration; ManualRefreshCooldown Duration}`,
  Feld `Frontend FrontendConfig \`yaml:"frontend"\`` in `Config`.
- Neuer Typ `Duration{Value time.Duration; Text string}` mit `UnmarshalYAML` über `time.ParseDuration` (damit
  Sekunden gehen). `Text` ist der Original-Text aus der YAML, damit `1m` als `1m` angezeigt wird und nicht als `1m0s`.
  `Interval`/`ParseInterval` bleiben unverändert (der Store nutzt sie weiter).
- Defaults, wenn Felder fehlen: Intervalle `[0s, 5s, 10s, 30s, 1m]`, Default `10s`, Sperrzeit `3s`
  (Defaults setzen in `Parse`, wie für andere optionale Werte üblich).
- Validierung (sammelt in `errs` wie die bestehende Validierung): keine negativen Werte, keine Duplikate,
  Default in der Liste, Sperrzeit ≥ 1s (sonst wäre der Klickschutz wirkungslos).

### API `GET /api/polling` (`backend/internal/api/server.go`)

Analog zu `handleCluster` (statisch, `notModified(w, r, s.startedAt)` als ETag), Setter `WithFrontend(cfg.Frontend)`,
Aufruf in `backend/cmd/dashboard/main.go` neben `WithCluster`.

```json
{
  "intervals": [{ "value": "0s", "milliseconds": 0 }, { "value": "10s", "milliseconds": 10000 }],
  "defaultInterval": "10s",
  "manualRefreshCooldownMilliseconds": 3000
}
```

- `value` ist der Text aus der `config.yaml` (Anzeige + ID), `milliseconds` die Zahl für den Timer – so muss das
  Frontend keine Go-Dauern parsen.
- `openapi.yaml`: Pfad `/api/polling` und Schemas `PollingResponse`, `PollingInterval` ergänzen.

### Tests

- `config_test.go`: Defaults ohne Abschnitt; gültige Konfiguration; Fehler bei Default nicht in Liste, Duplikat,
  negativem Wert, ungültiger Dauer, Sperrzeit < 1s.
- `server_test.go` (bestehendes Muster für `/api/cluster`): Antwort-JSON und 304 mit ETag.

## Frontend

### API-Typen und Konstanten
- `src/api/types.ts`: `PollingResponse`, `PollingInterval` gemäß `openapi.yaml`.
- `src/config.ts`: `POLL_INTERVAL_MS` entfällt als Standard für Seiten; bleibt nur als Fallback, falls
  `/api/polling` nicht ladbar ist (dann: Intervall 10 s, Sperrzeit 3 s, Dropdown nur mit diesem Wert, Warnung via
  `PollError`). `CLUSTER_POLL_INTERVAL_MS` bleibt unverändert.

### `src/hooks/PollingSettings.tsx` (neu): Context + Provider
- Lädt `/api/polling` einmal beim Start (`getConditional` ohne ETag). Bis dahin zeigt der Provider `<Loading />`.
- Context-Werte: `intervals`, `selected` (Wert-String), `intervalMs`, `setSelected`, `cooldownMs`,
  `refreshToken` (Zähler), `refreshAll()` (erhöht den Zähler).
- Startwert = `defaultInterval`; nicht persistiert.

### `src/hooks/usePolling.ts`
- Ohne expliziten `intervalMs` nutzt der Hook `intervalMs` und `refreshToken` aus dem Context; beide stehen in den
  Effect-Dependencies → Intervallwechsel und Klick auf „Aktualisieren“ lösen sofort eine Abfrage aus.
- `intervalMs === 0`: nach der Abfrage **keinen** Timer setzen (nur Erstabfrage, URL-Wechsel, `refresh()` aus
  Aktionen und `refreshAll()`).
- Mit explizitem Intervall (Cluster-Angaben in `NamespacePage`) unverändert und unabhängig von der Auswahl.

### `src/components/PollingControl.tsx` (neu), eingebunden in `App.tsx`
- In der `Toolbar` rechtsbündig (`Box sx={{ flexGrow: 1 }}` als Abstandhalter): MUI-`Select` (size small) mit
  allen Intervallen; Label „Aktualisierung“, Eintrag `0s` als „Manuell“, sonst der `value`.
- Bei `intervalMs === 0` zusätzlich `IconButton` mit Lucide `RefreshCw`, Tooltip „Aktualisieren“. Klick →
  `refreshAll()` und Button für `cooldownMs` deaktivieren (Timer per `setTimeout`, Aufräumen beim Unmount);
  währenddessen dreht sich das Icon (CSS-Animation via `sx`). Mehrfachklicks in der Sperrzeit sind dadurch
  unmöglich.
- Provider in `App.tsx` um `AppBar` und `Routes` legen.

### Tests (Vitest, bestehende Muster `usePolling.test.tsx`, `test/fetchMock.ts`)
- `usePolling`: mit Context-Intervall 0 genau eine Abfrage, weitere erst nach `refreshAll()`.
- `PollingControl.test.tsx`: Dropdown zeigt die Werte aus `/api/polling` mit Default; bei „Manuell“ erscheint der
  Button; zweiter Klick innerhalb der Sperrzeit löst keine Abfrage aus (Fake-Timers), danach wieder.
- Bestehende Seiten-Tests (`NamespacePage.test.tsx`, …) bei Bedarf in den Provider einbetten bzw. `/api/polling`
  im `mockFetch` beantworten.

## Dokumentation
- `CLAUDE.md`: Konfiguration um Abschnitt `frontend` (Beispiel + Regeln), API-Liste um `GET /api/polling`,
  „Datenaktualisierung“ um Dropdown/„0s“/Sperrzeit, „Getroffene Entscheidungen“: „Polling alle 10 Sekunden“ →
  „Polling-Intervall wählbar, Liste/Default/Sperrzeit aus der `config.yaml`, Auswahl nicht gespeichert“;
  Frontend-Struktur um `PollingSettings`/`PollingControl`.
- `README.md`: entsprechende Abschnitte (Konfiguration, API, Bedienung).
- `backend/config.example.yaml` und `backend/deploy/config.yaml`: Abschnitt `frontend` ergänzen.
- `backend/internal/api/openapi.yaml`: siehe oben.

## Verifikation
1. `make test` und `make lint`.
2. `make run`, Browser auf `localhost:8080`: Dropdown oben rechts zeigt Default `10s`; im Netzwerk-Tab Abfragen im
   gewählten Takt; Wechsel auf „Manuell“ → keine automatischen Abfragen, Icon erscheint; Klick lädt neu und ist
   für die Sperrzeit gesperrt; `/api/swagger` zeigt `/api/polling`.
