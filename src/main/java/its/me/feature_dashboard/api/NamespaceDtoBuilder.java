package its.me.feature_dashboard.api;

import java.util.ArrayList;
import java.util.LinkedHashSet;
import java.util.List;
import java.util.Map;
import java.util.Set;
import java.util.function.Function;
import java.util.stream.Collectors;

import its.me.feature_dashboard.actions.ActionObject;
import its.me.feature_dashboard.actions.ActionRegistry;
import its.me.feature_dashboard.api.Dtos.ContainerDto;
import its.me.feature_dashboard.api.Dtos.Extras;
import its.me.feature_dashboard.api.Dtos.IngressDto;
import its.me.feature_dashboard.api.Dtos.NamespaceDto;
import its.me.feature_dashboard.api.Dtos.NodePortServiceDto;
import its.me.feature_dashboard.api.Dtos.Parents;
import its.me.feature_dashboard.api.Dtos.PipelineDto;
import its.me.feature_dashboard.api.Dtos.PodDto;
import its.me.feature_dashboard.api.Dtos.ServiceDto;
import its.me.feature_dashboard.config.DashboardProperties.Project;
import its.me.feature_dashboard.config.DashboardProperties.TeamsConfig;
import its.me.feature_dashboard.copycommands.CopyCommandRegistry;
import its.me.feature_dashboard.links.LinkRegistry;
import its.me.feature_dashboard.model.Kind;
import its.me.feature_dashboard.model.NamespaceView;
import its.me.feature_dashboard.model.ObjectRef;
import its.me.feature_dashboard.model.Stored;
import its.me.feature_dashboard.model.StoredIngress;
import its.me.feature_dashboard.model.StoredPod;
import its.me.feature_dashboard.model.StoredService;
import its.me.feature_dashboard.pipelines.PipelineState;
import its.me.feature_dashboard.project.ServiceAreas;

/**
 * Baut den DTO-Teilbaum eines Namespaces und hängt an jedes Objekt Aktionen, Links, Kopier-Befehle und laufende
 * Pipelines. Pods hängen unter jedem Service, der sie referenziert; Pods ohne Service und Services, die weder
 * ClusterIP noch NodePort sind, entfallen.
 */
public class NamespaceDtoBuilder {

	private final ActionRegistry registry;
	private final LinkRegistry links;
	private final CopyCommandRegistry copyCommands;
	private final PipelineState pipelines;
	private final String dnsName;
	private final Map<String, Project> projects;
	private final TeamsConfig teams;

	public NamespaceDtoBuilder(ActionRegistry registry, LinkRegistry links, CopyCommandRegistry copyCommands,
			PipelineState pipelines, String dnsName, List<Project> projects, TeamsConfig teams) {
		this.registry = registry;
		this.links = links;
		this.copyCommands = copyCommands;
		this.pipelines = pipelines;
		this.dnsName = dnsName;
		this.projects = projects.stream().collect(Collectors.toMap(Project::projectName, Function.identity(), (a, b) -> a));
		this.teams = teams;
	}

	public NamespaceDto build(NamespaceView view) {
		var ns = view.namespace();
		String project = ns.project();
		Parents nsParents = new Parents(project, ns.name(), null);
		Map<String, StoredPod> pods = view.pods().stream().collect(Collectors.toMap(StoredPod::name, Function.identity()));
		List<ServiceDto> services = new ArrayList<>();
		List<NodePortServiceDto> nodePorts = new ArrayList<>();
		for (StoredService svc : view.services()) {
			switch (svc.type()) {
				case "ClusterIP" -> services.add(service(svc, pods, nsParents));
				case "NodePort" -> {
					Extras x = extras(Kind.SERVICE, ns.name(), svc.name(), svc, project);
					nodePorts.add(new NodePortServiceDto(Dtos.KIND_NODE_PORT_SERVICE, svc.name(), nsParents,
							nodePortAddresses(svc), x.actions(), x.links(), x.copyCommands(), x.pipelines()));
				}
				default -> {
				}
			}
		}
		List<IngressDto> ingresses = new ArrayList<>();
		for (StoredIngress ing : view.ingresses()) {
			Extras x = extras(Kind.INGRESS, ns.name(), ing.name(), ing, project);
			ingresses.add(new IngressDto(Kind.INGRESS.value(), ing.name(), nsParents, ingressUrls(ing), x.actions(),
					x.links(), x.copyCommands(), x.pipelines()));
		}
		Extras x = extras(Kind.NAMESPACE, ns.name(), ns.name(), ns, project);
		return new NamespaceDto(Kind.NAMESPACE.value(), ns.name(), new Parents(project, null, null), teamOf(ns),
				x.actions(), x.links(), x.copyCommands(), x.pipelines(), services, nodePorts, ingresses);
	}

	/** ID des konfigurierten Teams, dem der Namespace per Label zugeordnet ist, sonst null. */
	private String teamOf(Stored ns) {
		if (teams == null || !teams.configured()) {
			return null;
		}
		String id = ns.meta().labels().get(teams.labelKey());
		return id == null ? null : teams.find(id).map(t -> t.teamId()).orElse(null);
	}

	private ServiceDto service(StoredService svc, Map<String, StoredPod> pods, Parents parents) {
		Parents podParents = new Parents(parents.project(), parents.namespace(), svc.name());
		List<PodDto> podDtos = new ArrayList<>();
		boolean running = false;
		for (String name : svc.replicas().stream().sorted().toList()) {
			StoredPod p = pods.get(name);
			if (p == null) {
				continue;
			}
			running = running || p.ready();
			Extras x = extras(Kind.POD, parents.namespace(), p.name(), p, parents.project());
			podDtos.add(new PodDto(Kind.POD.value(), p.name(), podParents, p.ready(), containers(p), x.actions(),
					x.links(), x.copyCommands(), x.pipelines()));
		}
		Extras x = extras(Kind.SERVICE, parents.namespace(), svc.name(), svc, parents.project());
		return new ServiceDto(Kind.SERVICE.value(), svc.name(), parents, running,
				ServiceAreas.areaIds(projects.get(parents.project()), svc.meta().labels()), x.actions(), x.links(),
				x.copyCommands(), x.pipelines(), podDtos);
	}

	private Extras extras(Kind kind, String namespace, String name, Stored obj, String project) {
		ObjectRef ref = new ObjectRef(kind, namespace, name);
		List<PipelineDto> running = pipelines == null ? List.of()
				: pipelines.forRef(ref)
					.stream()
					.map(e -> new PipelineDto(e.action(), registry.title(e.action()), e.webUrl(), e.startedAt()))
					.toList();
		return new Extras(registry.actionsFor(new ActionObject(ref, obj, project)), links.forObject(ref, obj, project),
				copyCommands.forObject(ref, obj, project), running);
	}

	/** Je Port mit vergebenem NodePort die Adresse {@code <dns-name>:<node-port>} (Reihenfolge der Spec). */
	private List<String> nodePortAddresses(StoredService svc) {
		String host = dnsName.contains(":") ? "[" + dnsName + "]" : dnsName;
		return svc.ports().stream().filter(p -> p.nodePort() != 0).map(p -> host + ":" + p.nodePort()).toList();
	}

	private static List<ContainerDto> containers(StoredPod p) {
		return p.containers().stream().map(c -> new ContainerDto(c.name(), c.image())).toList();
	}

	/**
	 * Je Rule mit Host und je Path eine https-URL (Reihenfolge der Spec, ohne Duplikate). Rules ohne Host entfallen.
	 */
	static List<String> ingressUrls(StoredIngress ing) {
		Set<String> result = new LinkedHashSet<>();
		for (StoredIngress.Rule rule : ing.rules()) {
			if (rule.host().isEmpty()) {
				continue;
			}
			if (rule.paths().isEmpty()) {
				result.add("https://" + rule.host() + "/");
			}
			for (StoredIngress.Path p : rule.paths()) {
				result.add("https://" + rule.host() + (p.path().isEmpty() ? "/" : p.path()));
			}
		}
		return List.copyOf(result);
	}
}
