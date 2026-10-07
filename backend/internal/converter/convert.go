// Package converter wandelt Kubernetes-Objekte aus dem Informer-Cache in Stored-Objects um und löst Referenzen auf.
//
// Die Eingaben stammen aus den Listern und zeigen direkt in den Cache: Sie werden nur gelesen, nie verändert. Alle
// Maps werden geklont, damit niemand über ein Stored-Object den Cache verändert.
package converter

import (
	"maps"
	"slices"
	"strconv"
	"strings"

	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	"k8s.io/apimachinery/pkg/util/intstr"

	"feature-dashboard/internal/model"
)

// Namespace konvertiert einen Namespace, der zum angegebenen Projekt gehört.
func Namespace(ns *corev1.Namespace, project string) model.StoredNamespace {
	return model.StoredNamespace{
		Meta:    model.Meta{Name: ns.Name, Labels: maps.Clone(ns.Labels), Annotations: maps.Clone(ns.Annotations)},
		Project: project,
	}
}

// Pod konvertiert einen Pod.
func Pod(p *corev1.Pod) model.StoredPod {
	sp := model.StoredPod{
		Meta:       meta(p.Name, p.Namespace, p.Labels, p.Annotations),
		Containers: make([]model.Container, 0, len(p.Spec.Containers)),
	}
	for _, c := range p.Status.Conditions {
		if c.Type == corev1.PodReady {
			sp.Ready = c.Status == corev1.ConditionTrue
		}
	}
	for _, c := range p.Spec.Containers {
		sc := model.Container{Name: c.Name, Image: c.Image}
		for _, port := range c.Ports {
			sc.Ports = append(sc.Ports, model.ContainerPort{
				Name: port.Name, ContainerPort: port.ContainerPort, Protocol: string(port.Protocol),
			})
		}
		sp.Containers = append(sp.Containers, sc)
	}
	return sp
}

// Service konvertiert einen Service ohne Referenzen aufzulösen (siehe ResolveService und NamespaceView).
func Service(s *corev1.Service) model.StoredService {
	ss := model.StoredService{
		Meta:     meta(s.Name, s.Namespace, s.Labels, s.Annotations),
		Type:     string(s.Spec.Type),
		Selector: maps.Clone(s.Spec.Selector),
		Ports:    make([]model.ServicePort, 0, len(s.Spec.Ports)),
		Replicas: []string{},
	}
	if ss.Type == "" {
		ss.Type = string(corev1.ServiceTypeClusterIP)
	}
	for _, p := range s.Spec.Ports {
		target := p.TargetPort
		if target.Type == intstr.Int && target.IntVal == 0 {
			// Ohne targetPort gilt laut Kubernetes der Service-Port.
			target = intstr.FromInt32(p.Port)
		}
		ss.Ports = append(ss.Ports, model.ServicePort{
			Name: p.Name, Protocol: string(p.Protocol), Port: p.Port,
			TargetPort: target.String(), NodePort: p.NodePort,
		})
	}
	return ss
}

// Ingress konvertiert einen Ingress.
func Ingress(i *networkingv1.Ingress) model.StoredIngress {
	si := model.StoredIngress{
		Meta:  meta(i.Name, i.Namespace, i.Labels, i.Annotations),
		Rules: make([]model.IngressRule, 0, len(i.Spec.Rules)),
	}
	for _, r := range i.Spec.Rules {
		rule := model.IngressRule{Host: r.Host, Paths: []model.IngressPath{}}
		if r.HTTP != nil {
			for _, p := range r.HTTP.Paths {
				path := model.IngressPath{Path: p.Path}
				if p.PathType != nil {
					path.PathType = string(*p.PathType)
				}
				if svc := p.Backend.Service; svc != nil {
					path.Backend.ServiceName = svc.Name
				}
				rule.Paths = append(rule.Paths, path)
			}
		}
		si.Rules = append(si.Rules, rule)
	}
	return si
}

func meta(name, namespace string, labels, annotations map[string]string) model.Meta {
	return model.Meta{Name: name, Namespace: namespace, Labels: maps.Clone(labels), Annotations: maps.Clone(annotations)}
}

// ResolveService ermittelt die Replicas eines Services und löst benannte targetPorts über deren Container-Ports auf.
func ResolveService(svc *model.StoredService, pods []model.StoredPod) {
	svc.Replicas = []string{}
	var replicas []model.StoredPod
	if len(svc.Selector) > 0 {
		for _, p := range pods {
			if matches(p.Labels, svc.Selector) {
				replicas = append(replicas, p)
				svc.Replicas = append(svc.Replicas, p.Name)
			}
		}
	}
	for i := range svc.Ports {
		port := &svc.Ports[i]
		port.ResolvedTargetPort = nil
		if n, err := strconv.ParseInt(port.TargetPort, 10, 32); err == nil {
			v := int32(n)
			port.ResolvedTargetPort = &v
			continue
		}
		port.ResolvedTargetPort = findContainerPort(replicas, port.TargetPort, port.Protocol)
	}
}

func findContainerPort(pods []model.StoredPod, name, protocol string) *int32 {
	for _, p := range pods {
		for _, c := range p.Containers {
			for _, cp := range c.Ports {
				if cp.Name == name && protocolEqual(cp.Protocol, protocol) {
					v := cp.ContainerPort
					return &v
				}
			}
		}
	}
	return nil
}

func protocolEqual(a, b string) bool {
	if a == "" {
		a = string(corev1.ProtocolTCP)
	}
	if b == "" {
		b = string(corev1.ProtocolTCP)
	}
	return a == b
}

func matches(labels, selector map[string]string) bool {
	for k, v := range selector {
		if labels[k] != v {
			return false
		}
	}
	return true
}

// NamespaceView konvertiert alle Objekte eines Namespaces und löst bei den Services Replicas und benannte
// targetPorts über die Pods auf. Das Ergebnis ist je Typ nach Name sortiert.
func NamespaceView(ns *corev1.Namespace, project string, pods []*corev1.Pod, services []*corev1.Service, ingresses []*networkingv1.Ingress) model.NamespaceView {
	v := model.NamespaceView{
		Namespace: Namespace(ns, project),
		Pods:      make([]model.StoredPod, 0, len(pods)),
		Services:  make([]model.StoredService, 0, len(services)),
		Ingresses: make([]model.StoredIngress, 0, len(ingresses)),
	}
	for _, p := range pods {
		v.Pods = append(v.Pods, Pod(p))
	}
	for _, s := range services {
		v.Services = append(v.Services, Service(s))
	}
	for _, i := range ingresses {
		v.Ingresses = append(v.Ingresses, Ingress(i))
	}
	byName := func(a, b model.Meta) int { return strings.Compare(a.Name, b.Name) }
	slices.SortFunc(v.Pods, func(a, b model.StoredPod) int { return byName(a.Meta, b.Meta) })
	slices.SortFunc(v.Services, func(a, b model.StoredService) int { return byName(a.Meta, b.Meta) })
	slices.SortFunc(v.Ingresses, func(a, b model.StoredIngress) int { return byName(a.Meta, b.Meta) })
	for i := range v.Services {
		ResolveService(&v.Services[i], v.Pods)
	}
	return v
}
