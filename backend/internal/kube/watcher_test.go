package kube

import (
	"context"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/client-go/kubernetes/fake"

	"feature-dashboard/internal/config"
	"feature-dashboard/internal/model"
	"feature-dashboard/internal/project"
	"feature-dashboard/internal/store"
)

var featureLabels = map[string]string{"feature": "true", "project": "foo"}

func namespace(name string, lbls map[string]string) *corev1.Namespace {
	return &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: name, Labels: lbls}}
}

func pod(ns, name string, port int32) *corev1.Pod {
	return &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: ns, Labels: map[string]string{"app": "web"}},
		Spec: corev1.PodSpec{Containers: []corev1.Container{{
			Name: "web", Image: "nginx", Ports: []corev1.ContainerPort{{Name: "http", ContainerPort: port}},
		}}},
	}
}

func service(ns string) *corev1.Service {
	return &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{Name: "web", Namespace: ns},
		Spec: corev1.ServiceSpec{
			Selector: map[string]string{"app": "web"},
			Ports:    []corev1.ServicePort{{Name: "http", Port: 80, TargetPort: intstr.FromString("http")}},
		},
	}
}

func eventually(t *testing.T, msg string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal(msg)
}

func resolvedPort(st *store.Store, ns string) int32 {
	var svc model.StoredService
	ok, err := st.Get(model.ObjectRef{Kind: model.KindService, Namespace: ns, Name: "web"}, &svc)
	if !ok || err != nil || len(svc.Ports) == 0 || svc.Ports[0].ResolvedTargetPort == nil {
		return 0
	}
	return *svc.Ports[0].ResolvedTargetPort
}

func TestWatcher(t *testing.T) {
	client := fake.NewClientset(
		namespace("feat-1", featureLabels),
		namespace("other", map[string]string{"feature": "true"}),
		pod("feat-1", "web-1", 8080),
		service("feat-1"),
		pod("other", "web-1", 8080),
	)
	st := store.New()
	matcher := project.NewMatcher([]config.Project{{Name: "foo", NamespaceLabels: featureLabels}})
	w := NewWatcher(client, st, matcher, 0)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go w.Run(ctx)
	eventually(t, "Watcher nicht ready", w.Ready)
	// Der fake-Client kann Events zwischen List und Watch verlieren; kurz warten, bis die Watches stehen.
	time.Sleep(200 * time.Millisecond)

	nss, _, _ := st.Namespaces()
	if len(nss) != 1 || nss[0].Name != "feat-1" || nss[0].Project != "foo" {
		t.Fatalf("Namespaces = %+v", nss)
	}
	if got := resolvedPort(st, "feat-1"); got != 8080 {
		t.Fatalf("aufgelöster targetPort = %d", got)
	}

	// Pod-Update ändert den aufgelösten Port am Service.
	p := pod("feat-1", "web-1", 9090)
	if _, err := client.CoreV1().Pods("feat-1").Update(ctx, p, metav1.UpdateOptions{}); err != nil {
		t.Fatal(err)
	}
	eventually(t, "Port nach Pod-Update nicht aktualisiert", func() bool { return resolvedPort(st, "feat-1") == 9090 })

	// Namespace wird nachträglich zur Feature-Umgebung → Objekte werden übernommen.
	if _, err := client.CoreV1().Services("other").Create(ctx, service("other"), metav1.CreateOptions{}); err != nil {
		t.Fatal(err)
	}
	if _, err := client.CoreV1().Namespaces().Update(ctx, namespace("other", featureLabels), metav1.UpdateOptions{}); err != nil {
		t.Fatal(err)
	}
	eventually(t, "Namespace other nicht übernommen", func() bool { return resolvedPort(st, "other") == 8080 })

	// Label entfernt → Namespace verschwindet.
	if _, err := client.CoreV1().Namespaces().Update(ctx, namespace("feat-1", nil), metav1.UpdateOptions{}); err != nil {
		t.Fatal(err)
	}
	eventually(t, "Namespace feat-1 nicht entfernt", func() bool {
		_, ok := st.NamespaceLastUpdated("feat-1")
		return !ok
	})

	// Full-Rebuild bei unverändertem Cluster ändert keine Timestamps.
	before, _ := st.NamespaceLastUpdated("other")
	beforeProjects := st.ProjectsLastUpdated()
	if err := w.rebuildForTest(ctx); err != nil {
		t.Fatal(err)
	}
	if after, _ := st.NamespaceLastUpdated("other"); after != before || st.ProjectsLastUpdated() != beforeProjects {
		t.Fatal("Rebuild ohne Änderungen darf Timestamps nicht ändern")
	}
}
