package nodeportservice

import (
	"context"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"

	"feature-dashboard/internal/actions"
	"feature-dashboard/internal/model"
)

func storedNodePort(name string, labels map[string]string) actions.StoredObject {
	svc := model.StoredService{Meta: model.Meta{Name: name, Namespace: "feat-1", Labels: labels}, Type: "NodePort"}
	return obj(svc)
}

func clusterService(name string, labels map[string]string) *corev1.Service {
	return &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: "feat-1", Labels: labels, UID: "uid-1"},
		Spec:       corev1.ServiceSpec{Type: corev1.ServiceTypeNodePort},
	}
}

func TestDeleteApplicability(t *testing.T) {
	h := NewDelete(actions.Deps{})
	created := map[string]string{LabelCreatedByAction: CreateActionName}
	if !h.IsApplicable(storedNodePort("web-node-port", created)) {
		t.Fatal("Aktion für erstellten NodePort-Service erwartet")
	}
	if h.IsApplicable(storedNodePort("manual", nil)) {
		t.Fatal("keine Aktion für manuell erstellte NodePort-Services")
	}
	if h.IsApplicable(storedNodePort("other", map[string]string{LabelCreatedByAction: "other-action"})) {
		t.Fatal("keine Aktion für Services anderer Aktionen")
	}
	clusterIP := model.StoredService{Meta: model.Meta{Name: "web", Labels: created}, Type: "ClusterIP"}
	if h.IsApplicable(obj(clusterIP)) {
		t.Fatal("keine Aktion für ClusterIP-Services")
	}
}

func TestDeleteExecute(t *testing.T) {
	created := map[string]string{LabelCreatedByAction: CreateActionName}
	client := fake.NewClientset(
		clusterService("web-node-port", created),
		clusterService("relabeled", nil),
	)
	h := NewDelete(actions.Deps{Client: client})
	ctx := context.Background()

	if res := h.Execute(ctx, storedNodePort("web-node-port", created), nil); !res.Success {
		t.Fatalf("res = %+v", res)
	}
	if _, err := client.CoreV1().Services("feat-1").Get(ctx, "web-node-port", metav1.GetOptions{}); err == nil {
		t.Fatal("Service muss gelöscht sein")
	}

	// Store-Stand veraltet: im Cluster fehlt das Label inzwischen → nicht löschen.
	if res := h.Execute(ctx, storedNodePort("relabeled", created), nil); res.Success {
		t.Fatal("Service ohne Label darf nicht gelöscht werden")
	}
	if _, err := client.CoreV1().Services("feat-1").Get(ctx, "relabeled", metav1.GetOptions{}); err != nil {
		t.Fatal("Service muss erhalten bleiben")
	}

	if res := h.Execute(ctx, storedNodePort("web-node-port", created), nil); res.Success {
		t.Fatal("Löschen eines nicht existierenden Services muss fehlschlagen")
	}
}

func TestCreateThenDelete(t *testing.T) {
	base := baseService()
	reg, client := setup(t, base)
	ctx := context.Background()
	if res, err := reg.Execute(ctx, CreateActionName, obj(base), map[string]any{FieldPorts: []any{"http"}}); err != nil || !res.Success {
		t.Fatalf("Erstellen: res = %+v, err = %v", res, err)
	}
	svc, err := client.CoreV1().Services("feat-1").Get(ctx, "web-node-port", metav1.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}
	stored := storedNodePort(svc.Name, svc.Labels)
	h := NewDelete(actions.Deps{Client: client})
	if !h.IsApplicable(stored) {
		t.Fatal("Lösch-Aktion muss für den erstellten Service verfügbar sein")
	}
	if res := h.Execute(ctx, stored, nil); !res.Success {
		t.Fatalf("Löschen: res = %+v", res)
	}
}
