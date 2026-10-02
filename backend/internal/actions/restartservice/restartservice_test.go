package restartservice

import (
	"context"
	"errors"
	"strings"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/fake"
	k8stesting "k8s.io/client-go/testing"

	"feature-dashboard/internal/actions"
	"feature-dashboard/internal/actions/nodeportservice"
	"feature-dashboard/internal/model"
)

func service(labels map[string]string, selector map[string]string, replicas ...string) actions.StoredObject {
	svc := model.StoredService{
		Meta: model.Meta{Name: "web", Namespace: "feat-1", Labels: labels}, Type: "ClusterIP",
		Selector: selector, Replicas: replicas,
	}
	return actions.StoredObject{Ref: model.ObjectRef{Kind: model.KindService, Namespace: "feat-1", Name: "web"}, Object: svc}
}

func pod(name string, labels map[string]string, controlled bool) *corev1.Pod {
	p := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: "feat-1", Labels: labels, UID: types.UID("uid-" + name)}}
	if controlled {
		yes := true
		p.OwnerReferences = []metav1.OwnerReference{{Kind: "ReplicaSet", Name: "web-rs", Controller: &yes}}
	}
	return p
}

func TestApplicability(t *testing.T) {
	h := New(actions.Deps{})
	sel := map[string]string{"app": "web"}
	if !h.IsApplicable(service(nil, sel, "web-1")) {
		t.Fatal("Service mit Replicas muss applicable sein")
	}
	if h.IsApplicable(service(nil, nil, "web-1")) {
		t.Fatal("Service ohne Selector darf nicht applicable sein")
	}
	if h.IsApplicable(service(nil, sel)) {
		t.Fatal("Service ohne Replicas darf nicht applicable sein")
	}
	created := map[string]string{nodeportservice.LabelCreatedByAction: nodeportservice.CreateActionName}
	if h.IsApplicable(service(created, sel, "web-1")) {
		t.Fatal("vom Dashboard erstellte NodePort-Services dürfen die Aktion nicht haben")
	}
	pod := actions.StoredObject{Ref: model.ObjectRef{Kind: model.KindPod}, Object: model.StoredPod{}}
	if h.IsApplicable(pod) {
		t.Fatal("Pods dürfen die Aktion nicht haben")
	}
}

func TestExecute(t *testing.T) {
	web := map[string]string{"app": "web"}
	terminating := pod("web-terminating", web, true)
	now := metav1.Now()
	terminating.DeletionTimestamp = &now
	client := fake.NewClientset(
		pod("web-1", web, true),
		pod("web-2", web, true),
		pod("web-bare", web, false),
		terminating,
		pod("other", map[string]string{"app": "other"}, true),
	)
	h := New(actions.Deps{Client: client})

	res := h.Execute(context.Background(), service(nil, web, "web-1", "web-2"), nil)
	if !res.Success || !strings.Contains(res.Message, "2 Pod(s) gelöscht") || !strings.Contains(res.Message, "web-bare") {
		t.Fatalf("res = %+v", res)
	}
	remaining, _ := client.CoreV1().Pods("feat-1").List(context.Background(), metav1.ListOptions{})
	var names []string
	for _, p := range remaining.Items {
		names = append(names, p.Name)
	}
	if strings.Join(names, ",") != "other,web-bare,web-terminating" {
		t.Fatalf("verbleibende Pods = %v", names)
	}
}

func TestExecuteReportsFailures(t *testing.T) {
	web := map[string]string{"app": "web"}
	client := fake.NewClientset(pod("web-1", web, true), pod("web-2", web, true))
	client.PrependReactor("delete", "pods", func(a k8stesting.Action) (bool, runtime.Object, error) {
		if a.(k8stesting.DeleteAction).GetName() == "web-2" {
			return true, nil, errors.New("forbidden")
		}
		return false, nil, nil
	})
	res := New(actions.Deps{Client: client}).Execute(context.Background(), service(nil, web, "web-1", "web-2"), nil)
	if res.Success || !strings.Contains(res.Message, "web-2 (forbidden)") || !strings.Contains(res.Message, "1 Pod(s) gelöscht") {
		t.Fatalf("res = %+v", res)
	}
}

func TestExecuteRejectsEmptySelector(t *testing.T) {
	client := fake.NewClientset(pod("web-1", nil, true))
	res := New(actions.Deps{Client: client}).Execute(context.Background(), service(nil, nil, "web-1"), nil)
	if res.Success {
		t.Fatal("ohne Selector darf nichts gelöscht werden")
	}
	if list, _ := client.CoreV1().Pods("feat-1").List(context.Background(), metav1.ListOptions{}); len(list.Items) != 1 {
		t.Fatal("Pod muss erhalten bleiben")
	}
}
