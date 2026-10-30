package its.me.feature_dashboard.kube;

import java.util.HashMap;
import java.util.List;
import java.util.Map;
import java.util.Optional;
import java.util.concurrent.CompletableFuture;
import java.util.concurrent.CopyOnWriteArrayList;
import java.util.concurrent.atomic.AtomicBoolean;

import org.slf4j.Logger;
import org.slf4j.LoggerFactory;
import org.springframework.context.SmartLifecycle;

import io.fabric8.kubernetes.api.model.HasMetadata;
import io.fabric8.kubernetes.api.model.Namespace;
import io.fabric8.kubernetes.api.model.Pod;
import io.fabric8.kubernetes.api.model.Service;
import io.fabric8.kubernetes.api.model.networking.v1.Ingress;
import io.fabric8.kubernetes.client.KubernetesClient;
import io.fabric8.kubernetes.client.informers.ResourceEventHandler;
import io.fabric8.kubernetes.client.informers.SharedIndexInformer;
import io.fabric8.kubernetes.client.informers.cache.Cache;
import its.me.feature_dashboard.converter.Converter;
import its.me.feature_dashboard.model.ClusterSource;
import its.me.feature_dashboard.project.ProjectMatcher;

/**
 * Informer-Cache als einzige Datenquelle: Informer für Namespace, Service, Pod und Ingress. Es gibt keine eigene
 * Kopie daneben; gelesen wird aus den Stores der Informer, deren Objekte nie verändert und nur über den Converter
 * gelesen werden.
 *
 * <p>Die Event-Handler merken sich nur, welcher Projekt-Namespace sich geändert hat (Revision je Namespace), und
 * melden das an die Listener. Der Event-Hub baut die geänderten Namespaces daraufhin neu auf.
 */
public class Cluster implements ClusterSource, SmartLifecycle {

	private static final Logger log = LoggerFactory.getLogger(Cluster.class);

	private final KubernetesClient client;
	private final ProjectMatcher matcher;

	private final Map<String, Long> revisions = new HashMap<>();
	private long lastRevision;
	private final List<Runnable> listeners = new CopyOnWriteArrayList<>();
	private final AtomicBoolean ready = new AtomicBoolean();

	private SharedIndexInformer<Namespace> namespaces;
	private SharedIndexInformer<Service> services;
	private SharedIndexInformer<Pod> pods;
	private SharedIndexInformer<Ingress> ingresses;
	private volatile boolean running;

	public Cluster(KubernetesClient client, ProjectMatcher matcher) {
		this.client = client;
		this.matcher = matcher;
	}

	/** Startet die Informer; sobald der erste vollständige Stand da ist, ist der Cluster bereit. */
	@Override
	public void start() {
		namespaces = informer(client.namespaces().runnableInformer(0), this::namespaceChanged);
		services = informer(client.services().inAnyNamespace().runnableInformer(0), this::objectChanged);
		pods = informer(client.pods().inAnyNamespace().runnableInformer(0), this::objectChanged);
		ingresses = informer(client.network().v1().ingresses().inAnyNamespace().runnableInformer(0), this::objectChanged);
		running = true;
		CompletableFuture<?>[] started = List.of(namespaces, services, pods, ingresses)
			.stream()
			.map(i -> i.start().toCompletableFuture())
			.toArray(CompletableFuture[]::new);
		CompletableFuture.allOf(started).whenComplete((ok, error) -> {
			if (error != null) {
				log.error("Informer-Cache nicht synchronisiert", error);
				return;
			}
			// Events vor dem Sync können in beliebiger Reihenfolge kommen; danach steht jeder Projekt-Namespace sicher drin.
			namespaces.getStore().list().forEach(ns -> namespaceChanged(ns.getMetadata().getName()));
			ready.set(true);
			log.info("Informer-Cache synchronisiert");
		});
	}

	private <T extends HasMetadata> SharedIndexInformer<T> informer(SharedIndexInformer<T> informer,
			java.util.function.BiConsumer<String, String> onChange) {
		informer.itemStore(new TrimmingItemStore<>());
		informer.addEventHandler(new ResourceEventHandler<T>() {
			@Override
			public void onAdd(T obj) {
				call(obj);
			}

			@Override
			public void onUpdate(T oldObj, T newObj) {
				call(newObj);
			}

			@Override
			public void onDelete(T obj, boolean deletedFinalStateUnknown) {
				call(obj);
			}

			private void call(T obj) {
				onChange.accept(obj.getMetadata().getNamespace(), obj.getMetadata().getName());
			}
		});
		return informer;
	}

	@Override
	public void stop() {
		running = false;
		ready.set(false);
		for (SharedIndexInformer<?> i : new SharedIndexInformer<?>[] { namespaces, services, pods, ingresses }) {
			if (i != null) {
				i.stop();
			}
		}
	}

	@Override
	public boolean isRunning() {
		return running;
	}

	/** Meldet, ob der Informer-Cache synchronisiert ist. */
	public boolean isReady() {
		return ready.get();
	}

	/**
	 * Nimmt einen Namespace in die Revisionen auf, wenn er zu einem Projekt gehört, sonst entfernt sie ihn. Auch ein
	 * Wechsel des Projekts ergibt eine neue Revision.
	 */
	void namespaceChanged(String ignoredNamespace, String name) {
		namespaceChanged(name);
	}

	private void namespaceChanged(String name) {
		Namespace ns = namespaces.getStore().getByKey(name);
		boolean member = ns != null && matcher.match(name, ns.getMetadata().getLabels()).isPresent();
		synchronized (this) {
			if (member) {
				touch(name);
			}
			else if (revisions.remove(name) == null) {
				return;
			}
		}
		notifyListeners();
	}

	/** Markiert den Namespace eines Services, Ingresses oder Pods als geändert, wenn er zu einem Projekt gehört. */
	void objectChanged(String namespace, String ignoredName) {
		synchronized (this) {
			if (!revisions.containsKey(namespace)) {
				return;
			}
			touch(namespace);
		}
		notifyListeners();
	}

	private void touch(String namespace) {
		revisions.put(namespace, ++lastRevision);
	}

	private void notifyListeners() {
		listeners.forEach(Runnable::run);
	}

	@Override
	public void onChange(Runnable listener) {
		listeners.add(listener);
	}

	@Override
	public synchronized Map<String, Long> namespaceRevisions() {
		return new HashMap<>(revisions);
	}

	@Override
	public Optional<RevisedNamespaceView> namespace(String name) {
		if (namespaces == null) {
			return Optional.empty();
		}
		Namespace ns = namespaces.getStore().getByKey(name);
		if (ns == null) {
			return Optional.empty();
		}
		Optional<String> project = matcher.match(name, ns.getMetadata().getLabels());
		if (project.isEmpty()) {
			return Optional.empty();
		}
		var view = Converter.namespaceView(ns, project.get(), pods.getIndexer().byIndex(Cache.NAMESPACE_INDEX, name),
				services.getIndexer().byIndex(Cache.NAMESPACE_INDEX, name), ingresses.getIndexer().byIndex(Cache.NAMESPACE_INDEX, name));
		long revision;
		synchronized (this) {
			revision = revisions.getOrDefault(name, 0L);
		}
		return Optional.of(new RevisedNamespaceView(view, revision));
	}
}
