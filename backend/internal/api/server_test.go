package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"feature-dashboard/internal/actions"
	"feature-dashboard/internal/config"
	"feature-dashboard/internal/model"
	"feature-dashboard/internal/store"
)

type podAction struct{ calls int }

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
	return actions.Result{Success: true, Message: "ok"}
}

func setup(t *testing.T) (*store.Store, http.Handler, *podAction) {
	t.Helper()
	st := store.New()
	err := st.Update(func(tx *store.Tx) error {
		if err := tx.PutNamespace(model.StoredNamespace{Meta: model.Meta{Name: "feat-1"}, Project: "foo"}); err != nil {
			return err
		}
		_, err := tx.Put(model.KindPod, "feat-1", "web-1", model.StoredPod{Meta: model.Meta{Name: "web-1", Namespace: "feat-1"}, Phase: "Running"})
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	action := &podAction{}
	reg := actions.NewRegistry()
	reg.Register("restart", func(actions.Deps) actions.Handler { return action })
	if err := reg.Activate([]config.ActionHandlerConfig{{Name: "restart", Enabled: true, Title: "Neustart", LucideIcon: "rotate-ccw"}}, actions.Deps{}); err != nil {
		t.Fatal(err)
	}
	projects := []config.Project{{Name: "foo"}, {Name: "bar"}}
	return st, NewServer(st, reg, projects, func() bool { return true }).Handler(), action
}

func do(h http.Handler, method, path, body, etag string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	if etag != "" {
		req.Header.Set("If-None-Match", etag)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func TestProjects(t *testing.T) {
	st, h, _ := setup(t)
	rec := do(h, "GET", "/api/projects", "", "")
	if rec.Code != 200 {
		t.Fatalf("status = %d", rec.Code)
	}
	var resp ProjectsResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if len(resp.Projects) != 2 || len(resp.Projects[0].Namespaces) != 1 || resp.Projects[1].Namespaces == nil {
		t.Fatalf("resp = %+v", resp)
	}
	etag := rec.Header().Get("ETag")
	if rec := do(h, "GET", "/api/projects", "", etag); rec.Code != 304 || rec.Body.Len() != 0 {
		t.Fatalf("304 erwartet, got %d", rec.Code)
	}

	// Pod-Änderung betrifft nur den Namespace-ETag, nicht die Projekt-Liste.
	_ = st.Update(func(tx *store.Tx) error {
		_, err := tx.Put(model.KindPod, "feat-1", "web-2", model.StoredPod{Meta: model.Meta{Name: "web-2"}})
		return err
	})
	if rec := do(h, "GET", "/api/projects", "", etag); rec.Code != 304 {
		t.Fatalf("304 erwartet, got %d", rec.Code)
	}
	_ = st.Update(func(tx *store.Tx) error { tx.DeleteNamespace("feat-1"); return nil })
	if rec := do(h, "GET", "/api/projects", "", etag); rec.Code != 200 {
		t.Fatalf("200 erwartet, got %d", rec.Code)
	}
}

func TestNamespace(t *testing.T) {
	st, h, _ := setup(t)
	rec := do(h, "GET", "/api/projects/foo/namespaces/feat-1", "", "")
	if rec.Code != 200 {
		t.Fatalf("status = %d: %s", rec.Code, rec.Body)
	}
	var resp NamespaceResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if resp.Namespace.Name != "feat-1" || len(resp.Pods) != 1 || len(resp.Pods[0].Actions) != 1 || len(resp.Namespace.Actions) != 0 {
		t.Fatalf("resp = %+v", resp)
	}
	if !strings.Contains(rec.Body.String(), `"formFields":[{"id":"reason"`) {
		t.Fatalf("Formular-Felder fehlen: %s", rec.Body)
	}
	etag := rec.Header().Get("ETag")
	if rec := do(h, "GET", "/api/projects/foo/namespaces/feat-1", "", etag); rec.Code != 304 {
		t.Fatalf("304 erwartet, got %d", rec.Code)
	}
	_ = st.Update(func(tx *store.Tx) error { _, err := tx.Delete(model.KindPod, "feat-1", "web-1"); return err })
	if rec := do(h, "GET", "/api/projects/foo/namespaces/feat-1", "", etag); rec.Code != 200 {
		t.Fatalf("200 erwartet, got %d", rec.Code)
	}
	if rec := do(h, "GET", "/api/projects/bar/namespaces/feat-1", "", ""); rec.Code != 404 {
		t.Fatalf("404 bei falschem Projekt erwartet, got %d", rec.Code)
	}
	if rec := do(h, "GET", "/api/projects/foo/namespaces/nope", "", ""); rec.Code != 404 {
		t.Fatalf("404 erwartet, got %d", rec.Code)
	}
}

func TestAction(t *testing.T) {
	_, h, action := setup(t)
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
		if rec := do(h, "PATCH", c.path, c.body, ""); rec.Code != c.status {
			t.Errorf("%s: status = %d, want %d (%s)", c.name, rec.Code, c.status, rec.Body)
		}
	}
	if action.calls != 1 {
		t.Fatalf("Aktion %d-mal ausgeführt", action.calls)
	}
}

func TestDocsAndHealth(t *testing.T) {
	_, h, _ := setup(t)
	for _, path := range []string{"/healthz", "/readyz", "/api/openapi.yaml", "/api/swagger"} {
		if rec := do(h, "GET", path, "", ""); rec.Code != 200 {
			t.Errorf("%s: status = %d", path, rec.Code)
		}
	}
}
