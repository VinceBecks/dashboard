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
