package its.me.feature_dashboard.actions.nodeportservice;

import java.util.ArrayList;
import java.util.HashSet;
import java.util.List;
import java.util.Map;
import java.util.Set;
import java.util.stream.Collectors;

import org.springframework.stereotype.Component;

import io.fabric8.kubernetes.api.model.IntOrString;
import io.fabric8.kubernetes.api.model.Service;
import io.fabric8.kubernetes.api.model.ServiceBuilder;
import io.fabric8.kubernetes.api.model.ServicePort;
import io.fabric8.kubernetes.api.model.ServicePortBuilder;
import io.fabric8.kubernetes.client.KubernetesClient;
import io.fabric8.kubernetes.client.KubernetesClientException;
import its.me.feature_dashboard.actions.ActionHandler;
import its.me.feature_dashboard.actions.ActionHandlerType;
import its.me.feature_dashboard.actions.ActionObject;
import its.me.feature_dashboard.actions.ActionResult;
import its.me.feature_dashboard.actions.FormField;
import its.me.feature_dashboard.actions.HandlerDeps;
import its.me.feature_dashboard.config.KubernetesNames;
import its.me.feature_dashboard.model.ClusterSource;
import its.me.feature_dashboard.model.Kind;
import its.me.feature_dashboard.model.ObjectRef;
import its.me.feature_dashboard.model.StoredService;

/**
 * Erstellt zu einem ClusterIP-Service einen NodePort-Service „&lt;name&gt;-node-port“ mit demselben Selector und den
 * gewählten Port-Mappings. Die NodePorts vergibt Kubernetes zufällig. Der NodePort-Service erhält eine ownerReference
 * auf den Basis-Service; wird dieser gelöscht, löscht der Garbage-Collector auch den NodePort-Service.
 */
@Component
public class CreateNodePortServiceHandlerType implements ActionHandlerType {

	@Override
	public String type() {
		return NodePortServices.CREATE_ACTION_NAME;
	}

	@Override
	public ActionHandler create(HandlerDeps deps) {
		return new Handler(deps.client(), deps.store());
	}

	static class Handler implements ActionHandler {

		private final KubernetesClient client;
		private final ClusterSource store;

		Handler(KubernetesClient client, ClusterSource store) {
			this.client = client;
			this.store = store;
		}

		/** Gilt für ClusterIP-Services mit Selector und Ports, zu denen es noch keinen NodePort-Service gibt. */
		@Override
		public boolean isApplicable(ActionObject obj) {
			if (!(obj.object() instanceof StoredService svc) || !"ClusterIP".equals(svc.type()) || svc.selector().isEmpty()
					|| svc.ports().isEmpty()) {
				return false;
			}
			String name = svc.name() + NodePortServices.NAME_SUFFIX;
			if (!KubernetesNames.isDns1035Label(name)) {
				return false;
			}
			return store.get(new ObjectRef(Kind.SERVICE, obj.ref().namespace(), name)).isEmpty();
		}

		@Override
		public List<FormField> formFields(ActionObject obj) {
			StoredService svc = (StoredService) obj.object();
			List<FormField.Option> options = svc.ports()
				.stream()
				.map(p -> new FormField.Option(portKey(p), portLabel(p)))
				.toList();
			return List.of(new FormField(NodePortServices.FIELD_PORTS, "Target-Ports", FormField.MULTI_SELECT, true,
					"Target-Ports, für die der NodePort-Service ein Port-Mapping erhält. Die NodePorts werden zufällig vergeben.",
					options));
		}

		@Override
		public ActionResult execute(ActionObject obj, Map<String, Object> inputs) {
			if (!(obj.object() instanceof StoredService base)) {
				return ActionResult.failure("Objekt ist kein Service");
			}
			Set<String> selected = new HashSet<>();
			if (inputs.get(NodePortServices.FIELD_PORTS) instanceof List<?> list) {
				list.stream().filter(String.class::isInstance).map(String.class::cast).forEach(selected::add);
			}
			// Der Basis-Service wird frisch gelesen: Die ownerReference braucht seine UID, der Cache kann veraltet sein.
			Service current;
			try {
				current = client.services().inNamespace(obj.ref().namespace()).withName(base.name()).get();
			}
			catch (KubernetesClientException e) {
				return ActionResult.failure("Service %s konnte nicht gelesen werden: %s".formatted(base.name(), e.getMessage()));
			}
			if (current == null) {
				return ActionResult.failure("Service %s existiert nicht mehr".formatted(base.name()));
			}
			if (!"ClusterIP".equals(current.getSpec().getType())) {
				return ActionResult.failure("Service %s ist nicht mehr vom Typ ClusterIP".formatted(base.name()));
			}
			List<ServicePort> ports = new ArrayList<>();
			for (StoredService.ServicePort p : base.ports()) {
				if (!selected.contains(portKey(p))) {
					continue;
				}
				// NodePort leer: Kubernetes vergibt einen zufälligen freien Port.
				ports.add(new ServicePortBuilder().withName(p.name().isEmpty() ? null : p.name())
					.withProtocol(p.protocol().isEmpty() ? null : p.protocol())
					.withPort(p.port())
					.withTargetPort(targetPort(p.targetPort()))
					.build());
			}
			if (ports.isEmpty()) {
				return ActionResult.failure("Keine gültigen Ports ausgewählt");
			}
			String name = base.name() + NodePortServices.NAME_SUFFIX;
			// Ohne controller/blockOwnerDeletion: dafür bräuchte das Backend zusätzlich update auf services/finalizers.
			Service svc = new ServiceBuilder().withNewMetadata()
				.withName(name)
				.withNamespace(obj.ref().namespace())
				.withLabels(Map.of(NodePortServices.LABEL_MANAGED_BY, NodePortServices.MANAGED_BY_VALUE,
						NodePortServices.LABEL_CREATED_BY_ACTION, NodePortServices.CREATE_ACTION_NAME))
				.withAnnotations(Map.of(NodePortServices.ANNOTATION_BASE_SERVICE, base.name()))
				.addNewOwnerReference()
				.withApiVersion("v1")
				.withKind("Service")
				.withName(current.getMetadata().getName())
				.withUid(current.getMetadata().getUid())
				.endOwnerReference()
				.endMetadata()
				.withNewSpec()
				.withType("NodePort")
				.withSelector(base.selector())
				.withPorts(ports)
				.endSpec()
				.build();
			Service created;
			try {
				created = client.services().inNamespace(obj.ref().namespace()).resource(svc).create();
			}
			catch (KubernetesClientException e) {
				return ActionResult.failure("NodePort-Service %s konnte nicht erstellt werden: %s".formatted(name, e.getMessage()));
			}
			return ActionResult.ok("NodePort-Service %s erstellt (%s)".formatted(created.getMetadata().getName(),
					nodePorts(created)));
		}

		private static IntOrString targetPort(String value) {
			try {
				return new IntOrString(Integer.parseInt(value));
			}
			catch (NumberFormatException e) {
				return new IntOrString(value);
			}
		}
	}

	/**
	 * Identifiziert einen Service-Port: Namen sind bei mehreren Ports Pflicht und eindeutig, ein einzelner Port darf
	 * unbenannt sein.
	 */
	static String portKey(StoredService.ServicePort p) {
		return p.name().isEmpty() ? Integer.toString(p.port()) : p.name();
	}

	/**
	 * Beschreibt einen Service-Port für die Auswahl: benannt „&lt;Target-Port-Nummer&gt; (&lt;Name&gt;)“, sonst
	 * „Service-Port=&lt;Port&gt;, Container-Port=&lt;Target-Port&gt;“; nicht auflösbare Target-Ports als Rohwert.
	 */
	static String portLabel(StoredService.ServicePort p) {
		String target = p.resolvedTargetPort() != null ? p.resolvedTargetPort().toString() : p.targetPort();
		if (!p.name().isEmpty()) {
			return "%s (%s)".formatted(target, p.name());
		}
		return "Service-Port=%d, Container-Port=%s".formatted(p.port(), target);
	}

	private static String nodePorts(Service svc) {
		return svc.getSpec()
			.getPorts()
			.stream()
			.map(p -> "%d → NodePort %d".formatted(p.getPort(), p.getNodePort() == null ? 0 : p.getNodePort()))
			.collect(Collectors.joining(", "));
	}
}
