package objecttemplate

import (
	"testing"

	"feature-dashboard/internal/model"
)

func TestRender(t *testing.T) {
	svc := model.StoredService{
		Meta:     model.Meta{Name: "web", Namespace: "feat-1", Labels: map[string]string{"app.kubernetes.io/name": "web"}},
		Selector: map[string]string{"app.kubernetes.io/name": "web", "tier": "backend"},
	}
	d, err := NewData(model.ObjectRef{Kind: model.KindService, Namespace: "feat-1", Name: "web"}, svc, "foo")
	if err != nil {
		t.Fatal(err)
	}
	d.Vars["base"] = "https://kibana"
	tpl, err := Parse("t", `{{ .Vars.base }}/{{ .Project }}/{{ .Kind }}/{{ .Name }}?q={{ range $k, $v := .Selector }}{{ replace $k "." "_" }}={{ $v }};{{ end }}`)
	if err != nil {
		t.Fatal(err)
	}
	got, err := Render(tpl, d)
	if err != nil {
		t.Fatal(err)
	}
	if want := "https://kibana/foo/service/web?q=app_kubernetes_io/name=web;tier=backend;"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}

	missing, _ := Parse("m", "{{ .Vars.unknown }}")
	if _, err := Render(missing, d); err == nil {
		t.Error("fehlende Variable muss einen Fehler liefern")
	}
	joined, _ := Parse("j", `[{{ join .Services "," }}]`)
	if got, err := Render(joined, d); err != nil || got != "[]" {
		t.Errorf("leere Services: got %q, err %v", got, err)
	}
	d.Services = []string{"a", "b"}
	if got, err := Render(joined, d); err != nil || got != "[a,b]" {
		t.Errorf("Services: got %q, err %v", got, err)
	}
	if _, err := NewData(model.ObjectRef{}, "kein Objekt", ""); err == nil {
		t.Error("unbekannter Objekt-Typ muss einen Fehler liefern")
	}
}

func TestRison(t *testing.T) {
	cases := map[string]string{
		`kubernetes.pod.name:"web-1"`: `'kubernetes.pod.name:"web-1"'`,
		`it's !important`:             `'it!'s !!important'`,
		``:                            `''`,
	}
	for in, want := range cases {
		if got := Rison(in); got != want {
			t.Errorf("Rison(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestShellQuote(t *testing.T) {
	for in, want := range map[string]string{
		"":          `''`,
		"app":       `'app'`,
		"a b":       `'a b'`,
		"it's":      `'it'\''s'`,
		"$HOME; rm": `'$HOME; rm'`,
	} {
		if got := ShellQuote(in); got != want {
			t.Errorf("ShellQuote(%q) = %s, want %s", in, got, want)
		}
	}
}

func TestTrimPrefix(t *testing.T) {
	tpl, err := Parse("t", `{{ trimPrefix (printf "%s-" .Project) .Namespace }}|{{ .Namespace | trimPrefix "anderes-" }}`)
	if err != nil {
		t.Fatal(err)
	}
	got, err := Render(tpl, Data{Namespace: "myproject-foo-bar", Project: "myproject"})
	if want := "foo-bar|myproject-foo-bar"; err != nil || got != want {
		t.Fatalf("got %q (%v), want %q", got, err, want)
	}
}

func TestExtractRenewJiraTicketNumber(t *testing.T) {
	for in, want := range map[string]string{
		"renew-f-feat1234-login":  "1234",
		"renew-hf-hotfix42":       "42",
		"renew-if-intfix7-x":      "7",
		"renew-f-bugfix99":        "99",
		"renew-f-story":           "",
		"renew-x-feat1234":        "",
		"foo-renew-f-feat1234":    "",
		"myproject-feature-login": "",
	} {
		if got := ExtractRenewJiraTicketNumber(in); got != want {
			t.Errorf("ExtractRenewJiraTicketNumber(%q) = %q, want %q", in, got, want)
		}
	}
}
