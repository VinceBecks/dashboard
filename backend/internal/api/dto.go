package api

import (
	"encoding/json"
	"slices"

	corev1 "k8s.io/api/core/v1"

	"feature-dashboard/internal/actions"
	"feature-dashboard/internal/links"
	"feature-dashboard/internal/model"
	"feature-dashboard/internal/store"
)

// Parents ordnen ein Objekt in die Hierarchie Projekt → Namespace → Service ein.
type Parents struct {
	Project   string `json:"project"`
	Namespace string `json:"namespace,omitempty"`
	Service   string `json:"service,omitempty"`
}

// ObjectHeader identifiziert ein Objekt samt seinen Parents. Er ist auch der Inhalt eines delete-Events.
type ObjectHeader struct {
	Kind    model.Kind `json:"kind"`
	Name    string     `json:"name"`
	Parents Parents    `json:"parents"`
}

// ObjectExtras sind die Aktionen und Links, die jedes Objekt trägt.
type ObjectExtras struct {
	Actions []actions.Action `json:"actions"`
	Links   []links.Link     `json:"links"`
}

// Kind-Listen sind omitzero: Im Snapshot sind sie immer gesetzt (ggf. leer), in upsert-Events fehlen sie.

// NamespaceDTO ist eine Feature-Umgebung mit ihren Anwendungs-Services und Ingresses.
type NamespaceDTO struct {
	ObjectHeader
	ObjectExtras
	Services  []ServiceDTO `json:"services,omitzero"`
	Ingresses []IngressDTO `json:"ingresses,omitzero"`
}

// ServiceDTO ist ein Anwendungs-Service (ClusterIP) mit seinen Replicas.
type ServiceDTO struct {
	ObjectHeader
	// Running gilt, wenn mindestens ein Replica ready ist.
	Running bool `json:"running"`
	ObjectExtras
	Pods []PodDTO `json:"pods,omitzero"`
}

// PodDTO ist ein Replica eines Services.
type PodDTO struct {
	ObjectHeader
	Ready      bool           `json:"ready"`
	Containers []ContainerDTO `json:"containers"`
	ObjectExtras
}

// ContainerDTO ist ein Container eines Pods.
type ContainerDTO struct {
	Name  string `json:"name"`
	Image string `json:"image"`
}

// IngressDTO ist ein Ingress mit den fertig aufgelösten URLs seiner Rules.
type IngressDTO struct {
	ObjectHeader
	URLs []string `json:"urls"`
	ObjectExtras
}

// buildNamespace baut den kompletten Teilbaum eines Namespaces. Pods werden unter jedem Service eingehängt, der
// sie referenziert; Pods ohne Service und Services, die nicht vom Typ ClusterIP sind, entfallen.
func (s *Server) buildNamespace(view store.NamespaceView) NamespaceDTO {
	ns := view.Namespace
	project := ns.Project
	nsParents := Parents{Project: project, Namespace: ns.Name}
	dto := NamespaceDTO{
		ObjectHeader: ObjectHeader{Kind: model.KindNamespace, Name: ns.Name, Parents: Parents{Project: project}},
		ObjectExtras: s.extras(model.KindNamespace, ns.Name, ns.Name, ns, project),
		Services:     []ServiceDTO{},
		Ingresses:    make([]IngressDTO, 0, len(view.Ingresses)),
	}

	pods := make(map[string]model.StoredPod, len(view.Pods))
	for _, p := range view.Pods {
		pods[p.Name] = p
	}
	for _, svc := range view.Services {
		if svc.Type != string(corev1.ServiceTypeClusterIP) {
			continue
		}
		sd := ServiceDTO{
			ObjectHeader: ObjectHeader{Kind: model.KindService, Name: svc.Name, Parents: nsParents},
			ObjectExtras: s.extras(model.KindService, ns.Name, svc.Name, svc, project),
			Pods:         []PodDTO{},
		}
		podParents := Parents{Project: project, Namespace: ns.Name, Service: svc.Name}
		for _, name := range slices.Sorted(slices.Values(svc.Replicas)) {
			p, ok := pods[name]
			if !ok {
				continue
			}
			sd.Running = sd.Running || p.Ready
			sd.Pods = append(sd.Pods, PodDTO{
				ObjectHeader: ObjectHeader{Kind: model.KindPod, Name: p.Name, Parents: podParents},
				Ready:        p.Ready,
				Containers:   containers(p.Containers),
				ObjectExtras: s.extras(model.KindPod, ns.Name, p.Name, p, project),
			})
		}
		dto.Services = append(dto.Services, sd)
	}
	for _, ing := range view.Ingresses {
		dto.Ingresses = append(dto.Ingresses, IngressDTO{
			ObjectHeader: ObjectHeader{Kind: model.KindIngress, Name: ing.Name, Parents: nsParents},
			URLs:         ingressURLs(ing),
			ObjectExtras: s.extras(model.KindIngress, ns.Name, ing.Name, ing, project),
		})
	}
	return dto
}

func containers(cs []model.Container) []ContainerDTO {
	result := make([]ContainerDTO, 0, len(cs))
	for _, c := range cs {
		result = append(result, ContainerDTO{Name: c.Name, Image: c.Image})
	}
	return result
}

// ingressURLs bildet je Rule mit Host und je Path eine https-URL (Reihenfolge der Spec, ohne Duplikate).
// Rules ohne Host werden übersprungen, da sich ohne Host keine Adresse angeben lässt.
func ingressURLs(ing model.StoredIngress) []string {
	result := []string{}
	add := func(u string) {
		if !slices.Contains(result, u) {
			result = append(result, u)
		}
	}
	for _, rule := range ing.Rules {
		if rule.Host == "" {
			continue
		}
		if len(rule.Paths) == 0 {
			add("https://" + rule.Host + "/")
		}
		for _, p := range rule.Paths {
			path := p.Path
			if path == "" {
				path = "/"
			}
			add("https://" + rule.Host + path)
		}
	}
	return result
}

// flatObject ist ein Objekt des Teilbaums ohne Kind-Listen, so wie es in einem upsert-Event verschickt wird.
type flatObject struct {
	key       string
	parentKey string
	header    ObjectHeader
	data      json.RawMessage
}

// objectKey identifiziert ein Objekt eindeutig über Typ, Parents und Name.
func objectKey(h ObjectHeader) string {
	return string(h.Kind) + "\x00" + h.Parents.Project + "\x00" + h.Parents.Namespace + "\x00" + h.Parents.Service + "\x00" + h.Name
}

// flatten zerlegt den Teilbaum in einzelne Objekte ohne Kind-Listen, Parent vor Kind.
func flatten(ns NamespaceDTO) ([]flatObject, error) {
	var result []flatObject
	add := func(h ObjectHeader, parentKey string, v any) (string, error) {
		data, err := json.Marshal(v)
		if err != nil {
			return "", err
		}
		key := objectKey(h)
		result = append(result, flatObject{key: key, parentKey: parentKey, header: h, data: data})
		return key, nil
	}

	head := ns
	head.Services, head.Ingresses = nil, nil
	nsKey, err := add(ns.ObjectHeader, "", head)
	if err != nil {
		return nil, err
	}
	for _, svc := range ns.Services {
		svcHead := svc
		svcHead.Pods = nil
		svcKey, err := add(svc.ObjectHeader, nsKey, svcHead)
		if err != nil {
			return nil, err
		}
		for _, p := range svc.Pods {
			if _, err := add(p.ObjectHeader, svcKey, p); err != nil {
				return nil, err
			}
		}
	}
	for _, ing := range ns.Ingresses {
		if _, err := add(ing.ObjectHeader, nsKey, ing); err != nil {
			return nil, err
		}
	}
	return result, nil
}
