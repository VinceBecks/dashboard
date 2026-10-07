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
    action-title: Pod Löschen
    lucide-icon: trash
    lucide-icon-color: red
    projects: [foo-projekt]
    extra-handler-configs:
      force-delete: true
# Der frühere Abschnitt store wird ignoriert (YAML-Prüfung nicht strikt).
store:
  recreate-store-interval: 2d
`

func TestParse(t *testing.T) {
	cfg, err := parse([]byte(sample))
	if err != nil {
		t.Fatal(err)
	}
	if got := cfg.Projects[0].NamespaceLabels["mycomp.de/is-feature-environment"]; got != "true" {
		t.Errorf("bool-Label als String erwartet, got %q", got)
	}
	a := cfg.ActionHandlers[0]
	if a.Title != "Pod Löschen" || a.Extra["force-delete"] != true || len(a.Projects) != 1 || a.Projects[0] != "foo-projekt" {
		t.Errorf("unerwarteter ActionHandler: %+v", a)
	}
}

func TestValidate(t *testing.T) {
	_, err := parse([]byte(`
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
	cfg, err := parse([]byte(`
actionHandlers:
  - action-name: deploy
    handler: gitlab-pipeline
    projects: []
  - action-name: delete-pod
    projects: []
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
	cfg, err := parse([]byte(`
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
	for i, want := range []string{"Andere", "Sonstige", "Andere"} {
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
		_, err := parse([]byte("projects:\n  - project-name: foo\n    namespace-labels: {a: b}\n    service-areas: " + areas + "\n"))
		if err == nil {
			t.Errorf("%s: Fehler erwartet", name)
		}
	}
}

func TestLinks(t *testing.T) {
	cfg, err := parse([]byte(`
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
		_, err := parse([]byte("projects:\n  - project-name: foo\n    namespace-labels: {a: b}\nlinks: " + links + "\n"))
		if err == nil {
			t.Errorf("%s: Fehler erwartet", name)
		}
	}
}

func TestActionProjectsValidation(t *testing.T) {
	const projects = "projects:\n  - project-name: foo\n    namespace-labels: {a: b}\n"
	valid := map[string]string{
		"Projekt gesetzt":           `[{action-name: x, projects: [foo]}]`,
		"leere Liste (deaktiviert)": `[{action-name: x, projects: []}]`,
	}
	for name, actions := range valid {
		if _, err := parse([]byte(projects + "actionHandlers: " + actions + "\n")); err != nil {
			t.Errorf("%s: %v", name, err)
		}
	}
	invalid := map[string]string{
		"projects fehlt":      `[{action-name: x}]`,
		"unbekanntes Projekt": `[{action-name: x, projects: [nope]}]`,
	}
	for name, actions := range invalid {
		if _, err := parse([]byte(projects + "actionHandlers: " + actions + "\n")); err == nil {
			t.Errorf("%s: Fehler erwartet", name)
		}
	}
}

// clusterYAML ist der Pflicht-Abschnitt cluster, den alle Test-Configs brauchen.
const clusterYAML = "cluster:\n  dns-name: nodes.example\n"

// parse ergänzt den Pflicht-Abschnitt cluster, damit Tests sich auf ihr Thema beschränken.
func parse(data []byte) (*Config, error) {
	return Parse(append([]byte(clusterYAML), data...))
}

func TestClusterDNSName(t *testing.T) {
	for _, valid := range []string{"k8s-nodes.mycomp.de", "Nodes.MyComp.de", "10.0.0.12"} {
		cfg, err := Parse([]byte("cluster:\n  dns-name: \"" + valid + "\"\n"))
		if err != nil {
			t.Errorf("%q: %v", valid, err)
		} else if cfg.Cluster.DNSName != valid {
			t.Errorf("DNSName = %q, want %q", cfg.Cluster.DNSName, valid)
		}
	}
	for _, invalid := range []string{"", "https://nodes.mycomp.de", "nodes.mycomp.de:30000", "nodes mycomp"} {
		if _, err := Parse([]byte("cluster:\n  dns-name: \"" + invalid + "\"\n")); err == nil {
			t.Errorf("%q: Fehler erwartet", invalid)
		}
	}
	if _, err := Parse([]byte("projects: []\n")); err == nil {
		t.Error("ohne cluster: Fehler erwartet")
	}
}

func TestCopyCommands(t *testing.T) {
	cfg, err := parse([]byte(`
projects: [{project-name: foo, namespace-labels: {a: b}}]
copy-commands:
  - command-id: pod-shell
    title: Shell
    lucide-icon: terminal
    projects: [foo]
    targets:
      pod:
        command: kubectl exec -it -n {{ .Namespace }} {{ .Name }} -c {{ .Container }} -- sh
`))
	if err != nil {
		t.Fatal(err)
	}
	c := cfg.CopyCommands[0]
	if c.ID != "pod-shell" || c.Title != "Shell" || c.Targets["pod"].Command == "" || c.Projects[0] != "foo" {
		t.Fatalf("copy-command = %+v", c)
	}
}

func TestCopyCommandsValidation(t *testing.T) {
	const pod = `targets: {pod: {command: "kubectl get pod"}}`
	cases := map[string]string{
		"id fehlt":          `[{title: T, projects: [foo], ` + pod + `}]`,
		"doppelte id":       `[{command-id: x, title: T, projects: [foo], ` + pod + `}, {command-id: x, title: T, projects: [foo], ` + pod + `}]`,
		"title fehlt":       `[{command-id: x, projects: [foo], ` + pod + `}]`,
		"projects fehlt":    `[{command-id: x, title: T, ` + pod + `}]`,
		"unbekanntes Proj.": `[{command-id: x, title: T, projects: [nope], ` + pod + `}]`,
		"keine targets":     `[{command-id: x, title: T, projects: [foo]}]`,
		"ungültiger Kind":   `[{command-id: x, title: T, projects: [foo], targets: {deployment: {command: "x"}}}]`,
		"leerer command":    `[{command-id: x, title: T, projects: [foo], targets: {service: {command: "  "}}}]`,
	}
	for name, commands := range cases {
		_, err := parse([]byte("projects:\n  - project-name: foo\n    namespace-labels: {a: b}\ncopy-commands: " + commands + "\n"))
		if err == nil {
			t.Errorf("%s: Fehler erwartet", name)
		}
	}
}

func TestPipelines(t *testing.T) {
	cfg, err := parse([]byte("projects: []\n"))
	if err != nil {
		t.Fatal(err)
	}
	if p := cfg.Pipelines; p.ConfigMapName != DefaultPipelinesConfigMap || p.Poll() != 15*time.Second || p.ConfigMapNamespace != "" {
		t.Fatalf("Standardwerte = %+v", p)
	}
	cfg, err = parse([]byte("pipelines: {configmap-namespace: dash, configmap-name: pl, poll-interval: 1m}\n"))
	if err != nil {
		t.Fatal(err)
	}
	if p := cfg.Pipelines; p.ConfigMapNamespace != "dash" || p.ConfigMapName != "pl" || p.Poll() != time.Minute {
		t.Fatalf("Pipelines = %+v", p)
	}
	for _, invalid := range []string{"1s", "abc", "-5s"} {
		if _, err := parse([]byte("pipelines: {poll-interval: " + invalid + "}\n")); err == nil {
			t.Errorf("%q: Fehler erwartet", invalid)
		}
	}
}
