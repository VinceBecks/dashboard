package its.me.feature_dashboard.testsupport;

import java.util.ArrayList;
import java.util.Comparator;
import java.util.HashMap;
import java.util.LinkedHashMap;
import java.util.List;
import java.util.Map;
import java.util.Optional;
import java.util.concurrent.CopyOnWriteArrayList;

import its.me.feature_dashboard.converter.Converter;
import its.me.feature_dashboard.model.ClusterSource;
import its.me.feature_dashboard.model.Meta;
import its.me.feature_dashboard.model.NamespaceView;
import its.me.feature_dashboard.model.Stored;
import its.me.feature_dashboard.model.StoredIngress;
import its.me.feature_dashboard.model.StoredNamespace;
import its.me.feature_dashboard.model.StoredPod;
import its.me.feature_dashboard.model.StoredService;

/**
 * In-Memory-Datenquelle nur für Tests (entspricht internal/testsource): Tests legen Objekte direkt an, ohne
 * Fake-Cluster und Informer. Replicas und targetPorts werden wie im Cluster beim Lesen aufgelöst.
 */
public class TestSource implements ClusterSource {

	private final Map<String, StoredNamespace> namespaces = new LinkedHashMap<>();
	private final Map<String, Map<String, Stored>> objects = new HashMap<>();
	private final Map<String, Long> revisions = new HashMap<>();
	private final List<Runnable> listeners = new CopyOnWriteArrayList<>();
	private long lastRevision;

	public synchronized TestSource namespace(String name, String project, Map<String, String> labels) {
		namespaces.put(name, new StoredNamespace(new Meta(name, null, labels, null), project));
		objects.computeIfAbsent(name, n -> new LinkedHashMap<>());
		return touch(name);
	}

	public TestSource namespace(String name, String project) {
		return namespace(name, project, Map.of());
	}

	public synchronized TestSource put(Stored obj) {
		String ns = obj.meta().namespace();
		objects.computeIfAbsent(ns, n -> new LinkedHashMap<>()).put(obj.getClass().getSimpleName() + "/" + obj.name(), obj);
		return touch(ns);
	}

	public synchronized TestSource remove(Class<? extends Stored> type, String namespace, String name) {
		objects.getOrDefault(namespace, new HashMap<>()).remove(type.getSimpleName() + "/" + name);
		return touch(namespace);
	}

	public synchronized TestSource removeNamespace(String name) {
		namespaces.remove(name);
		objects.remove(name);
		revisions.remove(name);
		notifyListeners();
		return this;
	}

	private TestSource touch(String namespace) {
		if (namespaces.containsKey(namespace)) {
			revisions.put(namespace, ++lastRevision);
		}
		notifyListeners();
		return this;
	}

	private void notifyListeners() {
		listeners.forEach(Runnable::run);
	}

	@Override
	public synchronized Map<String, Long> namespaceRevisions() {
		return new HashMap<>(revisions);
	}

	@Override
	public void onChange(Runnable listener) {
		listeners.add(listener);
	}

	@Override
	public synchronized Optional<RevisedNamespaceView> namespace(String name) {
		StoredNamespace ns = namespaces.get(name);
		if (ns == null) {
			return Optional.empty();
		}
		List<StoredPod> pods = new ArrayList<>();
		List<StoredService> services = new ArrayList<>();
		List<StoredIngress> ingresses = new ArrayList<>();
		objects.getOrDefault(name, Map.of()).values().forEach(o -> {
			switch (o) {
				case StoredPod p -> pods.add(p);
				case StoredService s -> services.add(s);
				case StoredIngress i -> ingresses.add(i);
				case StoredNamespace n -> {
				}
			}
		});
		pods.sort(Comparator.comparing(StoredPod::name));
		ingresses.sort(Comparator.comparing(StoredIngress::name));
		List<StoredService> resolved = services.stream()
			.sorted(Comparator.comparing(StoredService::name))
			.map(s -> Converter.resolveService(s, pods))
			.toList();
		return Optional.of(new RevisedNamespaceView(new NamespaceView(ns, resolved, ingresses, pods),
				revisions.getOrDefault(name, 0L)));
	}

	// Kurzformen für Tests

	public static Meta meta(String namespace, String name, Map<String, String> labels) {
		return new Meta(name, namespace, labels, null);
	}

	public static StoredPod pod(String namespace, String name, boolean ready, Map<String, String> labels,
			StoredPod.Container... containers) {
		return new StoredPod(meta(namespace, name, labels), ready, List.of(containers));
	}

	public static StoredService service(String namespace, String name, String type, Map<String, String> selector,
			StoredService.ServicePort... ports) {
		return new StoredService(meta(namespace, name, Map.of()), type, selector, List.of(ports), List.of());
	}

	public static StoredService.ServicePort port(String name, int port, String targetPort, int nodePort) {
		return new StoredService.ServicePort(name, "TCP", port, targetPort, null, nodePort);
	}
}
