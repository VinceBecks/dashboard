package api

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"feature-dashboard/internal/actions"
	"feature-dashboard/internal/config"
	"feature-dashboard/internal/copycommands"
	"feature-dashboard/internal/links"
	"feature-dashboard/internal/model"
	"feature-dashboard/internal/pipelines"
	"feature-dashboard/internal/testsource"
)

// podAction ist eine Test-Aktion auf Pods; ist fail gesetzt, schlägt Execute mit dieser Meldung fehl.
type podAction struct {
	calls int
	fail  string
}

func (p *podAction) Configure(map[string]any) error { return nil }
func (p *podAction) IsApplicable(o actions.StoredObject) bool {
	_, ok := o.Object.(model.StoredPod)
	return ok
}
func (p *podAction) FormFields(actions.StoredObject) []actions.FormField {
	return []actions.FormField{{ID: "reason", Label: "Grund", Type: actions.FieldText, IsRequired: true}}
}
func (p *podAction) Execute(context.Context, actions.StoredObject, map[string]any) actions.Result {
	p.calls++
	if p.fail != "" {
		return actions.Result{Success: false, Message: p.fail}
	}
	return actions.Result{Success: true, Message: "ok"}
}

func setup(t *testing.T) (*testsource.Source, *Server, *podAction) {
	t.Helper()
	st := testsource.New()
	err := st.Update(func(tx *testsource.Tx) error {
		if err := tx.PutNamespace(model.StoredNamespace{Meta: model.Meta{Name: "feat-1"}, Project: "foo"}); err != nil {
			return err
		}
		if _, err := tx.Put(model.KindService, "feat-1", "web", model.StoredService{
			Meta: model.Meta{Name: "web", Namespace: "feat-1"}, Type: "ClusterIP", Replicas: []string{"web-1"},
		}); err != nil {
			return err
		}
		_, err := tx.Put(model.KindPod, "feat-1", "web-1", model.StoredPod{Meta: model.Meta{Name: "web-1", Namespace: "feat-1"}})
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	action := &podAction{}
	reg := actions.NewRegistry()
	reg.Register("restart", func(actions.Deps) actions.Handler { return action })
	if err := reg.Activate([]config.ActionHandlerConfig{{Name: "restart", Title: "Neustart", Icon: "rotate-ccw", Projects: []string{"foo"}}}, actions.Deps{}); err != nil {
		t.Fatal(err)
	}
	projects := []config.Project{{Name: "foo"}, {Name: "bar"}}
	srv := NewServer(st, reg, projects, func() bool { return true }).WithCluster(config.ClusterConfig{DNSName: "nodes.example"})
	return st, srv, action
}

func do(h http.Handler, method, path, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

// subscribe meldet einen Client am Hub an und dekodiert den Snapshot.
func subscribe(t *testing.T, srv *Server) (Snapshot, *Subscription, string) {
	t.Helper()
	sub, err := srv.hub.Subscribe()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(sub.Cancel)
	var snap Snapshot
	if err := json.Unmarshal(sub.Snapshot, &snap); err != nil {
		t.Fatal(err)
	}
	return snap, sub, string(sub.Snapshot)
}

// sync gleicht den Hub mit dem Store ab (sonst Aufgabe von Hub.Run) und liefert die verteilten Events.
func syncHub(srv *Server, sub *Subscription) []string {
	srv.hub.mu.Lock()
	srv.hub.syncLocked()
	srv.hub.mu.Unlock()
	var got []string
	for {
		select {
		case ev := <-sub.Events:
			got = append(got, ev.Type+" "+string(ev.Data))
		default:
			return got
		}
	}
}

func TestSnapshot(t *testing.T) {
	st, srv, _ := setup(t)
	_ = st.Update(func(tx *testsource.Tx) error {
		for _, svc := range []model.StoredService{
			{Meta: model.Meta{Name: "shop-node-port"}, Type: "NodePort", Ports: []model.ServicePort{
				{Name: "http", Port: 80, NodePort: 30080}, {Name: "intern", Port: 9090}, {Name: "admin", Port: 8081, NodePort: 30081},
			}},
			{Meta: model.Meta{Name: "lb"}, Type: "LoadBalancer"},
			{Meta: model.Meta{Name: "worker"}, Type: "ClusterIP"},
		} {
			if _, err := tx.Put(model.KindService, "feat-1", svc.Name, svc); err != nil {
				return err
			}
		}
		_, err := tx.Put(model.KindIngress, "feat-1", "web", model.StoredIngress{Meta: model.Meta{Name: "web"}, Rules: []model.IngressRule{
			{Host: "feat-1.example", Paths: []model.IngressPath{{Path: "/"}, {Path: "/api"}, {Path: "/"}}},
			{Paths: []model.IngressPath{{Path: "/ohne-host"}}},
		}})
		return err
	})
	snap, _, raw := subscribe(t, srv)
	if len(snap.Projects) != 2 || snap.Projects[0].Name != "foo" || len(snap.Projects[0].Namespaces) != 1 ||
		snap.Projects[1].Name != "bar" || snap.Projects[1].Namespaces == nil {
		t.Fatalf("snapshot = %s", raw)
	}
	var ns NamespaceDTO
	if err := json.Unmarshal(snap.Projects[0].Namespaces[0], &ns); err != nil {
		t.Fatal(err)
	}
	if len(ns.Services) != 2 || ns.Services[0].Name != "web" || ns.Services[1].Name != "worker" {
		t.Fatalf("nur ClusterIP-Services erwartet: %+v", ns.Services)
	}
	if len(ns.Services[1].Pods) != 0 || ns.Services[1].Running {
		t.Fatalf("worker ohne Pods darf nicht laufen: %+v", ns.Services[1])
	}
	for _, want := range []string{
		`{"kind":"namespace","name":"feat-1","parents":{"project":"foo"},"actions":[],"links":[],"copyCommands":[],"pipelines":[],"services":[`,
		`{"kind":"pod","name":"web-1","parents":{"project":"foo","namespace":"feat-1","service":"web"},"ready":false,"containers":[],"actions":[{"id":"restart"`,
		`"formFields":[{"id":"reason"`,
		`{"kind":"ingress","name":"web","parents":{"project":"foo","namespace":"feat-1"},"urls":["https://feat-1.example/","https://feat-1.example/api"],"actions":[],"links":[],"copyCommands":[],"pipelines":[]}`,
		`"pods":[]`,
		`"nodePortServices":[{"kind":"node-port-service","name":"shop-node-port","parents":{"project":"foo","namespace":"feat-1"},"addresses":["nodes.example:30080","nodes.example:30081"],"actions":[],"links":[],"copyCommands":[],"pipelines":[]}]`,
	} {
		if !strings.Contains(raw, want) {
			t.Errorf("%s fehlt in %s", want, raw)
		}
	}
	if strings.Contains(raw, "labels") || strings.Contains(raw, "rules") {
		t.Errorf("Rohdaten dürfen nicht gesendet werden: %s", raw)
	}
}

func TestEvents(t *testing.T) {
	st, srv, _ := setup(t)
	_, sub, _ := subscribe(t, srv)
	if got := syncHub(srv, sub); len(got) != 0 {
		t.Fatalf("ohne Änderung keine Events erwartet: %v", got)
	}

	// Pod wird ready → upsert für Service (running) und Pod, Parent zuerst.
	_ = st.Update(func(tx *testsource.Tx) error {
		_, err := tx.Put(model.KindPod, "feat-1", "web-1", model.StoredPod{Meta: model.Meta{Name: "web-1", Namespace: "feat-1"}, Ready: true})
		return err
	})
	got := syncHub(srv, sub)
	want := []string{
		`upsert {"kind":"service","name":"web","parents":{"project":"foo","namespace":"feat-1"},"running":true,"areas":["others"],"actions":[],"links":[],"copyCommands":[],"pipelines":[]}`,
		`upsert {"kind":"pod","name":"web-1","parents":{"project":"foo","namespace":"feat-1","service":"web"},"ready":true,"containers":[],"actions":[{"id":"restart","title":"Neustart","icon":"rotate-ccw","iconColor":"","formFields":[{"id":"reason","label":"Grund","type":"text","isRequired":true}]}],"links":[],"copyCommands":[],"pipelines":[]}`,
	}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatalf("events =\n%s\nwant\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}

	// NodePort-Service angelegt und wieder gelöscht.
	_ = st.Update(func(tx *testsource.Tx) error {
		_, err := tx.Put(model.KindService, "feat-1", "web-node-port", model.StoredService{
			Meta: model.Meta{Name: "web-node-port"}, Type: "NodePort", Ports: []model.ServicePort{{Port: 80, NodePort: 30080}},
		})
		return err
	})
	got = syncHub(srv, sub)
	if len(got) != 1 || got[0] != `upsert {"kind":"node-port-service","name":"web-node-port","parents":{"project":"foo","namespace":"feat-1"},"addresses":["nodes.example:30080"],"actions":[],"links":[],"copyCommands":[],"pipelines":[]}` {
		t.Fatalf("events = %v", got)
	}
	_ = st.Update(func(tx *testsource.Tx) error {
		_, err := tx.Delete(model.KindService, "feat-1", "web-node-port")
		return err
	})
	got = syncHub(srv, sub)
	if len(got) != 1 || got[0] != `delete {"kind":"node-port-service","name":"web-node-port","parents":{"project":"foo","namespace":"feat-1"}}` {
		t.Fatalf("events = %v", got)
	}

	// Service gelöscht → nur delete für den Service, der Pod fällt implizit mit weg.
	_ = st.Update(func(tx *testsource.Tx) error { _, err := tx.Delete(model.KindService, "feat-1", "web"); return err })
	got = syncHub(srv, sub)
	if len(got) != 1 || got[0] != `delete {"kind":"service","name":"web","parents":{"project":"foo","namespace":"feat-1"}}` {
		t.Fatalf("events = %v", got)
	}

	// Projektwechsel → delete im alten, upsert im neuen Projekt.
	_ = st.Update(func(tx *testsource.Tx) error {
		return tx.PutNamespace(model.StoredNamespace{Meta: model.Meta{Name: "feat-1"}, Project: "bar"})
	})
	got = syncHub(srv, sub)
	if len(got) != 2 || got[0] != `delete {"kind":"namespace","name":"feat-1","parents":{"project":"foo"}}` ||
		!strings.HasPrefix(got[1], `upsert {"kind":"namespace","name":"feat-1","parents":{"project":"bar"}`) {
		t.Fatalf("events = %v", got)
	}

	// Namespace entfernt → ein delete.
	_ = st.Update(func(tx *testsource.Tx) error { tx.DeleteNamespace("feat-1"); return nil })
	got = syncHub(srv, sub)
	if len(got) != 1 || got[0] != `delete {"kind":"namespace","name":"feat-1","parents":{"project":"bar"}}` {
		t.Fatalf("events = %v", got)
	}
}

func TestFullResyncSendsOnlyRealDifferences(t *testing.T) {
	_, srv, _ := setup(t)
	_, sub, _ := subscribe(t, srv)
	srv.hub.mu.Lock()
	srv.hub.forgetRevisionsLocked()
	srv.hub.mu.Unlock()
	if got := syncHub(srv, sub); len(got) != 0 {
		t.Fatalf("Neuaufbau ohne Änderungen darf keine Events erzeugen: %v", got)
	}
	for _, ns := range srv.hub.namespaces {
		if ns.revision.IsZero() {
			t.Fatal("nach dem Neuaufbau muss die Revision wieder gesetzt sein")
		}
	}
}

func TestSlowSubscriberIsDropped(t *testing.T) {
	st, srv, _ := setup(t)
	_, sub, _ := subscribe(t, srv)
	for i := range subscriberBuffer + 1 {
		_ = st.Update(func(tx *testsource.Tx) error {
			_, err := tx.Put(model.KindPod, "feat-1", "web-1", model.StoredPod{Meta: model.Meta{Name: "web-1"}, Containers: []model.Container{{Name: "app", Image: strconv.Itoa(i)}}})
			return err
		})
		srv.hub.mu.Lock()
		srv.hub.syncLocked()
		srv.hub.mu.Unlock()
	}
	n := 0
	for range sub.Events {
		n++
	}
	if n != subscriberBuffer {
		t.Fatalf("%d Events vor dem Schließen, want %d", n, subscriberBuffer)
	}
}

func TestServiceAreas(t *testing.T) {
	st, _, _ := setup(t)
	projects := []config.Project{{Name: "foo", OthersAreaLabel: "Sonstige", ServiceAreas: []config.ServiceArea{
		{Name: "business", Label: "Business", LabelCombinations: []map[string]string{{"part-of": "shop"}}},
		{Name: "monitored", Label: "Überwacht", LabelCombinations: []map[string]string{{"monitored": "true"}}},
	}}}
	srv := NewServer(st, actions.NewRegistry(), projects, func() bool { return true })
	_, sub, raw := subscribe(t, srv)
	for _, want := range []string{
		`"serviceAreas":[{"id":"business","label":"Business"},{"id":"monitored","label":"Überwacht"},{"id":"others","label":"Sonstige"}]`,
		`{"kind":"service","name":"web","parents":{"project":"foo","namespace":"feat-1"},"running":false,"areas":["others"]`,
	} {
		if !strings.Contains(raw, want) {
			t.Errorf("%s fehlt in %s", want, raw)
		}
	}

	// Label-Änderung am Service → upsert mit neuen Bereichen (mehrfach, Config-Reihenfolge).
	_ = st.Update(func(tx *testsource.Tx) error {
		_, err := tx.Put(model.KindService, "feat-1", "web", model.StoredService{
			Meta: model.Meta{Name: "web", Labels: map[string]string{"part-of": "shop", "monitored": "true"}},
			Type: "ClusterIP", Replicas: []string{"web-1"},
		})
		return err
	})
	got := syncHub(srv, sub)
	if len(got) != 1 || !strings.Contains(got[0], `"name":"web",`) || !strings.Contains(got[0], `"areas":["business","monitored"]`) {
		t.Fatalf("events = %v", got)
	}
}

func TestEventStream(t *testing.T) {
	st, srv, _ := setup(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go srv.Run(ctx)
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	res, err := http.Get(ts.URL + "/api/events")
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	if ct := res.Header.Get("Content-Type"); ct != "text/event-stream" {
		t.Fatalf("Content-Type = %q", ct)
	}
	r := bufio.NewReader(res.Body)
	readEvent := func() string {
		var lines []string
		for {
			line, err := r.ReadString('\n')
			if err != nil {
				t.Fatal(err)
			}
			if line == "\n" {
				return strings.Join(lines, "")
			}
			lines = append(lines, line)
		}
	}
	if ev := readEvent(); !strings.HasPrefix(ev, "id: 1\nevent: snapshot\ndata: {\"teams\":[],\"projects\":[{\"name\":\"foo\"") {
		t.Fatalf("erstes Event = %q", ev)
	}
	_ = st.Update(func(tx *testsource.Tx) error { tx.DeleteNamespace("feat-1"); return nil })
	want := "id: 2\nevent: delete\ndata: {\"kind\":\"namespace\",\"name\":\"feat-1\",\"parents\":{\"project\":\"foo\"}}\n"
	if ev := readEvent(); ev != want {
		t.Fatalf("zweites Event = %q", ev)
	}

	// Hub beendet → Stream endet.
	cancel()
	if _, err := io.ReadAll(r); err != nil {
		t.Fatal(err)
	}
}

func TestSnapshotEndpoint(t *testing.T) {
	_, srv, _ := setup(t)
	rec := do(srv.Handler(), "GET", "/api/snapshot", "")
	if rec.Code != 200 || rec.Header().Get("Content-Type") != "application/json" {
		t.Fatalf("status = %d, Content-Type = %q", rec.Code, rec.Header().Get("Content-Type"))
	}
	srv.hub.mu.Lock()
	subscribers := len(srv.hub.subscribers)
	srv.hub.mu.Unlock()
	if subscribers != 0 {
		t.Fatalf("%d Abonnenten nach /api/snapshot, erwartet 0", subscribers)
	}
	// Gleicher Inhalt wie das snapshot-Event des Streams.
	_, _, streamed := subscribe(t, srv)
	if got := rec.Body.String(); got != streamed {
		t.Fatalf("Snapshot = %s\nStream  = %s", got, streamed)
	}
}

func TestSnapshotNotReady(t *testing.T) {
	st, _, _ := setup(t)
	h := NewServer(st, actions.NewRegistry(), nil, func() bool { return false }).Handler()
	if rec := do(h, "GET", "/api/snapshot", ""); rec.Code != 503 {
		t.Fatalf("503 erwartet, got %d", rec.Code)
	}
}

func TestEventsNotReady(t *testing.T) {
	st, _, _ := setup(t)
	h := NewServer(st, actions.NewRegistry(), nil, func() bool { return false }).Handler()
	if rec := do(h, "GET", "/api/events", ""); rec.Code != 503 {
		t.Fatalf("503 erwartet, got %d", rec.Code)
	}
}

func TestAction(t *testing.T) {
	_, srv, action := setup(t)
	h := srv.Handler()
	pod := `{"object":{"kind":"pod","namespace":"feat-1","name":"web-1"},"inputs":{"reason":"test"}}`
	cases := []struct {
		name, path, body string
		status           int
	}{
		{"unbekannte Aktion", "/api/actions/nope", pod, 404},
		{"kaputter Body", "/api/actions/restart", "{", 400},
		{"unbekanntes Objekt", "/api/actions/restart", `{"object":{"kind":"pod","namespace":"feat-1","name":"x"}}`, 404},
		{"ungültiger Typ", "/api/actions/restart", `{"object":{"kind":"deployment","namespace":"feat-1","name":"x"}}`, 400},
		{"nicht anwendbar", "/api/actions/restart", `{"object":{"kind":"namespace","name":"feat-1"}}`, 400},
		{"Pflichtfeld fehlt", "/api/actions/restart", `{"object":{"kind":"pod","namespace":"feat-1","name":"web-1"}}`, 400},
		{"erfolgreich", "/api/actions/restart", pod, 200},
	}
	for _, c := range cases {
		if rec := do(h, "PATCH", c.path, c.body); rec.Code != c.status {
			t.Errorf("%s: status = %d, want %d (%s)", c.name, rec.Code, c.status, rec.Body)
		}
	}
	if action.calls != 1 {
		t.Fatalf("Aktion %d-mal ausgeführt", action.calls)
	}

	// Kann der Handler die Aktion nicht ausführen, kommt 422 mit seiner Meldung, nie 200.
	action.fail = "Pod web-1 konnte nicht gelöscht werden"
	rec := do(h, "PATCH", "/api/actions/restart", pod)
	if rec.Code != http.StatusUnprocessableEntity || !strings.Contains(rec.Body.String(), `"error":"Pod web-1 konnte nicht gelöscht werden"`) {
		t.Fatalf("fehlgeschlagene Aktion: status = %d, body = %s", rec.Code, rec.Body)
	}
}

func TestDocsAndHealth(t *testing.T) {
	_, srv, _ := setup(t)
	h := srv.Handler()
	for _, path := range []string{"/healthz", "/readyz", "/api/openapi.yaml", "/api/swagger"} {
		if rec := do(h, "GET", path, ""); rec.Code != 200 {
			t.Errorf("%s: status = %d", path, rec.Code)
		}
	}
}

func TestLinks(t *testing.T) {
	st, _, _ := setup(t)
	cfg, err := config.Parse([]byte(`
cluster: {dns-name: nodes.example}
projects: [{project-name: foo, namespace-labels: {a: b}}]
links:
  - link-id: logs
    title: Logs
    icon: scroll-text
    projects: [foo]
    targets:
      namespace: {url: "https://kibana.example/{{ .Kind }}/{{ .Name }}"}
      pod: {url: "https://kibana.example/{{ .Kind }}/{{ .Name }}"}
`))
	if err != nil {
		t.Fatal(err)
	}
	reg, err := links.NewRegistry(cfg)
	if err != nil {
		t.Fatal(err)
	}
	srv := NewServer(st, actions.NewRegistry(), cfg.Projects, func() bool { return true }).WithLinks(reg)
	_, _, body := subscribe(t, srv)
	for _, want := range []string{
		`"links":[{"id":"logs","title":"Logs","icon":"scroll-text","iconColor":"","url":"https://kibana.example/namespace/feat-1"}]`,
		`"url":"https://kibana.example/pod/web-1"`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("%s fehlt in %s", want, body)
		}
	}
}

func TestCopyCommands(t *testing.T) {
	st, _, _ := setup(t)
	_ = st.Update(func(tx *testsource.Tx) error {
		_, err := tx.Put(model.KindPod, "feat-1", "web-1", model.StoredPod{
			Meta:       model.Meta{Name: "web-1", Namespace: "feat-1"},
			Containers: []model.Container{{Name: "app"}, {Name: "sidecar"}},
		})
		return err
	})
	cfg, err := config.Parse([]byte(`
cluster: {dns-name: nodes.example}
projects: [{project-name: foo, namespace-labels: {a: b}}]
copy-commands:
  - command-id: shell
    title: Shell
    icon: terminal
    projects: [foo]
    targets:
      pod: {command: "kubectl exec -it -n {{ .Namespace }} {{ .Name }} -c {{ .Container }} -- sh"}
      namespace: {command: "kubectl config set-context --current --namespace={{ .Namespace }}"}
`))
	if err != nil {
		t.Fatal(err)
	}
	reg, err := copycommands.NewRegistry(cfg)
	if err != nil {
		t.Fatal(err)
	}
	srv := NewServer(st, actions.NewRegistry(), cfg.Projects, func() bool { return true }).WithCopyCommands(reg)
	_, _, body := subscribe(t, srv)
	for _, want := range []string{
		`"copyCommands":[{"id":"shell","title":"Shell","icon":"terminal","iconColor":"","command":"kubectl config set-context --current --namespace=feat-1"}]`,
		`{"id":"shell/app","title":"Shell (app)","icon":"terminal","iconColor":"","command":"kubectl exec -it -n feat-1 web-1 -c app -- sh"}`,
		`{"id":"shell/sidecar","title":"Shell (sidecar)","icon":"terminal","iconColor":"","command":"kubectl exec -it -n feat-1 web-1 -c sidecar -- sh"}`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("%s fehlt in %s", want, body)
		}
	}
}

func TestPipelines(t *testing.T) {
	st, _, _ := setup(t)
	reg := actions.NewRegistry()
	reg.Register("deploy", func(actions.Deps) actions.Handler { return &podAction{} })
	if err := reg.Activate([]config.ActionHandlerConfig{{Name: "deploy", Title: "Deployen", Projects: []string{"foo"}}}, actions.Deps{}); err != nil {
		t.Fatal(err)
	}
	state := pipelines.NewState()
	srv := NewServer(st, reg, []config.Project{{Name: "foo"}}, func() bool { return true }).WithPipelines(state)
	_, sub, raw := subscribe(t, srv)
	if !strings.Contains(raw, `"parents":{"project":"foo"},"actions":[],"links":[],"copyCommands":[],"pipelines":[]`) {
		t.Fatalf("Snapshot ohne leere pipelines: %s", raw)
	}

	started := time.Date(2026, 10, 6, 14, 0, 0, 0, time.UTC)
	state.Set(pipelines.Entry{Kind: model.KindNamespace, Namespace: "feat-1", Name: "feat-1",
		Action: "deploy", ID: 4711, WebURL: "https://gitlab.example/p/-/pipelines/4711", StartedAt: started})
	got := syncHub(srv, sub)
	if len(got) != 1 || !strings.HasPrefix(got[0], `upsert {"kind":"namespace","name":"feat-1"`) ||
		!strings.Contains(got[0], `"pipelines":[{"action":"deploy","title":"Deployen","url":"https://gitlab.example/p/-/pipelines/4711","startedAt":"2026-10-06T14:00:00Z"}]`) {
		t.Fatalf("events = %v", got)
	}

	state.Remove(model.ObjectRef{Kind: model.KindNamespace, Namespace: "feat-1", Name: "feat-1"}, "deploy")
	got = syncHub(srv, sub)
	if len(got) != 1 || !strings.Contains(got[0], `"pipelines":[]`) {
		t.Fatalf("events = %v", got)
	}
}

func TestTeams(t *testing.T) {
	st, _, _ := setup(t)
	teams := config.TeamsConfig{LabelKey: "mycomp.de/team", Teams: []config.Team{{ID: "checkout", Label: "Team Checkout", TeamColor: "#2e7d32"}}}
	srv := NewServer(st, actions.NewRegistry(), []config.Project{{Name: "foo"}}, func() bool { return true }).WithTeams(teams)
	_, sub, raw := subscribe(t, srv)
	if !strings.HasPrefix(raw, `{"teams":[{"id":"checkout","label":"Team Checkout","teamColor":"#2e7d32"}],"projects":`) {
		t.Fatalf("Snapshot = %s", raw)
	}
	if strings.Contains(raw, `"team":`) {
		t.Fatalf("Namespace ohne Team darf kein team tragen: %s", raw)
	}

	// Label gesetzt → upsert des Namespaces mit team; unbekannter Wert → kein team.
	setTeam := func(value string) []string {
		_ = st.Update(func(tx *testsource.Tx) error {
			return tx.PutNamespace(model.StoredNamespace{Meta: model.Meta{Name: "feat-1", Labels: map[string]string{"mycomp.de/team": value}}, Project: "foo"})
		})
		return syncHub(srv, sub)
	}
	if got := setTeam("checkout"); len(got) != 1 || !strings.HasPrefix(got[0], `upsert {"kind":"namespace","name":"feat-1","parents":{"project":"foo"},"team":"checkout",`) {
		t.Fatalf("events = %v", got)
	}
	if got := setTeam("gibts-nicht"); len(got) != 1 || strings.Contains(got[0], `"team":`) {
		t.Fatalf("events = %v", got)
	}
}

func TestActionsRespectProjects(t *testing.T) {
	st, srv, action := setup(t)
	// Namespace in Projekt bar; die Aktion "restart" ist nur für foo freigegeben.
	_ = st.Update(func(tx *testsource.Tx) error {
		if err := tx.PutNamespace(model.StoredNamespace{Meta: model.Meta{Name: "bar-1"}, Project: "bar"}); err != nil {
			return err
		}
		if _, err := tx.Put(model.KindService, "bar-1", "web", model.StoredService{Meta: model.Meta{Name: "web"}, Type: "ClusterIP", Replicas: []string{"web-1"}}); err != nil {
			return err
		}
		_, err := tx.Put(model.KindPod, "bar-1", "web-1", model.StoredPod{Meta: model.Meta{Name: "web-1", Namespace: "bar-1"}})
		return err
	})
	_, _, raw := subscribe(t, srv)
	if !strings.Contains(raw, `{"kind":"pod","name":"web-1","parents":{"project":"bar","namespace":"bar-1","service":"web"},"ready":false,"containers":[],"actions":[]`) {
		t.Fatalf("keine Aktionen in Projekt bar erwartet: %s", raw)
	}
	body := `{"object":{"kind":"pod","namespace":"bar-1","name":"web-1"},"inputs":{"reason":"x"}}`
	if rec := do(srv.Handler(), "PATCH", "/api/actions/restart", body); rec.Code != 400 {
		t.Fatalf("400 erwartet, got %d: %s", rec.Code, rec.Body)
	}
	if action.calls != 0 {
		t.Fatal("Aktion darf in fremdem Projekt nicht ausgeführt werden")
	}
}
