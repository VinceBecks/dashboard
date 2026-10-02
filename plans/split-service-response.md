# Plan: Review und überarbeitete Fassung von `plans/split-service-response.md`

## Context

Der Nutzer hat in `plans/split-service-response.md` eine Erweiterung beschrieben: Je Projekt definieren
`service-areas` Bereiche, Services werden über Label-Kombinationen zugeordnet, und nicht zugeordnete Services landen
in `others` („Andere“). Das Dashboard soll daraus mehrere Bereiche bauen. Gewünscht ist eine Prüfung und Verbesserung.

Die Grundidee ist gut:
- Die Zuordnung steht in der Config und nicht im Code.
- Die Logik liegt im Backend, das Frontend bleibt generisch.
- Die ODER-Verknüpfung über die Kombinationen und die UND-Verknüpfung innerhalb einer Kombination sind ausdrucksstark
  genug.

Der Plan lässt aber einige Punkte offen und hat kleinere Inkonsistenzen. Zwei davon sind mit dem Nutzer geklärt:
- Bereiche gelten **nur für Anwendungs-Services (ClusterIP)**. Die NodePort-Liste bleibt unverändert.
- Die Response referenziert Services **über ihre Namen**, ohne die Service-Objekte zu duplizieren.

## Review-Ergebnis: Verbesserungen gegenüber dem Entwurf

1. **Einheitliche Schlüssel:** `labelCombinations` ist camelCase, alle anderen Keys sind kebab-case
   (`area-name`, `namespace-labels`, `action-name`). Besser `label-combinations`.
2. **Geltungsbereich festlegen:** Der Entwurf sagt nicht, ob NodePort-Services mitzählen. Festgelegt ist jetzt: nur
   ClusterIP. Vom Dashboard erzeugte `-node-port`-Services tragen ohnehin nicht die Labels des Basis-Services und
   würden sonst immer in „Andere“ landen.
3. **Response-Form:**
   - Ein neues Feld `serviceAreas: [{id, label, services: [namen]}]` neben dem unveränderten `services`.
   - Ein Service in mehreren Bereichen wird nicht doppelt übertragen.
   - Die Response bleibt abwärtskompatibel, nach demselben Muster wie `replicas`.
4. **Reihenfolge definieren:** Die Bereiche kommen in Config-Reihenfolge, `others` immer zuletzt. Services innerhalb
   eines Bereichs sind nach Namen sortiert, wie der Store sie schon liefert.
5. **Leere Bereiche:** Konfigurierte Bereiche werden immer geliefert, auch leer. So bleibt das Layout stabil und das
   Frontend entscheidet über die Anzeige. `others` wird nur geliefert, wenn es Services enthält.
6. **Validierung beim Start:**
   - `area-name` ist je Projekt eindeutig.
   - `others` ist als `area-name` reserviert.
   - `area-label` darf nicht leer sein.
   - Mindestens eine Label-Kombination ist nötig, und **keine Kombination darf leer sein**. Eine leere Kombination `{}`
     würde sonst still alle Services matchen.
7. **Label „Andere“ konfigurierbar:** Neues optionales Feld `others-area-label` je Projekt, Standard „Andere“.
   Projekte ganz ohne `service-areas` bekommen damit sinnvollerweise den Standard „Anwendungs-Services“.
   Alle Services landen dort in `others`, das Verhalten ist für sie also wie heute.
8. **Wiederholung zwischen Projekten:** Im Beispiel haben foo und bar denselben Bereich „Business“. Dafür reichen
   YAML-Anker (`&business` / `*business`). Eine globale Bereichsdefinition lohnt sich erst bei vielen Projekten und
   wird nicht eingebaut. In der Doku kommt ein Hinweis dazu.
9. **Ort der Berechnung:**
   - Die Zuordnung wird zur Antwortzeit in der API berechnet. Sie hängt nur von Service-Labels und der statischen
     Config ab.
   - Der Store bleibt frei von Darstellungslogik. Der **ETag-Mechanismus bleibt unverändert korrekt**, weil jede
     Label-Änderung am Service den Namespace-Timestamp ändert.
10. **Fehlende Teile im Entwurf:**
    - Das Frontend muss die Bereiche rendern.
    - `openapi.yaml` und der Seitenaufbau in `CLAUDE.md` müssen angepasst werden. Laut `CLAUDE.md` muss die
      `openapi.yaml` immer aktuell sein.
11. **Kleinigkeit:** `my-thir` im Beispiel ist vermutlich ein Tippfehler. Exakter Wertvergleich der Labels reicht
    vorerst. Eine Prüfung „Label vorhanden, Wert egal“ wäre eine spätere Erweiterung.

## Überarbeitete Ziel-Konfiguration

```yaml
projects:
  - project-name: foo-projekt
    namespace-labels: { ... }
    others-area-label: Andere             # optional, Standard "Andere"
    service-areas:
      - area-name: business-services      # eindeutig je Projekt, "others" reserviert
        area-label: Business
        label-combinations:               # ODER über Einträge, UND innerhalb eines Eintrags
          - app.kubernetes.io/part-of: my-other-demo-system
            app.kubernetes.io/foo: bar
          - app.kubernetes.io/part-of: my-third-system
            my.comp/bar: foo
      - area-name: infrastructure-services
        area-label: Infrastruktur
        label-combinations:
          - app.kubernetes.io/part-of: rabbitmq
          - app.kubernetes.io/part-of: database
```

Response von `GET /api/projects/{project}/namespaces/{namespace}`, ergänzt um:

```json
"serviceAreas": [
  { "id": "business-services", "label": "Business", "services": ["orders", "web"] },
  { "id": "infrastructure-services", "label": "Infrastruktur", "services": [] },
  { "id": "others", "label": "Andere", "services": ["debug-tool"] }
]
```

## Umsetzung

1. **Config** (`backend/internal/config/config.go`):
   - `Project` bekommt die Felder `ServiceAreas []ServiceArea` (`service-areas`) und `OthersAreaLabel`
     (`others-area-label`).
   - Neuer Typ `ServiceArea{Name, Label, LabelCombinations []map[string]string}`.
   - Die Validierung aus Punkt 6 kommt in `Validate()`. Tests dazu in `config_test.go`.
2. **Zuordnung** (neue Datei `backend/internal/project/areas.go`, neben dem Label-Matching im Matcher):
   - Funktion `AssignServiceAreas(p config.Project, services []model.StoredService) []Area`.
   - Sie betrachtet nur Services vom Typ `ClusterIP` und nutzt dieselbe Logik wie `hasAll` aus
     `internal/project/matcher.go`.
   - Tabellengetriebene Tests: Mehrfach-Zuordnung, UND/ODER, `others`, Reihenfolge, NodePort wird ignoriert.
3. **API** (`backend/internal/api/server.go`):
   - `NamespaceResponse.ServiceAreas` wird in `handleNamespace` befüllt.
   - Dafür braucht der Server die Projekt-Config per Namen. Sie liegt bereits in `s.projects`, eine Map kommt dazu.
   - Test in `server_test.go`.
4. **`openapi.yaml`:** Ein Schema `ServiceArea` und das Feld `serviceAreas` (required) in `NamespaceResponse`.
5. **Frontend:**
   - `src/api/types.ts` bekommt `ServiceArea` und `serviceAreas`.
   - `NamespacePage.tsx` ersetzt den Abschnitt „Anwendungs-Services“ durch einen Abschnitt je Bereich mit dem Label
     als Überschrift. Die Kacheln über `ServiceCard` bleiben, die Services werden per Name aus `data.services`
     aufgelöst.
   - Leere konfigurierte Bereiche zeigen „Keine vorhanden“.
   - Test in `NamespacePage.test.tsx` anpassen und ergänzen.
6. **Doku:**
   - `CLAUDE.md`: Konfigurationsbeispiel, fachlicher Begriff „Service-Bereich“, Seitenaufbau Punkt 4 und Hinweis auf
     YAML-Anker.
   - `plans/split-service-response.md` mit dieser überarbeiteten Fassung ersetzen.
   - `backend/config.example.yaml` und `backend/deploy/config.yaml` um ein Beispiel ergänzen.

## Verifikation

- `make lint test`. Neue Tests für Config-Validierung, Zuordnung, API-Response und Frontend-Darstellung.
- Lokal mit `make run` und Services mit passenden Labels, z. B. aus `helpers/demo-app`:
  - Die Namespace-Seite zeigt die Bereiche in Config-Reihenfolge und „Andere“ zuletzt.
  - Ein Service mit zwei passenden Kombinationen erscheint in beiden Bereichen.
  - Eine Label-Änderung per `kubectl label` verschiebt den Service nach dem nächsten Polling.
