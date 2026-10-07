package gitlabpipeline

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"feature-dashboard/internal/actions"
	"feature-dashboard/internal/pipelines"
)

// trackingGitLab beantwortet Trigger-Requests und Status-Abfragen; status[id] ist der Pipeline-Status.
func trackingGitLab(t *testing.T, status map[string]string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost:
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"id": 4711, "web_url": "https://gitlab.example/p/-/pipelines/4711"}`))
		case r.Header.Get("PRIVATE-TOKEN") != "glpat-read":
			w.WriteHeader(http.StatusUnauthorized)
		default:
			id := r.URL.Path[strings.LastIndex(r.URL.Path, "/")+1:]
			s, ok := status[id]
			if !ok {
				w.WriteHeader(http.StatusNotFound)
				return
			}
			_, _ = w.Write([]byte(`{"id": ` + id + `, "status": "` + s + `"}`))
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

func trackedHandler(t *testing.T, gitlabURL string, state *pipelines.State, statusTokenEnv string) (actions.Handler, error) {
	t.Helper()
	t.Setenv("GITLAB_TOKEN_TEST", token)
	t.Setenv("GITLAB_READ_TEST", "glpat-read")
	h := New(actions.Deps{Pipelines: state, ActionName: "deploy-feature"})
	return h, h.Configure(map[string]any{
		"gitlab-url": gitlabURL, "project": "team/foo-deploy", "ref": "main", "token-env": "GITLAB_TOKEN_TEST",
		"status-token-env": statusTokenEnv, "applies-to": []any{"namespace", "service"},
	})
}

func TestExecuteRemembersPipelineWithStatusToken(t *testing.T) {
	srv := trackingGitLab(t, map[string]string{"4711": "running"})
	state := pipelines.NewState()
	h, err := trackedHandler(t, srv.URL, state, "GITLAB_READ_TEST")
	if err != nil {
		t.Fatal(err)
	}
	if res := h.Execute(context.Background(), service("web"), nil); !res.Success {
		t.Fatalf("res = %+v", res)
	}
	got := state.For(service("web").Ref)
	if len(got) != 1 || got[0].ID != 4711 || got[0].Action != "deploy-feature" || got[0].Project != "team/foo-deploy" ||
		got[0].WebURL != "https://gitlab.example/p/-/pipelines/4711" || got[0].StartedAt.IsZero() {
		t.Fatalf("Einträge = %+v", got)
	}

	// Der Trigger ist als Status-Abfrage registriert: running bleibt, nach success entfernt der Tracker den Eintrag.
	tracker := pipelines.NewTracker(state, 0)
	tracker.CheckOnce(context.Background())
	if len(state.All()) != 1 {
		t.Fatal("laufende Pipeline darf nicht entfernt werden")
	}
	srv.Config.Handler = trackingGitLab(t, map[string]string{"4711": "success"}).Config.Handler
	tracker.CheckOnce(context.Background())
	if len(state.All()) != 0 {
		t.Fatalf("beendete Pipeline muss entfernt sein: %+v", state.All())
	}
}

func TestExecuteWithoutStatusTokenRemembersNothing(t *testing.T) {
	srv := trackingGitLab(t, nil)
	state := pipelines.NewState()
	h, err := trackedHandler(t, srv.URL, state, "")
	if err != nil {
		t.Fatal(err)
	}
	if res := h.Execute(context.Background(), namespace(), nil); !res.Success || len(state.All()) != 0 {
		t.Fatalf("res = %+v, Einträge = %+v", res, state.All())
	}
}

func TestStatusTokenEnvMustBeSet(t *testing.T) {
	if _, err := trackedHandler(t, "https://gitlab.example", nil, "GIBT_ES_NICHT"); err == nil || !strings.Contains(err.Error(), "GIBT_ES_NICHT") {
		t.Fatalf("err = %v", err)
	}
}

func TestPipelineStatus(t *testing.T) {
	srv := trackingGitLab(t, map[string]string{"1": "pending"})
	t.Setenv("GITLAB_TOKEN_TEST", token)
	t.Setenv("GITLAB_READ_TEST", "glpat-read")
	tr, err := NewTrigger(TriggerConfig{GitLabURL: srv.URL, Project: "team/foo", Ref: "main", TokenEnv: "GITLAB_TOKEN_TEST", StatusTokenEnv: "GITLAB_READ_TEST"})
	if err != nil {
		t.Fatal(err)
	}
	if s, err := tr.PipelineStatus(context.Background(), 1); err != nil || s != "pending" {
		t.Fatalf("status = %q, %v", s, err)
	}
	if _, err := tr.PipelineStatus(context.Background(), 2); !errors.Is(err, pipelines.ErrNotFound) {
		t.Fatalf("404 → ErrNotFound erwartet, got %v", err)
	}
	tr.statusToken = "falsch"
	if _, err := tr.PipelineStatus(context.Background(), 1); err == nil || errors.Is(err, pipelines.ErrNotFound) {
		t.Fatalf("401 → Fehler erwartet, got %v", err)
	}
}
