# Plan: NodePort-Services wieder im Dashboard

## Context

Seit der SSE-Umstellung (`plans/sse-einseiten-dashboard.md`) sendet das Backend nur ClusterIP-Services. Die
NodePort-Services, ihre Adresse `<dns-name>:<node-port>` und der Kopieren-Button sind entfallen, ebenso
`cluster.dns-name` in der Config. Beide NodePort-Aktionen sind seitdem in den Beispiel-Configs deaktiviert.

Gewünscht:
- NodePort-Services kommen wieder ans Frontend, mit Name und fertig berechneten Adressen `<dns-name>:<node-port>`.
- Im Snapshot stehen sie als eigenes Attribut `nodePortServices` am Namespace.
- Das Dashboard bekommt wieder einen Copy-to-Clipboard-Button.

Mit dem Nutzer geklärt:
- **Aktionen:** NodePort-Services tragen wieder `actions` und `links`. `node-port-service-on-service-base` und
  `delete-node-port-service` werden in den Beispiel-Configs wieder aktiviert.
- **`cluster.dns-name` ist Pflicht.** Ohne ihn startet das Backend nicht. Die Adressen werden immer im Backend
  berechnet. (Hinweis: Das Backend hat die Adressen bisher nie berechnet, das Frontend hat sie aus `GET /api/cluster`
  zusammengesetzt. Den Endpunkt gibt es nicht mehr, und er kommt auch nicht zurück.)

## API-Änderung

Neuer DTO-Typ `NodePortServiceDTO` mit dem eigenen `kind: "node-port-service"`. So kann der Client Events direkt
dem Attribut `nodePortServices` zuordnen, ohne sie mit `services` zu verwechseln.

```jsonc
"nodePortServices": [                       // nur Typ NodePort; nur im Snapshot, in upsert fehlt die Liste
  {
    "kind": "node-port-service",
    "name": "foo-backend-node-port",
    "parents": { "project": "foo-projekt", "namespace": "foo-feature-123" },
    "addresses": ["k8s-nodes.mycomp.de:30080"],   // je Port mit nodePort, Reihenfolge der Spec
    "actions": [ { "id": "delete-node-port-service", … } ],
    "links": []
  }
]
```

- Events nutzen dasselbe Schema wie bisher:
  - `upsert`/`delete` mit `kind: "node-port-service"`, Parents `{project, namespace}`
  - `delete` eines Namespaces entfernt auch die NodePort-Services
- Für `PATCH /api/actions` bleibt `kind: "service"`. Der Client bildet `node-port-service` auf `service` ab.
- Andere Service-Typen (LoadBalancer, ExternalName) werden weiter nicht gesendet.

## Backend

1. **Config (`backend/internal/config/config.go`):**
   - `ClusterConfig{DNSName}` mit `yaml:"cluster"` wieder einführen (Vorlage: `git show fc5df60:backend/internal/config/config.go`).
   - Prüfung: `cluster.dns-name` fehlt → Fehler. Sonst gültiger DNS-Name oder IP, ohne Schema und Port.
   - Tests: `TestClusterDNSName` wiederherstellen und auf „Pflicht“ anpassen. Bestehende Test-Configs (`sample`,
     `projects`-Konstanten, `TestLinks` in `internal/api/server_test.go`) bekommen `cluster.dns-name`.
2. **`backend/internal/api/dto.go`:**
   - `NodePortServiceDTO` (`ObjectHeader`, `Addresses []string`, `ObjectExtras`).
   - `NamespaceDTO.NodePortServices []NodePortServiceDTO` mit `json:"nodePortServices,omitzero"`.
   - Konstante `KindNodePortService model.Kind = "node-port-service"`; sie gilt nur für die Ausgabe.
     `model.Kind.Valid` bleibt unverändert.
   - In `buildNamespace` wird nach Typ verzweigt:
     - ClusterIP → wie bisher,
     - NodePort → `NodePortServiceDTO`, Aktionen und Links über das vorhandene
       `s.extras(model.KindService, …)`,
     - sonst überspringen.
   - Neu `nodePortAddresses(svc, dnsName)`: liefert `dnsName + ":" + nodePort` je `ServicePort` mit
     `NodePort != 0`. `model.ServicePort.NodePort` ist schon gespeichert.
   - `flatten`: NodePort-Services als Kinder des Namespaces (parentKey = nsKey); im Kopf-Objekt
     `NodePortServices = nil`.
3. **`backend/internal/api/server.go`:** `NewServer` bekommt den DNS-Namen über `WithCluster(cfg.Cluster)`, wie
   früher. `backend/cmd/dashboard/main.go` ruft das auf.
4. **`events.go`:** keine Änderung. Der Diff arbeitet generisch über `flatten`.

## Frontend

- `src/api/types.ts`:
  - `ObjectKind` (nur für `ObjectRef`) bleibt.
  - Neuer Typ `NodePortServiceObject { kind: 'node-port-service'; addresses: string[] }` plus `ObjectBase`.
  - `NamespaceObject.nodePortServices?`; `DashboardObject` und `ObjectHeader.kind` werden um
    `'node-port-service'` erweitert.
- `src/api/state.ts`:
  - `applyUpsert`/`applyDelete` behandeln `node-port-service` analog zu `ingress` in der Liste
    `nodePortServices`.
  - Ein Namespace-Upsert behält `nodePortServices`.
  - `refOf` bildet `node-port-service` auf `kind: 'service'` ab.
- `src/components/CopyButton.tsx` wiederherstellen (`git show fc5df60:frontend/src/components/CopyButton.tsx`). Er
  bringt einen Fallback ohne Secure Context mit.
- `src/components/NamespaceCard.tsx`: neuer Abschnitt unter den Ingresses. Je NodePort-Service:
  - Name, dazu `ObjectButtons` (Aktionen und Links),
  - je Adresse die Adresse in Monospace und ein `CopyButton`.
- Tests:
  - `state.test.ts`: Upsert/Delete von `node-port-service`, `refOf`
  - `NamespaceCard.test.tsx`: Adresse und Kopieren-Button sichtbar
  - Fixture in `src/test/fixtures.ts` ergänzen

## Configs und Doku

- `backend/config.example.yaml`: `cluster: dns-name: demo-cluster-control-plane`.
- `backend/deploy/config.yaml`: `cluster: dns-name: k8s-nodes.mycomp.de.local`.
- Beide NodePort-Aktionen wieder auf alle Projekte setzen.
- `backend/internal/api/openapi.yaml`:
  - Schema `NodePortServiceObject`
  - `NamespaceObject.nodePortServices`
  - `kind`-Enums in `ObjectBase`/`DeleteEvent` um `node-port-service` erweitern
- `backend/internal/api/example-api.jsonc`: Beispiel `nodePortServices` ergänzen.
- `CLAUDE.md`:
  - Config-Abschnitt `cluster` (Pflicht)
  - API-Abschnitt
  - Seitenbeschreibung (NodePort-Abschnitt mit Kopieren)
  - Hinweis zu NodePort-Aktionen und „Getroffene Entscheidungen“ korrigieren
- `README.md`:
  - `cluster` in Übersicht, eigener Abschnitt und Startprüfungen
  - NodePort-Fußnote bei den Aktionen entfernen
  - SSE-Abschnitt ergänzen
- Plan als `plans/nodeport-services-sse.md` ablegen.

## Verifikation

- `make lint`, `make test`. Neue Go-Tests in `internal/api/server_test.go`:
  - **Snapshot:** enthält `nodePortServices` mit `addresses: ["<dns>:<nodePort>"]` und Aktionen. NodePort-Services
    stehen nicht mehr in `services`.
  - **Events:** NodePort-Service anlegen → `upsert` mit `kind:"node-port-service"`; löschen → `delete`.
  - **Config:** Ohne `cluster.dns-name` gibt es einen Startfehler.
- Im kind-Cluster (Zugriff per `--server https://host.docker.internal:49777 --tls-server-name
  demo-cluster-control-plane`), nach dem Ausrollen des neuen Images durch den Nutzer:
  - Im Dashboard „NodePort-Service erstellen“ auslösen → Kachel zeigt `demo-cluster-control-plane:<port>` mit
    Kopieren-Button.
  - Löschen über die Aktion → Eintrag verschwindet.
  - Mitschnitt per `curl -sN …/api/events` zeigt die erwarteten `upsert`/`delete`.
