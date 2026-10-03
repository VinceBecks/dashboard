# Plan: NodePort-Service mit dem Basis-Service löschen

## Kontext

Die Aktion `node-port-service-on-service-base` (`backend/internal/actions/nodeportservice/nodeportservice.go`)
erstellt `<service>-node-port` mit den Labels `feature-dashboard/created-by-action` und
`app.kubernetes.io/managed-by` sowie der Annotation `feature-dashboard/base-service`. Eine Verknüpfung, die beim
Löschen greift, gibt es **nicht**: keine `ownerReferences`, und kein Code reagiert auf das Löschen des
Basis-Services (die Annotation wird nirgends ausgewertet). Wird der ClusterIP-Service gelöscht, bleibt der
NodePort-Service übrig – mit einem Selector, der weiter auf die Pods zeigt.

Ziel: Wird der Basis-Service gelöscht, wird auch der NodePort-Service gelöscht.

## Ansatz: OwnerReference + Kubernetes-Garbage-Collector

Der NodePort-Service erhält beim Erstellen eine `ownerReference` auf den Basis-Service. Das Löschen übernimmt
dann der Garbage-Collector von Kubernetes – auch wenn das Dashboard nicht läuft, ohne neue Informer-Logik und ohne
neue Typen. Passt zu den Leitplanken: Die Löschung kommt wie gewohnt über das Informer-Delete-Event in den Store.

## Änderungen im Code

### `backend/internal/actions/nodeportservice/nodeportservice.go` – `Handler.Execute`

1. Vor dem Erstellen den Basis-Service frisch aus dem Cluster lesen
   (`h.client.CoreV1().Services(ns).Get(ctx, base.Name, …)`), weil `model.StoredService` keine UID enthält.
   - `NotFound` → `Result{Success: false, Message: "Service <name> existiert nicht mehr"}`
     (gleiche Formulierung wie in `delete.go`).
   - anderer Fehler → `Result{Success: false, …}`.
   - Kein `ClusterIP` mehr → Fehler-Result (Store-Stand veraltet; analog zur erneuten Prüfung in `DeleteHandler`).
2. In `ObjectMeta` ergänzen:
   ```go
   OwnerReferences: []metav1.OwnerReference{{
       APIVersion: "v1", Kind: "Service", Name: current.Name, UID: current.UID,
   }},
   ```
   Bewusst **ohne** `Controller` und `BlockOwnerDeletion` (sonst bräuchte das Backend wegen
   `OwnerReferencesPermissionEnforcement` zusätzlich `update` auf `services/finalizers`). Hintergrund-Löschung
   (Standard) reicht.
3. Kommentar am Typ `Handler` um „wird mit dem Basis-Service per ownerReference gelöscht“ ergänzen.

Die Annotation `feature-dashboard/base-service` bleibt (lesbar für Menschen, harmlos).

`delete.go`, Store, Converter, Informer, API, Frontend: keine Änderung. RBAC (`backend/deploy/rbac.yaml`): `get`
auf Services ist bereits vorhanden, keine Änderung.

### Tests `backend/internal/actions/nodeportservice/nodeportservice_test.go`

- `setup` legt den Basis-Service zusätzlich im Fake-Client an (`fake.NewClientset(&corev1.Service{… UID: "base-uid", Type: ClusterIP})`),
  da `Execute` ihn jetzt liest; ebenso `TestCreateThenDelete` in `delete_test.go` (nutzt `setup`).
- Bestehenden Create-Test (Zeile ~142, prüft Labels/Annotation) um Prüfung der `OwnerReferences`
  (Kind, Name, UID `base-uid`) erweitern.
- Neuer Test: Basis-Service fehlt im Cluster → `Success == false`, kein Service erstellt.
- Neuer Test: Basis-Service im Cluster inzwischen kein `ClusterIP` → `Success == false`.

(Der Fake-Client hat keinen Garbage-Collector; das kaskadierende Löschen selbst wird manuell im Cluster geprüft.)

## Änderungen in der Dokumentation

- `README.md`, Abschnitt `node-port-service-on-service-base` (Zeilen ~365–370):
  - unter „Erstellt wird ein Service mit:“ ergänzen: „einer `ownerReference` auf den Basis-Service“.
  - neuer Punkt **Lebensdauer:** „Wird der Basis-Service gelöscht, löscht Kubernetes (Garbage-Collector) auch den
    NodePort-Service. Vor dem Erstellen liest die Aktion den Basis-Service frisch aus dem Cluster.“
  - Hinweis: Vor dieser Änderung erstellte NodePort-Services haben keine `ownerReference` und müssen bei Bedarf
    weiter manuell bzw. über `delete-node-port-service` gelöscht werden.
- `CLAUDE.md`, Tabelle Backend-Pakete, Zeile `internal/actions/nodeportservice`: ergänzen „…; erstellte Services
  haben eine ownerReference auf den Basis-Service und werden mit ihm gelöscht“.
- `openapi.yaml`: keine Änderung (API unverändert).

## Verifikation

1. `make test` und `make lint`.
2. Manuell im (kind-)Cluster: Image mit `make docker` bauen/deployen, über das Dashboard einen NodePort-Service
   erstellen, `kubectl get svc <svc>-node-port -o yaml` → `ownerReferences` vorhanden.
   Dann `kubectl delete svc <svc>` → `<svc>-node-port` verschwindet nach kurzer Zeit, auch aus dem Dashboard.
