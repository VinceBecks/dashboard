package kube

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync/atomic"
	"time"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	corelisters "k8s.io/client-go/listers/core/v1"
	networkinglisters "k8s.io/client-go/listers/networking/v1"
	"k8s.io/client-go/tools/cache"

	"feature-dashboard/internal/converter"
	"feature-dashboard/internal/model"
	"feature-dashboard/internal/project"
	"feature-dashboard/internal/store"
)

const (
	syncTimeout   = 2 * time.Minute
	retryInterval = 10 * time.Second
)

// Watcher beobachtet Namespaces, Services, Ingresses und Pods und hält den Store aktuell.
//
// Alle Events und Full-Rebuilds werden von genau einer Goroutine (Run) verarbeitet. Events tragen nur den
// Objekt-Schlüssel; der aktuelle Zustand wird bei der Verarbeitung aus dem Informer-Cache gelesen.
type Watcher struct {
	client   kubernetes.Interface
	store    *store.Store
	matcher  *project.Matcher
	interval time.Duration

	events     chan event
	commands   chan func()
	generation int
	current    *informerSet
	ready      atomic.Bool
}

type event struct {
	generation int
	kind       model.Kind
	key        string
}

type informerSet struct {
	generation int
	factory    informers.SharedInformerFactory
	stop       chan struct{}
	namespaces corelisters.NamespaceLister
	services   corelisters.ServiceLister
	pods       corelisters.PodLister
	ingresses  networkinglisters.IngressLister
}

// NewWatcher erzeugt einen Watcher. recreateInterval 0 deaktiviert den periodischen Full-Rebuild.
func NewWatcher(client kubernetes.Interface, st *store.Store, matcher *project.Matcher, recreateInterval time.Duration) *Watcher {
	return &Watcher{
		client:   client,
		store:    st,
		matcher:  matcher,
		interval: recreateInterval,
		events:   make(chan event, 1024),
		commands: make(chan func()),
	}
}

// Ready meldet, ob der erste Full-Rebuild erfolgreich war.
func (w *Watcher) Ready() bool { return w.ready.Load() }

// Run verarbeitet Events bis ctx beendet wird.
func (w *Watcher) Run(ctx context.Context) {
	defer func() {
		if w.current != nil {
			w.stopSet(w.current)
		}
	}()

	for w.current == nil {
		if err := w.rebuild(ctx); err != nil {
			slog.Error("initialer Aufbau des Stores fehlgeschlagen", "error", err)
			select {
			case <-ctx.Done():
				return
			case <-time.After(retryInterval):
			}
		}
	}

	var recreate <-chan time.Time
	if w.interval > 0 {
		ticker := time.NewTicker(w.interval)
		defer ticker.Stop()
		recreate = ticker.C
	}

	for {
		select {
		case <-ctx.Done():
			return
		case <-recreate:
			if err := w.rebuild(ctx); err != nil {
				slog.Error("Neuaufbau des Stores fehlgeschlagen", "error", err)
			}
		case cmd := <-w.commands:
			cmd()
		case ev := <-w.events:
			if ev.generation != w.current.generation {
				continue
			}
			if err := w.store.Update(func(tx *store.Tx) error { return w.handle(tx, w.current, ev) }); err != nil {
				slog.Error("Event-Verarbeitung fehlgeschlagen", "kind", ev.kind, "key", ev.key, "error", err)
			}
		}
	}
}

// rebuild startet frische Informer, baut daraus einen neuen Store-Inhalt und tauscht ihn atomar aus.
func (w *Watcher) rebuild(ctx context.Context) error {
	start := time.Now()
	w.generation++
	set := w.newSet(w.generation)
	set.factory.Start(set.stop)

	syncCtx, cancel := context.WithTimeout(ctx, syncTimeout)
	defer cancel()
	for typ, ok := range set.factory.WaitForCacheSync(syncCtx.Done()) {
		if !ok {
			w.stopSet(set)
			return fmt.Errorf("informer-Cache für %v nicht synchronisiert", typ)
		}
	}

	fresh := store.New()
	err := fresh.Update(func(tx *store.Tx) error {
		nss, err := set.namespaces.List(labels.Everything())
		if err != nil {
			return err
		}
		for _, ns := range nss {
			if err := w.handleNamespace(tx, set, ns.Name); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		w.stopSet(set)
		return err
	}

	w.store.ReplaceAll(fresh)
	old := w.current
	w.current = set
	if old != nil {
		w.stopSet(old)
	}
	w.ready.Store(true)
	slog.Info("Store neu aufgebaut", "generation", set.generation, "duration", time.Since(start))
	return nil
}

func (w *Watcher) newSet(generation int) *informerSet {
	factory := informers.NewSharedInformerFactoryWithOptions(w.client, 0, informers.WithTransform(stripManagedFields))
	set := &informerSet{
		generation: generation,
		factory:    factory,
		stop:       make(chan struct{}),
		namespaces: factory.Core().V1().Namespaces().Lister(),
		services:   factory.Core().V1().Services().Lister(),
		pods:       factory.Core().V1().Pods().Lister(),
		ingresses:  factory.Networking().V1().Ingresses().Lister(),
	}
	w.register(set, factory.Core().V1().Namespaces().Informer(), model.KindNamespace)
	w.register(set, factory.Core().V1().Services().Informer(), model.KindService)
	w.register(set, factory.Core().V1().Pods().Informer(), model.KindPod)
	w.register(set, factory.Networking().V1().Ingresses().Informer(), model.KindIngress)
	return set
}

func (w *Watcher) register(set *informerSet, inf cache.SharedIndexInformer, kind model.Kind) {
	enqueue := func(obj any) {
		key, err := cache.DeletionHandlingMetaNamespaceKeyFunc(obj)
		if err != nil {
			slog.Warn("Schlüssel nicht ermittelbar", "kind", kind, "error", err)
			return
		}
		select {
		case w.events <- event{generation: set.generation, kind: kind, key: key}:
		case <-set.stop:
		}
	}
	_, _ = inf.AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc:    enqueue,
		UpdateFunc: func(_, obj any) { enqueue(obj) },
		DeleteFunc: enqueue,
	})
}

func (w *Watcher) stopSet(set *informerSet) {
	close(set.stop)
	set.factory.Shutdown()
}

func (w *Watcher) handle(tx *store.Tx, set *informerSet, ev event) error {
	if ev.kind == model.KindNamespace {
		return w.handleNamespace(tx, set, ev.key)
	}
	namespace, name, err := cache.SplitMetaNamespaceKey(ev.key)
	if err != nil {
		return err
	}
	if !tx.HasNamespace(namespace) {
		return nil
	}
	switch ev.kind {
	case model.KindPod:
		obj, err := set.pods.Pods(namespace).Get(name)
		if apierrors.IsNotFound(err) {
			return converter.Remove(tx, ev.kind, namespace, name)
		} else if err != nil {
			return err
		}
		return converter.ApplyPod(tx, obj)
	case model.KindService:
		obj, err := set.services.Services(namespace).Get(name)
		if apierrors.IsNotFound(err) {
			return converter.Remove(tx, ev.kind, namespace, name)
		} else if err != nil {
			return err
		}
		return converter.ApplyService(tx, obj)
	case model.KindIngress:
		obj, err := set.ingresses.Ingresses(namespace).Get(name)
		if apierrors.IsNotFound(err) {
			return converter.Remove(tx, ev.kind, namespace, name)
		} else if err != nil {
			return err
		}
		return converter.ApplyIngress(tx, obj)
	}
	return fmt.Errorf("unbekannter Objekt-Typ %q", ev.kind)
}

// handleNamespace gleicht einen Namespace mit dem Store ab. Wird er neu als Feature-Umgebung erkannt,
// werden alle seine Objekte aus dem Informer-Cache übernommen.
func (w *Watcher) handleNamespace(tx *store.Tx, set *informerSet, name string) error {
	ns, err := set.namespaces.Get(name)
	if apierrors.IsNotFound(err) {
		tx.DeleteNamespace(name)
		return nil
	} else if err != nil {
		return err
	}
	projectName, ok := w.matcher.Match(ns.Name, ns.Labels)
	if !ok {
		tx.DeleteNamespace(name)
		return nil
	}
	existed := tx.HasNamespace(name)
	if err := tx.PutNamespace(converter.Namespace(ns, projectName)); err != nil {
		return err
	}
	if existed {
		return nil
	}
	return loadNamespaceObjects(tx, set, name)
}

func loadNamespaceObjects(tx *store.Tx, set *informerSet, namespace string) error {
	pods, err := set.pods.Pods(namespace).List(labels.Everything())
	if err != nil {
		return err
	}
	services, err := set.services.Services(namespace).List(labels.Everything())
	if err != nil {
		return err
	}
	ingresses, err := set.ingresses.Ingresses(namespace).List(labels.Everything())
	if err != nil {
		return err
	}
	var errs []error
	// Reihenfolge Pods → Services → Ingresses, damit Referenzen direkt auflösbar sind.
	for _, p := range pods {
		errs = append(errs, converter.ApplyPod(tx, p))
	}
	for _, s := range services {
		errs = append(errs, converter.ApplyService(tx, s))
	}
	for _, i := range ingresses {
		errs = append(errs, converter.ApplyIngress(tx, i))
	}
	return errors.Join(errs...)
}

// stripManagedFields reduziert den Speicherbedarf des Informer-Caches.
func stripManagedFields(obj any) (any, error) {
	if m, err := meta.Accessor(obj); err == nil {
		m.SetManagedFields(nil)
	}
	return obj, nil
}
