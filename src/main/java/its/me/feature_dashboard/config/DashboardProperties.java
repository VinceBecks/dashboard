package its.me.feature_dashboard.config;

import java.time.Duration;
import java.util.List;
import java.util.Map;

import org.springframework.boot.context.properties.ConfigurationProperties;

/**
 * Fachliche Konfiguration des Dashboards unter {@code dashboard:} in der application.yml (entspricht der config.yaml
 * des Go-Backends). Label-Schlüssel mit {@code .} oder {@code /} stehen in eckigen Klammern, z. B.
 * {@code "[mycomp.de/project-name]": foo}. Die handler-spezifischen {@code extra-handler-configs} bindet jeder
 * ActionHandler selbst (siehe {@code ExtraConfig}).
 *
 * <p>Fehlende Listen und Maps werden zu leeren Werten, mit einer Ausnahme: {@code projects} von Aktionen, Links und
 * Kopier-Befehlen bleibt {@code null}, wenn es fehlt, damit die Prüfung „Pflichtfeld“ greift.
 */
@ConfigurationProperties(prefix = "dashboard")
public record DashboardProperties(
		List<Project> projects,
		List<ActionHandlerConfig> actionHandlers,
		ClusterConfig cluster,
		PipelinesConfig pipelines,
		TeamsConfig teams,
		Map<String, String> linkVariables,
		List<LinkConfig> links,
		List<CopyCommandConfig> copyCommands,
		String staticDir) {

	public DashboardProperties {
		projects = projects == null ? List.of() : projects;
		actionHandlers = actionHandlers == null ? List.of() : actionHandlers;
		cluster = cluster == null ? new ClusterConfig(null) : cluster;
		pipelines = pipelines == null ? new PipelinesConfig(null) : pipelines;
		teams = teams == null ? new TeamsConfig(null, null) : teams;
		linkVariables = linkVariables == null ? Map.of() : linkVariables;
		links = links == null ? List.of() : links;
		copyCommands = copyCommands == null ? List.of() : copyCommands;
	}

	/** Reservierte ID des Bereichs für Services, die keinem Bereich zugeordnet sind. */
	public static final String OTHERS_AREA_NAME = "others";

	/** Projekt mit den Labels, die ein Namespace tragen muss, um dazuzugehören. */
	public record Project(
			String projectName,
			Map<String, String> namespaceLabels,
			List<ServiceArea> serviceAreas,
			String othersAreaLabel,
			Map<String, String> linkVariablesOverrides) {

		public Project {
			namespaceLabels = namespaceLabels == null ? Map.of() : namespaceLabels;
			serviceAreas = serviceAreas == null ? List.of() : serviceAreas;
			linkVariablesOverrides = linkVariablesOverrides == null ? Map.of() : linkVariablesOverrides;
		}

		/** Label des others-Bereichs, Standard „Andere“. */
		public String othersLabel() {
			return othersAreaLabel == null || othersAreaLabel.isEmpty() ? "Andere" : othersAreaLabel;
		}
	}

	/**
	 * Bereich von Anwendungs-Services: Ein Service gehört dazu, wenn er alle Labels mindestens einer Kombination trägt.
	 */
	public record ServiceArea(String areaName, String areaLabel, List<Map<String, String>> labelCombinations) {
		public ServiceArea {
			labelCombinations = labelCombinations == null ? List.of() : labelCombinations;
		}
	}

	/** Genau eine Aktion und damit genau eine ActionHandler-Instanz. */
	public record ActionHandlerConfig(
			String actionName,
			String handler,
			String actionTitle,
			String icon,
			String iconColor,
			List<String> projects) {

		/** Meldet, ob die Aktion in mindestens einem Projekt angeboten wird. */
		public boolean active() {
			return projects != null && !projects.isEmpty();
		}

		/** Handler-Typ der Aktion; ohne {@code handler} gilt der action-name. */
		public String handlerType() {
			return handler == null || handler.isEmpty() ? actionName : handler;
		}
	}

	/** Angaben zum Cluster; der DNS-Name der Nodes bildet die NodePort-Adressen. */
	public record ClusterConfig(String dnsName) {
	}

	/** Abstand der Status-Abfragen laufender GitLab-Pipelines, Standard 15s, mindestens 5s. */
	public record PipelinesConfig(Duration pollInterval) {
		public static final Duration DEFAULT_POLL_INTERVAL = Duration.ofSeconds(15);
		public static final Duration MIN_POLL_INTERVAL = Duration.ofSeconds(5);

		public PipelinesConfig {
			pollInterval = pollInterval == null ? DEFAULT_POLL_INTERVAL : pollInterval;
		}
	}

	/** Entwickler-Teams: Ein Namespace gehört zu einem Team, wenn sein Label {@code labelKey} die Team-ID trägt. */
	public record TeamsConfig(String labelKey, List<Team> teams) {
		public TeamsConfig {
			teams = teams == null ? List.of() : teams;
		}

		/** Team mit dieser ID. */
		public java.util.Optional<Team> find(String id) {
			return teams.stream().filter(t -> t.teamId() != null && t.teamId().equals(id)).findFirst();
		}

		public boolean configured() {
			return labelKey != null && !labelKey.isEmpty();
		}
	}

	public record Team(String teamId, String teamLabel, String teamColor) {
	}

	/** Absprung (z. B. nach Kibana); je Objekt-Typ ein URL-Template. */
	public record LinkConfig(
			String linkId,
			String title,
			String icon,
			String iconColor,
			List<String> projects,
			Map<String, LinkTarget> targets) {
		public LinkConfig {
			targets = targets == null ? Map.of() : targets;
		}
	}

	public record LinkTarget(String url) {
	}

	/** Befehl zum Kopieren (z. B. kubectl exec); je Objekt-Typ ein Befehls-Template. */
	public record CopyCommandConfig(
			String commandId,
			String title,
			String icon,
			String iconColor,
			List<String> projects,
			Map<String, CommandTarget> targets) {
		public CopyCommandConfig {
			targets = targets == null ? Map.of() : targets;
		}
	}

	public record CommandTarget(String command) {
	}
}
