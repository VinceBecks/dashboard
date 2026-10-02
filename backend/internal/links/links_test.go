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
  kibanaUrl: https://kibana.mycomp.de
  kibanaDataView: logs-k8s
  timeRange: now-1h
projects:
  - project-name: foo
    namespace-labels: {a: b}
    link-variables:
      kibanaDataView: logs-foo
  - project-name: bar
    namespace-labels: {a: c}
links:
  - link-id: kibana-logs
    title: Logs in Kibana
    lucide-icon: scroll-text
    lucide-icon-color: "#00bfb3"
    targets:
      namespace:
        query: 'kubernetes.namespace:"{{ .Namespace }}"'
      pod:
        query: 'kubernetes.namespace:"{{ .Namespace }}" and kubernetes.pod.name:"{{ .Name }}"'
      service:
        query: >-
          kubernetes.namespace:"{{ .Namespace }}"
          {{- range $k, $v := .Selector }} and kubernetes.labels.{{ replace $k "." "_" }}:"{{ $v }}"{{ end }}
    url: |-
      {{ .Vars.kibanaUrl }}/app/discover#/?_g=(time:(from:{{ .Vars.timeRange }},to:now))
      &_a=(dataSource:(dataViewId:{{ rison .Vars.kibanaDataView }},type:dataView),
      query:(language:kuery,query:{{ rison .Target.query }}))
  - link-id: grafana-pod
    title: Grafana
    projects: [bar]
    targets:
      pod: {}
    url: "https://grafana.mycomp.de/d/k8s-pod?orgId=1"
    query-params:
      var-namespace: "{{ .Namespace }}"
      var-pod: "{{ .Name }}"
  - link-id: by-label
    title: Team-Wiki
    targets:
      service: {}
    url: '{{ index .Labels "team" | printf "https://wiki.mycomp.de/%s" }}'
`

func registry(t *testing.T, yaml string) *Registry {
	t.Helper()
	cfg, err := config.Parse([]byte(yaml))
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

func TestKibanaLinks(t *testing.T) {
	r := registry(t, kibanaConfig)

	pod := r.For(ref(model.KindPod, "web-1"), model.StoredPod{Meta: model.Meta{Name: "web-1"}}, "foo")
	if len(pod) != 1 || pod[0].ID != "kibana-logs" || pod[0].Title != "Logs in Kibana" || pod[0].LucideIcon != "scroll-text" {
		t.Fatalf("Pod-Links = %+v", pod)
	}
	want := `/?_g=(time:(from:now-1h,to:now))&_a=(dataSource:(dataViewId:'logs-foo',type:dataView),` +
		`query:(language:kuery,query:'kubernetes.namespace:"feat-1" and kubernetes.pod.name:"web-1"'))`
	if got := fragment(t, pod[0].URL); got != want {
		t.Errorf("Fragment\n got  %s\n want %s", got, want)
	}
	if !strings.HasPrefix(pod[0].URL, "https://kibana.mycomp.de/app/discover#") || strings.Contains(pod[0].URL, " ") {
		t.Errorf("URL = %s", pod[0].URL)
	}

	svc := model.StoredService{
		Meta:     model.Meta{Name: "web", Labels: map[string]string{"team": "shop"}},
		Selector: map[string]string{"app.kubernetes.io/name": "web", "tier": "backend"},
	}
	links := r.For(ref(model.KindService, "web"), svc, "bar")
	if len(links) != 2 || links[1].URL != "https://wiki.mycomp.de/shop" {
		t.Fatalf("Service-Links = %+v", links)
	}
	if got := fragment(t, links[0].URL); !strings.Contains(got,
		`dataViewId:'logs-k8s'`) || !strings.Contains(got,
		`query:'kubernetes.namespace:"feat-1" and kubernetes.labels.app_kubernetes_io/name:"web" and kubernetes.labels.tier:"backend"'`) {
		t.Errorf("Service-Fragment = %s", got)
	}

	if links := r.For(ref(model.KindIngress, "web"), model.StoredIngress{}, "foo"); len(links) != 0 {
		t.Errorf("Ingress ohne Target darf keine Links haben: %+v", links)
	}
}

func TestQueryParamsAndProjects(t *testing.T) {
	r := registry(t, kibanaConfig)
	pod := model.StoredPod{Meta: model.Meta{Name: "web-1"}}
	if links := r.For(ref(model.KindPod, "web-1"), pod, "foo"); len(links) != 1 {
		t.Fatalf("grafana-pod nur für Projekt bar erwartet: %+v", links)
	}
	links := r.For(ref(model.KindPod, "web 1&x"), pod, "bar")
	if len(links) != 2 || links[1].URL != "https://grafana.mycomp.de/d/k8s-pod?orgId=1&var-namespace=feat-1&var-pod=web+1%26x" {
		t.Fatalf("Links = %+v", links)
	}
}

func TestBrokenLinksAreOmitted(t *testing.T) {
	// Fehlende Variable, Nicht-http- und relative URL führen zum Weglassen nur des betroffenen Links.
	r := registry(t, `
projects: [{project-name: foo, namespace-labels: {a: b}}]
links:
  - {link-id: missing-var, title: X, targets: {pod: {}}, url: "{{ .Vars.nope }}"}
  - {link-id: js, title: X, targets: {pod: {}}, url: "javascript:alert(1)"}
  - {link-id: relative, title: X, targets: {pod: {}}, url: "/relative"}
  - {link-id: ok, title: X, targets: {pod: {}}, url: "https://ok.example/{{ .Name }}"}
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
projects: [{project-name: foo, namespace-labels: {a: b}}]
links:
  - {link-id: broken, title: X, targets: {pod: {q: "{{ .Name "}}, url: "https://x"}
`))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := NewRegistry(cfg); err == nil {
		t.Fatal("kaputtes Template muss beim Start auffallen")
	}
}

func TestEncodeQueryParams(t *testing.T) {
	r := registry(t, `
projects: [{project-name: foo, namespace-labels: {a: b}}]
links:
  - link-id: encoded
    title: X
    targets: {pod: {}}
    url: "https://grafana.example/d/pod?orgId=1"
    query-params:
      var-pod: "{{ .Name }}"
      filter: "a:b,c/d"
  - link-id: raw
    title: X
    targets: {pod: {}}
    encode-query-params: false
    url: "https://tool.example/view?orgId=1"
    query-params:
      var-pod: "{{ .Name }}"
      filter: "a:b,c/d"
      pre: "%2Fbereits%2Fkodiert"
  - link-id: raw-invalid
    title: X
    targets: {pod: {}}
    encode-query-params: false
    url: "https://tool.example/view"
    query-params:
      q: "mit Leerzeichen"
`)
	links := r.For(ref(model.KindPod, "web-1"), model.StoredPod{}, "foo")
	if len(links) != 2 {
		t.Fatalf("raw-invalid muss weggelassen werden: %+v", links)
	}
	if want := "https://grafana.example/d/pod?filter=a%3Ab%2Cc%2Fd&orgId=1&var-pod=web-1"; links[0].URL != want {
		t.Errorf("kodiert:\n got  %s\n want %s", links[0].URL, want)
	}
	if want := "https://tool.example/view?orgId=1&filter=a:b,c/d&pre=%2Fbereits%2Fkodiert&var-pod=web-1"; links[1].URL != want {
		t.Errorf("unkodiert:\n got  %s\n want %s", links[1].URL, want)
	}
}
