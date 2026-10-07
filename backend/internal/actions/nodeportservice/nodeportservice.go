// Package nodeportservice implementiert die Aktion „NodePort-Service auf Basis eines ClusterIP-Services erstellen“.
package nodeportservice

import (
	"context"
	"fmt"
	"log/slog"
	"maps"
	"strconv"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/apimachinery/pkg/util/validation"
	"k8s.io/client-go/kubernetes"

	"feature-dashboard/internal/actions"
	"feature-dashboard/internal/model"
)

const (
	// CreateActionName ist der action-name des Handlers, der NodePort-Services erstellt.
	CreateActionName = "node-port-service-on-service-base"
	// DeleteActionName ist der action-name des Handlers, der sie wieder löscht.
	DeleteActionName = "delete-node-port-service"

	// NameSuffix wird an den Namen des Basis-Services angehängt.
	NameSuffix = "-node-port"
	// FieldPorts ist die Feld-ID der Port-Auswahl im Formular.
	FieldPorts = "ports"

	// LabelCreatedByAction markiert Services, die eine Dashboard-Aktion erstellt hat; Wert ist der action-name.
	// Nur Services mit LabelCreatedByAction = CreateActionName darf DeleteHandler löschen.
	LabelCreatedByAction = "feature-dashboard/created-by-action"
	// AnnotationBaseService enthält den Namen des ClusterIP-Services, auf dem der NodePort-Service basiert.
	AnnotationBaseService = "feature-dashboard/base-service"

	labelManagedBy = "app.kubernetes.io/managed-by"
	managedByValue = "feature-dashboard"
)

// Handler erstellt zu einem ClusterIP-Service einen NodePort-Service „<name>-node-port“ mit demselben Selector
// und den im Formular ausgewählten Port-Mappings. Die NodePorts vergibt Kubernetes zufällig.
// Der NodePort-Service erhält eine ownerReference auf den Basis-Service; wird dieser gelöscht, löscht der
// Garbage-Collector von Kubernetes auch den NodePort-Service.
type Handler struct {
	client kubernetes.Interface
	store  actions.ObjectReader
}

// New ist die Factory für die Registry.
func New(deps actions.Deps) actions.Handler {
	return &Handler{client: deps.Client, store: deps.Store}
}

// Configure: keine extra-handler-configs.
func (h *Handler) Configure(map[string]any) error { return nil }

// IsApplicable gilt für ClusterIP-Services mit Selector und Ports, zu denen es noch keinen NodePort-Service gibt.
func (h *Handler) IsApplicable(obj actions.StoredObject) bool {
	svc, ok := obj.Object.(model.StoredService)
	if !ok || svc.Type != string(corev1.ServiceTypeClusterIP) || len(svc.Selector) == 0 || len(svc.Ports) == 0 {
		return false
	}
	name := svc.Name + NameSuffix
	if len(validation.IsDNS1035Label(name)) > 0 {
		return false
	}
	exists, err := h.store.Get(model.ObjectRef{Kind: model.KindService, Namespace: obj.Ref.Namespace, Name: name}, &model.StoredService{})
	if err != nil {
		slog.Warn("Prüfung auf vorhandenen NodePort-Service fehlgeschlagen", "service", obj.Ref, "error", err)
		return false
	}
	return !exists
}

// FormFields: Auswahl der Target-Ports des Basis-Services, zu denen ein Port-Mapping erstellt wird.
func (h *Handler) FormFields(obj actions.StoredObject) []actions.FormField {
	svc, _ := obj.Object.(model.StoredService)
	options := make([]actions.Option, 0, len(svc.Ports))
	for _, p := range svc.Ports {
		options = append(options, actions.Option{Value: portKey(p), Label: portLabel(p)})
	}
	return []actions.FormField{{
		ID:          FieldPorts,
		Label:       "Target-Ports",
		Type:        actions.FieldMultiSelect,
		IsRequired:  true,
		Description: "Target-Ports, für die der NodePort-Service ein Port-Mapping erhält. Die NodePorts werden zufällig vergeben.",
		Options:     options,
	}}
}

// Execute erstellt den NodePort-Service. inputs[FieldPorts] ist durch die Registry bereits validiert.
func (h *Handler) Execute(ctx context.Context, obj actions.StoredObject, inputs map[string]any) actions.Result {
	base, ok := obj.Object.(model.StoredService)
	if !ok {
		return actions.Result{Success: false, Message: "Objekt ist kein Service"}
	}
	selected := map[string]bool{}
	if list, ok := inputs[FieldPorts].([]any); ok {
		for _, v := range list {
			if s, ok := v.(string); ok {
				selected[s] = true
			}
		}
	}

	// Der Basis-Service wird frisch gelesen: Die ownerReference braucht seine UID, und der Cache-Stand kann
	// veraltet sein.
	current, err := h.client.CoreV1().Services(obj.Ref.Namespace).Get(ctx, base.Name, metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		return actions.Result{Success: false, Message: fmt.Sprintf("Service %s existiert nicht mehr", base.Name)}
	} else if err != nil {
		return actions.Result{Success: false, Message: fmt.Sprintf("Service %s konnte nicht gelesen werden: %v", base.Name, err)}
	}
	if current.Spec.Type != corev1.ServiceTypeClusterIP {
		return actions.Result{Success: false, Message: fmt.Sprintf("Service %s ist nicht mehr vom Typ ClusterIP", base.Name)}
	}

	svc := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:        base.Name + NameSuffix,
			Namespace:   obj.Ref.Namespace,
			Labels:      map[string]string{labelManagedBy: managedByValue, LabelCreatedByAction: CreateActionName},
			Annotations: map[string]string{AnnotationBaseService: base.Name},
			// Ohne Controller/BlockOwnerDeletion: dafür bräuchte das Backend zusätzlich update auf
			// services/finalizers. Für das Löschen durch den Garbage-Collector genügt die Referenz.
			OwnerReferences: []metav1.OwnerReference{{
				APIVersion: "v1",
				Kind:       "Service",
				Name:       current.Name,
				UID:        current.UID,
			}},
		},
		Spec: corev1.ServiceSpec{
			Type:     corev1.ServiceTypeNodePort,
			Selector: maps.Clone(base.Selector),
		},
	}
	for _, p := range base.Ports {
		if !selected[portKey(p)] {
			continue
		}
		svc.Spec.Ports = append(svc.Spec.Ports, corev1.ServicePort{
			Name:       p.Name,
			Protocol:   corev1.Protocol(p.Protocol),
			Port:       p.Port,
			TargetPort: intstr.Parse(p.TargetPort),
			// NodePort 0: Kubernetes vergibt einen zufälligen freien Port.
		})
	}
	if len(svc.Spec.Ports) == 0 {
		return actions.Result{Success: false, Message: "Keine gültigen Ports ausgewählt"}
	}

	created, err := h.client.CoreV1().Services(obj.Ref.Namespace).Create(ctx, svc, metav1.CreateOptions{})
	if err != nil {
		return actions.Result{Success: false, Message: fmt.Sprintf("NodePort-Service %s konnte nicht erstellt werden: %v", svc.Name, err)}
	}
	return actions.Result{Success: true, Message: fmt.Sprintf("NodePort-Service %s erstellt (%s)", created.Name, nodePorts(created))}
}

// portKey identifiziert einen Service-Port eindeutig: Namen sind bei mehreren Ports Pflicht und eindeutig,
// ein einzelner Port darf unbenannt sein.
func portKey(p model.ServicePort) string {
	if p.Name != "" {
		return p.Name
	}
	return strconv.Itoa(int(p.Port))
}

// portLabel beschreibt einen Service-Port für die Auswahl im Formular:
//   - benannter Port:   „<Target-Port-Nummer> (<Port-Name>)“, z. B. „80 (http)“;
//     ist der Target-Port nicht auflösbar, steht statt der Nummer sein Rohwert.
//   - unbenannter Port: „Service-Port=<Port>, Container-Port=<Target-Port-Nummer oder Rohwert>“.
func portLabel(p model.ServicePort) string {
	var label string
	if p.ResolvedTargetPort != nil && p.Name != "" {
		label = fmt.Sprintf("%d (%s)", *p.ResolvedTargetPort, p.Name)
	} else if p.Name != "" {
		label = fmt.Sprintf("%s (%s)", p.TargetPort, p.Name)
	} else if p.ResolvedTargetPort != nil {
		label = fmt.Sprintf("Service-Port=%d, Container-Port=%d", p.Port, *p.ResolvedTargetPort)
	} else {
		label = fmt.Sprintf("Service-Port=%d, Container-Port=%s", p.Port, p.TargetPort)
	}
	return label
}

func nodePorts(svc *corev1.Service) string {
	result := ""
	for i, p := range svc.Spec.Ports {
		if i > 0 {
			result += ", "
		}
		result += fmt.Sprintf("%d → NodePort %d", p.Port, p.NodePort)
	}
	return result
}
