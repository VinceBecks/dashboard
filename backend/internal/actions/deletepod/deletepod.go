// Package deletepod implementiert die Aktion „Pod löschen“.
package deletepod

import (
	"context"
	"fmt"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"

	"feature-dashboard/internal/actions"
	"feature-dashboard/internal/model"
)

// Handler löscht einen Pod. Mit extra-handler-config force-delete: true ohne Grace-Period.
type Handler struct {
	client      kubernetes.Interface
	forceDelete bool
}

// New ist die Factory für die Registry.
func New(deps actions.Deps) actions.Handler {
	return &Handler{client: deps.Client}
}

// Configure liest force-delete.
func (h *Handler) Configure(extra map[string]any) error {
	v, ok := extra["force-delete"]
	if !ok {
		return nil
	}
	b, ok := v.(bool)
	if !ok {
		return fmt.Errorf("force-delete muss true oder false sein")
	}
	h.forceDelete = b
	return nil
}

// IsApplicable gilt für alle Pods.
func (h *Handler) IsApplicable(obj actions.StoredObject) bool {
	return obj.Ref.Kind == model.KindPod
}

// FormFields: keine Eingaben nötig.
func (h *Handler) FormFields(actions.StoredObject) []actions.FormField { return nil }

// Execute löscht den Pod.
func (h *Handler) Execute(ctx context.Context, obj actions.StoredObject, _ map[string]any) actions.Result {
	opts := metav1.DeleteOptions{}
	if h.forceDelete {
		zero := int64(0)
		opts.GracePeriodSeconds = &zero
	}
	if err := h.client.CoreV1().Pods(obj.Ref.Namespace).Delete(ctx, obj.Ref.Name, opts); err != nil {
		return actions.Result{Success: false, Message: fmt.Sprintf("Pod %s konnte nicht gelöscht werden: %v", obj.Ref.Name, err)}
	}
	return actions.Result{Success: true, Message: fmt.Sprintf("Pod %s wird gelöscht", obj.Ref.Name)}
}
