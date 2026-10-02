# Plan: `projects` auch für ActionHandler

## Context

Bei Links legt `projects` fest, in welchen Projekten ein Link angeboten wird. Das soll es genauso für Aktionen in
`actionHandlers` geben. Mit dem Nutzer geklärt: **Pflichtfeld wie bei Links.** Fehlt `projects`, bricht der Start
ab. `projects: []` bedeutet „in keinem Projekt“ und erzeugt eine Warnung beim Start.

Heute kennt eine Aktion das Projekt ihres Objekts nicht. `actions.StoredObject` (`internal/actions/actions.go`) hat
nur `Ref` und `Object`. Nur `gitlab-pipeline` hat einen eigenen Projektfilter: `projects` in seinen
`extra-handler-configs`. Dafür schlägt er das Projekt über `projectOf` im Store nach
(`internal/actions/gitlabpipeline/gitlabpipeline.go`). Dieser Sonderweg wird durch den allgemeinen Mechanismus
ersetzt.

## Änderungen

1. **Config** (`backend/internal/config/config.go`):
   - `ActionHandlerConfig` bekommt `Projects []string \`yaml:"projects"\``.
   - Validierung wie bei Links, nur für `enabled: true`: `nil` ist ein Fehler („projects fehlt“), unbekannte Projekte
     sind ein Fehler.
   - Die Projekt-Prüfung teilen sich `validateLinks` und die Aktionen über eine kleine Hilfsfunktion
     `validateProjects(at, projects, known)`.
   - Tests in `config_test.go`.
2. **Projekt am Objekt** (`backend/internal/actions/actions.go`):
   - `StoredObject` bekommt `Project string`.
   - Die API befüllt es. In `internal/api/server.go` erhält `storedObject(...)` den Parameter `project`, den
     `extras` schon kennt.
   - `load(ref)` für `PATCH /api/actions/...` liest zusätzlich den Namespace aus dem Store und setzt dessen Projekt.
3. **Registry** (`backend/internal/actions/registry.go`):
   - `ActionsFor` und `Execute` prüfen vor dem Handler `slices.Contains(cfg.Projects, obj.Project)`. Bei `Execute`
     führt ein Fehlschlag zu `ErrNotApplicable`, also 400 wie bisher.
   - `Activate` loggt eine Warnung bei `projects: []`.
   - Die Handler selbst müssen sich nicht um Projekte kümmern.
   - Tests in `registry_test.go`: Aktion nur im erlaubten Projekt sichtbar und ausführbar, `[]` heißt nirgends.
4. **`gitlab-pipeline` vereinfachen:**
   - `projects` aus den `extra-handler-configs` entfällt. Steht es noch dort, bricht der Start mit einem Hinweis
     auf das neue Feld ab, damit es nicht still ignoriert wird.
   - `projectOf` mit Store-Abfrage entfällt. `.Project` in den Templates kommt aus `obj.Project`.
   - `store` wird in diesem Handler nicht mehr gebraucht.
   - Tests anpassen.
5. **Bestehende Configs ergänzen**, damit sie weiter starten:
   - `backend/config.example.yaml`: alle aktiven Aktionen mit `projects: [foo-projekt, bar-projekt]`, auch das
     auskommentierte gitlab-Beispiel.
   - `backend/deploy/config.yaml`: alle aktiven Aktionen mit allen drei Projekten
     (`foo-projekt, bar-projekt, foobar-projekt`). So bleibt das Verhalten unverändert.
6. **Doku** (`CLAUDE.md`): Das Beispiel für ActionHandler bekommt `projects`, mit Erläuterung „Pflicht, `[]` = in
   keinem Projekt“. Beim gitlab-Beispiel wandert `projects` aus `extra-handler-configs` nach oben.
   API und Frontend ändern sich nicht. Die Aktionen fehlen einfach an Objekten anderer Projekte.

## Verifikation

- `make lint test`.
- Backend mit `backend/config.example.yaml` und mit der ConfigMap-Config aus `backend/deploy/config.yaml` starten.
  Kein Config-Fehler darf auftreten.
- Gegenprobe: Eine Aktion ohne `projects` muss den Start mit klarer Meldung abbrechen.
