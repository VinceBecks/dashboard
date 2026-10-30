package its.me.feature_dashboard.kube;

import java.util.List;
import java.util.stream.Stream;

import io.fabric8.kubernetes.api.model.Container;
import io.fabric8.kubernetes.api.model.ContainerBuilder;
import io.fabric8.kubernetes.api.model.HasMetadata;
import io.fabric8.kubernetes.api.model.Pod;
import io.fabric8.kubernetes.api.model.PodCondition;
import io.fabric8.kubernetes.api.model.PodConditionBuilder;
import io.fabric8.kubernetes.api.model.PodSpec;
import io.fabric8.kubernetes.api.model.PodStatus;
import io.fabric8.kubernetes.client.informers.cache.BasicItemStore;
import io.fabric8.kubernetes.client.informers.cache.Cache;
import io.fabric8.kubernetes.client.informers.cache.ItemStore;

/**
 * Item-Store des Informers, der jedes Objekt vor dem Speichern auf die genutzten Felder reduziert: ohne
 * managedFields, bei Pods nur Container (Name, Image, Ports), Phase und Ready-Condition. Werden weitere Felder
 * gebraucht, {@link #trim} erweitern.
 */
class TrimmingItemStore<T extends HasMetadata> implements ItemStore<T> {

	private final BasicItemStore<T> delegate = new BasicItemStore<>(Cache::metaNamespaceKeyFunc);

	@Override
	public String getKey(T obj) {
		return delegate.getKey(obj);
	}

	@Override
	public T put(String key, T obj) {
		return delegate.put(key, trim(obj));
	}

	@Override
	public T remove(String key) {
		return delegate.remove(key);
	}

	@Override
	public Stream<String> keySet() {
		return delegate.keySet();
	}

	@Override
	public Stream<T> values() {
		return delegate.values();
	}

	@Override
	public int size() {
		return delegate.size();
	}

	@Override
	public T get(String key) {
		return delegate.get(key);
	}

	/** Das Objekt stammt frisch aus List/Watch und wird hier noch von niemandem gelesen; es wird direkt reduziert. */
	static <T extends HasMetadata> T trim(T obj) {
		if (obj.getMetadata() != null) {
			obj.getMetadata().setManagedFields(null);
		}
		if (obj instanceof Pod pod) {
			List<Container> containers = pod.getSpec() == null || pod.getSpec().getContainers() == null ? List.of()
					: pod.getSpec()
						.getContainers()
						.stream()
						.map(c -> new ContainerBuilder().withName(c.getName()).withImage(c.getImage()).withPorts(c.getPorts()).build())
						.toList();
			PodSpec spec = new PodSpec();
			spec.setContainers(containers);
			pod.setSpec(spec);
			PodStatus old = pod.getStatus();
			PodStatus status = new PodStatus();
			if (old != null) {
				status.setPhase(old.getPhase());
				if (old.getConditions() != null) {
					List<PodCondition> conditions = old.getConditions()
						.stream()
						.filter(c -> "Ready".equals(c.getType()))
						.map(c -> new PodConditionBuilder().withType(c.getType()).withStatus(c.getStatus()).build())
						.toList();
					status.setConditions(conditions);
				}
			}
			pod.setStatus(status);
		}
		return obj;
	}
}
