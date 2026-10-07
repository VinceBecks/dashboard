package converter

import (
	"testing"

	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"

	"feature-dashboard/internal/model"
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

func namespace() *corev1.Namespace {
	return &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "ns1", Labels: map[string]string{"a": "b"}}}
}

func view(pods ...*corev1.Pod) model.NamespaceView {
	return NamespaceView(namespace(), "foo", pods, []*corev1.Service{testService()}, []*networkingv1.Ingress{testIngress()})
}

func TestNamespaceViewResolvesServices(t *testing.T) {
	// Ohne Pods: kein Replica, benannter targetPort nicht auflösbar, fehlender targetPort = Service-Port.
	svc := view().Services[0]
	if svc.Ports[0].ResolvedTargetPort != nil || len(svc.Replicas) != 0 {
		t.Fatalf("ohne Pods darf nichts aufgelöst sein: %+v", svc)
	}
	if got := *svc.Ports[1].ResolvedTargetPort; got != 9090 {
		t.Errorf("fehlender targetPort muss Service-Port ergeben, got %d", got)
	}

	// Mit Pod: Replica und Port aus dem Container.
	svc = view(testPod("web-1", 8080)).Services[0]
	if svc.Ports[0].ResolvedTargetPort == nil || *svc.Ports[0].ResolvedTargetPort != 8080 {
		t.Fatalf("targetPort nicht aufgelöst: %+v", svc.Ports[0])
	}
	if len(svc.Replicas) != 1 || svc.Replicas[0] != "web-1" {
		t.Fatalf("Replicas = %v", svc.Replicas)
	}

	// Geänderter Container-Port ändert die Auflösung.
	if got := *view(testPod("web-1", 8081)).Services[0].Ports[0].ResolvedTargetPort; got != 8081 {
		t.Fatalf("targetPort = %d", got)
	}

	// Pods, die der Selector nicht trifft, sind keine Replicas.
	other := testPod("db-1", 5432)
	other.Labels = map[string]string{"app": "db"}
	if svc := view(other).Services[0]; len(svc.Replicas) != 0 || svc.Ports[0].ResolvedTargetPort != nil {
		t.Fatalf("fremder Pod: %+v", svc)
	}
}

func TestNamespaceViewSortsAndConverts(t *testing.T) {
	v := view(testPod("web-2", 8080), testPod("web-1", 8080))
	if v.Namespace.Name != "ns1" || v.Namespace.Project != "foo" || v.Namespace.Labels["a"] != "b" {
		t.Fatalf("Namespace = %+v", v.Namespace)
	}
	if v.Pods[0].Name != "web-1" || v.Pods[1].Name != "web-2" {
		t.Fatalf("Pods nicht sortiert: %v, %v", v.Pods[0].Name, v.Pods[1].Name)
	}
	if got := v.Services[0].Replicas; len(got) != 2 || got[0] != "web-1" {
		t.Fatalf("Replicas = %v", got)
	}
	if r := v.Ingresses[0].Rules[0]; r.Host != "web.example" || r.Paths[0].Path != "/" || r.Paths[0].Backend.ServiceName != "web" {
		t.Fatalf("Ingress = %+v", v.Ingresses[0])
	}
}

func TestConversionDoesNotShareMaps(t *testing.T) {
	ns := namespace()
	v := NamespaceView(ns, "foo", nil, nil, nil)
	v.Namespace.Labels["a"] = "geändert"
	if ns.Labels["a"] != "b" {
		t.Fatal("Stored-Object darf den Cache nicht verändern")
	}
}

func TestPodConversion(t *testing.T) {
	p := Pod(testPod("web-1", 8080))
	if !p.Ready || p.Containers[0].Ports[0].Name != "http" {
		t.Fatalf("unerwartet: %+v", p)
	}
}
