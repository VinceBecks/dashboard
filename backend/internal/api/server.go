// Package api stellt die HTTP-API für das Dashboard bereit.
package api

import (
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"feature-dashboard/internal/actions"
	"feature-dashboard/internal/config"
	"feature-dashboard/internal/copycommands"
	"feature-dashboard/internal/links"
	"feature-dashboard/internal/model"
	"feature-dashboard/internal/pipelines"
)

//go:embed openapi.yaml
var openAPISpec []byte

// Server beantwortet die HTTP-Anfragen des Dashboards.
type Server struct {
	source    Source
	registry  *actions.Registry
	ready     func() bool
	staticDir string
	links     *links.Registry
	// copyCommands rendert die Kopier-Befehle je Objekt; nil heißt: keine.
	copyCommands *copycommands.Registry
	// pipelines kennt die laufenden GitLab-Pipelines; nil heißt: keine.
	pipelines *pipelines.State
	dnsName   string
	projects  map[string]config.Project
	hub       *Hub
}

// Source ist die Datenquelle des Servers, im Betrieb der Informer-Cache (kube.Cluster).
type Source interface {
	// Changes signalisiert Änderungen; welche Namespaces betroffen sind, zeigt NamespaceRevisions.
	Changes() <-chan struct{}
	// NamespaceRevisions liefert je Projekt-Namespace den Zeitpunkt der letzten Änderung.
	NamespaceRevisions() map[string]time.Time
	// Namespace liefert alle konvertierten Objekte eines Projekt-Namespaces.
	Namespace(name string) (model.NamespaceView, time.Time, bool, error)
	// Get liest ein einzelnes Objekt in out (*model.Stored*).
	Get(ref model.ObjectRef, out any) (bool, error)
}

// NewServer erzeugt den Server. ready meldet, ob der Informer-Cache synchronisiert ist.
func NewServer(source Source, registry *actions.Registry, projects []config.Project, ready func() bool) *Server {
	s := &Server{source: source, registry: registry, ready: ready}
	s.projects = make(map[string]config.Project, len(projects))
	for _, p := range projects {
		s.projects[p.Name] = p
	}
	s.hub = newHub(source, projects, s.buildNamespace)
	return s
}

// Run verteilt Änderungen an die Clients von GET /api/events, bis ctx endet. Danach werden alle
// Event-Streams beendet, damit der HTTP-Server herunterfahren kann.
func (s *Server) Run(ctx context.Context) {
	s.hub.Run(ctx)
}

// WithStaticDir lässt den Server zusätzlich den Frontend-Build aus dir ausliefern.
func (s *Server) WithStaticDir(dir string) *Server {
	s.staticDir = dir
	return s
}

// WithCluster setzt die Cluster-Angaben; der DNS-Name der Nodes bildet die NodePort-Adressen.
func (s *Server) WithCluster(c config.ClusterConfig) *Server {
	s.dnsName = c.DNSName
	return s
}

// WithPipelines lässt den Server an jedem Objekt dessen laufende Pipelines ausliefern. Änderungen daran lösen
// Events aus wie Änderungen im Cluster.
func (s *Server) WithPipelines(p *pipelines.State) *Server {
	s.pipelines = p
	s.hub.extra = p
	return s
}

// WithCopyCommands lässt den Server an jedem Objekt die konfigurierten Kopier-Befehle ausliefern.
func (s *Server) WithCopyCommands(r *copycommands.Registry) *Server {
	s.copyCommands = r
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
	mux.HandleFunc("GET /api/events", s.handleEvents)
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

// pingInterval hält Event-Streams über Proxys hinweg offen.
const pingInterval = 25 * time.Second

// handleEvents liefert den Server-Sent-Events-Stream: zuerst ein snapshot-Event mit allen Projekten, danach
// upsert- und delete-Events. Ein Reconnect erhält immer einen neuen Snapshot; Last-Event-ID wird ignoriert.
func (s *Server) handleEvents(w http.ResponseWriter, r *http.Request) {
	if !s.ready() {
		writeError(w, http.StatusServiceUnavailable, errors.New("informer-Cache ist noch nicht synchronisiert"))
		return
	}
	sub, err := s.hub.Subscribe()
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, err)
		return
	}
	defer sub.Cancel()

	h := w.Header()
	h.Set("Content-Type", "text/event-stream")
	h.Set("Cache-Control", "no-cache")
	h.Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)
	rc := http.NewResponseController(w)
	id := 0
	send := func(typ string, data []byte) error {
		id++
		if _, err := fmt.Fprintf(w, "id: %d\nevent: %s\ndata: %s\n\n", id, typ, data); err != nil {
			return err
		}
		return rc.Flush()
	}

	if err := send(EventSnapshot, sub.Snapshot); err != nil {
		return
	}
	ping := time.NewTicker(pingInterval)
	defer ping.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case ev, ok := <-sub.Events:
			if !ok {
				return
			}
			if err := send(ev.Type, ev.Data); err != nil {
				return
			}
		case <-ping.C:
			if _, err := fmt.Fprint(w, ": ping\n\n"); err != nil {
				return
			}
			if err := rc.Flush(); err != nil {
				return
			}
		}
	}
}

// extras liefert Aktionen, Links und Kopier-Befehle eines Objekts. obj ist ein model.Stored*-Wert.
func (s *Server) extras(kind model.Kind, namespace, name string, obj any, project string) ObjectExtras {
	ref := model.ObjectRef{Kind: kind, Namespace: namespace, Name: name}
	return ObjectExtras{
		Actions:      s.registry.ActionsFor(actions.StoredObject{Ref: ref, Object: obj, Project: project}),
		Links:        s.links.For(ref, obj, project),
		CopyCommands: s.copyCommands.For(ref, obj, project),
		Pipelines:    s.pipelinesOf(ref),
	}
}

// pipelinesOf liefert die laufenden Pipelines eines Objekts (nie nil).
func (s *Server) pipelinesOf(ref model.ObjectRef) []PipelineDTO {
	result := []PipelineDTO{}
	if s.pipelines == nil {
		return result
	}
	for _, e := range s.pipelines.For(ref) {
		result = append(result, PipelineDTO{Action: e.Action, Title: s.registry.Title(e.Action), URL: e.WebURL, StartedAt: e.StartedAt})
	}
	return result
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

// load liest das referenzierte Objekt als Kopie aus der Datenquelle.
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
	found, err := s.source.Get(ref, obj)
	if err != nil || !found {
		return actions.StoredObject{}, found, err
	}
	var ns model.StoredNamespace
	nsRef := model.ObjectRef{Kind: model.KindNamespace, Namespace: ref.Namespace, Name: ref.Namespace}
	if found, err := s.source.Get(nsRef, &ns); err != nil || !found {
		return actions.StoredObject{}, found, err
	}
	return actions.StoredObject{Ref: ref, Object: deref(obj), Project: ns.Project}, true, nil
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
