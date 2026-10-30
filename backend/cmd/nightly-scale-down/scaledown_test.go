package main

import (
	"context"
	"errors"
	"strings"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"

	"feature-dashboard/internal/actions/gitlabpipeline"
	"feature-dashboard/internal/actions/nightlyscaledown"
)

const key = nightlyscaledown.ExclusionLabelKey

func env(m map[string]string) func(string) string {
	return func(name string) string { return m[name] }
}

func validEnv() map[string]string {
	return map[string]string{
		EnvGitLabURL:     "https://gitlab.example",
		EnvGitLabProject: "team/scale-down",
		EnvGitLabRef:     "main",
		EnvTriggerToken:  "secret",
	}
}

func TestConfigFromEnv(t *testing.T) {
	c, err := configFromEnv(env(validEnv()))
	if err != nil {
		t.Fatal(err)
	}
	if c.Ref != "main" || c.Trigger.TokenEnv != EnvTriggerToken || c.Trigger.Project != "team/scale-down" {
		t.Fatalf("config = %+v", c)
	}

	// Alle fehlenden Pflichtvariablen werden gemeinsam gemeldet.
	_, err = configFromEnv(env(nil))
	for _, name := range []string{EnvGitLabURL, EnvGitLabProject, EnvGitLabRef, EnvTriggerToken} {
		if err == nil || !strings.Contains(err.Error(), name) {
			t.Errorf("Fehler zu %s erwartet: %v", name, err)
		}
	}

	e := validEnv()
	e[EnvGitLabTimeout] = "bald"
	if _, err := configFromEnv(env(e)); err == nil {
		t.Error("ungültiger Timeout: Fehler erwartet")
	}
}

func namespace(name string, labels map[string]string) *corev1.Namespace {
	return &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: name, Labels: labels}}
}

func client() *fake.Clientset {
	return fake.NewClientset(
		namespace("foo-feat-2", map[string]string{key: "true"}),
		namespace("foo-feat-1", map[string]string{key: "true", "a": "b"}),
		namespace("foo-feat-3", map[string]string{key: "false"}),
		namespace("foo-feat-4", nil),
	)
}

func TestExcludedNamespaces(t *testing.T) {
	got, err := excludedNamespaces(context.Background(), client())
	if err != nil || strings.Join(got, ",") != "foo-feat-1,foo-feat-2" {
		t.Fatalf("got %v, err %v", got, err)
	}
}

type fakeStarter struct {
	ref    string
	inputs map[string]string
	err    error
}

func (f *fakeStarter) Start(_ context.Context, ref string, _, inputs map[string]string) (gitlabpipeline.Pipeline, error) {
	f.ref, f.inputs = ref, inputs
	return gitlabpipeline.Pipeline{ID: 7, WebURL: "https://gitlab.example/p/-/pipelines/7"}, f.err
}

func TestStartScaleDown(t *testing.T) {
	cfg := config{Ref: "main"}
	s := &fakeStarter{}
	p, excluded, err := startScaleDown(context.Background(), cfg, client(), s)
	if err != nil || p.ID != 7 || len(excluded) != 2 {
		t.Fatalf("pipeline = %+v, excluded = %v, err = %v", p, excluded, err)
	}
	if s.ref != "main" || s.inputs[InputExcludeNamespaces] != "foo-feat-1,foo-feat-2" || len(s.inputs) != 1 {
		t.Fatalf("ref = %q, inputs = %v", s.ref, s.inputs)
	}

	// Ohne markierte Namespaces wird trotzdem gestartet, mit leerem Input.
	s = &fakeStarter{}
	if _, _, err := startScaleDown(context.Background(), cfg, fake.NewClientset(), s); err != nil {
		t.Fatal(err)
	}
	if v, ok := s.inputs[InputExcludeNamespaces]; !ok || v != "" {
		t.Fatalf("inputs = %v", s.inputs)
	}

	// Fehler beim Start werden durchgereicht.
	s = &fakeStarter{err: errors.New("GitLab antwortete mit 401")}
	if _, _, err := startScaleDown(context.Background(), cfg, client(), s); err == nil || !strings.Contains(err.Error(), "401") {
		t.Fatalf("err = %v", err)
	}
}
