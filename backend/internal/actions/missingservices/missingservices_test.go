package missingservices

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"feature-dashboard/internal/actions"
	"feature-dashboard/internal/config"
	"feature-dashboard/internal/model"
	"feature-dashboard/internal/testsource"
)

const token = "glptt-secret-token"

// fakeGitLab beantwortet Trigger-Requests mit 201 und merkt sich den Body des letzten Requests.
func fakeGitLab(t *testing.T) (*httptest.Server, *url.Values) {
	t.Helper()
	var last url.Values
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		last, _ = url.ParseQuery(string(raw))
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"id": 7, "web_url": "https://gitlab.example/p/7"}`))
	}))
	t.Cleanup(srv.Close)
	return srv, &last
}

func newStore(t *testing.T, services ...string) *testsource.Source {
	t.Helper()
	st := testsource.New()
	err := st.Update(func(tx *testsource.Tx) error {
		if err := tx.PutNamespace(model.StoredNamespace{Meta: model.Meta{Name: "feat-1"}, Project: "foo"}); err != nil {
			return err
		}
		for _, name := range services {
			if _, err := tx.Put(model.KindService, "feat-1", name, model.StoredService{Meta: model.Meta{Name: name, Namespace: "feat-1"}}); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return st
}

func validConfig(gitlabURL string) map[string]any {
	return map[string]any{
		"services": []any{
			map[string]any{"name": "backend", "label": "Backend"},
			map[string]any{"name": "frontend"},
			map[string]any{"name": "worker"},
		},
		"gitlab-url": gitlabURL,
		"project":    "team/foo-deploy",
		"ref":        "main",
		"token-env":  "GITLAB_TOKEN_TEST",
		"variables": map[string]any{
			"NAMESPACE": "{{ .Namespace }}",
			"SERVICES":  `{{ join .Services "," }}`,
		},
	}
}

func configured(t *testing.T, st actions.ObjectReader, gitlabURL string) actions.Handler {
	t.Helper()
	t.Setenv("GITLAB_TOKEN_TEST", token)
	h := New(actions.Deps{Store: st})
	if err := h.Configure(validConfig(gitlabURL)); err != nil {
		t.Fatal(err)
	}
	return h
}

func namespace() actions.StoredObject {
	ns := model.StoredNamespace{Meta: model.Meta{Name: "feat-1"}, Project: "foo"}
	return actions.StoredObject{Ref: model.ObjectRef{Kind: model.KindNamespace, Namespace: "feat-1", Name: "feat-1"}, Object: ns, Project: "foo"}
}

func TestFormFieldsListMissingServices(t *testing.T) {
	h := configured(t, newStore(t, "frontend", "unrelated"), "https://gitlab.example")
	fields := h.FormFields(namespace())
	if len(fields) != 1 || fields[0].ID != FieldServices || fields[0].Type != actions.FieldMultiSelect || !fields[0].IsRequired {
		t.Fatalf("fields = %+v", fields)
	}
	want := []actions.Option{{Value: "backend", Label: "Backend"}, {Value: "worker", Label: "worker"}}
	if got := fields[0].Options; len(got) != 2 || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("options = %+v", got)
	}

	h = configured(t, newStore(t, "backend", "frontend", "worker"), "https://gitlab.example")
	fields = h.FormFields(namespace())
	if len(fields[0].Options) != 0 || !strings.Contains(fields[0].Description, "bereits deployt") {
		t.Fatalf("fields = %+v", fields)
	}
}

func TestExecuteTriggersPipeline(t *testing.T) {
	srv, last := fakeGitLab(t)
	h := configured(t, newStore(t, "frontend"), srv.URL)
	res := h.Execute(context.Background(), namespace(), map[string]any{FieldServices: []any{"worker", "backend", "worker"}})
	if !res.Success || !strings.Contains(res.Message, "#7") {
		t.Fatalf("res = %+v", res)
	}
	want := map[string]string{"token": token, "ref": "main", "variables[NAMESPACE]": "feat-1", "variables[SERVICES]": "worker,backend"}
	for k, v := range want {
		if got := last.Get(k); got != v {
			t.Errorf("%s = %q, want %q", k, got, v)
		}
	}
}

func TestExecuteRejectsDeployedService(t *testing.T) {
	srv, last := fakeGitLab(t)
	h := configured(t, newStore(t, "frontend"), srv.URL)
	for _, inputs := range []map[string]any{
		{FieldServices: []any{"backend", "frontend"}},
		{FieldServices: []any{}},
		nil,
	} {
		if res := h.Execute(context.Background(), namespace(), inputs); res.Success {
			t.Errorf("inputs %v: Fehler erwartet", inputs)
		}
	}
	if *last != nil {
		t.Fatal("es darf kein Request gesendet werden")
	}
}

func TestRegistryValidatesSelection(t *testing.T) {
	t.Setenv("GITLAB_TOKEN_TEST", token)
	srv, _ := fakeGitLab(t)
	reg := actions.NewRegistry()
	reg.Register(HandlerType, New)
	err := reg.Activate([]config.ActionHandlerConfig{{Name: HandlerType, Projects: []string{"foo"}, Extra: validConfig(srv.URL)}},
		actions.Deps{Store: newStore(t, "frontend")})
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	var verr *actions.ValidationError
	if _, err := reg.Execute(ctx, HandlerType, namespace(), map[string]any{FieldServices: []any{"frontend"}}); !errors.As(err, &verr) {
		t.Fatalf("deployter Service: err = %v", err)
	}
	if _, err := reg.Execute(ctx, HandlerType, namespace(), map[string]any{FieldServices: []any{}}); !errors.As(err, &verr) {
		t.Fatalf("leere Auswahl: err = %v", err)
	}
	if res, err := reg.Execute(ctx, HandlerType, namespace(), map[string]any{FieldServices: []any{"backend"}}); err != nil || !res.Success {
		t.Fatalf("res = %+v, err = %v", res, err)
	}
	svc := actions.StoredObject{Ref: model.ObjectRef{Kind: model.KindService, Namespace: "feat-1", Name: "frontend"}, Object: model.StoredService{}, Project: "foo"}
	if got := reg.ActionsFor(svc); len(got) != 0 {
		t.Fatalf("Aktion darf nur an Namespaces hängen: %+v", got)
	}
}

func TestConfigureErrors(t *testing.T) {
	t.Setenv("GITLAB_TOKEN_TEST", token)
	cases := map[string]func(m map[string]any){
		"services fehlt": func(m map[string]any) { delete(m, "services") },
		"services leer":  func(m map[string]any) { m["services"] = []any{} },
		"name fehlt":     func(m map[string]any) { m["services"] = []any{map[string]any{"label": "X"}} },
		"name doppelt": func(m map[string]any) {
			m["services"] = []any{map[string]any{"name": "a"}, map[string]any{"name": "a"}}
		},
		"Token nicht gesetzt": func(m map[string]any) { m["token-env"] = "GITLAB_TOKEN_UNSET" },
		"ref fehlt":           func(m map[string]any) { delete(m, "ref") },
	}
	for name, mutate := range cases {
		cfg := validConfig("https://gitlab.example")
		mutate(cfg)
		if err := New(actions.Deps{}).Configure(cfg); err == nil {
			t.Errorf("%s: Fehler erwartet", name)
		}
	}
}
