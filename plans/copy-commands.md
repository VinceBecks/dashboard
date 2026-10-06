# Plan: Kopier-Befehle je Objekt-Typ (z. B. Shell im Pod)

## Context

Entwickler sollen sich typische Befehle direkt aus dem Dashboard kopieren können, etwa
`kubectl exec -it -n <ns> <pod> -c <container> -- sh` für einen Pod. Wie bei den Links werden die Befehle in der
`config.yaml` je Kubernetes-Typ als Go-Template definiert und vom Backend je Objekt gerendert. Das Frontend zeigt je
Befehl ein Icon; ein Klick legt den Befehl in die Zwischenablage. Es gibt keinen befehlsspezifischen Code im Frontend.

Mit dem Nutzer geklärt: **Je Container ein Befehl.** Nutzt ein Pod-Template `.Container`, entsteht für jeden
Container ein eigener Befehl mit dem Titel „Shell (app)“, „Shell (sidecar)“ usw.

## Config (neuer Abschnitt `copy-commands`, analog zu `links`)

```yaml
copy-commands:
  - command-id: pod-shell                 # eindeutig
    title: Shell im Pod öffnen
    lucide-icon: terminal
    lucide-icon-color: "#455a64"
    projects: [foo-projekt]               # Pflicht; [] = in keinem Projekt (Warnung beim Start)
    targets:                              # je Objekt-Typ ein Befehl: namespace | service | ingress | pod
      pod:
        command: kubectl exec -it -n {{ .Namespace }} {{ .Name }} -c {{ .Container }} -- sh
      namespace:
        command: kubectl config set-context --current --namespace={{ .Namespace }}
```

- **Template-Daten:** dieselben wie bei Links (`.Kind .Name .Namespace .Project .Labels .Annotations .Selector .Vars`)
  plus das neue `.Container`.
  - `.Vars` kommen aus `link-variables` und `link-variables-overrides`, z. B. für einen Kube-Context.
  - `.Container` ist nur bei Pods gefüllt.
- **Neue Template-Funktion `shellquote`:** setzt einen Wert in einfache Anführungszeichen und escaped sie, für
  Labels o. Ä.
- **Text des Befehls:** Er wird unverändert übernommen, nur Leerraum am Anfang und Ende wird entfernt. Mehrzeilige
  Befehle über `|-` bleiben mehrzeilig.
- **Prüfung beim Start:**
  - `command-id` ist eindeutig, `title` ist gesetzt, `projects` ist vorhanden und nennt nur bekannte Projekte.
  - Es gibt mindestens ein Target, die Typen sind gültig, und jedes Target hat ein nicht leeres `command`.
  - Die Templates werden geparst; ein Fehler bricht den Start ab.
- **Laufzeit:** Lässt sich ein Befehl für ein Objekt nicht rendern oder ist er leer, wird nur dieser Befehl
  weggelassen und geloggt (wie bei Links).
- NodePort-Services sind für die Konfiguration Objekte vom Typ `service` und bekommen die `service`-Befehle, so wie
  sie heute schon `service`-Links bekommen.

## Backend

- **`backend/internal/config/config.go`:**
  - `Config.CopyCommands []CopyCommand` (`yaml:"copy-commands"`)
  - `CopyCommand{ID command-id, Title, LucideIcon, LucideIconColor, Projects, Targets map[model.Kind]CommandTarget{Command}}`
  - Validierung `validateCopyCommands` nach dem Muster von `validateLinks`; nutzt `validateProjects`.
  - Tests in `config_test.go` nach dem Muster von `TestLinks` und `TestLinksValidation`.
- **`backend/internal/objecttemplate/objecttemplate.go`:**
  - `Data.Container string`
  - `Funcs["shellquote"]`
  - Test für `shellquote`
- **`backend/internal/copycommands/copycommands.go` (neues Paket, analog zu `internal/links`):**
  - `Registry` mit `NewRegistry(cfg)`; parst alle Templates beim Start.
  - `For(ref, obj, project) []Command` liefert nie nil. `Command{ID, Title, LucideIcon, LucideIconColor, Command}`.
  - **Pro Container:** Bei einem `model.StoredPod` mit mindestens einem Container wird je Container mit gesetztem
    `.Container` gerendert.
    - Sind alle Ergebnisse gleich (das Template nutzt `.Container` nicht), entsteht genau ein Befehl mit `ID =
      command-id` und Original-Titel.
    - Sonst entsteht je Container ein Befehl mit `ID = command-id + "/" + container` und Titel
      `title + " (" + container + ")"`, in der Reihenfolge der Container.
  - `.Vars` wird wie in `links.Registry.render` befüllt (global + Projekt-Overrides).
  - Tests: Rendern, Projekt-Filter, Typ ohne Target, Render-Fehler wird weggelassen, Pod mit 1 und 2 Containern,
    Template ohne `.Container`.
- **`backend/internal/api`:**
  - `ObjectExtras` bekommt `CopyCommands []copycommands.Command` (`json:"copyCommands"`), befüllt in
    `Server.extras` über `s.copyCommands.For(...)`.
  - `WithCopyCommands(r)` analog zu `WithLinks`.
  - Der Event-Hub braucht keine Änderung: Kommandos stecken im JSON der Objekte, Änderungen (z. B. ein neuer
    Container) ergeben automatisch `upsert`s.
  - Test in `server_test.go`: Snapshot enthält `copyCommands` am Pod je Container sowie am Namespace.
- **`backend/cmd/dashboard/main.go`:** `copycommands.NewRegistry(cfg)` (Fehler bricht den Start ab) →
  `.WithCopyCommands(...)`.

## API

Jedes Objekt trägt zusätzlich `copyCommands` (nie fehlend, ggf. leer):

```jsonc
"copyCommands": [
  { "id": "pod-shell/app", "title": "Shell im Pod öffnen (app)", "lucideIcon": "terminal",
    "lucideIconColor": "#455a64", "command": "kubectl exec -it -n foo-feature-123 foo-backend-7d9f -c app -- sh" }
]
```

`openapi.yaml`: Schema `CopyCommand`, Feld in `ObjectBase` (required). `example-api.jsonc`: Beispiel am Pod und am
Namespace ergänzen.

## Frontend

- **`src/api/types.ts`:** `CopyCommand { id; title; lucideIcon; lucideIconColor; command }`,
  `ObjectBase.copyCommands: CopyCommand[]`.
- **`src/components/CopyCommandButtons.tsx` (neu):**
  - je Befehl ein `IconButton` mit dem konfigurierten Lucide-Icon (`ActionIcon`),
  - Tooltip mit Titel und dem Befehl in Monospace, nach dem Klick kurz „Kopiert“ bzw. „Kopieren nicht möglich“,
  - Kopieren über das vorhandene `copyToClipboard` aus `src/components/CopyButton.tsx`, das einen Fallback ohne
    Secure Context mitbringt.
- **`src/components/ObjectButtons.tsx`:**
  - bekommt `copyCommands`, rechtsbündig zusammen mit den Links (Trennstrich zu den Aktionen),
  - Reihenfolge: Kopier-Befehle, dann Links,
  - rendert auch, wenn nur Kopier-Befehle vorhanden sind.
- **Aufrufer reichen `copyCommands` durch:** `NamespaceCard` (Namespace, Ingress, NodePort-Service) und `ServiceDialog`
  (Service, Pod). Pod-Befehle erscheinen damit im Service-Dialog beim jeweiligen Replica.
- **Fixtures:** in `src/test/fixtures.ts` `copyCommands: []` ergänzen und an einem Pod einen Befehl.
- **Tests:**
  - `CopyCommandButtons.test.tsx`: Klick kopiert den Befehl (`navigator.clipboard` per Mock), Tooltip-Titel
  - `NamespaceCard.test.tsx`: Pod-Befehl im Dialog sichtbar

## Configs und Doku

- **Beispiel-Configs:** In `backend/config.example.yaml` und `backend/deploy/config.yaml` je ein Beispiel:
  - Pod-Shell, je Container
  - Namespace-Context setzen
  - `kubectl logs -f` für Pods
- **`CLAUDE.md`:**
  - Paket-Tabelle (`internal/copycommands`)
  - Config-Abschnitt „Kopier-Befehle“ (Template-Daten inkl. `.Container`, `shellquote`, je Container)
  - API (`copyCommands` an jedem Objekt)
  - Seite (Icons neben den Links)
- **`README.md`:**
  - Übersichtstabelle der Config
  - eigener Abschnitt `copy-commands`
  - Templates-Abschnitt (`.Container`, `shellquote`)
  - Startprüfungen
  - SSE-Objektfelder
- Plan als `plans/copy-commands.md` ablegen.

## Verifikation

- `make lint`, `make test` mit den neuen Go- und Vitest-Tests aus den Abschnitten oben.
- Lokales Backend gegen kind (Kopie der kubeconfig mit `host.docker.internal`, Port 18081):
  - Im Snapshot tragen die Pods in `foo-some-feature` `copyCommands` mit
    `kubectl exec -it -n foo-some-feature <pod> -c nginx -- sh`.
  - Der Namespace trägt den Context-Befehl.
  - Der kopierte Exec-Befehl funktioniert über die Test-kubeconfig tatsächlich (`… -- sh -c 'echo ok'`). Dafür wird
    `-it` weggelassen; der Befehl liest nur und verändert nichts.
