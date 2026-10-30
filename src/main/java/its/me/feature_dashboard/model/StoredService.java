package its.me.feature_dashboard.model;

import java.util.List;
import java.util.Map;

/**
 * Service. {@code replicas} sind die Namen der Pods, die der Selector trifft; sie und {@code resolvedTargetPort}
 * setzt erst die Auflösung beim Aufbau der NamespaceView.
 */
public record StoredService(Meta meta, String type, Map<String, String> selector, List<ServicePort> ports,
		List<String> replicas) implements Stored {

	public StoredService {
		selector = SortedMaps.copyOf(selector);
		ports = List.copyOf(ports);
		replicas = List.copyOf(replicas);
	}

	/**
	 * Port-Mapping. {@code targetPort} ist der Rohwert (Zahl oder Name), {@code resolvedTargetPort} die aufgelöste
	 * Nummer oder null; {@code nodePort} ist 0, wenn keiner vergeben ist.
	 */
	public record ServicePort(String name, String protocol, int port, String targetPort, Integer resolvedTargetPort,
			int nodePort) {
	}
}
