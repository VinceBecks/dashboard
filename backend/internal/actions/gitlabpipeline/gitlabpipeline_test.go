package gitlabpipeline

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"feature-dashboard/internal/actions"
	"feature-dashboard/internal/model"
)

const token = "glptt-secret-token"

type request struct {
	path string
	form url.Values
}

// fakeGitLab beantwortet Trigger-Requests mit status und body und merkt sich den letzten Request.
func fakeGitLab(t *testing.T, status int, body string) (*httptest.Server, *request) {
	t.Helper()
	last := &request{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		last.path = r.URL.EscapedPath()
		last.form, _ = url.ParseQuery(string(raw))
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	return srv, last
}

func configured(t *testing.T, gitlabURL string, extra map[string]any) actions.Handler {
	t.Helper()
	t.Setenv("GITLAB_TOKEN_TEST", token)
	cfg := map[string]any{
		"gitlab-url": gitlabURL,
		"project":    "team/foo-deploy",
		"ref":        "feature/{{ .Namespace }}",
		"token-env":  "GITLAB_TOKEN_TEST",
		"applies-to": []any{"namespace", "service"},
		"variables": map[string]any{
			"NAMESPACE": "{{ .Namespace }}",
			"OBJECT":    "{{ .Kind }}/{{ .Name }}",
			"PROJECT":   "{{ .Project }}",
			"APP":       `{{ index .Labels "app" }}`,
			"ACTION":    "deploy",
			"REPLICAS":  2,
		},
	}
	for k, v := range extra {
		cfg[k] = v
	}
	h := New(actions.Deps{})
	if err := h.Configure(cfg); err != nil {
		t.Fatal(err)
	}
	return h
}

func service(name string) actions.StoredObject {
	svc := model.StoredService{Meta: model.Meta{Name: name, Namespace: "feat-1", Labels: map[string]string{"app": "web"}}, Type: "ClusterIP"}
	return actions.StoredObject{Ref: model.ObjectRef{Kind: model.KindService, Namespace: "feat-1", Name: name}, Object: svc, Project: "foo-projekt"}
}

func namespace() actions.StoredObject {
	ns := model.StoredNamespace{Meta: model.Meta{Name: "feat-1"}, Project: "foo-projekt"}
	return actions.StoredObject{Ref: model.ObjectRef{Kind: model.KindNamespace, Namespace: "feat-1", Name: "feat-1"}, Object: ns, Project: "foo-projekt"}
}

func TestExecuteTriggersPipeline(t *testing.T) {
	srv, last := fakeGitLab(t, http.StatusCreated, `{"id": 4711, "web_url": "https://gitlab.example/team/foo-deploy/-/pipelines/4711"}`)
	h := configured(t, srv.URL+"/", nil)

	res := h.Execute(context.Background(), service("web"), nil)
	if !res.Success || res.Message != "Pipeline #4711 gestartet: https://gitlab.example/team/foo-deploy/-/pipelines/4711" {
		t.Fatalf("res = %+v", res)
	}
	if last.path != "/api/v4/projects/team%2Ffoo-deploy/trigger/pipeline" {
		t.Errorf("path = %q", last.path)
	}
	want := map[string]string{
		"token":                token,
		"ref":                  "feature/feat-1",
		"variables[NAMESPACE]": "feat-1",
		"variables[OBJECT]":    "service/web",
		"variables[PROJECT]":   "foo-projekt",
		"variables[APP]":       "web",
		"variables[ACTION]":    "deploy",
		"variables[REPLICAS]":  "2",
	}
	for k, v := range want {
		if got := last.form.Get(k); got != v {
			t.Errorf("%s = %q, want %q", k, got, v)
		}
	}
}

func TestExecuteReportsGitLabErrors(t *testing.T) {
	srv, _ := fakeGitLab(t, http.StatusBadRequest, `{"message": {"base": ["Reference not found"]}}`)
	h := configured(t, srv.URL, nil)
	res := h.Execute(context.Background(), namespace(), nil)
	if res.Success || !strings.Contains(res.Message, "400") || !strings.Contains(res.Message, "Reference not found") {
		t.Fatalf("res = %+v", res)
	}
	if strings.Contains(res.Message, token) {
		t.Fatal("Token darf nicht in der Meldung stehen")
	}

	srv, _ = fakeGitLab(t, http.StatusUnauthorized, `{"message": "401 Unauthorized"}`)
	res = configured(t, srv.URL, nil).Execute(context.Background(), namespace(), nil)
	if res.Success || !strings.Contains(res.Message, "401 Unauthorized") {
		t.Fatalf("res = %+v", res)
	}

	srv.Close()
	res = configured(t, srv.URL, nil).Execute(context.Background(), namespace(), nil)
	if res.Success || strings.Contains(res.Message, token) {
		t.Fatalf("res = %+v", res)
	}
}

func TestExecuteFailsOnMissingTemplateKey(t *testing.T) {
	srv, last := fakeGitLab(t, http.StatusCreated, `{"id": 1}`)
	h := configured(t, srv.URL, map[string]any{"variables": map[string]any{"X": "{{ .Labels.missing }}"}})
	if res := h.Execute(context.Background(), namespace(), nil); res.Success {
		t.Fatalf("Fehler erwartet, res = %+v", res)
	}
	if last.form != nil {
		t.Fatal("bei Template-Fehler darf kein Request gesendet werden")
	}
}

func TestApplicability(t *testing.T) {
	h := configured(t, "https://gitlab.example", nil)
	if !h.IsApplicable(namespace()) || !h.IsApplicable(service("web")) {
		t.Fatal("Namespace und Service müssen applicable sein")
	}
	pod := actions.StoredObject{Ref: model.ObjectRef{Kind: model.KindPod, Namespace: "feat-1", Name: "p"}, Object: model.StoredPod{}}
	if h.IsApplicable(pod) {
		t.Fatal("Pod darf nicht applicable sein")
	}
}

func TestConfigureErrors(t *testing.T) {
	t.Setenv("GITLAB_TOKEN_TEST", token)
	valid := func() map[string]any {
		return map[string]any{
			"gitlab-url": "https://gitlab.example", "project": "1", "ref": "main",
			"token-env": "GITLAB_TOKEN_TEST", "applies-to": []any{"namespace"},
		}
	}
	if err := New(actions.Deps{}).Configure(valid()); err != nil {
		t.Fatalf("gültige Config: %v", err)
	}
	cases := map[string]func(m map[string]any){
		"Token nicht gesetzt": func(m map[string]any) { m["token-env"] = "GITLAB_TOKEN_UNSET" },
		"ungültige URL":       func(m map[string]any) { m["gitlab-url"] = "gitlab.example" },
		"ungültiger Kind":     func(m map[string]any) { m["applies-to"] = []any{"deployment"} },
		"applies-to fehlt":    func(m map[string]any) { delete(m, "applies-to") },
		"ref fehlt":           func(m map[string]any) { delete(m, "ref") },
		"project fehlt":       func(m map[string]any) { delete(m, "project") },
		"kaputtes Template":   func(m map[string]any) { m["variables"] = map[string]any{"X": "{{ .Name "} },
		"ungültiger Timeout":  func(m map[string]any) { m["timeout"] = "bald" },
		"projects (veraltet)": func(m map[string]any) { m["projects"] = []any{"foo-projekt"} },
	}
	for name, mutate := range cases {
		cfg := valid()
		mutate(cfg)
		err := New(actions.Deps{}).Configure(cfg)
		if err == nil {
			t.Errorf("%s: Fehler erwartet", name)
		} else if strings.Contains(err.Error(), token) {
			t.Errorf("%s: Token in Fehlermeldung", name)
		}
	}
}

func TestVariablesCanTrimProject(t *testing.T) {
	srv, last := fakeGitLab(t, http.StatusCreated, `{"id": 1, "web_url": "https://gitlab.example/p/-/pipelines/1"}`)
	h := configured(t, srv.URL, map[string]any{
		"ref":       "feature/{{ trimproject .Namespace }}",
		"variables": map[string]any{"FEATURE": "{{ trimproject .Namespace }}"},
	})
	ns := model.StoredNamespace{Meta: model.Meta{Name: "foo-projekt-login"}, Project: "foo-projekt"}
	obj := actions.StoredObject{Ref: model.ObjectRef{Kind: model.KindNamespace, Namespace: ns.Name, Name: ns.Name}, Object: ns, Project: "foo-projekt"}
	if res := h.Execute(context.Background(), obj, nil); !res.Success {
		t.Fatalf("res = %+v", res)
	}
	if got := last.form.Get("variables[FEATURE]"); got != "login" {
		t.Errorf("FEATURE = %q", got)
	}
	if got := last.form.Get("ref"); got != "feature/login" {
		t.Errorf("ref = %q", got)
	}
}
