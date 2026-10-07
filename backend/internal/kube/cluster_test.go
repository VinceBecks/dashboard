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

func resolvedPort(c *Cluster, ns string) int32 {
	var svc model.StoredService
	ok, err := c.Get(model.ObjectRef{Kind: model.KindService, Namespace: ns, Name: "web"}, &svc)
	if !ok || err != nil || len(svc.Ports) == 0 || svc.Ports[0].ResolvedTargetPort == nil {
		return 0
	}
	return *svc.Ports[0].ResolvedTargetPort
}

func TestCluster(t *testing.T) {
	client := fake.NewClientset(
		namespace("feat-1", featureLabels),
		namespace("other", map[string]string{"feature": "true"}),
		pod("feat-1", "web-1", 8080),
		service("feat-1"),
		pod("other", "web-1", 8080),
	)
	matcher := project.NewMatcher([]config.Project{{Name: "foo", NamespaceLabels: featureLabels}})
	c := NewCluster(client, matcher)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go c.Run(ctx)
	eventually(t, "Cluster nicht ready", c.Ready)
	// Der fake-Client kann Events zwischen List und Watch verlieren; kurz warten, bis die Watches stehen.
	time.Sleep(200 * time.Millisecond)

	revs := c.NamespaceRevisions()
	if _, ok := revs["feat-1"]; !ok || len(revs) != 1 {
		t.Fatalf("Revisionen = %v", revs)
	}
	view, _, ok, err := c.Namespace("feat-1")
	if err != nil || !ok || view.Namespace.Project != "foo" || len(view.Pods) != 1 || len(view.Services) != 1 {
		t.Fatalf("Namespace = %+v, %v, %v", view, ok, err)
	}
	if _, _, ok, _ := c.Namespace("other"); ok {
		t.Fatal("Namespace ohne Projekt darf nicht geliefert werden")
	}
	if got := resolvedPort(c, "feat-1"); got != 8080 {
		t.Fatalf("aufgelöster targetPort = %d", got)
	}

	// Pod-Update ändert den aufgelösten Port am Service und erhöht die Revision nur von feat-1.
	before := c.NamespaceRevisions()["feat-1"]
	drain(c)
	if _, err := client.CoreV1().Pods("feat-1").Update(ctx, pod("feat-1", "web-1", 9090), metav1.UpdateOptions{}); err != nil {
		t.Fatal(err)
	}
	eventually(t, "Port nach Pod-Update nicht aktualisiert", func() bool { return resolvedPort(c, "feat-1") == 9090 })
	if !c.NamespaceRevisions()["feat-1"].After(before) {
		t.Fatal("Revision von feat-1 muss steigen")
	}
	select {
	case <-c.Changes():
	default:
		t.Fatal("Änderungs-Signal fehlt")
	}

	// Pod in fremdem Namespace → keine Revision.
	if _, err := client.CoreV1().Pods("other").Update(ctx, pod("other", "web-1", 9999), metav1.UpdateOptions{}); err != nil {
		t.Fatal(err)
	}
	time.Sleep(100 * time.Millisecond)
	if _, ok := c.NamespaceRevisions()["other"]; ok {
		t.Fatal("fremder Namespace darf keine Revision bekommen")
	}

	// Namespace wird nachträglich zur Feature-Umgebung → Objekte sind sofort lesbar.
	if _, err := client.CoreV1().Services("other").Create(ctx, service("other"), metav1.CreateOptions{}); err != nil {
		t.Fatal(err)
	}
	if _, err := client.CoreV1().Namespaces().Update(ctx, namespace("other", featureLabels), metav1.UpdateOptions{}); err != nil {
		t.Fatal(err)
	}
	eventually(t, "Namespace other nicht übernommen", func() bool { return resolvedPort(c, "other") == 9999 })

	// Label entfernt → Namespace verschwindet aus den Revisionen.
	if _, err := client.CoreV1().Namespaces().Update(ctx, namespace("feat-1", nil), metav1.UpdateOptions{}); err != nil {
		t.Fatal(err)
	}
	eventually(t, "Namespace feat-1 nicht entfernt", func() bool {
		_, ok := c.NamespaceRevisions()["feat-1"]
		return !ok
	})
	if services, err := c.Services("feat-1"); err != nil || services == nil || len(services) != 0 {
		t.Fatalf("Services eines entfernten Namespaces = %#v, %v", services, err)
	}
}

// drain leert ein anstehendes Änderungs-Signal.
func drain(c *Cluster) {
	select {
	case <-c.Changes():
	default:
	}
}

func TestTrimKeepsWhatTheConverterReads(t *testing.T) {
	p := pod("feat-1", "web-1", 8080)
	p.Spec.Containers[0].Env = []corev1.EnvVar{{Name: "SECRET", Value: "x"}}
	p.Spec.Volumes = []corev1.Volume{{Name: "data"}}
	p.Status.Conditions = []corev1.PodCondition{{Type: corev1.PodScheduled, Status: corev1.ConditionTrue}, {Type: corev1.PodReady, Status: corev1.ConditionTrue}}
	p.ManagedFields = []metav1.ManagedFieldsEntry{{Manager: "kubectl"}}

	obj, err := trim(p)
	if err != nil {
		t.Fatal(err)
	}
	got := obj.(*corev1.Pod)
	c := got.Spec.Containers[0]
	if c.Name != "web" || c.Image != "nginx" || c.Ports[0].ContainerPort != 8080 || c.Env != nil {
		t.Fatalf("Container = %+v", c)
	}
	if got.Spec.Volumes != nil || got.ManagedFields != nil || len(got.Status.Conditions) != 1 || got.Status.Conditions[0].Type != corev1.PodReady {
		t.Fatalf("Pod = %+v", got)
	}
	if got.Labels["app"] != "web" {
		t.Fatal("Labels müssen erhalten bleiben")
	}
}
