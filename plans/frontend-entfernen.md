# Plan: Frontend aus dem Repository entfernen

## Kontext

Das Repository soll künftig nur noch das Backend enthalten. Das Frontend (`frontend/`) wird entfernt, ebenso alles,
was es baut, testet oder ausliefert (Makefile, Dockerfile, Doku). **Am Backend (`backend/`) ändert sich nichts** –
auch nicht `--static-dir`, `static.go`, `openapi.yaml` oder die Manifeste in `backend/deploy/`.

Gemäß CLAUDE.md wird dieser Plan bei der Umsetzung zusätzlich als `plans/frontend-entfernen.md` abgelegt.

## Wichtige Konsequenz (kein Code, nur Hinweis)

Das Backend hat **kein CORS**. Ein künftig separat ausgeliefertes Frontend muss daher unter **derselben Origin**
laufen (z. B. Ingress: `/api` → Backend, `/` → Frontend, oder Dev-Proxy wie bisher bei Vite). Sonst scheitern
`GET /api/events` und `PATCH /api/actions/...` im Browser. Das wird in README/CLAUDE.md vermerkt.
`--static-dir` bleibt im Backend erhalten, wird aber im Image nicht mehr genutzt (leer = keine Auslieferung).

## Änderungen

1. **`frontend/` löschen** (`git rm -r frontend`).

2. **`Makefile`**
   - `NPM`-Variable, alle `frontend-*`-Targets und die `frontend/node_modules`-Regel entfernen, ebenso aus `.PHONY`.
   - `build: backend-build`, `test: backend-test`, `lint: backend-vet`.
   - `run`: nur noch `backend-run` (kein Frontend-Build, kein `STATIC_DIR=frontend/dist`).
   - `STATIC_DIR ?=` und die `--static-dir`-Übergabe in `backend-run` behalten (optional, Backend-Feature bleibt
     nutzbar) – Kommentar oben anpassen („Einstiegspunkt für das Backend“).
   - `clean`: nur noch `rm -rf backend/bin`.

3. **`Dockerfile`**
   - Stage `frontend` (node) entfernen.
   - `COPY --from=frontend /src/dist /static` entfernen.
   - `ENTRYPOINT ["/dashboard", "--config", "/etc/dashboard/config.yaml"]` (ohne `--static-dir /static`).
   - Kopfkommentar anpassen (nur Backend; CA-Secret nur noch für `go mod download`).

4. **`.dockerignore`**: Zeilen `**/node_modules` und `frontend/dist` entfernen.

5. **`CLAUDE.md`**
   - Basis-Informationen: Zeile „Frontend: React + TypeScript …“ ersetzen durch Hinweis, dass das Frontend nicht
     mehr in diesem Repo liegt und die API (`openapi.yaml`) konsumiert; Same-Origin-Hinweis (kein CORS).
   - „Aktueller Stand“: Satz zur Frontend-Auslieferung im Image streichen.
   - Build-Tabelle: Frontend-Targets und -Hinweise, `frontend-*`-Einzelschritte, „Frontend-Stack/-Struktur“ entfernen.
   - Abschnitt `## Frontend` (Libs, „maximal generisch“, Seite, Datenaktualisierung) entfernen. Den Grundsatz
     „keine aktionsspezifische Logik im Client, fehlende Info → API erweitern“ als API-Leitplanke behalten.
   - Getroffene Entscheidungen: Frontend-Zeilen (Vite/Auslieferung, Bestätigungsdialog, Reiter) entfernen bzw. als
     API-Entscheidung umformulieren; Einträge in der Config-Doku wie „das Frontend zeigt …“ dürfen als Beschreibung
     des API-Zwecks bleiben, wo sie das Feld erklären.
   - „Am Frontend ist dafür keine Änderung nötig“ → „Am Client ist dafür keine Änderung nötig.“

6. **`README.md`**
   - Stack-Tabelle: Zeile Frontend entfernen.
   - Startanleitung (`make run`, `make frontend-dev`) auf Backend reduzieren.
   - Deployment: „Image enthält Backend und Frontend“ → nur Backend; Startbefehl ohne `--static-dir /static`;
     Proxy-Abschnitt: „`npm ci`“ streichen, Browser-Hinweis auf Swagger-UI reduzieren.
   - Entwicklungs-Tabelle wie Makefile anpassen.
   - `--static-dir`-Zeile in der Flag-Tabelle bleibt (Backend unverändert), Text: „optional, statische Dateien
     ausliefern; im Image nicht genutzt“.
   - Same-Origin-Hinweis für ein externes Frontend ergänzen.

7. **`erklaerung.md`**: Abschnitt „6. Frontend-Rundgang“, Lesereihenfolge-Zeilen 10/11 (`frontend/src/...`),
   Frontend-Testdateien und „`types.ts` mitziehen“ entfernen; Diagramme können „Frontend“ als externen Client behalten.

8. **Nicht geändert**: alles unter `backend/` (inkl. `static.go`, `openapi.yaml`, `example-api.jsonc`, `deploy/`),
   `helpers/`, bestehende Pläne in `plans/` (historisch).

## Verifikation

- `make build`, `make test`, `make lint` laufen ohne npm/Node durch.
- `make docker` baut das Image; Container startet ohne `--static-dir` (`/healthz` ok, `/` liefert 404, `/api/swagger` erreichbar).
- `grep -rn -i "frontend\|npm\|vite" Makefile Dockerfile .dockerignore` → keine Treffer;
  `git diff --stat backend/` → leer.
