package its.me.feature_dashboard.api;

import java.time.Instant;
import java.util.List;

import com.fasterxml.jackson.annotation.JsonInclude;
import com.fasterxml.jackson.annotation.JsonPropertyOrder;

import its.me.feature_dashboard.actions.Action;
import its.me.feature_dashboard.copycommands.CopyCommand;
import its.me.feature_dashboard.links.Link;

/**
 * Ausgabetypen der API (siehe openapi.yaml). Die Feldreihenfolge entspricht dem Go-Backend, damit Snapshot und
 * Events gleich aussehen. Kind-Listen sind im Snapshot gesetzt und in upsert-Events null (entfallen).
 */
public final class Dtos {

	/** kind eines NodePort-Services in der API; für Aktionen ist er ein Objekt vom Typ service. */
	public static final String KIND_NODE_PORT_SERVICE = "node-port-service";

	private Dtos() {
	}

	/** Parents bis zum Projekt, damit ein Event ohne weitere Daten einsortiert werden kann. */
	@JsonPropertyOrder({ "project", "namespace", "service" })
	public record Parents(String project, @JsonInclude(JsonInclude.Include.NON_EMPTY) String namespace,
			@JsonInclude(JsonInclude.Include.NON_EMPTY) String service) {
	}

	/** Kopf eines Objekts; Inhalt eines delete-Events. */
	@JsonPropertyOrder({ "kind", "name", "parents" })
	public record ObjectHeader(String kind, String name, Parents parents) {
	}

	/** Aktionen, Links, Kopier-Befehle und laufende Pipelines eines Objekts. */
	public record Extras(List<Action> actions, List<Link> links, List<CopyCommand> copyCommands,
			List<PipelineDto> pipelines) {
	}

	@JsonPropertyOrder({ "action", "title", "url", "startedAt" })
	public record PipelineDto(String action, String title, String url, Instant startedAt) {
	}

	@JsonPropertyOrder({ "kind", "name", "parents", "team", "actions", "links", "copyCommands", "pipelines", "services",
			"nodePortServices", "ingresses" })
	public record NamespaceDto(String kind, String name, Parents parents,
			@JsonInclude(JsonInclude.Include.NON_EMPTY) String team, List<Action> actions, List<Link> links,
			List<CopyCommand> copyCommands, List<PipelineDto> pipelines,
			@JsonInclude(JsonInclude.Include.NON_NULL) List<ServiceDto> services,
			@JsonInclude(JsonInclude.Include.NON_NULL) List<NodePortServiceDto> nodePortServices,
			@JsonInclude(JsonInclude.Include.NON_NULL) List<IngressDto> ingresses) {

		ObjectHeader header() {
			return new ObjectHeader(kind, name, parents);
		}

		NamespaceDto withoutChildren() {
			return new NamespaceDto(kind, name, parents, team, actions, links, copyCommands, pipelines, null, null, null);
		}
	}

	@JsonPropertyOrder({ "kind", "name", "parents", "running", "areas", "actions", "links", "copyCommands", "pipelines",
			"pods" })
	public record ServiceDto(String kind, String name, Parents parents, boolean running, List<String> areas,
			List<Action> actions, List<Link> links, List<CopyCommand> copyCommands, List<PipelineDto> pipelines,
			@JsonInclude(JsonInclude.Include.NON_NULL) List<PodDto> pods) {

		ObjectHeader header() {
			return new ObjectHeader(kind, name, parents);
		}

		ServiceDto withoutChildren() {
			return new ServiceDto(kind, name, parents, running, areas, actions, links, copyCommands, pipelines, null);
		}
	}

	@JsonPropertyOrder({ "kind", "name", "parents", "ready", "containers", "actions", "links", "copyCommands",
			"pipelines" })
	public record PodDto(String kind, String name, Parents parents, boolean ready, List<ContainerDto> containers,
			List<Action> actions, List<Link> links, List<CopyCommand> copyCommands, List<PipelineDto> pipelines) {

		ObjectHeader header() {
			return new ObjectHeader(kind, name, parents);
		}
	}

	@JsonPropertyOrder({ "name", "image" })
	public record ContainerDto(String name, String image) {
	}

	@JsonPropertyOrder({ "kind", "name", "parents", "addresses", "actions", "links", "copyCommands", "pipelines" })
	public record NodePortServiceDto(String kind, String name, Parents parents, List<String> addresses,
			List<Action> actions, List<Link> links, List<CopyCommand> copyCommands, List<PipelineDto> pipelines) {

		ObjectHeader header() {
			return new ObjectHeader(kind, name, parents);
		}
	}

	@JsonPropertyOrder({ "kind", "name", "parents", "urls", "actions", "links", "copyCommands", "pipelines" })
	public record IngressDto(String kind, String name, Parents parents, List<String> urls, List<Action> actions,
			List<Link> links, List<CopyCommand> copyCommands, List<PipelineDto> pipelines) {

		ObjectHeader header() {
			return new ObjectHeader(kind, name, parents);
		}
	}

	/** Entwickler-Team im Snapshot; NamespaceDto.team verweist auf die ID. */
	@JsonPropertyOrder({ "id", "label", "teamColor" })
	public record TeamDto(String id, String label, String teamColor) {
	}

	/** Service-Bereich; im Dashboard ein Reiter der Namespace-Kachel. */
	@JsonPropertyOrder({ "id", "label" })
	public record AreaDto(String id, String label) {
	}
}
