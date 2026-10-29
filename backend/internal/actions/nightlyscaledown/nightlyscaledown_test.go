package nightlyscaledown

import (
	"context"
	"errors"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"

	"feature-dashboard/internal/actions"
	"feature-dashboard/internal/config"
	"feature-dashboard/internal/model"
)

const key = "mycomp.de/skip-nightly-scale-down"

func extra(mode string) map[string]any {
	return map[string]any{"label-key": key, "mode": mode}
}

func setup(t *testing.T, labels map[string]string) (*fake.Clientset, *actions.Registry, actions.StoredObject) {
	t.Helper()
	client := fake.NewClientset(&corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "feat-1", Labels: labels}})
	reg := actions.NewRegistry()
	reg.Register(HandlerType, New)
	err := reg.Activate([]config.ActionHandlerConfig{
		{Name: "exclude", Handler: HandlerType, Projects: []string{"foo"}, Extra: extra(ModeMark)},
		{Name: "include", Handler: HandlerType, Projects: []string{"foo"}, Extra: extra(ModeUnmark)},
	}, actions.Deps{Client: client})
	if err != nil {
		t.Fatal(err)
	}
	return client, reg, namespace(labels)
}

func namespace(labels map[string]string) actions.StoredObject {
	ns := model.StoredNamespace{Meta: model.Meta{Name: "feat-1", Labels: labels}, Project: "foo"}
	return actions.StoredObject{Ref: model.ObjectRef{Kind: model.KindNamespace, Namespace: "feat-1", Name: "feat-1"}, Object: ns, Project: "foo"}
}

func offered(reg *actions.Registry, obj actions.StoredObject) []string {
	var ids []string
	for _, a := range reg.ActionsFor(obj) {
		ids = append(ids, a.ID)
	}
	return ids
}

func labels(t *testing.T, client *fake.Clientset) map[string]string {
	t.Helper()
	ns, err := client.CoreV1().Namespaces().Get(context.Background(), "feat-1", metav1.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}
	return ns.Labels
}

func TestApplicability(t *testing.T) {
	_, reg, _ := setup(t, nil)
	cases := map[string]struct {
		labels map[string]string
		want   string
	}{
		"ohne Label":    {nil, "exclude"},
		"Label gesetzt": {map[string]string{key: "true"}, "include"},
		"anderer Wert":  {map[string]string{key: "false"}, "exclude"},
		"fremdes Label": {map[string]string{"a": "b"}, "exclude"},
	}
	for name, c := range cases {
		if got := offered(reg, namespace(c.labels)); len(got) != 1 || got[0] != c.want {
			t.Errorf("%s: angeboten = %v, want [%s]", name, got, c.want)
		}
	}
	pod := actions.StoredObject{Ref: model.ObjectRef{Kind: model.KindPod, Namespace: "feat-1", Name: "web-1"}, Object: model.StoredPod{}, Project: "foo"}
	if got := offered(reg, pod); len(got) != 0 {
		t.Fatalf("an Pods angeboten: %v", got)
	}
}

func TestMarkAndUnmark(t *testing.T) {
	client, reg, obj := setup(t, map[string]string{"mycomp.de/project-name": "foo"})

	res, err := reg.Execute(context.Background(), "exclude", obj, nil)
	if err != nil || !res.Success || res.Message != "Namespace feat-1 wird nachts nicht mehr herunterskaliert" {
		t.Fatalf("res = %+v, %v", res, err)
	}
	l := labels(t, client)
	if l[key] != "true" || l["mycomp.de/project-name"] != "foo" {
		t.Fatalf("Labels = %v", l)
	}

	// Gesetzt: nur noch aufheben möglich.
	if _, err := reg.Execute(context.Background(), "exclude", namespace(l), nil); !errors.Is(err, actions.ErrNotApplicable) {
		t.Fatalf("erneutes Markieren: err = %v", err)
	}
	res, err = reg.Execute(context.Background(), "include", namespace(l), nil)
	if err != nil || !res.Success || res.Message != "Namespace feat-1 wird nachts wieder herunterskaliert" {
		t.Fatalf("res = %+v, %v", res, err)
	}
	if l := labels(t, client); l[key] != "" || l["mycomp.de/project-name"] != "foo" {
		t.Fatalf("Labels nach Aufheben = %v", l)
	}
}

func TestUnmarkNotApplicableWithoutLabel(t *testing.T) {
	_, reg, obj := setup(t, nil)
	if _, err := reg.Execute(context.Background(), "include", obj, nil); !errors.Is(err, actions.ErrNotApplicable) {
		t.Fatalf("err = %v", err)
	}
}

func TestConfigure(t *testing.T) {
	h := New(actions.Deps{}).(*Handler)
	if err := h.Configure(extra(ModeMark)); err != nil || h.value != DefaultLabelValue {
		t.Fatalf("err = %v, value = %q", err, h.value)
	}
	invalid := map[string]map[string]any{
		"label-key fehlt":      {"mode": ModeMark},
		"label-key ungültig":   {"label-key": "a b", "mode": ModeMark},
		"label-value ungültig": {"label-key": key, "label-value": "nicht gültig!", "mode": ModeMark},
		"mode fehlt":           {"label-key": key},
		"mode unbekannt":       {"label-key": key, "mode": "toggle"},
	}
	for name, e := range invalid {
		if err := New(actions.Deps{}).Configure(e); err == nil {
			t.Errorf("%s: Fehler erwartet", name)
		}
	}
}
