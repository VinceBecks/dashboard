package assignteam

import (
	"context"
	"strings"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"

	"feature-dashboard/internal/actions"
	"feature-dashboard/internal/config"
	"feature-dashboard/internal/model"
)

var teams = config.TeamsConfig{LabelKey: "mycomp.de/team", Teams: []config.Team{
	{ID: "checkout", Label: "Team Checkout"},
	{ID: "search", Label: "Team Suche"},
}}

func setup(t *testing.T, labels map[string]string) (*fake.Clientset, *actions.Registry, actions.StoredObject) {
	t.Helper()
	client := fake.NewClientset(&corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "feat-1", Labels: labels}})
	reg := actions.NewRegistry()
	reg.Register(HandlerType, New)
	err := reg.Activate([]config.ActionHandlerConfig{{Name: "assign-team", Title: "Team zuordnen", Projects: []string{"foo"}}},
		actions.Deps{Client: client, Teams: teams})
	if err != nil {
		t.Fatal(err)
	}
	ns := model.StoredNamespace{Meta: model.Meta{Name: "feat-1", Labels: labels}, Project: "foo"}
	return client, reg, actions.StoredObject{Ref: model.ObjectRef{Kind: model.KindNamespace, Namespace: "feat-1", Name: "feat-1"}, Object: ns, Project: "foo"}
}

func labels(t *testing.T, client *fake.Clientset) map[string]string {
	t.Helper()
	ns, err := client.CoreV1().Namespaces().Get(context.Background(), "feat-1", metav1.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}
	return ns.Labels
}

func TestFormFieldsOfferTeamsAndNoTeam(t *testing.T) {
	_, reg, obj := setup(t, map[string]string{"mycomp.de/team": "search"})
	got := reg.ActionsFor(obj)
	if len(got) != 1 {
		t.Fatalf("Aktionen = %+v", got)
	}
	f := got[0].FormFields[0]
	if f.ID != FieldTeam || f.Type != actions.FieldSelect || !f.IsRequired || f.Description != "Aktuell: Team Suche" {
		t.Fatalf("Feld = %+v", f)
	}
	var values []string
	for _, o := range f.Options {
		values = append(values, o.Value+"="+o.Label)
	}
	if strings.Join(values, ",") != "checkout=Team Checkout,search=Team Suche,-=Kein Team" {
		t.Fatalf("Optionen = %v", values)
	}
	// Nur an Namespaces.
	if len(reg.ActionsFor(actions.StoredObject{Ref: model.ObjectRef{Kind: model.KindService}, Object: model.StoredService{}, Project: "foo"})) != 0 {
		t.Fatal("darf an Services nicht angeboten werden")
	}
}

func TestAssignAndRemoveTeam(t *testing.T) {
	client, reg, obj := setup(t, map[string]string{"mycomp.de/project-name": "foo"})

	res, err := reg.Execute(context.Background(), "assign-team", obj, map[string]any{FieldTeam: "checkout"})
	if err != nil || !res.Success || res.Message != "Namespace feat-1 gehört jetzt zu Team Checkout" {
		t.Fatalf("res = %+v, %v", res, err)
	}
	if l := labels(t, client); l["mycomp.de/team"] != "checkout" || l["mycomp.de/project-name"] != "foo" {
		t.Fatalf("Labels = %v", l)
	}

	res, err = reg.Execute(context.Background(), "assign-team", obj, map[string]any{FieldTeam: NoTeam})
	if err != nil || !res.Success {
		t.Fatalf("res = %+v, %v", res, err)
	}
	if l := labels(t, client); l["mycomp.de/team"] != "" || l["mycomp.de/project-name"] != "foo" {
		t.Fatalf("Labels nach Entfernen = %v", l)
	}

	// Unbekanntes Team lehnt die Registry ab.
	if _, err := reg.Execute(context.Background(), "assign-team", obj, map[string]any{FieldTeam: "gibts-nicht"}); err == nil {
		t.Fatal("unbekanntes Team muss abgelehnt werden")
	}
}

func TestConfigureNeedsTeams(t *testing.T) {
	if err := New(actions.Deps{}).Configure(nil); err == nil {
		t.Fatal("ohne Abschnitt teams muss Configure fehlschlagen")
	}
}
