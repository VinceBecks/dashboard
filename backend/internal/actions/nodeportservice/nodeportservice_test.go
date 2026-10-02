package nodeportservice

import (
	"context"
	"errors"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/client-go/kubernetes/fake"

	"feature-dashboard/internal/actions"
	"feature-dashboard/internal/config"
	"feature-dashboard/internal/model"
	"feature-dashboard/internal/store"
)

const actionName = CreateActionName

func ptr(v int32) *int32 { return &v }

func baseService() model.StoredService {
	return model.StoredService{
		Meta:     model.Meta{Name: "web", Namespace: "feat-1"},
		Type:     "ClusterIP",
		Selector: map[string]string{"app": "web"},
		Ports: []model.ServicePort{
			{Name: "http", Protocol: "TCP", Port: 80, TargetPort: "http", ResolvedTargetPort: ptr(8080)},
			{Name: "metrics", Protocol: "TCP", Port: 9090, TargetPort: "9091", ResolvedTargetPort: ptr(9091)},
		},
	}
}

func obj(svc model.StoredService) actions.StoredObject {
	return actions.StoredObject{Ref: model.ObjectRef{Kind: model.KindService, Namespace: "feat-1", Name: svc.Name}, Object: svc}
}

func setup(t *testing.T, services ...model.StoredService) (*actions.Registry, *fake.Clientset) {
	t.Helper()
	st := store.New()
	err := st.Update(func(tx *store.Tx) error {
		if err := tx.PutNamespace(model.StoredNamespace{Meta: model.Meta{Name: "feat-1"}, Project: "foo"}); err != nil {
			return err
		}
		for _, s := range services {
			if _, err := tx.Put(model.KindService, "feat-1", s.Name, s); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	client := fake.NewClientset()
	reg := actions.NewRegistry()
	reg.Register(actionName, New)
	if err := reg.Activate([]config.ActionHandlerConfig{{Name: actionName, Enabled: true}}, actions.Deps{Client: client, Store: st}); err != nil {
		t.Fatal(err)
	}
	return reg, client
}

func TestApplicability(t *testing.T) {
	base := baseService()
	reg, _ := setup(t, base)
	if got := reg.ActionsFor(obj(base)); len(got) != 1 {
		t.Fatalf("Aktion für ClusterIP-Service erwartet, got %+v", got)
	}

	nodePort := base
	nodePort.Name = "other"
	nodePort.Type = "NodePort"
	if got := reg.ActionsFor(obj(nodePort)); len(got) != 0 {
		t.Fatal("keine Aktion für NodePort-Services erwartet")
	}

	noSelector := base
	noSelector.Selector = nil
	if got := reg.ActionsFor(obj(noSelector)); len(got) != 0 {
		t.Fatal("keine Aktion für Services ohne Selector erwartet")
	}

	pod := actions.StoredObject{Ref: model.ObjectRef{Kind: model.KindPod, Namespace: "feat-1", Name: "web"}, Object: model.StoredPod{}}
	if got := reg.ActionsFor(pod); len(got) != 0 {
		t.Fatal("keine Aktion für Pods erwartet")
	}

	existing := model.StoredService{Meta: model.Meta{Name: "web-node-port", Namespace: "feat-1"}, Type: "NodePort"}
	reg, _ = setup(t, base, existing)
	if got := reg.ActionsFor(obj(base)); len(got) != 0 {
		t.Fatal("keine Aktion, wenn der NodePort-Service bereits existiert")
	}
}

func TestFormFields(t *testing.T) {
	reg, _ := setup(t, baseService())
	fields := reg.ActionsFor(obj(baseService()))[0].FormFields
	if len(fields) != 1 || fields[0].ID != FieldPorts || fields[0].Type != actions.FieldMultiSelect || !fields[0].IsRequired {
		t.Fatalf("FormFields = %+v", fields)
	}
	want := []actions.Option{
		{Value: "http", Label: "http: Target-Port http (8080) (Service-Port 80)"},
		{Value: "metrics", Label: "metrics: Target-Port 9091 (Service-Port 9090)"},
	}
	for i, o := range fields[0].Options {
		if o != want[i] {
			t.Errorf("Option %d = %+v, want %+v", i, o, want[i])
		}
	}
}

func TestExecute(t *testing.T) {
	base := baseService()
	reg, client := setup(t, base)
	ctx := context.Background()

	var verr *actions.ValidationError
	if _, err := reg.Execute(ctx, actionName, obj(base), map[string]any{}); !errors.As(err, &verr) {
		t.Fatalf("Pflichtfeld-Fehler erwartet, got %v", err)
	}
	if _, err := reg.Execute(ctx, actionName, obj(base), map[string]any{FieldPorts: []any{"unknown"}}); !errors.As(err, &verr) {
		t.Fatalf("Fehler bei unbekanntem Port erwartet, got %v", err)
	}

	res, err := reg.Execute(ctx, actionName, obj(base), map[string]any{FieldPorts: []any{"http"}})
	if err != nil || !res.Success {
		t.Fatalf("res = %+v, err = %v", res, err)
	}
	svc, err := client.CoreV1().Services("feat-1").Get(ctx, "web-node-port", metav1.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if svc.Spec.Type != corev1.ServiceTypeNodePort || svc.Spec.Selector["app"] != "web" {
		t.Fatalf("Spec = %+v", svc.Spec)
	}
	wantPorts := []corev1.ServicePort{{Name: "http", Protocol: "TCP", Port: 80, TargetPort: intstr.FromString("http")}}
	if len(svc.Spec.Ports) != 1 || svc.Spec.Ports[0] != wantPorts[0] {
		t.Fatalf("Ports = %+v", svc.Spec.Ports)
	}
	if svc.Annotations[AnnotationBaseService] != "web" || svc.Labels[labelManagedBy] != managedByValue ||
		svc.Labels[LabelCreatedByAction] != CreateActionName {
		t.Fatalf("Metadaten = %+v %+v", svc.Labels, svc.Annotations)
	}

	// Existiert der Service im Cluster schon (Store noch nicht aktualisiert), meldet die Aktion einen Fehler.
	res, err = reg.Execute(ctx, actionName, obj(base), map[string]any{FieldPorts: []any{"metrics"}})
	if err != nil || res.Success {
		t.Fatalf("Fehlschlag erwartet, res = %+v, err = %v", res, err)
	}
}

func TestNumericTargetPort(t *testing.T) {
	base := baseService()
	reg, client := setup(t, base)
	ctx := context.Background()
	if res, err := reg.Execute(ctx, actionName, obj(base), map[string]any{FieldPorts: []any{"metrics"}}); err != nil || !res.Success {
		t.Fatalf("res = %+v, err = %v", res, err)
	}
	svc, _ := client.CoreV1().Services("feat-1").Get(ctx, "web-node-port", metav1.GetOptions{})
	if got := svc.Spec.Ports[0].TargetPort; got != intstr.FromInt32(9091) {
		t.Fatalf("TargetPort = %+v", got)
	}
}
