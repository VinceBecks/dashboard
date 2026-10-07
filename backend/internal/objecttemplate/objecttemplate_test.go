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

func TestTrimProject(t *testing.T) {
	ns := model.StoredNamespace{Meta: model.Meta{Name: "myproject-foo-bar"}, Project: "myproject"}
	ref := model.ObjectRef{Kind: model.KindNamespace, Namespace: "myproject-foo-bar", Name: "myproject-foo-bar"}
	d, err := NewData(ref, ns, "myproject")
	if err != nil {
		t.Fatal(err)
	}
	tpl, err := Parse("t", `{{ trimproject .Namespace }}|{{ .Namespace | trimproject }}|{{ trimproject "anderes-foo" }}|{{ trimproject "myproject" }}`)
	if err != nil {
		t.Fatal(err)
	}
	got, err := Render(tpl, d)
	if want := "foo-bar|foo-bar|anderes-foo|myproject"; err != nil || got != want {
		t.Fatalf("got %q (%v), want %q", got, err, want)
	}

	// Dasselbe Template mit einem anderen Projekt: Die Bindung gilt nur für den jeweiligen Aufruf.
	d.Project = "myproject-foo"
	if got, _ := Render(tpl, d); got != "bar|bar|anderes-foo|myproject" {
		t.Fatalf("anderes Projekt: got %q", got)
	}
	d.Project = ""
	if got, _ := Render(tpl, d); got != "myproject-foo-bar|myproject-foo-bar|anderes-foo|myproject" {
		t.Fatalf("ohne Projekt: got %q", got)
	}
}

func TestRenderIsSafeForConcurrentUse(t *testing.T) {
	tpl, err := Parse("t", `{{ trimproject .Namespace }}`)
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan string, 2)
	for _, project := range []string{"a", "a-b"} {
		go func() {
			d := Data{Namespace: "a-b-c", Project: project}
			var got string
			for range 200 {
				got, _ = Render(tpl, d)
			}
			done <- project + "=" + got
		}()
	}
	results := map[string]bool{<-done: true, <-done: true}
	if !results["a=b-c"] || !results["a-b=c"] {
		t.Fatalf("Ergebnisse = %v", results)
	}
}
