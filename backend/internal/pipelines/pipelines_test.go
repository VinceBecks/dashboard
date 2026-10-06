package pipelines

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/fake"
	k8stesting "k8s.io/client-go/testing"

	"feature-dashboard/internal/model"
)

var (
	ctx   = context.Background()
	nsRef = model.ObjectRef{Kind: model.KindNamespace, Namespace: "feat-1", Name: "feat-1"}
	start = time.Date(2026, 10, 6, 14, 0, 0, 0, time.UTC)
)

func entry(ref model.ObjectRef, action string, id int) Entry {
	return Entry{Kind: ref.Kind, Namespace: ref.Namespace, Name: ref.Name, Action: action, ID: id,
		Project: "team/foo", WebURL: "https://gitlab.example/team/foo/-/pipelines/" + string(rune('0'+id%10)), StartedAt: start}
}

func configMapData(t *testing.T, client *fake.Clientset) string {
	t.Helper()
	cm, err := client.CoreV1().ConfigMaps("dash").Get(ctx, "pipelines", metav1.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}
	return cm.Data[DataKey]
}

func loaded(t *testing.T, client *fake.Clientset) *State {
	t.Helper()
	s := NewState(client, "dash", "pipelines")
	if err := s.Load(ctx); err != nil {
		t.Fatal(err)
	}
	return s
}

func TestStatePersistsAndSurvivesRestart(t *testing.T) {
	client := fake.NewClientset()
	s := loaded(t, client)
	if got := configMapData(t, client); got != "[]" {
		t.Fatalf("neue ConfigMap = %q", got)
	}

	pod := model.ObjectRef{Kind: model.KindPod, Namespace: "feat-1", Name: "web-1"}
	for _, e := range []Entry{entry(nsRef, "deploy", 1), entry(nsRef, "deploy", 2), entry(nsRef, "tests", 3), entry(pod, "deploy", 4)} {
		if err := s.Set(ctx, e); err != nil {
			t.Fatal(err)
		}
	}
	// deploy auf dem Namespace wurde durch Pipeline 2 ersetzt.
	if got := s.For(nsRef); len(got) != 2 || got[0].ID != 2 || got[1].Action != "tests" {
		t.Fatalf("For(ns) = %+v", got)
	}
	if !strings.Contains(configMapData(t, client), `"id":2`) || strings.Contains(configMapData(t, client), `"id":1,`) {
		t.Fatalf("ConfigMap = %s", configMapData(t, client))
	}

	// Neustart: ein neuer State liest denselben Stand.
	restarted := loaded(t, client)
	if !reflect.DeepEqual(restarted.For(nsRef), s.For(nsRef)) || len(restarted.All()) != 3 {
		t.Fatalf("nach Neustart: %+v", restarted.All())
	}

	if err := restarted.Remove(ctx, nsRef, "deploy"); err != nil {
		t.Fatal(err)
	}
	if got := restarted.For(nsRef); len(got) != 1 || got[0].Action != "tests" {
		t.Fatalf("nach Remove: %+v", got)
	}
	if got := restarted.For(model.ObjectRef{Kind: model.KindService, Namespace: "feat-1", Name: "web"}); got == nil || len(got) != 0 {
		t.Fatalf("For ohne Einträge = %#v", got)
	}
}

func TestStateRevisionsAndChanges(t *testing.T) {
	s := loaded(t, fake.NewClientset())
	<-s.Changes() // Signal von Load
	if err := s.Set(ctx, entry(nsRef, "deploy", 1)); err != nil {
		t.Fatal(err)
	}
	select {
	case <-s.Changes():
	default:
		t.Fatal("Änderungs-Signal fehlt")
	}
	rev := s.NamespaceRevisions()["feat-1"]
	// Unverändert gesetzt → kein Signal, gleiche Revision.
	_ = s.Set(ctx, entry(nsRef, "deploy", 1))
	select {
	case <-s.Changes():
		t.Fatal("unerwartetes Signal")
	default:
	}
	_ = s.Remove(ctx, nsRef, "deploy")
	if !s.NamespaceRevisions()["feat-1"].After(rev) {
		t.Fatal("Remove muss die Revision erhöhen")
	}
}

func TestStateKeepsMemoryOnWriteError(t *testing.T) {
	client := fake.NewClientset()
	s := loaded(t, client)
	client.PrependReactor("update", "configmaps", func(k8stesting.Action) (bool, runtime.Object, error) {
		return true, nil, errors.New("kaputt")
	})
	if err := s.Set(ctx, entry(nsRef, "deploy", 1)); err == nil {
		t.Fatal("Fehler erwartet")
	}
	if len(s.All()) != 0 {
		t.Fatal("bei Schreibfehler darf der Speicher nicht geändert werden")
	}
}

func TestStateRetriesOnConflict(t *testing.T) {
	client := fake.NewClientset()
	s := loaded(t, client)
	conflicts := 0
	client.PrependReactor("update", "configmaps", func(k8stesting.Action) (bool, runtime.Object, error) {
		if conflicts == 0 {
			conflicts++
			return true, nil, apierrors.NewConflict(corev1.Resource("configmaps"), "pipelines", errors.New("geändert"))
		}
		return false, nil, nil
	})
	if err := s.Set(ctx, entry(nsRef, "deploy", 1)); err != nil {
		t.Fatal(err)
	}
	if conflicts != 1 || !strings.Contains(configMapData(t, client), `"id":1`) {
		t.Fatalf("conflicts=%d, ConfigMap=%s", conflicts, configMapData(t, client))
	}
}

func TestStateLoadRejectsBrokenJSON(t *testing.T) {
	client := fake.NewClientset(&corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{Name: "pipelines", Namespace: "dash"},
		Data:       map[string]string{DataKey: "{kaputt"},
	})
	if err := NewState(client, "dash", "pipelines").Load(ctx); err == nil {
		t.Fatal("Fehler erwartet")
	}
}

type fakeChecker map[int]string

func (f fakeChecker) PipelineStatus(_ context.Context, id int) (string, error) {
	switch status, ok := f[id]; {
	case !ok:
		return "", ErrNotFound
	case status == "error":
		return "", errors.New("gitlab weg")
	default:
		return status, nil
	}
}

func TestTracker(t *testing.T) {
	s := loaded(t, fake.NewClientset())
	s.RegisterChecker("deploy", fakeChecker{1: "running", 2: "success", 3: "error", 5: "canceled"})
	refs := map[int]model.ObjectRef{}
	for id := 1; id <= 6; id++ {
		ref := model.ObjectRef{Kind: model.KindPod, Namespace: "feat-1", Name: "pod-" + string(rune('0'+id))}
		refs[id] = ref
		action := "deploy"
		if id == 6 {
			action = "nicht-mehr-konfiguriert"
		}
		if err := s.Set(ctx, entry(ref, action, id)); err != nil {
			t.Fatal(err)
		}
	}
	NewTracker(s, time.Minute).CheckOnce(ctx)

	var remaining []int
	for _, e := range s.All() {
		remaining = append(remaining, e.ID)
	}
	// 1 läuft, 3 ist nicht abfragbar → bleiben; 2 fertig, 4 unbekannt, 5 abgebrochen, 6 ohne Checker → weg.
	if len(remaining) != 2 || !(remaining[0]+remaining[1] == 4) {
		t.Fatalf("übrig = %v", remaining)
	}
}

func TestFinished(t *testing.T) {
	for status, want := range map[string]bool{"success": true, "failed": true, "canceled": true, "skipped": true,
		"running": false, "pending": false, "created": false, "manual": false} {
		if Finished(status) != want {
			t.Errorf("Finished(%q) = %v", status, !want)
		}
	}
}
