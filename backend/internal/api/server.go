// Package api stellt die HTTP-API für das Dashboard bereit.
package api

import (
	_ "embed"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"feature-dashboard/internal/actions"
	"feature-dashboard/internal/config"
	"feature-dashboard/internal/links"
	"feature-dashboard/internal/model"
	"feature-dashboard/internal/project"
	"feature-dashboard/internal/store"
)

//go:embed openapi.yaml
var openAPISpec []byte

// Server beantwortet die HTTP-Anfragen des Dashboards.
type Server struct {
	store     *store.Store
	registry  *actions.Registry
	projects  []config.Project
	ready     func() bool
	staticDir string
	links     *links.Registry
	cluster   config.ClusterConfig
	startedAt time.Time
}

// NewServer erzeugt den Server. ready meldet, ob der Store initial befüllt ist.
func NewServer(st *store.Store, registry *actions.Registry, projects []config.Project, ready func() bool) *Server {
	return &Server{store: st, registry: registry, projects: projects, ready: ready, startedAt: time.Now()}
}

// WithStaticDir lässt den Server zusätzlich den Frontend-Build aus dir ausliefern.
func (s *Server) WithStaticDir(dir string) *Server {
	s.staticDir = dir
	return s
}

// WithCluster setzt die Cluster-Angaben für GET /api/cluster.
func (s *Server) WithCluster(c config.ClusterConfig) *Server {
	s.cluster = c
	return s
}

// WithLinks lässt den Server an jedem Objekt die konfigurierten Links ausliefern.
func (s *Server) WithLinks(r *links.Registry) *Server {
	s.links = r
	return s
}

// Handler liefert den HTTP-Handler mit allen Routen.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })
	mux.HandleFunc("GET /readyz", s.handleReady)
	mux.HandleFunc("GET /api/cluster", s.handleCluster)
	mux.HandleFunc("GET /api/projects", s.handleProjects)
	mux.HandleFunc("GET /api/projects/{project}/namespaces/{namespace}", s.handleNamespace)
	mux.HandleFunc("PATCH /api/actions/{action}", s.handleAction)
	mux.HandleFunc("GET /api/openapi.yaml", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/yaml")
		_, _ = w.Write(openAPISpec)
	})
	mux.HandleFunc("GET /api/swagger", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte(swaggerHTML))
	})
	if s.staticDir != "" {
		mux.Handle("GET /", staticHandler(s.staticDir))
	}
	return mux
}

func (s *Server) handleReady(w http.ResponseWriter, _ *http.Request) {
	if !s.ready() {
		w.WriteHeader(http.StatusServiceUnavailable)
		return
	}
	w.WriteHeader(http.StatusOK)
}

// ClusterResponse ist die Antwort von GET /api/cluster.
type ClusterResponse struct {
	// DNSName ist der DNS-Name der Nodes für alle NodePort-Services; leer, wenn nicht konfiguriert.
	DNSName string `json:"dnsName"`
}

// handleCluster liefert statische Cluster-Angaben aus der config.yaml. Sie ändern sich erst mit einem Neustart,
// daher dient der Startzeitpunkt als ETag.
func (s *Server) handleCluster(w http.ResponseWriter, r *http.Request) {
	if notModified(w, r, s.startedAt) {
		return
	}
	writeJSON(w, http.StatusOK, ClusterResponse{DNSName: s.cluster.DNSName})
}

// ProjectsResponse ist die Antwort von GET /api/projects.
type ProjectsResponse struct {
	Projects []ProjectDTO `json:"projects"`
}

// ProjectDTO ist ein Projekt mit seinen Namespaces.
type ProjectDTO struct {
	Name       string         `json:"name"`
	Namespaces []NamespaceDTO `json:"namespaces"`
}

// ObjectExtras sind die Aktionen und Links, die jedes Objekt in der Response trägt.
type ObjectExtras struct {
	Actions []actions.Action `json:"actions"`
	Links   []links.Link     `json:"links"`
}

// NamespaceDTO ist ein Namespace mit seinen Aktionen und Links.
type NamespaceDTO struct {
	model.StoredNamespace
	ObjectExtras
}

// ServiceDTO ist ein Service mit seinen Aktionen und Links.
type ServiceDTO struct {
	model.StoredService
	ObjectExtras
}

// IngressDTO ist ein Ingress mit seinen Aktionen und Links.
type IngressDTO struct {
	model.StoredIngress
	ObjectExtras
}

// PodDTO ist ein Pod mit seinen Aktionen und Links.
type PodDTO struct {
	model.StoredPod
	ObjectExtras
}

// NamespaceResponse ist die Antwort von GET /api/projects/{project}/namespaces/{namespace}.
type NamespaceResponse struct {
	Namespace NamespaceDTO `json:"namespace"`
	Services  []ServiceDTO `json:"services"`
	Ingresses []IngressDTO `json:"ingresses"`
	Pods      []PodDTO     `json:"pods"`
	// ServiceAreas teilen die Anwendungs-Services in Bereiche auf und referenzieren sie über ihren Namen.
	ServiceAreas []project.Area `json:"serviceAreas"`
}

func (s *Server) handleProjects(w http.ResponseWriter, r *http.Request) {
	namespaces, lastUpdated, err := s.store.Namespaces()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	if notModified(w, r, lastUpdated) {
		return
	}
	byProject := map[string][]NamespaceDTO{}
	for _, ns := range namespaces {
		byProject[ns.Project] = append(byProject[ns.Project], NamespaceDTO{ns, s.extras(model.KindNamespace, ns.Name, ns.Name, ns, ns.Project)})
	}
	resp := ProjectsResponse{Projects: make([]ProjectDTO, 0, len(s.projects))}
	for _, p := range s.projects {
		nss := byProject[p.Name]
		if nss == nil {
			nss = []NamespaceDTO{}
		}
		resp.Projects = append(resp.Projects, ProjectDTO{Name: p.Name, Namespaces: nss})
	}
	writeJSON(w, http.StatusOK, resp)
}

func (s *Server) handleNamespace(w http.ResponseWriter, r *http.Request) {
	view, lastUpdated, ok, err := s.store.Namespace(r.PathValue("namespace"))
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	if !ok || view.Namespace.Project != r.PathValue("project") {
		writeError(w, http.StatusNotFound, errors.New("namespace nicht gefunden"))
		return
	}
	if notModified(w, r, lastUpdated) {
		return
	}
	ns := view.Namespace
	projectCfg, _ := s.project(ns.Project)
	resp := NamespaceResponse{
		ServiceAreas: project.AssignServiceAreas(projectCfg, view.Services),
		Namespace:    NamespaceDTO{ns, s.extras(model.KindNamespace, ns.Name, ns.Name, ns, ns.Project)},
		Services:     make([]ServiceDTO, 0, len(view.Services)),
		Ingresses:    make([]IngressDTO, 0, len(view.Ingresses)),
		Pods:         make([]PodDTO, 0, len(view.Pods)),
	}
	for _, o := range view.Services {
		resp.Services = append(resp.Services, ServiceDTO{o, s.extras(model.KindService, ns.Name, o.Name, o, ns.Project)})
	}
	for _, o := range view.Ingresses {
		resp.Ingresses = append(resp.Ingresses, IngressDTO{o, s.extras(model.KindIngress, ns.Name, o.Name, o, ns.Project)})
	}
	for _, o := range view.Pods {
		resp.Pods = append(resp.Pods, PodDTO{o, s.extras(model.KindPod, ns.Name, o.Name, o, ns.Project)})
	}
	writeJSON(w, http.StatusOK, resp)
}

// extras liefert Aktionen und Links eines Objekts. obj ist ein model.Stored*-Wert.
func (s *Server) extras(kind model.Kind, namespace, name string, obj any, project string) ObjectExtras {
	return ObjectExtras{
		Actions: s.registry.ActionsFor(storedObject(kind, namespace, name, obj)),
		Links:   s.links.For(model.ObjectRef{Kind: kind, Namespace: namespace, Name: name}, obj, project),
	}
}

// project liefert die Konfiguration eines Projekts.
func (s *Server) project(name string) (config.Project, bool) {
	for _, p := range s.projects {
		if p.Name == name {
			return p, true
		}
	}
	return config.Project{Name: name}, false
}

// ActionRequest ist der Body von PATCH /api/actions/{action}.
type ActionRequest struct {
	Object model.ObjectRef `json:"object"`
	Inputs map[string]any  `json:"inputs"`
}

func (s *Server) handleAction(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("action")
	if !s.registry.Has(name) {
		writeError(w, http.StatusNotFound, actions.ErrUnknownAction)
		return
	}
	var req ActionRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, errors.New("ungültiger Request-Body"))
		return
	}
	ref := req.Object
	if ref.Kind == model.KindNamespace && ref.Namespace == "" {
		ref.Namespace = ref.Name
	}
	obj, found, err := s.load(ref)
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	if !found {
		writeError(w, http.StatusNotFound, errors.New("objekt nicht gefunden"))
		return
	}

	result, err := s.registry.Execute(r.Context(), name, obj, req.Inputs)
	var verr *actions.ValidationError
	switch {
	case errors.As(err, &verr):
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": verr.Error(), "problems": verr.Problems})
		return
	case errors.Is(err, actions.ErrNotApplicable):
		writeError(w, http.StatusBadRequest, err)
		return
	case err != nil:
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	slog.Info("Aktion ausgeführt", "action", name, "object", ref, "success", result.Success, "message", result.Message)
	writeJSON(w, http.StatusOK, result)
}

// load liest das referenzierte Objekt als Kopie aus dem Store.
func (s *Server) load(ref model.ObjectRef) (actions.StoredObject, bool, error) {
	var obj any
	switch ref.Kind {
	case model.KindNamespace:
		obj = &model.StoredNamespace{}
	case model.KindService:
		obj = &model.StoredService{}
	case model.KindIngress:
		obj = &model.StoredIngress{}
	case model.KindPod:
		obj = &model.StoredPod{}
	default:
		return actions.StoredObject{}, false, errors.New("ungültiger Objekt-Typ")
	}
	found, err := s.store.Get(ref, obj)
	if err != nil || !found {
		return actions.StoredObject{}, found, err
	}
	return actions.StoredObject{Ref: ref, Object: deref(obj)}, true, nil
}

func deref(obj any) any {
	switch o := obj.(type) {
	case *model.StoredNamespace:
		return *o
	case *model.StoredService:
		return *o
	case *model.StoredIngress:
		return *o
	case *model.StoredPod:
		return *o
	}
	return obj
}

func storedObject(kind model.Kind, namespace, name string, obj any) actions.StoredObject {
	return actions.StoredObject{Ref: model.ObjectRef{Kind: kind, Namespace: namespace, Name: name}, Object: obj}
}

// notModified setzt den ETag und beantwortet die Anfrage mit 304, wenn das Frontend aktuell ist.
func notModified(w http.ResponseWriter, r *http.Request, lastUpdated time.Time) bool {
	etag := `"` + strconv.FormatInt(lastUpdated.UnixNano(), 10) + `"`
	w.Header().Set("ETag", etag)
	w.Header().Set("Cache-Control", "no-cache")
	for _, candidate := range strings.Split(r.Header.Get("If-None-Match"), ",") {
		candidate = strings.TrimPrefix(strings.TrimSpace(candidate), "W/")
		if candidate == etag || candidate == "*" {
			w.WriteHeader(http.StatusNotModified)
			return true
		}
	}
	return false
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		slog.Error("Antwort schreiben fehlgeschlagen", "error", err)
	}
}

func writeError(w http.ResponseWriter, status int, err error) {
	writeJSON(w, status, map[string]string{"error": err.Error()})
}

const swaggerHTML = `<!doctype html>
<html lang="de">
<head>
  <meta charset="utf-8">
  <title>Feature-Dashboard API</title>
  <link rel="stylesheet" href="https://unpkg.com/swagger-ui-dist@5.17.14/swagger-ui.css">
</head>
<body>
  <div id="swagger-ui"></div>
  <script src="https://unpkg.com/swagger-ui-dist@5.17.14/swagger-ui-bundle.js"></script>
  <script>window.ui = SwaggerUIBundle({ url: "/api/openapi.yaml", dom_id: "#swagger-ui" });</script>
</body>
</html>`
