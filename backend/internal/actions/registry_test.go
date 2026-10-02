package actions

import (
	"context"
	"errors"
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
		{Name: "scale", Enabled: true, Title: "Skalieren", LucideIcon: "scale"},
		{Name: "unknown-disabled", Enabled: false},
	}, Deps{})
	if err != nil {
		t.Fatal(err)
	}
	if err := NewRegistry().Activate([]config.ActionHandlerConfig{{Name: "x", Enabled: true}}, Deps{}); err == nil {
		t.Fatal("unbekannter aktivierter Handler muss Fehler liefern")
	}

	svc := StoredObject{Ref: model.ObjectRef{Kind: model.KindService, Namespace: "ns", Name: "s"}}
	pod := StoredObject{Ref: model.ObjectRef{Kind: model.KindPod, Namespace: "ns", Name: "p"}}
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
