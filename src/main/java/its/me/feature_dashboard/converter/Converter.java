package its.me.feature_dashboard.converter;

import java.util.ArrayList;
import java.util.Comparator;
import java.util.List;
import java.util.Map;

import io.fabric8.kubernetes.api.model.HasMetadata;
import io.fabric8.kubernetes.api.model.IntOrString;
import io.fabric8.kubernetes.api.model.Namespace;
import io.fabric8.kubernetes.api.model.Pod;
import io.fabric8.kubernetes.api.model.PodCondition;
import io.fabric8.kubernetes.api.model.Service;
import io.fabric8.kubernetes.api.model.networking.v1.HTTPIngressPath;
import io.fabric8.kubernetes.api.model.networking.v1.Ingress;
import io.fabric8.kubernetes.api.model.networking.v1.IngressRule;
import its.me.feature_dashboard.model.Meta;
import its.me.feature_dashboard.model.NamespaceView;
import its.me.feature_dashboard.model.StoredIngress;
import its.me.feature_dashboard.model.StoredNamespace;
import its.me.feature_dashboard.model.StoredPod;
import its.me.feature_dashboard.model.StoredService;
import its.me.feature_dashboard.model.StoredService.ServicePort;

/**
 * Wandelt Kubernetes-Objekte aus dem Informer-Cache in Stored-Objects um und löst Referenzen auf. Die Eingaben zeigen
 * direkt in den Cache: Sie werden nur gelesen, nie verändert; alle Maps werden kopiert.
 */
public final class Converter {

	private Converter() {
	}

	public static StoredNamespace namespace(Namespace ns, String project) {
		return new StoredNamespace(new Meta(ns.getMetadata().getName(), null, ns.getMetadata().getLabels(),
				ns.getMetadata().getAnnotations()), project);
	}

	public static StoredPod pod(Pod p) {
		boolean ready = false;
		if (p.getStatus() != null && p.getStatus().getConditions() != null) {
			for (PodCondition c : p.getStatus().getConditions()) {
				if ("Ready".equals(c.getType())) {
					ready = "True".equals(c.getStatus());
				}
			}
		}
		List<StoredPod.Container> containers = new ArrayList<>();
		if (p.getSpec() != null && p.getSpec().getContainers() != null) {
			for (var c : p.getSpec().getContainers()) {
				List<StoredPod.ContainerPort> ports = new ArrayList<>();
				if (c.getPorts() != null) {
					for (var port : c.getPorts()) {
						ports.add(new StoredPod.ContainerPort(nullToEmpty(port.getName()),
								port.getContainerPort() == null ? 0 : port.getContainerPort(),
								nullToEmpty(port.getProtocol())));
					}
				}
				containers.add(new StoredPod.Container(c.getName(), nullToEmpty(c.getImage()), ports));
			}
		}
		return new StoredPod(meta(p), ready, containers);
	}

	/** Konvertiert einen Service ohne Referenzen aufzulösen (siehe {@link #resolveService}). */
	public static StoredService service(Service s) {
		var spec = s.getSpec();
		String type = spec == null || spec.getType() == null || spec.getType().isEmpty() ? "ClusterIP" : spec.getType();
		List<ServicePort> ports = new ArrayList<>();
		if (spec != null && spec.getPorts() != null) {
			for (var p : spec.getPorts()) {
				int port = p.getPort() == null ? 0 : p.getPort();
				ports.add(new ServicePort(nullToEmpty(p.getName()), nullToEmpty(p.getProtocol()), port,
						targetPort(p.getTargetPort(), port), null, p.getNodePort() == null ? 0 : p.getNodePort()));
			}
		}
		return new StoredService(meta(s), type, spec == null ? null : spec.getSelector(), ports, List.of());
	}

	/** Ohne targetPort gilt laut Kubernetes der Service-Port. */
	private static String targetPort(IntOrString target, int port) {
		if (target == null) {
			return Integer.toString(port);
		}
		if (target.getStrVal() != null) {
			return target.getStrVal();
		}
		Integer value = target.getIntVal();
		return Integer.toString(value == null || value == 0 ? port : value);
	}

	public static StoredIngress ingress(Ingress i) {
		List<StoredIngress.Rule> rules = new ArrayList<>();
		if (i.getSpec() != null && i.getSpec().getRules() != null) {
			for (IngressRule r : i.getSpec().getRules()) {
				List<StoredIngress.Path> paths = new ArrayList<>();
				if (r.getHttp() != null && r.getHttp().getPaths() != null) {
					for (HTTPIngressPath p : r.getHttp().getPaths()) {
						String backend = p.getBackend() != null && p.getBackend().getService() != null
								? nullToEmpty(p.getBackend().getService().getName()) : "";
						paths.add(new StoredIngress.Path(nullToEmpty(p.getPathType()), nullToEmpty(p.getPath()), backend));
					}
				}
				rules.add(new StoredIngress.Rule(nullToEmpty(r.getHost()), paths));
			}
		}
		return new StoredIngress(meta(i), rules);
	}

	/** Ermittelt die Replicas eines Services und löst benannte targetPorts über deren Container-Ports auf. */
	public static StoredService resolveService(StoredService svc, List<StoredPod> pods) {
		List<StoredPod> replicas = new ArrayList<>();
		if (!svc.selector().isEmpty()) {
			for (StoredPod p : pods) {
				if (matches(p.meta().labels(), svc.selector())) {
					replicas.add(p);
				}
			}
		}
		List<ServicePort> ports = new ArrayList<>();
		for (ServicePort port : svc.ports()) {
			Integer resolved;
			try {
				resolved = Integer.parseInt(port.targetPort());
			}
			catch (NumberFormatException e) {
				resolved = findContainerPort(replicas, port.targetPort(), port.protocol());
			}
			ports.add(new ServicePort(port.name(), port.protocol(), port.port(), port.targetPort(), resolved,
					port.nodePort()));
		}
		return new StoredService(svc.meta(), svc.type(), svc.selector(), ports,
				replicas.stream().map(StoredPod::name).toList());
	}

	private static Integer findContainerPort(List<StoredPod> pods, String name, String protocol) {
		for (StoredPod p : pods) {
			for (StoredPod.Container c : p.containers()) {
				for (StoredPod.ContainerPort cp : c.ports()) {
					if (cp.name().equals(name) && protocolEqual(cp.protocol(), protocol)) {
						return cp.containerPort();
					}
				}
			}
		}
		return null;
	}

	private static boolean protocolEqual(String a, String b) {
		return (a.isEmpty() ? "TCP" : a).equals(b.isEmpty() ? "TCP" : b);
	}

	private static boolean matches(Map<String, String> labels, Map<String, String> selector) {
		for (var e : selector.entrySet()) {
			if (!e.getValue().equals(labels.get(e.getKey()))) {
				return false;
			}
		}
		return true;
	}

	/**
	 * Konvertiert alle Objekte eines Namespaces und löst bei den Services Replicas und benannte targetPorts über die
	 * Pods auf. Das Ergebnis ist je Typ nach Name sortiert.
	 */
	public static NamespaceView namespaceView(Namespace ns, String project, List<Pod> pods, List<Service> services,
			List<Ingress> ingresses) {
		Comparator<HasMetadata> byName = Comparator.comparing(o -> o.getMetadata().getName());
		List<StoredPod> storedPods = pods.stream().sorted(byName).map(Converter::pod).toList();
		List<StoredService> storedServices = services.stream()
			.sorted(byName)
			.map(Converter::service)
			.map(s -> resolveService(s, storedPods))
			.toList();
		List<StoredIngress> storedIngresses = ingresses.stream().sorted(byName).map(Converter::ingress).toList();
		return new NamespaceView(namespace(ns, project), storedServices, storedIngresses, storedPods);
	}

	private static Meta meta(HasMetadata o) {
		var m = o.getMetadata();
		return new Meta(m.getName(), m.getNamespace(), m.getLabels(), m.getAnnotations());
	}

	private static String nullToEmpty(String s) {
		return s == null ? "" : s;
	}
}
