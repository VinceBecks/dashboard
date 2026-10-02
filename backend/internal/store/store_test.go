package store

import (
	"sync"
	"testing"

	"feature-dashboard/internal/model"
)

func ns(name, project string) model.StoredNamespace {
	return model.StoredNamespace{Meta: model.Meta{Name: name, Labels: map[string]string{"a": "b"}}, Project: project}
}

func pod(name string) model.StoredPod {
	return model.StoredPod{Meta: model.Meta{Name: name, Namespace: "ns1"}, Phase: "Running"}
}

func TestPutAndTimestamps(t *testing.T) {
	s := New()
	p0 := s.ProjectsLastUpdated()
	must(t, s.Update(func(tx *Tx) error { return tx.PutNamespace(ns("ns1", "foo")) }))
	p1 := s.ProjectsLastUpdated()
	if !p1.After(p0) {
		t.Fatal("projects-Timestamp muss bei neuem Namespace steigen")
	}
	n1, _ := s.NamespaceLastUpdated("ns1")

	// Gleicher Inhalt → keine Timestamp-Änderung.
	must(t, s.Update(func(tx *Tx) error { return tx.PutNamespace(ns("ns1", "foo")) }))
	if s.ProjectsLastUpdated() != p1 {
		t.Fatal("unveränderter Namespace darf Timestamp nicht ändern")
	}

	// Pod in unbekanntem Namespace wird verworfen.
	must(t, s.Update(func(tx *Tx) error {
		changed, err := tx.Put(model.KindPod, "other", "p", pod("p"))
		if changed {
			t.Error("Pod in unbekanntem Namespace darf nicht gespeichert werden")
		}
		return err
	}))

	must(t, s.Update(func(tx *Tx) error { _, err := tx.Put(model.KindPod, "ns1", "p1", pod("p1")); return err }))
	n2, _ := s.NamespaceLastUpdated("ns1")
	if !n2.After(n1) {
		t.Fatal("Namespace-Timestamp muss bei neuem Pod steigen")
	}
	if s.ProjectsLastUpdated() != p1 {
		t.Fatal("Pod-Änderung darf projects-Timestamp nicht ändern")
	}
	must(t, s.Update(func(tx *Tx) error { _, err := tx.Put(model.KindPod, "ns1", "p1", pod("p1")); return err }))
	if n, _ := s.NamespaceLastUpdated("ns1"); n != n2 {
		t.Fatal("unveränderter Pod darf Timestamp nicht ändern")
	}

	must(t, s.Update(func(tx *Tx) error { tx.DeleteNamespace("ns1"); return nil }))
	if _, ok := s.NamespaceLastUpdated("ns1"); ok {
		t.Fatal("Namespace muss entfernt sein")
	}
	if !s.ProjectsLastUpdated().After(p1) {
		t.Fatal("projects-Timestamp muss beim Entfernen steigen")
	}
}

func TestReadsReturnCopies(t *testing.T) {
	s := New()
	must(t, s.Update(func(tx *Tx) error {
		if err := tx.PutNamespace(ns("ns1", "foo")); err != nil {
			return err
		}
		_, err := tx.Put(model.KindPod, "ns1", "p1", pod("p1"))
		return err
	}))
	v, _, ok, err := s.Namespace("ns1")
	if err != nil || !ok || len(v.Pods) != 1 {
		t.Fatalf("unerwartet: %+v %v %v", v, ok, err)
	}
	v.Namespace.Labels["a"] = "changed"
	v.Pods[0].Phase = "changed"
	var p model.StoredPod
	if ok, _ := s.Get(model.ObjectRef{Kind: model.KindPod, Namespace: "ns1", Name: "p1"}, &p); !ok || p.Phase != "Running" {
		t.Fatal("Store-Inhalt darf durch Leser nicht veränderbar sein")
	}
	var n model.StoredNamespace
	if ok, _ := s.Get(model.ObjectRef{Kind: model.KindNamespace, Namespace: "ns1", Name: "ns1"}, &n); !ok || n.Labels["a"] != "b" {
		t.Fatal("Store-Inhalt darf durch Leser nicht veränderbar sein")
	}
}

func TestReplaceAll(t *testing.T) {
	s := New()
	must(t, s.Update(func(tx *Tx) error {
		_ = tx.PutNamespace(ns("ns1", "foo"))
		_ = tx.PutNamespace(ns("ns2", "foo"))
		_, err := tx.Put(model.KindPod, "ns1", "p1", pod("p1"))
		return err
	}))
	p0 := s.ProjectsLastUpdated()
	n1, _ := s.NamespaceLastUpdated("ns1")
	n2, _ := s.NamespaceLastUpdated("ns2")

	// Identischer Inhalt → keine Timestamp-Änderung.
	fresh := New()
	must(t, fresh.Update(func(tx *Tx) error {
		_ = tx.PutNamespace(ns("ns1", "foo"))
		_ = tx.PutNamespace(ns("ns2", "foo"))
		_, err := tx.Put(model.KindPod, "ns1", "p1", pod("p1"))
		return err
	}))
	s.ReplaceAll(fresh)
	if s.ProjectsLastUpdated() != p0 {
		t.Fatal("identischer Rebuild darf projects-Timestamp nicht ändern")
	}
	if n, _ := s.NamespaceLastUpdated("ns1"); n != n1 {
		t.Fatal("identischer Rebuild darf Namespace-Timestamp nicht ändern")
	}

	// ns1 ohne Pod, ns2 entfernt.
	fresh = New()
	must(t, fresh.Update(func(tx *Tx) error { return tx.PutNamespace(ns("ns1", "foo")) }))
	s.ReplaceAll(fresh)
	if n, _ := s.NamespaceLastUpdated("ns1"); !n.After(n1) || !n.After(n2) {
		t.Fatal("geänderter Namespace braucht neuen Timestamp")
	}
	if _, ok := s.NamespaceLastUpdated("ns2"); ok {
		t.Fatal("ns2 muss entfernt sein")
	}
	if !s.ProjectsLastUpdated().After(p0) {
		t.Fatal("entfernter Namespace muss projects-Timestamp ändern")
	}
}

func TestConcurrentAccess(t *testing.T) {
	s := New()
	must(t, s.Update(func(tx *Tx) error { return tx.PutNamespace(ns("ns1", "foo")) }))
	var wg sync.WaitGroup
	for i := range 4 {
		wg.Add(2)
		go func() {
			defer wg.Done()
			for j := range 200 {
				_ = s.Update(func(tx *Tx) error {
					p := pod("p")
					p.Phase = string(rune('a' + (i+j)%26))
					_, err := tx.Put(model.KindPod, "ns1", "p", p)
					return err
				})
			}
		}()
		go func() {
			defer wg.Done()
			for range 200 {
				_, _, _, _ = s.Namespace("ns1")
				_, _, _ = s.Namespaces()
			}
		}()
	}
	wg.Wait()
}

func must(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}
