package copycommands

import (
	"reflect"
	"testing"

	"feature-dashboard/internal/config"
	"feature-dashboard/internal/model"
)

const sample = `
cluster: {dns-name: nodes.example}
link-variables:
  context: kind-demo
projects:
  - project-name: foo
    namespace-labels: {a: b}
  - project-name: bar
    namespace-labels: {a: c}
    link-variables-overrides:
      context: kind-bar
copy-commands:
  - command-id: pod-shell
    title: Shell
    icon: terminal
    projects: [foo, bar]
    targets:
      pod:
        command: kubectl --context {{ .Vars.context }} exec -it -n {{ .Namespace }} {{ .Name }} -c {{ .Container }} -- sh
  - command-id: logs
    title: Logs
    projects: [foo]
    targets:
      pod:
        command: kubectl logs -f -n {{ .Namespace }} {{ .Name }} --all-containers
      namespace:
        command: |
          kubectl config set-context --current --namespace={{ shellquote .Namespace }}
  - command-id: broken
    title: Kaputt
    projects: [foo]
    targets:
      service:
        command: echo {{ .Vars.unbekannt }}
`

func registry(t *testing.T) *Registry {
	t.Helper()
	cfg, err := config.Parse([]byte(sample))
	if err != nil {
		t.Fatal(err)
	}
	r, err := NewRegistry(cfg)
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func pod(containers ...string) model.StoredPod {
	p := model.StoredPod{Meta: model.Meta{Name: "web-1", Namespace: "feat-1"}}
	for _, c := range containers {
		p.Containers = append(p.Containers, model.Container{Name: c})
	}
	return p
}

var podRef = model.ObjectRef{Kind: model.KindPod, Namespace: "feat-1", Name: "web-1"}

func TestPodWithOneContainer(t *testing.T) {
	got := registry(t).For(podRef, pod("app"), "foo")
	want := []Command{
		{ID: "pod-shell", Title: "Shell", Icon: "terminal", Command: "kubectl --context kind-demo exec -it -n feat-1 web-1 -c app -- sh"},
		{ID: "logs", Title: "Logs", Command: "kubectl logs -f -n feat-1 web-1 --all-containers"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got  %+v\nwant %+v", got, want)
	}
}

func TestPodWithTwoContainers(t *testing.T) {
	got := registry(t).For(podRef, pod("app", "sidecar"), "bar")
	want := []Command{
		{ID: "pod-shell/app", Title: "Shell (app)", Icon: "terminal", Command: "kubectl --context kind-bar exec -it -n feat-1 web-1 -c app -- sh"},
		{ID: "pod-shell/sidecar", Title: "Shell (sidecar)", Icon: "terminal", Command: "kubectl --context kind-bar exec -it -n feat-1 web-1 -c sidecar -- sh"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got  %+v\nwant %+v", got, want)
	}

	// Template ohne .Container bleibt ein einziger Befehl, auch bei mehreren Containern.
	got = registry(t).For(podRef, pod("app", "sidecar"), "foo")
	if len(got) != 3 || got[2].ID != "logs" || got[2].Title != "Logs" {
		t.Fatalf("got %+v", got)
	}
}

func TestOtherKindsAndErrors(t *testing.T) {
	r := registry(t)
	ns := model.StoredNamespace{Meta: model.Meta{Name: "feat-1"}, Project: "foo"}
	got := r.For(model.ObjectRef{Kind: model.KindNamespace, Namespace: "feat-1", Name: "feat-1"}, ns, "foo")
	if len(got) != 1 || got[0].Command != "kubectl config set-context --current --namespace='feat-1'" {
		t.Fatalf("namespace: %+v", got)
	}
	// Render-Fehler (fehlende Variable) → Befehl entfällt; Typ ohne Target → keine Befehle.
	svc := model.StoredService{Meta: model.Meta{Name: "web"}}
	if got := r.For(model.ObjectRef{Kind: model.KindService, Namespace: "feat-1", Name: "web"}, svc, "foo"); len(got) != 0 {
		t.Fatalf("service: %+v", got)
	}
	ing := model.StoredIngress{Meta: model.Meta{Name: "web"}}
	if got := r.For(model.ObjectRef{Kind: model.KindIngress, Namespace: "feat-1", Name: "web"}, ing, "foo"); got == nil || len(got) != 0 {
		t.Fatalf("ingress: %#v", got)
	}
	// Projekt-Filter: logs gibt es nur in foo.
	if got := r.For(model.ObjectRef{Kind: model.KindNamespace, Namespace: "feat-1", Name: "feat-1"}, ns, "bar"); len(got) != 0 {
		t.Fatalf("bar: %+v", got)
	}
	// Ohne Registry: leere Liste.
	var none *Registry
	if got := none.For(podRef, pod("app"), "foo"); got == nil || len(got) != 0 {
		t.Fatalf("nil-Registry: %#v", got)
	}
}

func TestInvalidTemplateFailsAtStartup(t *testing.T) {
	cfg, err := config.Parse([]byte(`
cluster: {dns-name: nodes.example}
projects: [{project-name: foo, namespace-labels: {a: b}}]
copy-commands:
  - {command-id: x, title: X, projects: [foo], targets: {pod: {command: "echo {{ .Name "}}}
`))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := NewRegistry(cfg); err == nil {
		t.Fatal("Fehler erwartet")
	}
}
