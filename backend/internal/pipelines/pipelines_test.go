package pipelines

import (
	"context"
	"errors"
	"slices"
	"testing"
	"time"

	"feature-dashboard/internal/model"
)

var (
	nsRef = model.ObjectRef{Kind: model.KindNamespace, Namespace: "feat-1", Name: "feat-1"}
	start = time.Date(2026, 10, 6, 14, 0, 0, 0, time.UTC)
)

func entry(ref model.ObjectRef, action string, id int) Entry {
	return Entry{Kind: ref.Kind, Namespace: ref.Namespace, Name: ref.Name, Action: action, ID: id,
		Project: "team/foo", WebURL: "https://gitlab.example/team/foo/-/pipelines/x", StartedAt: start}
}

func TestStateSetForRemove(t *testing.T) {
	s := NewState()
	pod := model.ObjectRef{Kind: model.KindPod, Namespace: "feat-1", Name: "web-1"}
	for _, e := range []Entry{entry(nsRef, "deploy", 1), entry(nsRef, "deploy", 2), entry(nsRef, "tests", 3), entry(pod, "deploy", 4)} {
		s.Set(e)
	}
	// deploy auf dem Namespace wurde durch Pipeline 2 ersetzt.
	if got := s.For(nsRef); len(got) != 2 || got[0].ID != 2 || got[1].Action != "tests" {
		t.Fatalf("For(ns) = %+v", got)
	}
	if len(s.All()) != 3 {
		t.Fatalf("All = %+v", s.All())
	}

	s.Remove(nsRef, "deploy")
	if got := s.For(nsRef); len(got) != 1 || got[0].Action != "tests" {
		t.Fatalf("nach Remove: %+v", got)
	}
	if got := s.For(model.ObjectRef{Kind: model.KindService, Namespace: "feat-1", Name: "web"}); got == nil || len(got) != 0 {
		t.Fatalf("For ohne Einträge = %#v", got)
	}
}

func TestStateRevisionsAndChanges(t *testing.T) {
	s := NewState()
	s.Set(entry(nsRef, "deploy", 1))
	select {
	case <-s.Changes():
	default:
		t.Fatal("Änderungs-Signal fehlt")
	}
	rev := s.NamespaceRevisions()["feat-1"]
	// Unverändert gesetzt → kein Signal, gleiche Revision.
	s.Set(entry(nsRef, "deploy", 1))
	select {
	case <-s.Changes():
		t.Fatal("unerwartetes Signal")
	default:
	}
	s.Remove(nsRef, "unbekannt")
	if s.NamespaceRevisions()["feat-1"] != rev {
		t.Fatal("Remove ohne Eintrag darf die Revision nicht ändern")
	}
	s.Remove(nsRef, "deploy")
	if !s.NamespaceRevisions()["feat-1"].After(rev) {
		t.Fatal("Remove muss die Revision erhöhen")
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
	s := NewState()
	s.RegisterChecker("deploy", fakeChecker{1: "running", 2: "success", 3: "error", 5: "canceled"})
	for id := 1; id <= 6; id++ {
		ref := model.ObjectRef{Kind: model.KindPod, Namespace: "feat-1", Name: "pod-" + string(rune('0'+id))}
		action := "deploy"
		if id == 6 {
			action = "nicht-mehr-konfiguriert"
		}
		s.Set(entry(ref, action, id))
	}
	NewTracker(s, time.Minute).CheckOnce(context.Background())

	var remaining []int
	for _, e := range s.All() {
		remaining = append(remaining, e.ID)
	}
	slices.Sort(remaining)
	// 1 läuft, 3 ist nicht abfragbar → bleiben; 2 fertig, 4 unbekannt, 5 abgebrochen, 6 ohne Checker → weg.
	if !slices.Equal(remaining, []int{1, 3}) {
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
