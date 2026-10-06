# Plan: Laufende GitLab-Pipelines am Objekt anzeigen (Ablage in einer ConfigMap)

## Context

`gitlab-pipeline` und `deploy-missing-services` (beide über `gitlabpipeline.Trigger`) starten Pipelines, aber im
Dashboard sieht man danach nichts mehr davon. Gewünscht:
- Am Objekt, auf dem die Aktion ausgelöst wurde, steht eine **Markierung „Pipeline läuft“** mit **Link zur Pipeline**.
- **Beendete Pipelines verschwinden**: erfolgreiche, abgebrochene und übersprungene. Fehlgeschlagene Pipelines werden
  vorerst nicht gesondert behandelt; ein Modal dafür kommt später.
- Je Objekt und Aktion zählt **nur die letzte** Pipeline.
- Ein **Neustart des Backends** darf nichts verlieren.

Mit dem Nutzer geklärt:
- **Ablage in einer ConfigMap im Dashboard-Namespace.** Annotationen an den Objekten scheiden aus, weil ArgoCD die
  Feature-Umgebungen mit Auto-Sync und Self-Heal verwaltet und fremde Annotationen wieder entfernt.
- **Eigenes Lese-Token (`read_api`)** für die Status-Abfrage. Das Trigger-Token kann keinen Status lesen.

## Ablage: ConfigMap `feature-dashboard-pipelines`

- **Anlegen:** Das Backend legt die ConfigMap **selbst** an, falls sie fehlt. Sie steht bewusst nicht in den
  Deploy-Manifesten, damit ArgoCD sie nicht verwaltet und nicht zurücksetzt.
- **Inhalt:** ein Schlüssel `pipelines.json` mit einer Liste laufender Pipelines:

  ```json
  [{"namespace": "foo-some-feature", "kind": "namespace", "name": "foo-some-feature", "action": "deploy-feature",
    "id": 4711, "project": "team/foo-deploy", "webUrl": "https://gitlab…/-/pipelines/4711", "startedAt": "2026-10-06T14:00:00Z"}]
  ```

  Je Objekt (`kind`, `namespace`, `name`) und Aktion gibt es höchstens einen Eintrag; ein neuer Start ersetzt ihn.
  Einträge gibt es nur, solange die Pipeline läuft.
- **Lesen und Schreiben:** Das Backend läuft immer mit genau einer Replica und ist deshalb der einzige Schreiber.
  - Beim Start wird die ConfigMap einmal gelesen.
  - Danach liegt der Stand im Speicher; jede Änderung wird sofort zurückgeschrieben (`Update` mit `resourceVersion`;
    bei einem Konflikt neu lesen und Änderung erneut anwenden).
  - Es gibt **keinen Informer** auf ConfigMaps. Die Leitplanke „nur vier Typen beobachten“ bleibt so gewahrt.
- **Größe:** Eine ConfigMap darf bis zu 1 MiB groß sein. Das reicht für tausende laufende Pipelines.
- **Konfiguration:** neuer optionaler Abschnitt in der `config.yaml`, alle Felder mit Standardwerten:

  ```yaml
  pipelines:
    configmap-namespace: feature-dashboard     # Standard: Umgebungsvariable POD_NAMESPACE, sonst feature-dashboard
    configmap-name: feature-dashboard-pipelines
    poll-interval: 15s
  ```

- Ist die ConfigMap beim Start nicht lesbar (außer bei `NotFound`), bricht der Start mit einer Fehlermeldung ab.

## Backend

1. **`internal/pipelines` (neu):**
   - **`state.go`:** `State` hält die laufenden Pipelines im Speicher, als JSON-Bytes je Schlüssel, passend zur
     Store-Philosophie. Geschützt durch einen Mutex.
     - Je Namespace merkt sich `State` einen Revisions-Zeitstempel und meldet Änderungen über `Changes()`
       (koaleszierend wie beim Store).
     - Funktionen: `Load(ctx)`, `Set(ctx, ref, action, entry)`, `Remove(ctx, ref, action)`, `For(ref) []Entry`,
       `All() []Entry`, `NamespaceRevisions()`.
     - Jede Änderung schreibt die ConfigMap über `kubernetes.Interface`.
   - **`tracker.go`:** eine Goroutine, die alle `poll-interval` für jeden Eintrag
     `GET {gitlab-url}/api/v4/projects/{project}/pipelines/{id}` mit dem Header `PRIVATE-TOKEN` abfragt.
     - **Beendet** (`success`, `failed`, `canceled`, `skipped`), **404** oder **Aktion nicht mehr konfiguriert**:
       `Remove`.
     - **Läuft noch:** nichts tun.
     - **GitLab nicht erreichbar:** loggen und im nächsten Durchlauf erneut versuchen.
     - Nach einem Neustart setzt der Tracker einfach mit dem Inhalt der ConfigMap fort.
   - **Tests:**
     - `State` mit dem Fake-Client: anlegen, ersetzen, entfernen, ConfigMap-Inhalt, Load nach einem simulierten
       Neustart, Konflikt-Retry
     - `Tracker` mit einem `httptest`-GitLab
2. **`internal/config`:** `PipelinesConfig` mit Standardwerten und Prüfung von `poll-interval` (mindestens 5 s).
   Dazu passende Tests.
3. **`internal/actions/gitlabpipeline`:**
   - `TriggerConfig` bekommt das optionale Feld `status-token-env`. Fehlt die Variable, startet das Backend nicht,
     genau wie beim Trigger-Token.
   - `Trigger.Run` liefert zusätzlich die Pipeline (`id`, `web_url`) aus der Trigger-Antwort.
   - Mit `status-token-env` rufen beide Handler nach dem Start `State.Set` auf. Schlägt das fehl, bleibt die Aktion
     erfolgreich; die Meldung bekommt einen Hinweis.
   - Ohne `status-token-env` wird nichts gespeichert, sonst bliebe die Markierung für immer stehen.
   - `actions.Deps` bekommt `Pipelines *pipelines.State`.
   - Der Tracker braucht je Aktion `gitlab-url` und Lese-Token. Die Handler melden diese Daten über eine kleine
     Schnittstelle an `State` bzw. den Tracker, statt dass der Tracker die Config selbst auswertet.
4. **API und Event-Hub:**
   - `ObjectExtras` bekommt `Pipelines []PipelineDTO` (`action`, `title`, `url`, `startedAt`; nie nil), befüllt in
     `Server.extras` über `State.For(ref)`. `title` kommt aus `action-title` (`actions.Registry.Title(name)`).
   - Der Hub achtet zusätzlich auf `State.Changes()`. Ein Namespace gilt als geändert, wenn sich die Store-Revision
     **oder** die Pipeline-Revision geändert hat; `nsState` merkt sich dafür beide.
   - Der Diff selbst bleibt unverändert: Pipeline-Änderungen ändern das JSON der Objekte und erzeugen ein `upsert`.
   - Tests: Snapshot mit `pipelines`; `State.Set` und `State.Remove` erzeugen je ein `upsert`.
5. **`cmd/dashboard/main.go`:**
   - `State` erzeugen und laden, bevor die Registry aktiviert wird.
   - Tracker starten.
   - `WithPipelines(state)` am API-Server.
6. **RBAC (`backend/deploy/rbac.yaml`):** eine `Role` mit `RoleBinding` im Namespace `feature-dashboard`:
   - `configmaps`: `create`
   - `get` und `update` nur für `resourceNames: [feature-dashboard-pipelines]`
   - `deployment.yaml`: `POD_NAMESPACE` per Downward-API.
7. **Config und Doku:**
   - `config.example.yaml` und `deploy/config.yaml` bekommen `status-token-env` und den Abschnitt `pipelines`.
   - `gitlab-secret.example.yaml` bekommt das Lese-Token.
   - `openapi.yaml`: Schema `Pipeline`, Feld `pipelines` in `ObjectBase`.
   - `example-api.jsonc`: Beispiel mit `pipelines`.
   - `CLAUDE.md`:
     - Paket-Tabelle
     - Config-Abschnitt `pipelines`
     - Hinweis „Zustand nie an ArgoCD-verwalteten Objekten ablegen“
     - Erweiterte Leitplanke: Die ConfigMap ist die einzige Persistenz außerhalb des Stores, ohne Informer
     - Unter „Noch nicht festgelegt“: „Fehlgeschlagene Pipelines (Modal)“
   - `README.md`: Abschnitt bei `gitlab-pipeline`, Config, RBAC-Tabelle.
   - Plan als `plans/pipeline-status.md` ablegen.

## Frontend

- **`src/api/types.ts`:** `Pipeline { action; title; url; startedAt }`, `ObjectBase.pipelines: Pipeline[]`.
- **`src/components/PipelineBadges.tsx` (neu):**
  - je laufender Pipeline ein kleiner Chip mit rotierendem Icon `loader-circle` und dem Text „Pipeline läuft“,
  - Tooltip mit dem Titel der Aktion und der Startzeit,
  - der Chip ist ein Link (`href`, neuer Tab) zur Pipeline.
- **`ObjectButtons`:** zeigt `PipelineBadges` vor den Aktionen. Damit erscheinen die Chips auf der Kachel (Namespace,
  Ingress, NodePort) und im Service-Dialog (Service, Pod).
- **`ServiceDot`:** Läuft beim Service oder bei einem seiner Pods eine Pipeline, bekommt der Punkt einen pulsierenden
  Rand.
- **Tests:** `PipelineBadges`, `ObjectButtons` mit Pipeline, `ServiceDot`-Markierung; Fixtures ergänzen.

## Verifikation

- `make lint` und `make test` mit den neuen Go-Tests (State, Tracker, Trigger-Antwort, Hub, Config) und Vitest.
- Ende-zu-Ende gegen kind, mit einem lokalen Fake-GitLab (kleines Skript im Scratchpad), das Trigger und Status
  beantwortet:
  1. Lokales Backend mit einer Test-Config starten, deren `gitlab-url` auf das Fake-GitLab zeigt und die eine eigene
     Test-ConfigMap `feature-dashboard-pipelines-test` im Namespace `feature-dashboard` nutzt.
  2. Aktion am Namespace `foo-some-feature` auslösen. Danach steht der Eintrag in der ConfigMap
     (`kubectl get cm … -o yaml`), und der SSE-Stream liefert ein `upsert` mit `pipelines`.
  3. Backend neu starten. Die Markierung ist weiter da, und der Tracker verfolgt die Pipeline weiter.
  4. Fake-Status auf `success` setzen. Innerhalb eines Poll-Intervalls verschwinden Eintrag und Markierung.
- Zum Schluss die Test-ConfigMap löschen.
