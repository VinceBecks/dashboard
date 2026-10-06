package links

import (
	"net/url"
	"strings"
	"testing"

	"feature-dashboard/internal/config"
	"feature-dashboard/internal/model"
)

const kibanaConfig = `
link-variables:
  kibanaDiscover: https://kibana.mycomp.de/app/discover#/?_g=(time:(from:now-1h,to:now))
  kibanaDataView: logs-k8s
  wikiUrl: https://wiki.mycomp.de
projects:
  - project-name: foo
    namespace-labels: {a: b}
    link-variables-overrides:
      kibanaDataView: logs-foo
  - project-name: bar
    namespace-labels: {a: c}
links:
  - link-id: kibana-logs
    title: Logs in Kibana
    lucide-icon: scroll-text
    lucide-icon-color: "#00bfb3"
    projects: [foo, bar]
    targets:
      pod:
        url: |-
          {{ .Vars.kibanaDiscover }}&_a=(dataSource:(dataViewId:{{ rison .Vars.kibanaDataView }},type:dataView),
          query:(language:kuery,query:{{ rison (printf "kubernetes.namespace:%q and kubernetes.pod.name:%q" .Namespace .Name) }}))
      service:
        url: |-
          {{ .Vars.kibanaDiscover }}&_a=(dataSource:(dataViewId:{{ rison .Vars.kibanaDataView }},type:dataView),
          query:(language:kuery,query:'kubernetes.namespace:"{{ .Namespace }}"
          {{- range $k, $v := .Selector }} and kubernetes.labels.{{ replace $k "." "_" }}:"{{ $v }}"{{ end }}'))
  - link-id: wiki
    title: Team-Wiki
    projects: [bar]
    targets:
      service:
        url: '{{ .Vars.wikiUrl }}/{{ pathescape (index .Labels "team") }}?service={{ urlquery .Name }}&raw=a:b'
  - link-id: nowhere
    title: Nirgends
    projects: []
    targets:
      pod:
        url: "https://nowhere.example"
`

func registry(t *testing.T, yaml string) *Registry {
	t.Helper()
	cfg, err := config.Parse([]byte("cluster: {dns-name: nodes.example}\n" + yaml))
	if err != nil {
		t.Fatal(err)
	}
	r, err := NewRegistry(cfg)
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func ref(kind model.Kind, name string) model.ObjectRef {
	return model.ObjectRef{Kind: kind, Namespace: "feat-1", Name: name}
}

// fragment liest das (dekodierte) Fragment eines Kibana-Links.
func fragment(t *testing.T, raw string) string {
	t.Helper()
	u, err := url.Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	return u.Fragment
}

func TestKibanaLinkPerKind(t *testing.T) {
	r := registry(t, kibanaConfig)

	pod := r.For(ref(model.KindPod, "web-1"), model.StoredPod{}, "foo")
	if len(pod) != 1 || pod[0].ID != "kibana-logs" || pod[0].LucideIcon != "scroll-text" {
		t.Fatalf("Pod-Links = %+v", pod)
	}
	want := `/?_g=(time:(from:now-1h,to:now))&_a=(dataSource:(dataViewId:'logs-foo',type:dataView),` +
		`query:(language:kuery,query:'kubernetes.namespace:"feat-1" and kubernetes.pod.name:"web-1"'))`
	if got := fragment(t, pod[0].URL); got != want {
		t.Errorf("Pod-Fragment\n got  %s\n want %s", got, want)
	}
	if bar := r.For(ref(model.KindPod, "web-1"), model.StoredPod{}, "bar"); len(bar) != 1 || !strings.Contains(fragment(t, bar[0].URL), "dataViewId:'logs-k8s'") {
		t.Errorf("ohne Override muss die globale Data-View gelten: %+v", bar)
	}
	if strings.Contains(pod[0].URL, " ") {
		t.Errorf("URL darf keine rohen Leerzeichen enthalten: %s", pod[0].URL)
	}

	svc := model.StoredService{Selector: map[string]string{"app.kubernetes.io/name": "web", "tier": "backend"}}
	links := r.For(ref(model.KindService, "web"), svc, "foo")
	if len(links) != 1 {
		t.Fatalf("Service-Links = %+v", links)
	}
	got := fragment(t, links[0].URL)
	if !strings.Contains(got, `dataViewId:'logs-foo'`) ||
		!strings.Contains(got, `query:'kubernetes.namespace:"feat-1" and kubernetes.labels.app_kubernetes_io/name:"web" and kubernetes.labels.tier:"backend"'`) {
		t.Errorf("Service-Fragment = %s", got)
	}

	if links := r.For(ref(model.KindNamespace, "feat-1"), model.StoredNamespace{}, "foo"); len(links) != 0 {
		t.Errorf("Namespace ohne Target darf keine Links haben: %+v", links)
	}
}

func TestEncodingInTemplateAndProjects(t *testing.T) {
	r := registry(t, kibanaConfig)
	svc := model.StoredService{Meta: model.Meta{Labels: map[string]string{"team": "shop/core"}}}

	if links := r.For(ref(model.KindService, "web"), svc, "foo"); len(links) != 1 || links[0].ID != "kibana-logs" {
		t.Fatalf("wiki nur für Projekt bar: %+v", links)
	}
	links := r.For(ref(model.KindService, "web & co"), svc, "bar")
	if len(links) != 2 || links[1].URL != "https://wiki.mycomp.de/shop%2Fcore?service=web+%26+co&raw=a:b" {
		t.Fatalf("Links = %+v", links)
	}
	if links := r.For(ref(model.KindPod, "p"), model.StoredPod{}, "unknown"); len(links) != 0 {
		t.Fatalf("Projekt ohne Freigabe darf keine Links haben: %+v", links)
	}
}

func TestBrokenLinksAreOmitted(t *testing.T) {
	r := registry(t, `
projects: [{project-name: foo, namespace-labels: {a: b}}]
links:
  - {link-id: missing-var, title: X, projects: [foo], targets: {pod: {url: "{{ .Vars.nope }}"}}}
  - {link-id: js, title: X, projects: [foo], targets: {pod: {url: "javascript:alert(1)"}}}
  - {link-id: relative, title: X, projects: [foo], targets: {pod: {url: "/relative"}}}
  - {link-id: ok, title: X, projects: [foo], targets: {pod: {url: "https://ok.example/{{ .Name }}"}}}
`)
	links := r.For(ref(model.KindPod, "p"), model.StoredPod{}, "foo")
	if len(links) != 1 || links[0].ID != "ok" || links[0].URL != "https://ok.example/p" {
		t.Fatalf("Links = %+v", links)
	}
	if got := (*Registry)(nil).For(ref(model.KindPod, "p"), model.StoredPod{}, "foo"); got == nil || len(got) != 0 {
		t.Fatalf("nil-Registry muss leere Liste liefern: %#v", got)
	}
}

func TestInvalidTemplateFailsAtStartup(t *testing.T) {
	cfg, err := config.Parse([]byte(`
cluster: {dns-name: nodes.example}
projects: [{project-name: foo, namespace-labels: {a: b}}]
links:
  - {link-id: broken, title: X, projects: [foo], targets: {pod: {url: "https://x/{{ .Name "}}}
`))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := NewRegistry(cfg); err == nil {
		t.Fatal("kaputtes Template muss beim Start auffallen")
	}
}
