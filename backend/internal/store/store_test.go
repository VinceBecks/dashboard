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
	return model.StoredPod{Meta: model.Meta{Name: name, Namespace: "ns1"}, Ready: true}
}

func TestPutAndTimestamps(t *testing.T) {
	s := New()
	must(t, s.Update(func(tx *Tx) error { return tx.PutNamespace(ns("ns1", "foo")) }))
	expectChange(t, s, true)
	n1, _ := s.NamespaceLastUpdated("ns1")

	// Gleicher Inhalt → keine Timestamp-Änderung, kein Signal.
	must(t, s.Update(func(tx *Tx) error { return tx.PutNamespace(ns("ns1", "foo")) }))
	if n, _ := s.NamespaceLastUpdated("ns1"); n != n1 {
		t.Fatal("unveränderter Namespace darf Timestamp nicht ändern")
	}
	expectChange(t, s, false)

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
	must(t, s.Update(func(tx *Tx) error { _, err := tx.Put(model.KindPod, "ns1", "p1", pod("p1")); return err }))
	if n, _ := s.NamespaceLastUpdated("ns1"); n != n2 {
		t.Fatal("unveränderter Pod darf Timestamp nicht ändern")
	}

	must(t, s.Update(func(tx *Tx) error { tx.DeleteNamespace("ns1"); return nil }))
	if _, ok := s.NamespaceLastUpdated("ns1"); ok {
		t.Fatal("Namespace muss entfernt sein")
	}
	if _, ok := s.NamespaceRevisions()["ns1"]; ok {
		t.Fatal("NamespaceRevisions darf ns1 nicht mehr enthalten")
	}
	expectChange(t, s, true)
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
	v.Pods[0].Ready = false
	var p model.StoredPod
	if ok, _ := s.Get(model.ObjectRef{Kind: model.KindPod, Namespace: "ns1", Name: "p1"}, &p); !ok || !p.Ready {
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
	expectChange(t, s, true)
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
	expectChange(t, s, false)
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
	expectChange(t, s, true)
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
					p.Labels = map[string]string{"v": string(rune('a' + (i+j)%26))}
					_, err := tx.Put(model.KindPod, "ns1", "p", p)
					return err
				})
			}
		}()
		go func() {
			defer wg.Done()
			for range 200 {
				_, _, _, _ = s.Namespace("ns1")
				_, _ = s.Namespaces()
			}
		}()
	}
	wg.Wait()
}

// expectChange prüft, ob Changes signalisiert wurde, und leert den Kanal.
func expectChange(t *testing.T, s *Store, want bool) {
	t.Helper()
	select {
	case <-s.Changes():
		if !want {
			t.Fatal("unerwartetes Änderungs-Signal")
		}
	default:
		if want {
			t.Fatal("Änderungs-Signal fehlt")
		}
	}
}

func must(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

func TestServices(t *testing.T) {
	s := New()
	must(t, s.Update(func(tx *Tx) error {
		if err := tx.PutNamespace(ns("ns1", "foo")); err != nil {
			return err
		}
		for _, name := range []string{"web", "api"} {
			if _, err := tx.Put(model.KindService, "ns1", name, model.StoredService{Meta: model.Meta{Name: name, Namespace: "ns1"}}); err != nil {
				return err
			}
		}
		return nil
	}))
	got, err := s.Services("ns1")
	if err != nil || len(got) != 2 || got[0].Name != "api" || got[1].Name != "web" {
		t.Fatalf("Services(ns1) = %+v, %v", got, err)
	}
	if got, err := s.Services("unknown"); err != nil || got == nil || len(got) != 0 {
		t.Fatalf("Services(unknown) = %#v, %v", got, err)
	}
}
