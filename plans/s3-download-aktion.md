# Plan: S3-Download-Aktion mit nachgeladenen Formular-Feldern

## Context

Zu Services liegen Dateien in einem self-hosted S3-Bucket unter `<namespace>/<service>/…`. Entwickler sollen sie über
eine Aktion am Service herunterladen: Beim Klick lädt das Frontend die Auswahl (multi-select, eine Option je Datei)
nach, danach liefert die Aktion eine Datei (bei genau einer) bzw. ein ZIP (bei mehreren). Heute kennt die API nur
`formFields` direkt am Action-DTO (bei jedem Snapshot/Event ausgewertet) und Antworten als `{success,message}`-JSON.
Beides passt nicht: S3 darf nicht bei jedem Snapshot abgefragt werden, und die Antwort ist eine Datei.

Entscheidungen (mit dem Nutzer geklärt): minio-go v7; Download über `PATCH /api/actions/{action}` (Antwort = Datei
statt JSON); Flag am Action-DTO + eigener Endpunkt fürs Nachladen; Prefix als Go-Template, Auflistung rekursiv.

## Änderungen

### 1. `internal/actions` (Interfaces, Registry)
- `actions.go`: zwei **optionale** Interfaces (bestehende Handler bleiben unverändert):
  - `LazyFormHandler { LoadFormFields(ctx, obj StoredObject) ([]FormField, error) }`: `FormFields(obj)` liefert
    bei solchen Handlern weiterhin `nil` (kein S3-Zugriff im Snapshot).
  - `DownloadHandler { Download(ctx, obj StoredObject, inputs map[string]any) (File, error) }` mit
    `File{Name, ContentType string; Size int64 (-1 = unbekannt); Write func(io.Writer) error}`.
    Fehler für den Nutzer als `UserError`-Typ (→ 422, Meldung nutzerfreundlich), andere → 500/502.
- `registry.go`:
  - `Action`-DTO: neues Feld `LazyFormFields bool` (`json:"lazyFormFields,omitempty"`), gesetzt in `ActionsFor`,
    wenn der Handler `LazyFormHandler` ist.
  - `FormFieldsFor(ctx, name, obj) ([]FormField, error)`: `find` → `applicable` → `LoadFormFields`
    (nicht-lazy Handler: `ErrNotLazy` → 400).
  - `Download(ctx, name, obj, inputs) (File, error)`: `find` → `applicable` → bei Lazy-Handlern Felder per
    `LoadFormFields` laden und mit dem vorhandenen privaten `validate` prüfen (Optionen = aktuelle S3-Liste, damit
    nur existierende Keys herunterladbar sind) → `Download`. `Execute` bleibt für die JSON-Aktionen.
  - Zugriff auf Handler-Typ: `Has`/`find` wiederverwenden; neue Methode `IsDownload(name) bool`.

### 2. `internal/actions/s3download` (neuer Handler-Typ `s3-download`)
- Konstante `HandlerType = "s3-download"`, Feld-ID `files`. Factory wie in `missingservices` (`New(deps)`),
  Konfig per `gitlabpipeline.DecodeExtra` + `errors.Join`.
- `extra-handler-configs`:
  ```yaml
  extra-handler-configs:
    endpoint: https://s3.mycomp.de       # Schema bestimmt TLS; Port optional
    bucket: feature-files
    prefix: "{{ .Namespace }}/{{ .Name }}/"   # Go-Template (objecttemplate), wird mit "/" abgeschlossen
    access-key-id-env: S3_ACCESS_KEY_ID        # Namen der Env-Variablen (Pflicht, müssen gesetzt sein)
    secret-access-key-env: S3_SECRET_ACCESS_KEY
    region: us-east-1                          # optional
    path-style: true                           # optional, Standard true (self-hosted)
    timeout: 30s                               # optional (List/Stat)
    service-selectors:                         # Pflicht, mind. ein Eintrag, keine leeren Einträge
      - app.kubernetes.io/part-of: shop        # UND innerhalb eines Eintrags
        app.kubernetes.io/component: backend
      - app.kubernetes.io/part-of: billing     # ODER über die Einträge
  ```
- `IsApplicable`: `Kind == service` und Service-Labels treffen `service-selectors`
  (`labelselector.MatchesAny`, siehe Abschnitt 2a).
- `LoadFormFields`: Prefix rendern (`objecttemplate.NewData`), `ListObjects` rekursiv, Verzeichnis-Marker (`/`) überspringen,
  Obergrenze (z. B. 1000) mit Hinweis in `Description`; ein `multi-select`-Feld `files`, `IsRequired: true`,
  Option: `Value` = relativer Pfad unter dem Prefix, `Label` = relativer Pfad + Größe. Keine Dateien → leere
  Optionen, Beschreibung „Keine Dateien vorhanden" (Ausführung scheitert an der Pflichtvalidierung).
- `Download`: Key = Prefix + Auswahl (nur validierte Werte). Eine Datei: `Stat` vorab (Fehler vor dem Header),
  `File{Name: basename, ContentType, Size}`, Inhalt per `io.Copy` aus `GetObject`. Mehrere: `File{Name: "<service>.zip",
  application/zip, Size -1}` mit `zip.Writer` direkt auf den Response gestreamt (kein Zwischenspeicher), Ordnerstruktur
  relativ zum Prefix erhalten, vorab `Stat` aller Keys.
- S3-Zugriff hinter kleinem Interface (`objectStore`: List/Stat/Get) mit minio-go-Adapter, damit Tests ohne Netz laufen.
- minio-go: `Options.Transport: http.DefaultTransport` setzen (CLAUDE.md: Proxy/CA über Standard-Transport, kein
  eigener Transport). Neue Abhängigkeit `github.com/minio/minio-go/v7` in `go.mod`.

### 2a. Neues Paket `internal/labelselector` (Label-Abgleich aus `project` herausgelöst)
Der Code zum Prüfen von Labels wird in einem eigenen, rein funktionalen Paket (importiert weder `config` noch
`project`) zusammengeführt, da er nun von drei Stellen genutzt wird:
- `HasAll(labels, required map[string]string) bool` (bisher `hasAll` in `project/matcher.go`)
- `MatchesAny(labels map[string]string, combinations []map[string]string) bool` (bisher `matchesAny` in
  `project/areas.go`; ODER über Einträge, UND innerhalb, leere Einträge treffen nie)
- `Validate(path string, combinations []map[string]string) []error`: Liste nicht leer, keine leeren Einträge (bisher inline
  in `config.validateServiceAreas`, `config.go` ~Z. 301–308; Fehlertexte bleiben gleich, `path` ersetzt `at`)
Anpassungen: `project/matcher.go` und `project/areas.go` nutzen `labelselector.*` und verlieren ihre privaten Funktionen;
`config.validateServiceAreas` ruft `labelselector.Validate(at+".label-combinations", …)`; `s3download` nutzt
`MatchesAny` und `Validate`. Tests (ODER/UND, leerer Eintrag) wandern aus `project/areas_test.go` nach
`labelselector/labelselector_test.go`; die project-Tests prüfen weiter `ServiceAreaIDs`/`Match`.
`CLAUDE.md`-Paketetabelle um `internal/labelselector` ergänzen.

### 3. `internal/api`
- `server.go`:
  - Neue Route `POST /api/actions/{action}/form-fields`, Body `{object}`; Antwort 200 `{"formFields":[…]}`;
    Fehler wie `handleAction` (404 unbekannt, 400 ungültig/nicht anwendbar/nicht lazy, 404 Objekt, 502 bei S3-Fehler
    mit nutzerfreundlicher Meldung als `{"error":…}`).
  - `handleAction`: ist `registry.IsDownload(name)`, wird `registry.Download` aufgerufen. Erfolg: Header
    `Content-Type`, `Content-Disposition: attachment; filename*=UTF-8''…` (+ Content-Length falls bekannt), dann
    `File.Write(w)`. Fehler vor dem ersten Byte: wie bisher JSON (400 Validierung, 422 Nutzerfehler, 500). Bricht das
    ZIP-Streaming nach dem Header ab, Verbindung abbrechen (`panic(http.ErrAbortHandler)`) und loggen, damit der
    Client keinen scheinbar vollständigen Download erhält.
  - Beachten: `http.Server` hat kein `WriteTimeout` (passt für lange Downloads); `MaxBytesReader` bleibt.
- `openapi.yaml`: neuer Pfad `form-fields`; `PATCH`-Response 200 zusätzlich als
  `application/octet-stream`/`application/zip` (binary) für Download-Aktionen; `Action.lazyFormFields`;
  Schema `FormFieldsResponse`. `example-api.jsonc`: Beispiel-Aktion mit `lazyFormFields: true`.

### 4. Verdrahtung und Deploy
- `cmd/dashboard/main.go`: `registry.Register(s3download.HandlerType, s3download.New)`.
- `backend/config.example.yaml` und `backend/deploy/config.yaml`: Beispiel-Aktion `download-service-files`.
- `backend/deploy/s3-secret.example.yaml` (`S3_ACCESS_KEY_ID`, `S3_SECRET_ACCESS_KEY`); `deployment.yaml`: weiteres
  `secretRef` (`feature-dashboard-s3`, `optional: true`); `network.example.yaml`: Hinweis, dass ein
  cluster-interner S3 in `NO_PROXY` gehört. RBAC unverändert.
- `CLAUDE.md`: neuer Handler/Paket in der Tabelle, Abschnitt zur Konfiguration, API-Endpunkt
  `POST /api/actions/{id}/form-fields` und Download-Antwort, Leitplanke „Lazy-Felder nie in `FormFields`".
  Plan zusätzlich nach `plans/s3-download-aktion.md` (Repo-Vorgabe) übernehmen.

## Tests
- `s3download`: Configure (Pflichtfelder, fehlende Env, leere/fehlende Selektoren, Template-Fehler), `IsApplicable`
  (ODER/UND, falscher Kind), `LoadFormFields` (Prefix, rekursiv, Marker, Limit), `Download` eine Datei / ZIP-Inhalt
  und Struktur / unbekannter Key abgelehnt, mit Fake-`objectStore`.
- `registry_test`: `ActionsFor` setzt `lazyFormFields` ohne Felder, `FormFieldsFor`, `Download`-Validierung.
- `server_test`: form-fields-Endpunkt (200/400/404), PATCH-Download (Header, Body, Fehlerpfade als JSON); bestehende
  JSON-String-Assertions bleiben durch `omitempty` unverändert.
- `labelselector`-Tests (siehe 2a); bestehende `project`- und `config`-Tests müssen unverändert grün bleiben.

## Verifikation
1. `make lint && make test` (race).
2. Lokal ein MinIO starten (`docker run minio/minio`), Testdateien unter `ns/svc/` ablegen, Env setzen,
   `make run CONFIG=…` mit der Beispielaktion; per curl:
   - `GET /api/snapshot`: Aktion trägt `lazyFormFields: true`, kein `formFields`.
   - `POST /api/actions/<id>/form-fields` → Optionen je Datei.
   - `PATCH /api/actions/<id>` mit einer Datei → Datei; mit zwei → ZIP (`unzip -l`); mit leerer Auswahl → 400.
3. Prüfen, dass Snapshot/Events keinen S3-Zugriff auslösen (MinIO-Log).
