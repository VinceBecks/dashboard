package gitlabpipeline

import (
	"context"
	"net/http"
	"strings"
	"testing"
)

func newTrigger(t *testing.T, gitlabURL string) *Trigger {
	t.Helper()
	t.Setenv("GITLAB_TOKEN_TEST", token)
	tr, err := NewTrigger(TriggerConfig{GitLabURL: gitlabURL, Project: "team/scale-down", Ref: "main", TokenEnv: "GITLAB_TOKEN_TEST"})
	if err != nil {
		t.Fatal(err)
	}
	return tr
}

func TestStartSendsInputs(t *testing.T) {
	srv, last := fakeGitLab(t, http.StatusCreated, `{"id": 42, "web_url": "https://gitlab.example/p/-/pipelines/42"}`)
	p, err := newTrigger(t, srv.URL).Start(context.Background(), "main", nil, map[string]string{"EXCLUDE_NAMESPACES": "a,b"})
	if err != nil || p.ID != 42 || p.WebURL != "https://gitlab.example/p/-/pipelines/42" {
		t.Fatalf("pipeline = %+v, err = %v", p, err)
	}
	if last.path != "/api/v4/projects/team%2Fscale-down/trigger/pipeline" {
		t.Errorf("path = %s", last.path)
	}
	if got := last.form.Get("inputs[EXCLUDE_NAMESPACES]"); got != "a,b" {
		t.Errorf("inputs[EXCLUDE_NAMESPACES] = %q", got)
	}
	if last.form.Get("ref") != "main" || last.form.Get("token") != token {
		t.Errorf("form = %v", last.form)
	}
	for key := range last.form {
		if strings.HasPrefix(key, "variables[") {
			t.Errorf("unerwartete Variable %s", key)
		}
	}
}

func TestStartReportsGitLabError(t *testing.T) {
	srv, _ := fakeGitLab(t, http.StatusBadRequest, `{"message": {"base": ["Input EXCLUDE_NAMESPACES unbekannt"]}}`)
	_, err := newTrigger(t, srv.URL).Start(context.Background(), "main", nil, map[string]string{"EXCLUDE_NAMESPACES": ""})
	if err == nil || !strings.Contains(err.Error(), "400") || !strings.Contains(err.Error(), "EXCLUDE_NAMESPACES unbekannt") {
		t.Fatalf("err = %v", err)
	}
}
