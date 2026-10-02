package config

import (
	"testing"
	"time"
)

const sample = `
projects:
  - project-name: foo-projekt
    namespace-labels:
      mycomp.de/is-feature-environment: true
      mycomp.de/project-name: foo-projekt
actionHandlers:
  - action-name: delete-pod
    enabled: true
    action-title: Pod Löschen
    lucide-icon: trash
    lucide-icon-color: red
    extra-handler-configs:
      force-delete: true
store:
  recreate-store-interval: 2d
`

func TestParse(t *testing.T) {
	cfg, err := Parse([]byte(sample))
	if err != nil {
		t.Fatal(err)
	}
	if got := cfg.Projects[0].NamespaceLabels["mycomp.de/is-feature-environment"]; got != "true" {
		t.Errorf("bool-Label als String erwartet, got %q", got)
	}
	a := cfg.ActionHandlers[0]
	if !a.Enabled || a.Title != "Pod Löschen" || a.Extra["force-delete"] != true {
		t.Errorf("unerwarteter ActionHandler: %+v", a)
	}
	if cfg.Store.RecreateInterval.Duration() != 48*time.Hour {
		t.Errorf("interval = %v", cfg.Store.RecreateInterval.Duration())
	}
}

func TestParseInterval(t *testing.T) {
	cases := map[string]time.Duration{"0": 0, "15m": 15 * time.Minute, "1h": time.Hour, "1d": 24 * time.Hour}
	for in, want := range cases {
		got, err := ParseInterval(in)
		if err != nil || got != want {
			t.Errorf("ParseInterval(%q) = %v, %v; want %v", in, got, err, want)
		}
	}
	for _, in := range []string{"1s", "abc", "-1h", "h"} {
		if _, err := ParseInterval(in); err == nil {
			t.Errorf("ParseInterval(%q): Fehler erwartet", in)
		}
	}
}

func TestValidate(t *testing.T) {
	_, err := Parse([]byte(`
projects:
  - project-name: a
    namespace-labels: {x: y}
  - project-name: a
actionHandlers:
  - action-name: x
  - action-name: x
`))
	if err == nil {
		t.Fatal("Validierungsfehler erwartet")
	}
}

func TestHandlerType(t *testing.T) {
	cfg, err := Parse([]byte(`
actionHandlers:
  - action-name: deploy
    handler: gitlab-pipeline
  - action-name: delete-pod
`))
	if err != nil {
		t.Fatal(err)
	}
	if got := cfg.ActionHandlers[0].HandlerType(); got != "gitlab-pipeline" {
		t.Errorf("HandlerType = %q", got)
	}
	if got := cfg.ActionHandlers[1].HandlerType(); got != "delete-pod" {
		t.Errorf("HandlerType ohne handler-Feld = %q", got)
	}
}

func TestServiceAreas(t *testing.T) {
	cfg, err := Parse([]byte(`
projects:
  - project-name: foo
    namespace-labels: {a: b}
    service-areas:
      - area-name: business
        area-label: Business
        label-combinations:
          - app.kubernetes.io/part-of: shop
            tier: backend
          - app.kubernetes.io/part-of: billing
  - project-name: bar
    namespace-labels: {a: c}
    others-area-label: Sonstige
    service-areas:
      - area-name: infra
        area-label: Infrastruktur
        label-combinations: [{managed: true}]
  - project-name: baz
    namespace-labels: {a: d}
`))
	if err != nil {
		t.Fatal(err)
	}
	area := cfg.Projects[0].ServiceAreas[0]
	if area.Name != "business" || len(area.LabelCombinations) != 2 || area.LabelCombinations[0]["tier"] != "backend" {
		t.Fatalf("area = %+v", area)
	}
	if got := cfg.Projects[1].ServiceAreas[0].LabelCombinations[0]["managed"]; got != "true" {
		t.Errorf("bool-Label = %q", got)
	}
	for i, want := range []string{"Andere", "Sonstige", "Anwendungs-Services"} {
		if got := cfg.Projects[i].OthersLabel(); got != want {
			t.Errorf("OthersLabel[%d] = %q, want %q", i, got, want)
		}
	}
}

func TestServiceAreasValidation(t *testing.T) {
	cases := map[string]string{
		"reservierter Name": `[{area-name: others, area-label: X, label-combinations: [{a: b}]}]`,
		"doppelter Name":    `[{area-name: x, area-label: X, label-combinations: [{a: b}]}, {area-name: x, area-label: Y, label-combinations: [{a: b}]}]`,
		"Name fehlt":        `[{area-label: X, label-combinations: [{a: b}]}]`,
		"Label fehlt":       `[{area-name: x, label-combinations: [{a: b}]}]`,
		"keine Kombination": `[{area-name: x, area-label: X}]`,
		"leere Kombination": `[{area-name: x, area-label: X, label-combinations: [{}]}]`,
	}
	for name, areas := range cases {
		_, err := Parse([]byte("projects:\n  - project-name: foo\n    namespace-labels: {a: b}\n    service-areas: " + areas + "\n"))
		if err == nil {
			t.Errorf("%s: Fehler erwartet", name)
		}
	}
}

func TestLinks(t *testing.T) {
	cfg, err := Parse([]byte(`
link-variables:
  kibanaUrl: https://kibana
projects:
  - project-name: foo
    namespace-labels: {a: b}
    link-variables-overrides:
      dataView: logs-foo
links:
  - link-id: kibana
    title: Logs
    projects: [foo]
    targets:
      pod:
        url: "{{ .Vars.kibanaUrl }}/pod/{{ urlquery .Name }}"
      namespace:
        url: "{{ .Vars.kibanaUrl }}/ns/{{ .Namespace }}"
  - link-id: nowhere
    title: Nirgends
    projects: []
    targets:
      pod:
        url: "https://x"
`))
	if err != nil {
		t.Fatal(err)
	}
	l := cfg.Links[0]
	if l.Targets["pod"].URL != "{{ .Vars.kibanaUrl }}/pod/{{ urlquery .Name }}" || l.Targets["namespace"].URL == "" {
		t.Fatalf("link = %+v", l)
	}
	if cfg.LinkVariables["kibanaUrl"] != "https://kibana" || cfg.Projects[0].LinkVariablesOverrides["dataView"] != "logs-foo" {
		t.Fatalf("link-variables = %v / %v", cfg.LinkVariables, cfg.Projects[0].LinkVariablesOverrides)
	}
	if cfg.Links[1].Projects == nil || len(cfg.Links[1].Projects) != 0 {
		t.Fatalf("projects: [] muss als leere Liste ankommen: %#v", cfg.Links[1].Projects)
	}
}

func TestLinksValidation(t *testing.T) {
	const pod = `targets: {pod: {url: "https://x"}}`
	cases := map[string]string{
		"id fehlt":          `[{title: T, projects: [foo], ` + pod + `}]`,
		"doppelte id":       `[{link-id: x, title: T, projects: [foo], ` + pod + `}, {link-id: x, title: T, projects: [foo], ` + pod + `}]`,
		"title fehlt":       `[{link-id: x, projects: [foo], ` + pod + `}]`,
		"projects fehlt":    `[{link-id: x, title: T, ` + pod + `}]`,
		"unbekanntes Proj.": `[{link-id: x, title: T, projects: [nope], ` + pod + `}]`,
		"keine targets":     `[{link-id: x, title: T, projects: [foo]}]`,
		"ungültiger Kind":   `[{link-id: x, title: T, projects: [foo], targets: {deployment: {url: "https://x"}}}]`,
		"target ohne url":   `[{link-id: x, title: T, projects: [foo], targets: {service: {}}}]`,
	}
	for name, links := range cases {
		_, err := Parse([]byte("projects:\n  - project-name: foo\n    namespace-labels: {a: b}\nlinks: " + links + "\n"))
		if err == nil {
			t.Errorf("%s: Fehler erwartet", name)
		}
	}
}

func TestClusterDNSName(t *testing.T) {
	for _, valid := range []string{"", "k8s-nodes.mycomp.de", "Nodes.MyComp.de", "10.0.0.12"} {
		cfg, err := Parse([]byte("cluster:\n  dns-name: \"" + valid + "\"\n"))
		if err != nil {
			t.Errorf("%q: %v", valid, err)
		} else if cfg.Cluster.DNSName != valid {
			t.Errorf("DNSName = %q, want %q", cfg.Cluster.DNSName, valid)
		}
	}
	for _, invalid := range []string{"https://nodes.mycomp.de", "nodes.mycomp.de:30000", "nodes mycomp"} {
		if _, err := Parse([]byte("cluster:\n  dns-name: \"" + invalid + "\"\n")); err == nil {
			t.Errorf("%q: Fehler erwartet", invalid)
		}
	}
}
