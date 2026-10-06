# Plan: Service-Bereiche als Reiter in den Namespace-Kacheln

## Context

Bei der SSE-Umstellung sind die Service-Bereiche (`service-areas`) ganz entfallen, einschließlich Config,
`internal/project/areas.go` und Quick-Filter. Der Nutzer möchte sie wieder in der `config.yaml` eintragen können.
Diesmal erscheinen die Bereiche als **Reiter in jeder Namespace-Kachel**, und unter jedem Reiter stehen die
Service-Punkte dieses Bereichs.

Mit dem Nutzer geklärt:
- **Reiter nur für Bereiche mit Services:** Leere Bereiche werden je Kachel ausgeblendet. „Andere“ kommt zuletzt und
  nur, wenn es Services ohne Bereich gibt. Bleibt nur ein Reiter übrig, oder sind gar keine Bereiche konfiguriert,
  zeigt die Kachel die Punkte wie heute ohne Reiterleiste.
- **Reiter enthalten nur die Service-Punkte (ClusterIP).** Ingresses und NodePort-Services bleiben außerhalb.

## Config (wie vor der SSE-Umstellung, Vorlage `git show fc5df60:…`)

```yaml
projects:
  - project-name: foo-projekt
    others-area-label: Andere          # optional, Standard "Andere"
    service-areas:
      - area-name: business-services   # eindeutig je Projekt, "others" reserviert
        area-label: Business
        label-combinations:            # ODER über Einträge, UND innerhalb; keine leeren Einträge
          - app.kubernetes.io/part-of: foo
```

- `backend/internal/config/config.go`: Folgendes kommt aus `fc5df60` zurück:
  - `Project.ServiceAreas`, `Project.OthersAreaLabel`, `ServiceArea`, `OthersAreaName`
  - `validateServiceAreas` samt Aufruf in `Validate`
  - `OthersLabel()` mit Standard „Andere“. Die frühere Sonderregel „Anwendungs-Services“ ohne Bereiche entfällt, denn
    ohne Bereiche gibt es keine Reiter.
- Tests `TestServiceAreas` und `TestServiceAreasValidation` aus `fc5df60` übernehmen und anpassen.

## Backend

- **`backend/internal/project/areas.go` (neu, angelehnt an das alte `AssignServiceAreas`):**
  `ServiceAreaIDs(p config.Project, labels map[string]string) []string` liefert die IDs aller passenden Bereiche in
  Config-Reihenfolge. Passt kein Bereich, liefert sie `["others"]`. `matchesAny` stammt aus der alten Datei.
  `hasAll` aus `internal/project/matcher.go` wird wiederverwendet. Tests nach dem Muster des alten `areas_test.go`.
- **`backend/internal/api/dto.go`:**
  - `ServiceDTO.Areas []string` (`json:"areas"`), gesetzt in `buildService` über `project.ServiceAreaIDs`.
    Ändern sich die Labels eines Services, ändert sich sein JSON, und der Hub schickt automatisch ein `upsert`.
  - `buildNamespace` braucht die Projekt-Config. `Server` hält dafür `projects map[string]config.Project`, gefüllt in
    `NewServer`.
- **`backend/internal/api/events.go`:**
  - `ProjectSnapshot.ServiceAreas []AreaDTO` (`json:"serviceAreas"`, `AreaDTO{ID, Label}`).
  - Inhalt: die konfigurierten Bereiche in Config-Reihenfolge, danach immer `{id: "others", label: OthersLabel()}`.
  - Die Bereiche sind statisch, deshalb gibt es dafür keine Events. Der Hub bekommt statt der Projektnamen die
    `[]config.Project`.

Beispiel (Ausschnitt Snapshot):

```jsonc
{ "name": "foo-projekt",
  "serviceAreas": [ { "id": "business-services", "label": "Business" }, { "id": "others", "label": "Andere" } ],
  "namespaces": [ { …, "services": [ { "kind": "service", "name": "business-app-1", "areas": ["business-services"], … } ] } ] }
```

## Frontend

- **`src/api/types.ts`:**
  - `ServiceArea { id; label }`
  - `ProjectSnapshot.serviceAreas: ServiceArea[]`
  - `ServiceObject.areas: string[]`
- **`src/api/state.ts`:**
  - Ein Projekt, das per Namespace-Upsert neu entsteht, bekommt `serviceAreas: []`.
  - Sonst bleibt `serviceAreas` unverändert, die Werte kommen nur aus dem Snapshot.
- **`src/pages/DashboardPage.tsx`:** reicht `project.serviceAreas` an `NamespaceCard` weiter.
- **`src/components/NamespaceCard.tsx`:**
  - Neue reine Funktion `groupByArea(services, areas)`, z. B. in `src/components/serviceAreas.ts`:
    - liefert `[{area, services}]` in der Reihenfolge von `serviceAreas`,
    - nur Bereiche mit Services,
    - Services mit unbekannter Bereichs-ID fallen unter „others“.
  - Mehr als eine Gruppe ergibt MUI `Tabs` (`variant="scrollable"`, klein) über den Service-Punkten. Je Reiter die
    Punkte dieses Bereichs; ein Service in mehreren Bereichen erscheint in jedem.
  - Der gewählte Reiter ist Zustand der Kachel (`useState`, nicht in der URL). Verschwindet er (Bereich leer
    geworden), gilt der erste.
  - Eine Gruppe oder keine Bereiche: Punkte wie heute, ohne Reiter. Die Punkt-Darstellung wird in eine kleine
    Komponente `ServiceDots` ausgelagert.
- **Tests:**
  - `serviceAreas.test.ts`: Reihenfolge, leere Bereiche, Mehrfachzuordnung, unbekannte ID
  - `NamespaceCard.test.tsx`: Reiter sichtbar und umschaltbar; ohne Bereiche keine Reiterleiste
  - Fixtures ergänzen (`areas`, `serviceAreas`)

## Configs und Doku

- **Beispiel-Configs:** `backend/config.example.yaml` und `backend/deploy/config.yaml` bekommen die `service-areas`-Blöcke
  aus `fc5df60` zurück (Business/Infrastruktur je Projekt).
- **`openapi.yaml`:**
  - `Snapshot.projects[].serviceAreas` (`ServiceArea`-Schema)
  - `ServiceObject.areas`
- **`example-api.jsonc`:** `serviceAreas` und `areas` ergänzen.
- **`CLAUDE.md`:**
  - Begriff „Service-Bereich“ in der Begriffstabelle
  - Config-Abschnitt `service-areas`
  - API (`serviceAreas`, `areas`)
  - Seitenbeschreibung (Reiter)
  - Eintrag unter „Getroffene Entscheidungen“ korrigieren
- **`README.md`:**
  - `projects`-Tabelle und Beispiel
  - Abschnitt „Service-Bereiche“ (Reiter statt Chip-Filter)
  - Startprüfungen
  - SSE-Abschnitt
  - Begriffstabelle
- Den Hinweis „ältere Configs mit `service-areas` werden ignoriert“ entfernen.
- Plan als `plans/service-areas-reiter.md` ablegen.

## Verifikation

- `make lint`, `make test` mit den neuen Go-Tests:
  - `project`: Zuordnung mit UND/ODER, mehrfach, `others`
  - `config`: Validierung
  - `api`: Snapshot enthält `serviceAreas` mit `others` zuletzt und `areas` am Service. Eine Label-Änderung
    erzeugt ein `upsert` mit neuen `areas`.
- Lokales Backend gegen kind (Kopie der kubeconfig mit `host.docker.internal`):
  - `curl -sN localhost:18081/api/events | head -c …`
  - Die `business-app-*`-Services tragen `areas: ["business-services"]`, `rabbitmq` und `database` tragen
    `["infrastructure-services"]`.
- Frontend lokal per `make frontend-dev` gegen das lokale Backend: Kachel `foo-some-feature` zeigt die Reiter
  „Business“ und „Infrastruktur“.
