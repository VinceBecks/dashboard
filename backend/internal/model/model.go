// Package model enthält die Stored-Objects: reduzierte, konvertierte Formen der Kubernetes-Objekte.
package model

// Kind bezeichnet den Typ eines Stored-Objects.
type Kind string

const (
	KindNamespace Kind = "namespace"
	KindService   Kind = "service"
	KindIngress   Kind = "ingress"
	KindPod       Kind = "pod"
)

// Valid meldet, ob k einer der vier relevanten Typen ist.
func (k Kind) Valid() bool {
	switch k {
	case KindNamespace, KindService, KindIngress, KindPod:
		return true
	}
	return false
}

// ObjectRef referenziert ein Objekt eindeutig. Für Namespaces ist Namespace == Name.
type ObjectRef struct {
	Kind      Kind   `json:"kind"`
	Namespace string `json:"namespace"`
	Name      string `json:"name"`
}

// Meta sind die für alle Objekte gespeicherten Kern-Informationen.
type Meta struct {
	Name        string            `json:"name"`
	Namespace   string            `json:"namespace,omitempty"`
	Labels      map[string]string `json:"labels,omitempty"`
	Annotations map[string]string `json:"annotations,omitempty"`
}

// StoredNamespace ist eine Feature-Umgebung.
type StoredNamespace struct {
	Meta
	Project string `json:"project"`
}

// StoredPod ist die reduzierte Form eines Pods.
type StoredPod struct {
	Meta
	Ready      bool        `json:"ready"`
	Containers []Container `json:"containers"`
}

// Container eines Pods.
type Container struct {
	Name  string          `json:"name"`
	Image string          `json:"image"`
	Ports []ContainerPort `json:"ports,omitempty"`
}

// ContainerPort ist ein Port-Mapping eines Containers.
type ContainerPort struct {
	Name          string `json:"name,omitempty"`
	ContainerPort int32  `json:"containerPort"`
	Protocol      string `json:"protocol,omitempty"`
}

// StoredService ist die reduzierte Form eines Services.
type StoredService struct {
	Meta
	Type     string            `json:"type"`
	Selector map[string]string `json:"selector,omitempty"`
	Ports    []ServicePort     `json:"ports"`
	// Replicas sind die Namen der Pods im Namespace, die der Selector referenziert.
	Replicas []string `json:"replicas"`
}

// ServicePort ist ein Port-Mapping eines Services.
type ServicePort struct {
	Name     string `json:"name,omitempty"`
	Protocol string `json:"protocol,omitempty"`
	Port     int32  `json:"port"`
	// TargetPort ist der Rohwert aus der Spec (Name oder Zahl).
	TargetPort string `json:"targetPort"`
	// ResolvedTargetPort ist die aufgelöste Port-Nummer, nil wenn nicht auflösbar.
	ResolvedTargetPort *int32 `json:"resolvedTargetPort,omitempty"`
	NodePort           int32  `json:"nodePort,omitempty"`
}

// StoredIngress ist die reduzierte Form eines Ingresses.
type StoredIngress struct {
	Meta
	Rules []IngressRule `json:"rules"`
}

// IngressRule ist eine Ingress-Regel.
type IngressRule struct {
	Host  string        `json:"host"`
	Paths []IngressPath `json:"paths"`
}

// IngressPath ist ein Pfad einer Ingress-Regel.
type IngressPath struct {
	PathType string         `json:"pathType"`
	Path     string         `json:"path"`
	Backend  IngressBackend `json:"backend"`
}

// IngressBackend referenziert einen Service-Port.
type IngressBackend struct {
	ServiceName       string `json:"serviceName"`
	ServicePortName   string `json:"servicePortName,omitempty"`
	ServicePortNumber *int32 `json:"servicePortNumber,omitempty"`
	// ResolvedServicePortNumber ist die aufgelöste Service-Port-Nummer, nil wenn nicht auflösbar.
	ResolvedServicePortNumber *int32 `json:"resolvedServicePortNumber,omitempty"`
}
