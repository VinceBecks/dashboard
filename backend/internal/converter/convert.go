// Package converter wandelt Kubernetes-Objekte in Stored-Objects um und löst Referenzen auf.
package converter

import (
	"maps"
	"strconv"

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
		Phase:      string(p.Status.Phase),
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

// Service konvertiert einen Service ohne Referenzen aufzulösen (siehe ResolveService).
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

// Ingress konvertiert einen Ingress ohne Referenzen aufzulösen (siehe ResolveIngress).
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
					if svc.Port.Name != "" {
						path.Backend.ServicePortName = svc.Port.Name
					} else {
						n := svc.Port.Number
						path.Backend.ServicePortNumber = &n
					}
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

// ResolveIngress löst die Service-Port-Nummern der Ingress-Backends auf.
func ResolveIngress(ing *model.StoredIngress, services []model.StoredService) {
	byName := make(map[string]model.StoredService, len(services))
	for _, s := range services {
		byName[s.Name] = s
	}
	for ri := range ing.Rules {
		for pi := range ing.Rules[ri].Paths {
			b := &ing.Rules[ri].Paths[pi].Backend
			b.ResolvedServicePortNumber = nil
			if b.ServicePortNumber != nil {
				v := *b.ServicePortNumber
				b.ResolvedServicePortNumber = &v
				continue
			}
			svc, ok := byName[b.ServiceName]
			if !ok || b.ServicePortName == "" {
				continue
			}
			for _, p := range svc.Ports {
				if p.Name == b.ServicePortName {
					v := p.Port
					b.ResolvedServicePortNumber = &v
					break
				}
			}
		}
	}
}

func matches(labels, selector map[string]string) bool {
	for k, v := range selector {
		if labels[k] != v {
			return false
		}
	}
	return true
}
