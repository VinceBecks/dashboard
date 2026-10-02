package nodeportservice

import (
	"context"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"

	"feature-dashboard/internal/actions"
	"feature-dashboard/internal/model"
)

// DeleteHandler löscht einen NodePort-Service, den Handler (CreateActionName) erstellt hat.
// Erkannt wird das ausschließlich am Label LabelCreatedByAction.
type DeleteHandler struct {
	client kubernetes.Interface
}

// NewDelete ist die Factory für die Registry.
func NewDelete(deps actions.Deps) actions.Handler {
	return &DeleteHandler{client: deps.Client}
}

// Configure: keine extra-handler-configs.
func (h *DeleteHandler) Configure(map[string]any) error { return nil }

// IsApplicable gilt nur für NodePort-Services mit LabelCreatedByAction = CreateActionName.
func (h *DeleteHandler) IsApplicable(obj actions.StoredObject) bool {
	svc, ok := obj.Object.(model.StoredService)
	return ok && createdByAction(svc.Type, svc.Labels)
}

// FormFields: keine Eingaben nötig.
func (h *DeleteHandler) FormFields(actions.StoredObject) []actions.FormField { return nil }

// Execute löscht den Service. Vorher wird am aktuellen Cluster-Objekt geprüft, dass er weiterhin von der
// Aktion stammt; die UID-Precondition verhindert, dass ein zwischenzeitlich neu angelegter Service gelöscht wird.
func (h *DeleteHandler) Execute(ctx context.Context, obj actions.StoredObject, _ map[string]any) actions.Result {
	services := h.client.CoreV1().Services(obj.Ref.Namespace)
	svc, err := services.Get(ctx, obj.Ref.Name, metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		return actions.Result{Success: false, Message: fmt.Sprintf("Service %s existiert nicht mehr", obj.Ref.Name)}
	} else if err != nil {
		return actions.Result{Success: false, Message: fmt.Sprintf("Service %s konnte nicht gelesen werden: %v", obj.Ref.Name, err)}
	}
	if !createdByAction(string(svc.Spec.Type), svc.Labels) {
		return actions.Result{Success: false, Message: fmt.Sprintf("Service %s wurde nicht vom Dashboard erstellt und wird nicht gelöscht", svc.Name)}
	}
	err = services.Delete(ctx, svc.Name, metav1.DeleteOptions{Preconditions: metav1.NewUIDPreconditions(string(svc.UID))})
	if err != nil {
		return actions.Result{Success: false, Message: fmt.Sprintf("Service %s konnte nicht gelöscht werden: %v", svc.Name, err)}
	}
	return actions.Result{Success: true, Message: fmt.Sprintf("NodePort-Service %s wird gelöscht", svc.Name)}
}

func createdByAction(serviceType string, labels map[string]string) bool {
	return serviceType == string(corev1.ServiceTypeNodePort) && labels[LabelCreatedByAction] == CreateActionName
}
