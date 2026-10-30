package its.me.feature_dashboard.config;

import java.util.ArrayList;
import java.util.HashSet;
import java.util.List;
import java.util.Locale;
import java.util.Map;
import java.util.Set;

import its.me.feature_dashboard.config.DashboardProperties.ActionHandlerConfig;
import its.me.feature_dashboard.config.DashboardProperties.CopyCommandConfig;
import its.me.feature_dashboard.config.DashboardProperties.LinkConfig;
import its.me.feature_dashboard.config.DashboardProperties.Project;
import its.me.feature_dashboard.config.DashboardProperties.ServiceArea;
import its.me.feature_dashboard.config.DashboardProperties.Team;
import its.me.feature_dashboard.config.DashboardProperties.TeamsConfig;
import its.me.feature_dashboard.model.Kind;

/**
 * Prüft die Konfiguration auf Konsistenz (wie {@code config.Validate} im Go-Backend). Alle Fehler werden gesammelt;
 * sind welche vorhanden, bricht der Start ab.
 */
public final class DashboardConfigValidator {

	private DashboardConfigValidator() {
	}

	/** Prüft die Konfiguration und wirft bei Fehlern eine {@link InvalidConfigException} mit allen Fehlern. */
	public static void validateOrThrow(DashboardProperties config) {
		List<String> errors = validate(config);
		if (!errors.isEmpty()) {
			throw new InvalidConfigException(errors);
		}
	}

	public static List<String> validate(DashboardProperties c) {
		List<String> errs = new ArrayList<>();
		Set<String> projects = new HashSet<>();
		for (int i = 0; i < c.projects().size(); i++) {
			Project p = c.projects().get(i);
			if (isEmpty(p.projectName())) {
				errs.add("projects[%d]: project-name fehlt".formatted(i));
			}
			else if (!projects.add(p.projectName())) {
				errs.add("projects[%d]: project-name \"%s\" ist nicht eindeutig".formatted(i, p.projectName()));
			}
			if (p.namespaceLabels().isEmpty()) {
				errs.add("projects[%d]: namespace-labels dürfen nicht leer sein".formatted(i));
			}
			errs.addAll(validateServiceAreas("projects[%d]".formatted(i), p.serviceAreas()));
		}
		errs.addAll(validateLinks(c.links(), projects));
		errs.addAll(validateCopyCommands(c.copyCommands(), projects));
		errs.addAll(validateCluster(c.cluster().dnsName()));
		if (c.pipelines().pollInterval().compareTo(DashboardProperties.PipelinesConfig.MIN_POLL_INTERVAL) < 0) {
			errs.add("pipelines.poll-interval \"%s\" muss mindestens %s sein".formatted(c.pipelines().pollInterval(),
					DashboardProperties.PipelinesConfig.MIN_POLL_INTERVAL));
		}
		errs.addAll(validateTeams(c.teams()));
		Set<String> actions = new HashSet<>();
		for (int i = 0; i < c.actionHandlers().size(); i++) {
			ActionHandlerConfig a = c.actionHandlers().get(i);
			if (isEmpty(a.actionName())) {
				errs.add("action-handlers[%d]: action-name fehlt".formatted(i));
			}
			else if (!actions.add(a.actionName())) {
				errs.add("action-handlers[%d]: action-name \"%s\" ist nicht eindeutig".formatted(i, a.actionName()));
			}
			errs.addAll(validateProjects("action-handlers[%d]".formatted(i), a.projects(), projects));
		}
		return errs;
	}

	private static List<String> validateServiceAreas(String prefix, List<ServiceArea> areas) {
		List<String> errs = new ArrayList<>();
		Set<String> names = new HashSet<>();
		for (int i = 0; i < areas.size(); i++) {
			ServiceArea a = areas.get(i);
			String at = "%s.service-areas[%d]".formatted(prefix, i);
			if (isEmpty(a.areaName())) {
				errs.add(at + ": area-name fehlt");
			}
			else if (DashboardProperties.OTHERS_AREA_NAME.equals(a.areaName())) {
				errs.add("%s: area-name \"%s\" ist reserviert".formatted(at, a.areaName()));
			}
			else if (!names.add(a.areaName())) {
				errs.add("%s: area-name \"%s\" ist nicht eindeutig".formatted(at, a.areaName()));
			}
			if (isEmpty(a.areaLabel())) {
				errs.add(at + ": area-label fehlt");
			}
			if (a.labelCombinations().isEmpty()) {
				errs.add(at + ": label-combinations dürfen nicht leer sein");
			}
			for (int j = 0; j < a.labelCombinations().size(); j++) {
				Map<String, String> combination = a.labelCombinations().get(j);
				if (combination == null || combination.isEmpty()) {
					errs.add("%s.label-combinations[%d]: leere Kombination würde alle Services treffen".formatted(at, j));
				}
			}
		}
		return errs;
	}

	private static List<String> validateLinks(List<LinkConfig> links, Set<String> projects) {
		List<String> errs = new ArrayList<>();
		Set<String> ids = new HashSet<>();
		for (int i = 0; i < links.size(); i++) {
			LinkConfig l = links.get(i);
			String at = "links[%d]".formatted(i);
			if (isEmpty(l.linkId())) {
				errs.add(at + ": link-id fehlt");
			}
			else if (!ids.add(l.linkId())) {
				errs.add("%s: link-id \"%s\" ist nicht eindeutig".formatted(at, l.linkId()));
			}
			if (isEmpty(l.title())) {
				errs.add(at + ": title fehlt");
			}
			errs.addAll(validateProjects(at, l.projects(), projects));
			if (l.targets().isEmpty()) {
				errs.add(at + ": targets dürfen nicht leer sein");
			}
			l.targets().forEach((kind, target) -> {
				if (Kind.parse(kind).isEmpty()) {
					errs.add("%s: targets: ungültiger Objekt-Typ \"%s\"".formatted(at, kind));
				}
				else if (target == null || isEmpty(target.url())) {
					errs.add("%s: targets.%s: url fehlt (Typ ohne Link einfach weglassen)".formatted(at, kind));
				}
			});
		}
		return errs;
	}

	private static List<String> validateCopyCommands(List<CopyCommandConfig> commands, Set<String> projects) {
		List<String> errs = new ArrayList<>();
		Set<String> ids = new HashSet<>();
		for (int i = 0; i < commands.size(); i++) {
			CopyCommandConfig c = commands.get(i);
			String at = "copy-commands[%d]".formatted(i);
			if (isEmpty(c.commandId())) {
				errs.add(at + ": command-id fehlt");
			}
			else if (!ids.add(c.commandId())) {
				errs.add("%s: command-id \"%s\" ist nicht eindeutig".formatted(at, c.commandId()));
			}
			if (isEmpty(c.title())) {
				errs.add(at + ": title fehlt");
			}
			errs.addAll(validateProjects(at, c.projects(), projects));
			if (c.targets().isEmpty()) {
				errs.add(at + ": targets dürfen nicht leer sein");
			}
			c.targets().forEach((kind, target) -> {
				if (Kind.parse(kind).isEmpty()) {
					errs.add("%s: targets: ungültiger Objekt-Typ \"%s\"".formatted(at, kind));
				}
				else if (target == null || isEmpty(target.command())) {
					errs.add("%s: targets.%s: command fehlt (Typ ohne Befehl einfach weglassen)".formatted(at, kind));
				}
			});
		}
		return errs;
	}

	/**
	 * Pflichtfeld projects von Links, Kopier-Befehlen und Aktionen: Es muss gesetzt sein (eine leere Liste heißt „in
	 * keinem Projekt“) und darf nur bekannte Projekte enthalten.
	 */
	private static List<String> validateProjects(String at, List<String> projects, Set<String> known) {
		if (projects == null) {
			return List.of(at + ": projects fehlt (leere Liste [] heißt: in keinem Projekt)");
		}
		List<String> errs = new ArrayList<>();
		for (String p : projects) {
			if (!known.contains(p)) {
				errs.add("%s: unbekanntes Projekt \"%s\"".formatted(at, p));
			}
		}
		return errs;
	}

	private static List<String> validateCluster(String dnsName) {
		if (isEmpty(dnsName)) {
			return List.of("cluster.dns-name fehlt (DNS-Name oder IP der Nodes für die NodePort-Adressen)");
		}
		if (!KubernetesNames.isIpLiteral(dnsName)
				&& !KubernetesNames.isDns1123Subdomain(dnsName.toLowerCase(Locale.ROOT))) {
			return List.of("cluster.dns-name \"%s\" ist weder ein gültiger DNS-Name noch eine IP (ohne Schema und Port angeben)"
				.formatted(dnsName));
		}
		return List.of();
	}

	private static List<String> validateTeams(TeamsConfig t) {
		if (isEmpty(t.labelKey()) && t.teams().isEmpty()) {
			return List.of();
		}
		List<String> errs = new ArrayList<>();
		if (isEmpty(t.labelKey())) {
			errs.add("teams.label-key fehlt");
		}
		else if (!KubernetesNames.isQualifiedName(t.labelKey())) {
			errs.add("teams.label-key \"%s\" ist kein gültiger Label-Schlüssel".formatted(t.labelKey()));
		}
		if (t.teams().isEmpty()) {
			errs.add("teams.teams darf nicht leer sein");
		}
		Set<String> ids = new HashSet<>();
		for (int i = 0; i < t.teams().size(); i++) {
			Team team = t.teams().get(i);
			String at = "teams.teams[%d]".formatted(i);
			if (isEmpty(team.teamId())) {
				errs.add(at + ": team-id fehlt");
			}
			else if (ids.contains(team.teamId())) {
				errs.add("%s: team-id \"%s\" ist nicht eindeutig".formatted(at, team.teamId()));
			}
			else if (!KubernetesNames.isValidLabelValue(team.teamId())) {
				errs.add("%s: team-id \"%s\" ist kein gültiger Label-Wert".formatted(at, team.teamId()));
			}
			ids.add(team.teamId());
			if (isEmpty(team.teamLabel())) {
				errs.add(at + ": team-label fehlt");
			}
		}
		return errs;
	}

	private static boolean isEmpty(String s) {
		return s == null || s.isEmpty();
	}

	/** Ungültige Konfiguration; die Meldung enthält alle Fehler, je Zeile einer. */
	public static class InvalidConfigException extends IllegalStateException {

		private final List<String> errors;

		public InvalidConfigException(List<String> errors) {
			super("Ungültige Konfiguration (dashboard.*):\n  - " + String.join("\n  - ", errors));
			this.errors = List.copyOf(errors);
		}

		public List<String> errors() {
			return errors;
		}
	}
}
