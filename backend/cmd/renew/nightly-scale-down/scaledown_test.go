package main

import (
	"context"
	"errors"
	"strings"
	"testing"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/fake"
	k8stesting "k8s.io/client-go/testing"

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
	if c.Ref != "main" || c.Trigger.TokenEnv != EnvTriggerToken || c.Trigger.Project != "team/scale-down" || c.TrimNamespacePrefix != "" {
		t.Fatalf("config = %+v", c)
	}

	e := validEnv()
	e[EnvTrimNamespacePrefix] = "foo-"
	if c, err := configFromEnv(env(e)); err != nil || c.TrimNamespacePrefix != "foo-" {
		t.Fatalf("config = %+v, err = %v", c, err)
	}

	// Alle fehlenden Pflichtvariablen werden gemeinsam gemeldet.
	_, err = configFromEnv(env(nil))
	for _, name := range []string{EnvGitLabURL, EnvGitLabProject, EnvGitLabRef, EnvTriggerToken} {
		if err == nil || !strings.Contains(err.Error(), name) {
			t.Errorf("Fehler zu %s erwartet: %v", name, err)
		}
	}

	e = validEnv()
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

func TestPipelineNamespaces(t *testing.T) {
	tests := []struct {
		names  []string
		prefix string
		want   string
	}{
		{[]string{"foo-feat-1", "foo-feat-2"}, "", "foo-feat-1,foo-feat-2"},
		{[]string{"foo-feat-1", "foo-feat-2"}, "foo-", "feat-1,feat-2"},
		{[]string{"bar-x", "foo-feat-1"}, "foo-", "bar-x,feat-1"}, // ohne Präfix unverändert, sortiert
		{[]string{"foo-"}, "foo-", ""},                            // leerer Name fällt weg
		{[]string{"feat-1", "foo-feat-1"}, "foo-", "feat-1"},      // Duplikat nur einmal
		{nil, "foo-", ""},
	}
	for _, tt := range tests {
		if got := strings.Join(pipelineNamespaces(tt.names, tt.prefix), ","); got != tt.want {
			t.Errorf("pipelineNamespaces(%v, %q) = %q, want %q", tt.names, tt.prefix, got, tt.want)
		}
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

// labelsOf liefert die Labels eines Namespaces aus dem Fake-Client.
func labelsOf(t *testing.T, c *fake.Clientset, name string) map[string]string {
	t.Helper()
	ns, err := c.CoreV1().Namespaces().Get(context.Background(), name, metav1.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}
	return ns.Labels
}

func TestStartScaleDown(t *testing.T) {
	cfg := config{Ref: "main"}
	s := &fakeStarter{}
	c := client()
	p, excluded, err := startScaleDown(context.Background(), cfg, c, s)
	if err != nil || p.ID != 7 || len(excluded) != 2 {
		t.Fatalf("pipeline = %+v, excluded = %v, err = %v", p, excluded, err)
	}
	if s.ref != "main" || s.inputs[InputExcludeNamespaces] != "foo-feat-1,foo-feat-2" || len(s.inputs) != 1 {
		t.Fatalf("ref = %q, inputs = %v", s.ref, s.inputs)
	}

	// Nach dem Start ist das Ausnahme-Label entfernt, andere Labels und andere Werte bleiben.
	for _, name := range []string{"foo-feat-1", "foo-feat-2"} {
		if _, ok := labelsOf(t, c, name)[key]; ok {
			t.Errorf("%s: Label %s sollte entfernt sein", name, key)
		}
	}
	if l := labelsOf(t, c, "foo-feat-1"); l["a"] != "b" {
		t.Errorf("foo-feat-1: andere Labels verloren: %v", l)
	}
	if l := labelsOf(t, c, "foo-feat-3"); l[key] != "false" {
		t.Errorf("foo-feat-3: Labels verändert: %v", l)
	}

	// Mit Präfix wird nur der Input gekürzt, die Labels werden an den echten Namespaces entfernt.
	s = &fakeStarter{}
	c = client()
	if _, _, err := startScaleDown(context.Background(), config{Ref: "main", TrimNamespacePrefix: "foo-"}, c, s); err != nil {
		t.Fatal(err)
	}
	if got := s.inputs[InputExcludeNamespaces]; got != "feat-1,feat-2" {
		t.Errorf("input = %q", got)
	}
	for _, name := range []string{"foo-feat-1", "foo-feat-2"} {
		if _, ok := labelsOf(t, c, name)[key]; ok {
			t.Errorf("%s: Label %s sollte entfernt sein", name, key)
		}
	}

	// Ohne markierte Namespaces wird trotzdem gestartet, mit leerem Input.
	s = &fakeStarter{}
	if _, _, err := startScaleDown(context.Background(), cfg, fake.NewClientset(), s); err != nil {
		t.Fatal(err)
	}
	if v, ok := s.inputs[InputExcludeNamespaces]; !ok || v != "" {
		t.Fatalf("inputs = %v", s.inputs)
	}

	// Fehler beim Start werden durchgereicht, die Labels bleiben.
	s = &fakeStarter{err: errors.New("GitLab antwortete mit 401")}
	c = client()
	if _, _, err := startScaleDown(context.Background(), cfg, c, s); err == nil || !strings.Contains(err.Error(), "401") || errors.Is(err, errRemoveExclusions) {
		t.Fatalf("err = %v", err)
	}
	if l := labelsOf(t, c, "foo-feat-1"); l[key] != "true" {
		t.Errorf("foo-feat-1: Label sollte bleiben: %v", l)
	}
}

func TestStartScaleDownRemoveFails(t *testing.T) {
	c := client()
	c.PrependReactor("patch", "namespaces", func(a k8stesting.Action) (bool, runtime.Object, error) {
		if a.(k8stesting.PatchAction).GetName() == "foo-feat-1" {
			return true, nil, errors.New("forbidden")
		}
		return false, nil, nil
	})
	p, _, err := startScaleDown(context.Background(), config{Ref: "main"}, c, &fakeStarter{})
	if !errors.Is(err, errRemoveExclusions) || !strings.Contains(err.Error(), "foo-feat-1") || p.ID != 7 {
		t.Fatalf("pipeline = %+v, err = %v", p, err)
	}
	// Der zweite Namespace wird trotzdem bearbeitet.
	if _, ok := labelsOf(t, c, "foo-feat-2")[key]; ok {
		t.Error("foo-feat-2: Label sollte entfernt sein")
	}
}

func TestRemoveExclusionsIgnoresNotFound(t *testing.T) {
	c := fake.NewClientset()
	c.PrependReactor("patch", "namespaces", func(a k8stesting.Action) (bool, runtime.Object, error) {
		return true, nil, apierrors.NewNotFound(corev1.Resource("namespaces"), "weg")
	})
	if err := removeExclusions(context.Background(), c, []string{"weg"}); err != nil {
		t.Fatal(err)
	}
}
