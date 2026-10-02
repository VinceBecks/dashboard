package converter

import (
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"

	"feature-dashboard/internal/model"
	"feature-dashboard/internal/store"
)

// Die Apply-/Remove-Funktionen schreiben ein Objekt in den Store und lösen anschließend alle
// abhängigen Stored-Objects im selben Namespace neu auf:
//   Pod     → Services (Replicas, targetPort) → Ingresses
//   Service → Ingresses (Backend-Port)
// Objekte in Namespaces, die nicht im Store liegen, werden verworfen.

// ApplyPod speichert einen Pod.
func ApplyPod(tx *store.Tx, p *corev1.Pod) error {
	if !tx.HasNamespace(p.Namespace) {
		return nil
	}
	if _, err := tx.Put(model.KindPod, p.Namespace, p.Name, Pod(p)); err != nil {
		return err
	}
	return ResolveNamespace(tx, p.Namespace)
}

// ApplyService speichert einen Service inkl. aufgelöster Referenzen.
func ApplyService(tx *store.Tx, s *corev1.Service) error {
	if !tx.HasNamespace(s.Namespace) {
		return nil
	}
	var pods []model.StoredPod
	if err := tx.List(model.KindPod, s.Namespace, &pods); err != nil {
		return err
	}
	svc := Service(s)
	ResolveService(&svc, pods)
	if _, err := tx.Put(model.KindService, s.Namespace, s.Name, svc); err != nil {
		return err
	}
	return resolveIngresses(tx, s.Namespace)
}

// ApplyIngress speichert einen Ingress inkl. aufgelöster Referenzen.
func ApplyIngress(tx *store.Tx, i *networkingv1.Ingress) error {
	if !tx.HasNamespace(i.Namespace) {
		return nil
	}
	var services []model.StoredService
	if err := tx.List(model.KindService, i.Namespace, &services); err != nil {
		return err
	}
	ing := Ingress(i)
	ResolveIngress(&ing, services)
	_, err := tx.Put(model.KindIngress, i.Namespace, i.Name, ing)
	return err
}

// Remove entfernt ein Objekt und löst Abhängige neu auf.
func Remove(tx *store.Tx, kind model.Kind, namespace, name string) error {
	changed, err := tx.Delete(kind, namespace, name)
	if err != nil || !changed {
		return err
	}
	switch kind {
	case model.KindPod:
		return ResolveNamespace(tx, namespace)
	case model.KindService:
		return resolveIngresses(tx, namespace)
	}
	return nil
}

// ResolveNamespace löst Services und Ingresses eines Namespaces neu auf.
func ResolveNamespace(tx *store.Tx, namespace string) error {
	var pods []model.StoredPod
	if err := tx.List(model.KindPod, namespace, &pods); err != nil {
		return err
	}
	var services []model.StoredService
	if err := tx.List(model.KindService, namespace, &services); err != nil {
		return err
	}
	for i := range services {
		ResolveService(&services[i], pods)
		if _, err := tx.Put(model.KindService, namespace, services[i].Name, services[i]); err != nil {
			return err
		}
	}
	return resolveIngresses(tx, namespace)
}

func resolveIngresses(tx *store.Tx, namespace string) error {
	var services []model.StoredService
	if err := tx.List(model.KindService, namespace, &services); err != nil {
		return err
	}
	var ingresses []model.StoredIngress
	if err := tx.List(model.KindIngress, namespace, &ingresses); err != nil {
		return err
	}
	for i := range ingresses {
		ResolveIngress(&ingresses[i], services)
		if _, err := tx.Put(model.KindIngress, namespace, ingresses[i].Name, ingresses[i]); err != nil {
			return err
		}
	}
	return nil
}
