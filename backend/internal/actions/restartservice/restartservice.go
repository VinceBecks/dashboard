// Package restartservice implementiert die Aktion „Alle Replika löschen (Neustart)“: Alle Pods, die ein Service
// selektiert, werden gelöscht, damit ihr Controller (z. B. ReplicaSet) sie neu erstellt.
package restartservice

import (
	"context"
	"fmt"
	"strings"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/client-go/kubernetes"

	"feature-dashboard/internal/actions"
	"feature-dashboard/internal/actions/nodeportservice"
	"feature-dashboard/internal/model"
)

// ActionName ist der action-name in der config.yaml.
const ActionName = "restart-service-replicas"

// Handler löscht alle Pods eines Services.
type Handler struct {
	client kubernetes.Interface
}

// New ist die Factory für die Registry.
func New(deps actions.Deps) actions.Handler {
	return &Handler{client: deps.Client}
}

// Configure: keine extra-handler-configs.
func (h *Handler) Configure(map[string]any) error { return nil }

// IsApplicable gilt für Services mit Selector und mindestens einem Replica. Ausgenommen sind NodePort-Services,
// die die Aktion node-port-service-on-service-base erstellt hat.
func (h *Handler) IsApplicable(obj actions.StoredObject) bool {
	svc, ok := obj.Object.(model.StoredService)
	if !ok || len(svc.Selector) == 0 || len(svc.Replicas) == 0 {
		return false
	}
	return svc.Labels[nodeportservice.LabelCreatedByAction] != nodeportservice.CreateActionName
}

// FormFields: keine Eingaben nötig.
func (h *Handler) FormFields(actions.StoredObject) []actions.FormField { return nil }

// Execute löscht alle aktuell selektierten Pods. Die Pods werden frisch aus dem Cluster gelesen; Pods ohne
// Controller werden übersprungen, da sie nach dem Löschen nicht neu entstehen würden.
func (h *Handler) Execute(ctx context.Context, obj actions.StoredObject, _ map[string]any) actions.Result {
	svc, ok := obj.Object.(model.StoredService)
	if !ok || len(svc.Selector) == 0 {
		// Ein leerer Selector würde alle Pods des Namespaces treffen.
		return actions.Result{Success: false, Message: "Service hat keinen Selector"}
	}
	podsClient := h.client.CoreV1().Pods(obj.Ref.Namespace)
	pods, err := podsClient.List(ctx, metav1.ListOptions{LabelSelector: labels.SelectorFromSet(svc.Selector).String()})
	if err != nil {
		return actions.Result{Success: false, Message: fmt.Sprintf("Pods konnten nicht gelesen werden: %v", err)}
	}

	var deleted, skipped, failed []string
	for _, pod := range pods.Items {
		switch {
		case pod.DeletionTimestamp != nil:
			continue // wird bereits gelöscht
		case metav1.GetControllerOf(&pod) == nil:
			skipped = append(skipped, pod.Name)
			continue
		}
		opts := metav1.DeleteOptions{Preconditions: metav1.NewUIDPreconditions(string(pod.UID))}
		if err := podsClient.Delete(ctx, pod.Name, opts); err != nil {
			failed = append(failed, fmt.Sprintf("%s (%v)", pod.Name, err))
			continue
		}
		deleted = append(deleted, pod.Name)
	}

	var msg []string
	if len(deleted) > 0 {
		msg = append(msg, fmt.Sprintf("%d Pod(s) gelöscht, sie werden neu gestartet: %s", len(deleted), strings.Join(deleted, ", ")))
	}
	if len(skipped) > 0 {
		msg = append(msg, fmt.Sprintf("Übersprungen, da ohne Controller (würden nicht neu gestartet): %s", strings.Join(skipped, ", ")))
	}
	if len(failed) > 0 {
		msg = append(msg, fmt.Sprintf("Fehlgeschlagen: %s", strings.Join(failed, ", ")))
	}
	if len(msg) == 0 {
		msg = append(msg, "Keine Pods zum Neustarten gefunden")
	}
	return actions.Result{Success: len(failed) == 0 && len(deleted) > 0, Message: strings.Join(msg, ". ")}
}
