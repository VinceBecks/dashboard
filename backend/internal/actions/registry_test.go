package actions

import (
	"context"
	"errors"
	"strings"
	"testing"

	"feature-dashboard/internal/config"
	"feature-dashboard/internal/model"
)

type stubHandler struct{ executed map[string]any }

func (s *stubHandler) Configure(map[string]any) error { return nil }
func (s *stubHandler) IsApplicable(o StoredObject) bool {
	return o.Ref.Kind == model.KindService
}
func (s *stubHandler) FormFields(StoredObject) []FormField {
	return []FormField{
		{ID: "replicas", Type: FieldNumber, IsRequired: true},
		{ID: "mode", Type: FieldSelect, Options: []Option{{Value: "a"}, {Value: "b"}}},
	}
}
func (s *stubHandler) Execute(_ context.Context, _ StoredObject, in map[string]any) Result {
	s.executed = in
	return Result{Success: true}
}

func TestRegistry(t *testing.T) {
	stub := &stubHandler{}
	r := NewRegistry()
	r.Register("scale", func(Deps) Handler { return stub })
	err := r.Activate([]config.ActionHandlerConfig{
		{Name: "scale", Title: "Skalieren", Icon: "scale", Projects: []string{"foo"}},
		{Name: "scale-disabled", Handler: "scale", Projects: []string{}},
	}, Deps{})
	if err != nil {
		t.Fatal(err)
	}
	if err := NewRegistry().Activate([]config.ActionHandlerConfig{{Name: "x", Projects: []string{"foo"}}}, Deps{}); err == nil {
		t.Fatal("unbekannter aktivierter Handler muss Fehler liefern")
	}

	svc := StoredObject{Ref: model.ObjectRef{Kind: model.KindService, Namespace: "ns", Name: "s"}, Project: "foo"}
	pod := StoredObject{Ref: model.ObjectRef{Kind: model.KindPod, Namespace: "ns", Name: "p"}, Project: "foo"}
	if got := r.ActionsFor(svc); len(got) != 1 || got[0].ID != "scale" || got[0].Title != "Skalieren" || len(got[0].FormFields) != 2 {
		t.Fatalf("ActionsFor = %+v", got)
	}
	if got := r.ActionsFor(pod); got == nil || len(got) != 0 {
		t.Fatalf("ActionsFor(pod) = %#v", got)
	}

	ctx := context.Background()
	if _, err := r.Execute(ctx, "nope", svc, nil); !errors.Is(err, ErrUnknownAction) {
		t.Fatalf("err = %v", err)
	}
	if _, err := r.Execute(ctx, "scale", pod, nil); !errors.Is(err, ErrNotApplicable) {
		t.Fatalf("err = %v", err)
	}
	var verr *ValidationError
	if _, err := r.Execute(ctx, "scale", svc, map[string]any{"mode": "c"}); !errors.As(err, &verr) || len(verr.Problems) != 2 {
		t.Fatalf("err = %v", err)
	}
	if res, err := r.Execute(ctx, "scale", svc, map[string]any{"replicas": 3.0, "mode": "a"}); err != nil || !res.Success {
		t.Fatalf("res = %+v, err = %v", res, err)
	}
}

type configuredHandler struct{ target string }

func (c *configuredHandler) Configure(extra map[string]any) error {
	c.target, _ = extra["target"].(string)
	return nil
}
func (c *configuredHandler) IsApplicable(StoredObject) bool      { return true }
func (c *configuredHandler) FormFields(StoredObject) []FormField { return nil }
func (c *configuredHandler) Execute(context.Context, StoredObject, map[string]any) Result {
	return Result{Success: true, Message: c.target}
}

func TestHandlerTypeServesMultipleActions(t *testing.T) {
	r := NewRegistry()
	r.Register("generic", func(Deps) Handler { return &configuredHandler{} })
	r.Register("legacy", func(Deps) Handler { return &configuredHandler{} })
	err := r.Activate([]config.ActionHandlerConfig{
		{Name: "a", Handler: "generic", Projects: []string{"foo"}, Extra: map[string]any{"target": "A"}},
		{Name: "b", Handler: "generic", Projects: []string{"foo"}, Extra: map[string]any{"target": "B"}},
		{Name: "legacy", Projects: []string{"foo"}, Extra: map[string]any{"target": "L"}},
	}, Deps{})
	if err != nil {
		t.Fatal(err)
	}
	obj := StoredObject{Ref: model.ObjectRef{Kind: model.KindNamespace, Namespace: "ns", Name: "ns"}, Project: "foo"}
	if got := r.ActionsFor(obj); len(got) != 3 || got[0].ID != "a" || got[1].ID != "b" || got[2].ID != "legacy" {
		t.Fatalf("ActionsFor = %+v", got)
	}
	for name, want := range map[string]string{"a": "A", "b": "B", "legacy": "L"} {
		if res, err := r.Execute(context.Background(), name, obj, nil); err != nil || res.Message != want {
			t.Errorf("%s: res = %+v, err = %v", name, res, err)
		}
	}
	if err := NewRegistry().Activate([]config.ActionHandlerConfig{{Name: "x", Handler: "missing", Projects: []string{"foo"}}}, Deps{}); err == nil ||
		!strings.Contains(err.Error(), `"missing"`) {
		t.Fatalf("Fehler mit Handler-Typ erwartet, got %v", err)
	}
}

func TestActionProjects(t *testing.T) {
	r := NewRegistry()
	r.Register("generic", func(Deps) Handler { return &configuredHandler{} })
	err := r.Activate([]config.ActionHandlerConfig{
		{Name: "foo-only", Handler: "generic", Projects: []string{"foo"}},
		{Name: "both", Handler: "generic", Projects: []string{"foo", "bar"}},
		{Name: "nowhere", Handler: "generic", Projects: []string{}},
	}, Deps{})
	if err != nil {
		t.Fatal(err)
	}
	ids := func(project string) []string {
		var result []string
		for _, a := range r.ActionsFor(StoredObject{Ref: model.ObjectRef{Kind: model.KindPod}, Project: project}) {
			result = append(result, a.ID)
		}
		return result
	}
	if got := ids("foo"); len(got) != 2 || got[0] != "foo-only" || got[1] != "both" {
		t.Errorf("foo: %v", got)
	}
	if got := ids("bar"); len(got) != 1 || got[0] != "both" {
		t.Errorf("bar: %v", got)
	}
	if got := ids("other"); len(got) != 0 {
		t.Errorf("other: %v", got)
	}

	ctx := context.Background()
	barPod := StoredObject{Ref: model.ObjectRef{Kind: model.KindPod}, Project: "bar"}
	if _, err := r.Execute(ctx, "foo-only", barPod, nil); !errors.Is(err, ErrNotApplicable) {
		t.Errorf("Ausführung in fremdem Projekt: err = %v", err)
	}
	if _, err := r.Execute(ctx, "both", barPod, nil); err != nil {
		t.Errorf("Ausführung im erlaubten Projekt: err = %v", err)
	}
}

type failingConfigure struct{ configuredHandler }

func (failingConfigure) Configure(map[string]any) error {
	return errors.New("darf nicht aufgerufen werden")
}

func TestDisabledActions(t *testing.T) {
	r := NewRegistry()
	r.Register("failing", func(Deps) Handler { return &failingConfigure{} })
	// projects: [] deaktiviert die Aktion: Der Handler wird nicht erzeugt, Configure nicht aufgerufen.
	if err := r.Activate([]config.ActionHandlerConfig{{Name: "off", Handler: "failing", Projects: []string{}}}, Deps{}); err != nil {
		t.Fatalf("deaktivierte Aktion darf nicht konfiguriert werden: %v", err)
	}
	if r.Has("off") {
		t.Fatal("deaktivierte Aktion darf nicht aktiv sein")
	}
	// Ein unbekannter Handler-Typ fällt auch bei deaktivierten Aktionen auf.
	if err := NewRegistry().Activate([]config.ActionHandlerConfig{{Name: "typo", Handler: "gibts-nicht", Projects: []string{}}}, Deps{}); err == nil {
		t.Fatal("unbekannter Handler-Typ muss auch bei deaktivierter Aktion auffallen")
	}
}
