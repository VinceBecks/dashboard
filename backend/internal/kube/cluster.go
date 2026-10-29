package kube

import (
	"context"
	"errors"
	"log/slog"
	"maps"
	"sync"
	"sync/atomic"
	"time"

	corev1 "k8s.io/api/core/v1"
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
)

// Cluster ist die einzige Datenquelle des Backends: der Informer-Cache für Namespaces, Services, Ingresses und
// Pods. Es gibt keine eigene Kopie daneben. Gelesen wird über die Lister; die Objekte daraus zeigen in den Cache und
// werden ausschließlich über den converter gelesen, nie verändert.
//
// Die Event-Handler merken sich nur, welcher Projekt-Namespace sich geändert hat (Revision je Namespace) und
// signalisieren das über Changes. Der Event-Hub baut die geänderten Namespaces daraufhin neu auf.
type Cluster struct {
	matcher    *project.Matcher
	factory    informers.SharedInformerFactory
	namespaces corelisters.NamespaceLister
	services   corelisters.ServiceLister
	pods       corelisters.PodLister
	ingresses  networkinglisters.IngressLister

	mu            sync.Mutex
	revisions     map[string]time.Time
	lastTimestamp time.Time
	changed       chan struct{}
	ready         atomic.Bool
}

// NewCluster richtet die vier Informer ein. Gestartet werden sie mit Run.
func NewCluster(client kubernetes.Interface, matcher *project.Matcher) *Cluster {
	factory := informers.NewSharedInformerFactoryWithOptions(client, 0, informers.WithTransform(trim))
	c := &Cluster{
		matcher:    matcher,
		factory:    factory,
		namespaces: factory.Core().V1().Namespaces().Lister(),
		services:   factory.Core().V1().Services().Lister(),
		pods:       factory.Core().V1().Pods().Lister(),
		ingresses:  factory.Networking().V1().Ingresses().Lister(),
		revisions:  map[string]time.Time{},
		changed:    make(chan struct{}, 1),
	}
	_, _ = factory.Core().V1().Namespaces().Informer().AddEventHandler(handler(c.namespaceChanged))
	for _, inf := range []cache.SharedIndexInformer{
		factory.Core().V1().Services().Informer(),
		factory.Core().V1().Pods().Informer(),
		factory.Networking().V1().Ingresses().Informer(),
	} {
		_, _ = inf.AddEventHandler(handler(c.objectChanged))
	}
	return c
}

// handler ruft fn bei Add, Update und Delete mit Namespace und Name des Objekts auf.
func handler(fn func(namespace, name string)) cache.ResourceEventHandlerFuncs {
	call := func(obj any) {
		key, err := cache.DeletionHandlingMetaNamespaceKeyFunc(obj)
		if err != nil {
			slog.Warn("Schlüssel nicht ermittelbar", "error", err)
			return
		}
		namespace, name, err := cache.SplitMetaNamespaceKey(key)
		if err != nil {
			slog.Warn("Schlüssel nicht zerlegbar", "key", key, "error", err)
			return
		}
		fn(namespace, name)
	}
	return cache.ResourceEventHandlerFuncs{AddFunc: call, UpdateFunc: func(_, obj any) { call(obj) }, DeleteFunc: call}
}

// Run startet die Informer, wartet auf den ersten vollständigen Stand und läuft bis ctx endet.
func (c *Cluster) Run(ctx context.Context) {
	c.factory.Start(ctx.Done())
	for typ, ok := range c.factory.WaitForCacheSync(ctx.Done()) {
		if !ok {
			slog.Error("Informer-Cache nicht synchronisiert", "type", typ)
			return
		}
	}
	// Events vor dem Sync können in beliebiger Reihenfolge kommen; danach steht jeder Projekt-Namespace sicher drin.
	namespaces, err := c.namespaces.List(labels.Everything())
	if err != nil {
		slog.Error("Namespaces nicht lesbar", "error", err)
		return
	}
	for _, ns := range namespaces {
		c.namespaceChanged("", ns.Name)
	}
	c.ready.Store(true)
	slog.Info("Informer-Cache synchronisiert")
	<-ctx.Done()
	c.factory.Shutdown()
}

// Ready meldet, ob der Informer-Cache synchronisiert ist.
func (c *Cluster) Ready() bool { return c.ready.Load() }

// namespaceChanged nimmt einen Namespace in die Revisionen auf, wenn er zu einem Projekt gehört, sonst entfernt es
// ihn. Auch ein Wechsel des Projekts ergibt eine neue Revision.
func (c *Cluster) namespaceChanged(_, name string) {
	ns, err := c.namespaces.Get(name)
	member := false
	if err == nil {
		_, member = c.matcher.Match(ns.Name, ns.Labels)
	} else if !apierrors.IsNotFound(err) {
		slog.Warn("Namespace nicht lesbar", "namespace", name, "error", err)
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if member {
		c.touch(name)
		return
	}

	// wenn es eine änderung an einem namespace gab, welcher zu keinem der projekte gehört.
	// dies ist relevant für folgenden Fall:
	//	wenn ein Namespace zu einem der Projekte gehörte. Durch eine Änderung an den Labels gehört es jetzt nicht mehr zu dem Projekt
	if _, ok := c.revisions[name]; ok {
		delete(c.revisions, name)
		c.notify()
	}
}

// objectChanged markiert den Namespace eines Services, Ingresses oder Pods als geändert, wenn er zu einem Projekt
// gehört. Objekte anderer Namespaces kosten nur diesen Lookup.
func (c *Cluster) objectChanged(namespace, _ string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if _, ok := c.revisions[namespace]; ok {
		c.touch(namespace)
	}
}

// touch setzt einen streng monoton steigenden Zeitstempel und signalisiert. Aufruf nur unter Lock.
func (c *Cluster) touch(namespace string) {
	ts := time.Now()
	if !ts.After(c.lastTimestamp) {
		ts = c.lastTimestamp.Add(time.Nanosecond)
	}
	c.lastTimestamp = ts
	c.revisions[namespace] = ts
	c.notify()
}

func (c *Cluster) notify() {
	select {
	case c.changed <- struct{}{}:
	default:
	}
}

// Changes signalisiert Änderungen (koaleszierend); welche Namespaces betroffen sind, zeigt NamespaceRevisions.
func (c *Cluster) Changes() <-chan struct{} { return c.changed }

// NamespaceRevisions liefert je Projekt-Namespace den Zeitpunkt der letzten Änderung darin.
func (c *Cluster) NamespaceRevisions() map[string]time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return maps.Clone(c.revisions)
}

// Namespace liefert alle Objekte eines Projekt-Namespaces, konvertiert und mit aufgelösten Referenzen, und den
// Zeitpunkt der letzten Änderung. ok ist false, wenn der Namespace fehlt oder zu keinem Projekt gehört.
func (c *Cluster) Namespace(name string) (model.NamespaceView, time.Time, bool, error) {
	ns, err := c.namespaces.Get(name)
	if apierrors.IsNotFound(err) {
		return model.NamespaceView{}, time.Time{}, false, nil
	} else if err != nil {
		return model.NamespaceView{}, time.Time{}, false, err
	}
	projectName, ok := c.matcher.Match(ns.Name, ns.Labels)
	if !ok {
		return model.NamespaceView{}, time.Time{}, false, nil
	}
	pods, err := c.pods.Pods(name).List(labels.Everything())
	if err != nil {
		return model.NamespaceView{}, time.Time{}, false, err
	}
	services, err := c.services.Services(name).List(labels.Everything())
	if err != nil {
		return model.NamespaceView{}, time.Time{}, false, err
	}
	ingresses, err := c.ingresses.Ingresses(name).List(labels.Everything())
	if err != nil {
		return model.NamespaceView{}, time.Time{}, false, err
	}
	c.mu.Lock()
	revision := c.revisions[name]
	c.mu.Unlock()
	return converter.NamespaceView(ns, projectName, pods, services, ingresses), revision, true, nil
}

// Get liest ein einzelnes Objekt eines Projekt-Namespaces in out (*model.StoredNamespace, *model.StoredService,
// *model.StoredIngress oder *model.StoredPod). Services tragen aufgelöste Replicas und targetPorts.
func (c *Cluster) Get(ref model.ObjectRef, out any) (bool, error) {
	view, _, ok, err := c.Namespace(ref.Namespace)
	if err != nil || !ok {
		return false, err
	}
	switch o := out.(type) {
	case *model.StoredNamespace:
		*o = view.Namespace
		return ref.Name == ref.Namespace, nil
	case *model.StoredService:
		return find(view.Services, func(s model.StoredService) string { return s.Name }, ref.Name, o), nil
	case *model.StoredIngress:
		return find(view.Ingresses, func(i model.StoredIngress) string { return i.Name }, ref.Name, o), nil
	case *model.StoredPod:
		return find(view.Pods, func(p model.StoredPod) string { return p.Name }, ref.Name, o), nil
	}
	return false, errors.New("ungültiger Zieltyp")
}

func find[T any](list []T, nameOf func(T) string, name string, out *T) bool {
	for _, item := range list {
		if nameOf(item) == name {
			*out = item
			return true
		}
	}
	return false
}

// Services liefert alle Services eines Projekt-Namespaces (leer, wenn er unbekannt ist).
func (c *Cluster) Services(namespace string) ([]model.StoredService, error) {
	view, _, ok, err := c.Namespace(namespace)
	if err != nil || !ok {
		return []model.StoredService{}, err
	}
	return view.Services, nil
}

// trim reduziert den Speicherbedarf des Informer-Caches auf die Felder, die der converter liest. Bei Pods ist das
// der größte Posten (Volumes, Env, Probes, Container-Status …). Andere Typen verlieren nur managedFields.
func trim(obj any) (any, error) {
	if m, err := meta.Accessor(obj); err == nil {
		m.SetManagedFields(nil)
	}
	pod, ok := obj.(*corev1.Pod)
	if !ok {
		return obj, nil
	}
	containers := make([]corev1.Container, 0, len(pod.Spec.Containers))
	for _, ct := range pod.Spec.Containers {
		containers = append(containers, corev1.Container{Name: ct.Name, Image: ct.Image, Ports: ct.Ports})
	}
	var conditions []corev1.PodCondition
	for _, cond := range pod.Status.Conditions {
		if cond.Type == corev1.PodReady {
			conditions = append(conditions, corev1.PodCondition{Type: cond.Type, Status: cond.Status})
		}
	}
	pod.Spec = corev1.PodSpec{Containers: containers}
	pod.Status = corev1.PodStatus{Phase: pod.Status.Phase, Conditions: conditions}
	return pod, nil
}
