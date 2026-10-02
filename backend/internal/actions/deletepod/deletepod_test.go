package deletepod

import (
	"context"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
	k8stesting "k8s.io/client-go/testing"

	"feature-dashboard/internal/actions"
	"feature-dashboard/internal/model"
)

func TestDeletePod(t *testing.T) {
	client := fake.NewClientset(&corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "p1", Namespace: "ns1"}})
	h := New(actions.Deps{Client: client})
	if err := h.Configure(map[string]any{"force-delete": true}); err != nil {
		t.Fatal(err)
	}
	obj := actions.StoredObject{Ref: model.ObjectRef{Kind: model.KindPod, Namespace: "ns1", Name: "p1"}}
	if !h.IsApplicable(obj) {
		t.Fatal("muss für Pods applicable sein")
	}
	if h.IsApplicable(actions.StoredObject{Ref: model.ObjectRef{Kind: model.KindService}}) {
		t.Fatal("darf für Services nicht applicable sein")
	}
	if res := h.Execute(context.Background(), obj, nil); !res.Success {
		t.Fatalf("Result = %+v", res)
	}
	var del k8stesting.DeleteActionImpl
	for _, a := range client.Actions() {
		if d, ok := a.(k8stesting.DeleteActionImpl); ok {
			del = d
		}
	}
	if del.Name != "p1" || del.DeleteOptions.GracePeriodSeconds == nil || *del.DeleteOptions.GracePeriodSeconds != 0 {
		t.Fatalf("Delete-Aufruf = %+v", del)
	}
	if res := h.Execute(context.Background(), obj, nil); res.Success {
		t.Fatal("zweites Löschen muss fehlschlagen")
	}
}

func TestConfigureRejectsInvalid(t *testing.T) {
	if err := New(actions.Deps{}).Configure(map[string]any{"force-delete": "yes"}); err == nil {
		t.Fatal("Fehler erwartet")
	}
}
