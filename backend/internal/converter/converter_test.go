package converter

import (
	"testing"

	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"

	"feature-dashboard/internal/model"
	"feature-dashboard/internal/store"
)

func testPod(name string, port int32) *corev1.Pod {
	return &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: "ns1", Labels: map[string]string{"app": "web"}},
		Spec: corev1.PodSpec{Containers: []corev1.Container{{
			Name: "web", Image: "nginx", Ports: []corev1.ContainerPort{{Name: "http", ContainerPort: port}},
		}}},
		Status: corev1.PodStatus{Phase: corev1.PodRunning, Conditions: []corev1.PodCondition{{Type: corev1.PodReady, Status: corev1.ConditionTrue}}},
	}
}

func testService() *corev1.Service {
	return &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{Name: "web", Namespace: "ns1"},
		Spec: corev1.ServiceSpec{
			Type:     corev1.ServiceTypeClusterIP,
			Selector: map[string]string{"app": "web"},
			Ports: []corev1.ServicePort{
				{Name: "http", Port: 80, TargetPort: intstr.FromString("http")},
				{Name: "metrics", Port: 9090},
			},
		},
	}
}

func testIngress() *networkingv1.Ingress {
	pt := networkingv1.PathTypePrefix
	return &networkingv1.Ingress{
		ObjectMeta: metav1.ObjectMeta{Name: "web", Namespace: "ns1"},
		Spec: networkingv1.IngressSpec{Rules: []networkingv1.IngressRule{{
			Host: "web.example",
			IngressRuleValue: networkingv1.IngressRuleValue{HTTP: &networkingv1.HTTPIngressRuleValue{Paths: []networkingv1.HTTPIngressPath{{
				Path: "/", PathType: &pt,
				Backend: networkingv1.IngressBackend{Service: &networkingv1.IngressServiceBackend{
					Name: "web", Port: networkingv1.ServiceBackendPort{Name: "http"},
				}},
			}}}},
		}}},
	}
}

func newStore(t *testing.T) *store.Store {
	s := store.New()
	err := s.Update(func(tx *store.Tx) error {
		return tx.PutNamespace(model.StoredNamespace{Meta: model.Meta{Name: "ns1"}, Project: "foo"})
	})
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func update(t *testing.T, s *store.Store, fn func(tx *store.Tx) error) {
	t.Helper()
	if err := s.Update(fn); err != nil {
		t.Fatal(err)
	}
}

func getService(t *testing.T, s *store.Store) model.StoredService {
	t.Helper()
	var svc model.StoredService
	if ok, err := s.Get(model.ObjectRef{Kind: model.KindService, Namespace: "ns1", Name: "web"}, &svc); !ok || err != nil {
		t.Fatalf("Service fehlt: %v", err)
	}
	return svc
}

func getIngress(t *testing.T, s *store.Store) model.StoredIngress {
	t.Helper()
	var ing model.StoredIngress
	if ok, err := s.Get(model.ObjectRef{Kind: model.KindIngress, Namespace: "ns1", Name: "web"}, &ing); !ok || err != nil {
		t.Fatalf("Ingress fehlt: %v", err)
	}
	return ing
}

func TestServiceResolutionFollowsPods(t *testing.T) {
	s := newStore(t)
	update(t, s, func(tx *store.Tx) error { return ApplyService(tx, testService()) })
	update(t, s, func(tx *store.Tx) error { return ApplyIngress(tx, testIngress()) })

	svc := getService(t, s)
	if svc.Ports[0].ResolvedTargetPort != nil || len(svc.Replicas) != 0 {
		t.Fatalf("ohne Pods darf nichts aufgelöst sein: %+v", svc)
	}
	if got := *svc.Ports[1].ResolvedTargetPort; got != 9090 {
		t.Errorf("fehlender targetPort muss Service-Port ergeben, got %d", got)
	}
	if got := *getIngress(t, s).Rules[0].Paths[0].Backend.ResolvedServicePortNumber; got != 80 {
		t.Errorf("Ingress-Port = %d", got)
	}

	// Pod-Create löst den Service neu auf.
	update(t, s, func(tx *store.Tx) error { return ApplyPod(tx, testPod("web-1", 8080)) })
	svc = getService(t, s)
	if svc.Ports[0].ResolvedTargetPort == nil || *svc.Ports[0].ResolvedTargetPort != 8080 {
		t.Fatalf("targetPort nicht aufgelöst: %+v", svc.Ports[0])
	}
	if len(svc.Replicas) != 1 || svc.Replicas[0] != "web-1" {
		t.Fatalf("Replicas = %v", svc.Replicas)
	}

	// Pod-Update ändert die aufgelöste Port-Nummer.
	update(t, s, func(tx *store.Tx) error { return ApplyPod(tx, testPod("web-1", 8081)) })
	if got := *getService(t, s).Ports[0].ResolvedTargetPort; got != 8081 {
		t.Fatalf("targetPort nach Pod-Update = %d", got)
	}

	// Pod-Delete entfernt Replica und Auflösung.
	update(t, s, func(tx *store.Tx) error { return Remove(tx, model.KindPod, "ns1", "web-1") })
	svc = getService(t, s)
	if svc.Ports[0].ResolvedTargetPort != nil || len(svc.Replicas) != 0 {
		t.Fatalf("nach Pod-Delete: %+v", svc)
	}

	// Service-Delete entfernt Ingress-Auflösung.
	update(t, s, func(tx *store.Tx) error { return Remove(tx, model.KindService, "ns1", "web") })
	if getIngress(t, s).Rules[0].Paths[0].Backend.ResolvedServicePortNumber != nil {
		t.Fatal("Ingress-Port darf ohne Service nicht aufgelöst sein")
	}
}

func TestObjectsOutsideKnownNamespacesAreDropped(t *testing.T) {
	s := store.New()
	update(t, s, func(tx *store.Tx) error { return ApplyPod(tx, testPod("web-1", 8080)) })
	if _, ok := s.NamespaceLastUpdated("ns1"); ok {
		t.Fatal("Namespace darf nicht entstehen")
	}
}

func TestPodConversion(t *testing.T) {
	p := Pod(testPod("web-1", 8080))
	if !p.Ready || p.Containers[0].Ports[0].Name != "http" {
		t.Fatalf("unerwartet: %+v", p)
	}
}
