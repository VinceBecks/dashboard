package api

import (
	"encoding/json"
	"net"
	"slices"
	"strconv"
	"time"

	corev1 "k8s.io/api/core/v1"

	"feature-dashboard/internal/actions"
	"feature-dashboard/internal/copycommands"
	"feature-dashboard/internal/links"
	"feature-dashboard/internal/model"
	"feature-dashboard/internal/project"
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

// ObjectExtras sind die Aktionen, Links und Kopier-Befehle, die jedes Objekt trägt.
type ObjectExtras struct {
	Actions      []actions.Action       `json:"actions"`
	Links        []links.Link           `json:"links"`
	CopyCommands []copycommands.Command `json:"copyCommands"`
	// Pipelines sind die laufenden GitLab-Pipelines, die eine Aktion auf diesem Objekt gestartet hat.
	Pipelines []PipelineDTO `json:"pipelines"`
}

// PipelineDTO ist eine laufende Pipeline mit Link nach GitLab.
type PipelineDTO struct {
	// Action ist die Aktion-ID, Title ihr action-title.
	Action    string    `json:"action"`
	Title     string    `json:"title"`
	URL       string    `json:"url"`
	StartedAt time.Time `json:"startedAt"`
}

// Kind-Listen sind omitzero: Im Snapshot sind sie immer gesetzt (ggf. leer), in upsert-Events fehlen sie.

// KindNodePortService kennzeichnet NodePort-Services in der Ausgabe, damit der Client sie von den
// Anwendungs-Services unterscheiden kann. Für Aktionen bleibt der Objekt-Typ model.KindService.
const KindNodePortService model.Kind = "node-port-service"

// NamespaceDTO ist eine Feature-Umgebung mit ihren Anwendungs-Services, NodePort-Services und Ingresses.
type NamespaceDTO struct {
	ObjectHeader
	// Team ist die ID des zugeordneten Teams (Snapshot.Teams); leer ohne oder mit unbekanntem Team.
	Team string `json:"team,omitempty"`
	ObjectExtras
	Services         []ServiceDTO         `json:"services,omitzero"`
	NodePortServices []NodePortServiceDTO `json:"nodePortServices,omitzero"`
	Ingresses        []IngressDTO         `json:"ingresses,omitzero"`
}

// NodePortServiceDTO ist ein Service vom Typ NodePort mit seinen fertigen Adressen <dns-name>:<node-port>.
type NodePortServiceDTO struct {
	ObjectHeader
	Addresses []string `json:"addresses"`
	ObjectExtras
}

// ServiceDTO ist ein Anwendungs-Service (ClusterIP) mit seinen Replicas.
type ServiceDTO struct {
	ObjectHeader
	// Running gilt, wenn mindestens ein Replica ready ist.
	Running bool `json:"running"`
	// Areas sind die IDs der Service-Bereiche (ProjectSnapshot.ServiceAreas), zu denen der Service gehört.
	Areas []string `json:"areas"`
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
func (s *Server) buildNamespace(view model.NamespaceView) NamespaceDTO {
	ns := view.Namespace
	project := ns.Project
	nsParents := Parents{Project: project, Namespace: ns.Name}
	dto := NamespaceDTO{
		ObjectHeader:     ObjectHeader{Kind: model.KindNamespace, Name: ns.Name, Parents: Parents{Project: project}},
		Team:             s.teamOf(ns),
		ObjectExtras:     s.extras(model.KindNamespace, ns.Name, ns.Name, ns, project),
		Services:         []ServiceDTO{},
		NodePortServices: []NodePortServiceDTO{},
		Ingresses:        make([]IngressDTO, 0, len(view.Ingresses)),
	}

	pods := make(map[string]model.StoredPod, len(view.Pods))
	for _, p := range view.Pods {
		pods[p.Name] = p
	}
	for _, svc := range view.Services {
		switch corev1.ServiceType(svc.Type) {
		case corev1.ServiceTypeClusterIP:
			dto.Services = append(dto.Services, s.buildService(svc, pods, nsParents))
		case corev1.ServiceTypeNodePort:
			dto.NodePortServices = append(dto.NodePortServices, NodePortServiceDTO{
				ObjectHeader: ObjectHeader{Kind: KindNodePortService, Name: svc.Name, Parents: nsParents},
				Addresses:    nodePortAddresses(svc, s.dnsName),
				ObjectExtras: s.extras(model.KindService, ns.Name, svc.Name, svc, project),
			})
		}
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

// teamOf liefert die ID des konfigurierten Teams, dem der Namespace per Label zugeordnet ist, sonst "".
func (s *Server) teamOf(ns model.StoredNamespace) string {
	if s.teams.LabelKey == "" {
		return ""
	}
	if team, ok := s.teams.Find(ns.Labels[s.teams.LabelKey]); ok {
		return team.ID
	}
	return ""
}

// buildService baut einen Anwendungs-Service mit den Replicas, die er referenziert.
func (s *Server) buildService(svc model.StoredService, pods map[string]model.StoredPod, parents Parents) ServiceDTO {
	sd := ServiceDTO{
		ObjectHeader: ObjectHeader{Kind: model.KindService, Name: svc.Name, Parents: parents},
		ObjectExtras: s.extras(model.KindService, parents.Namespace, svc.Name, svc, parents.Project),
		Areas:        project.ServiceAreaIDs(s.projects[parents.Project], svc.Labels),
		Pods:         []PodDTO{},
	}
	podParents := Parents{Project: parents.Project, Namespace: parents.Namespace, Service: svc.Name}
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
			ObjectExtras: s.extras(model.KindPod, parents.Namespace, p.Name, p, parents.Project),
		})
	}
	return sd
}

// nodePortAddresses liefert je Port mit vergebenem NodePort die Adresse <dns-name>:<node-port> (Reihenfolge der Spec).
func nodePortAddresses(svc model.StoredService, dnsName string) []string {
	result := []string{}
	for _, p := range svc.Ports {
		if p.NodePort != 0 {
			result = append(result, net.JoinHostPort(dnsName, strconv.Itoa(int(p.NodePort))))
		}
	}
	return result
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
	head.Services, head.NodePortServices, head.Ingresses = nil, nil, nil
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
	for _, np := range ns.NodePortServices {
		if _, err := add(np.ObjectHeader, nsKey, np); err != nil {
			return nil, err
		}
	}
	for _, ing := range ns.Ingresses {
		if _, err := add(ing.ObjectHeader, nsKey, ing); err != nil {
			return nil, err
		}
	}
	return result, nil
}
