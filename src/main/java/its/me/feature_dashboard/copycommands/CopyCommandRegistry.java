package its.me.feature_dashboard.copycommands;

import java.util.ArrayList;
import java.util.EnumMap;
import java.util.HashMap;
import java.util.List;
import java.util.Map;

import org.slf4j.Logger;
import org.slf4j.LoggerFactory;

import its.me.feature_dashboard.config.DashboardProperties;
import its.me.feature_dashboard.config.DashboardProperties.CopyCommandConfig;
import its.me.feature_dashboard.model.Kind;
import its.me.feature_dashboard.model.ObjectRef;
import its.me.feature_dashboard.model.Stored;
import its.me.feature_dashboard.model.StoredPod;
import its.me.feature_dashboard.template.ObjectTemplate;
import its.me.feature_dashboard.template.ObjectTemplates;
import its.me.feature_dashboard.template.TemplateData;

/** Hält die beim Start geparsten Befehls-Templates und rendert die Kopier-Befehle je Objekt. */
public class CopyCommandRegistry {

	private static final Logger log = LoggerFactory.getLogger(CopyCommandRegistry.class);

	private record Compiled(CopyCommandConfig cfg, Map<Kind, ObjectTemplate> commands) {
	}

	private final List<Compiled> commands = new ArrayList<>();
	private final Map<String, String> globalVars;
	private final Map<String, Map<String, String>> projectVars = new HashMap<>();

	/** Parst alle Befehls-Templates; Fehler brechen den Start ab. */
	public CopyCommandRegistry(DashboardProperties cfg) {
		this.globalVars = cfg.linkVariables();
		cfg.projects().forEach(p -> projectVars.put(p.projectName(), p.linkVariablesOverrides()));
		List<String> errors = new ArrayList<>();
		for (CopyCommandConfig c : cfg.copyCommands()) {
			if (c.projects() != null && c.projects().isEmpty()) {
				log.warn("Kopier-Befehl ist in keinem Projekt verfügbar (projects ist leer): command={}", c.commandId());
			}
			Map<Kind, ObjectTemplate> templates = new EnumMap<>(Kind.class);
			c.targets().forEach((kind, target) -> {
				try {
					templates.put(Kind.parse(kind).orElseThrow(), ObjectTemplates
						.parse("copy-commands.%s.targets.%s.command".formatted(c.commandId(), kind), target.command()));
				}
				catch (ObjectTemplates.TemplateException e) {
					errors.add(e.getMessage());
				}
			});
			commands.add(new Compiled(c, templates));
		}
		if (!errors.isEmpty()) {
			throw new IllegalStateException(String.join("\n", errors));
		}
	}

	/**
	 * Befehle zu einem Objekt (nie null). Bei Pods wird je Container gerendert: Nutzt das Template {@code container},
	 * entsteht je Container ein eigener Befehl. Nicht renderbare Befehle werden weggelassen und geloggt.
	 */
	public List<CopyCommand> forObject(ObjectRef ref, Stored obj, String project) {
		List<CopyCommand> result = new ArrayList<>();
		Map<String, String> vars = new HashMap<>(globalVars);
		vars.putAll(projectVars.getOrDefault(project, Map.of()));
		TemplateData data = TemplateData.of(ref, obj, project).withVars(vars);
		List<String> containers = new ArrayList<>();
		if (obj instanceof StoredPod pod && !pod.containers().isEmpty()) {
			pod.containers().forEach(c -> containers.add(c.name()));
		}
		else {
			containers.add("");
		}
		for (Compiled c : commands) {
			ObjectTemplate tpl = c.commands().get(ref.kind());
			if (tpl == null || c.cfg().projects() == null || !c.cfg().projects().contains(project)) {
				continue;
			}
			try {
				result.addAll(commands(c.cfg(), containers, renderPerContainer(tpl, data, containers)));
			}
			catch (RuntimeException e) {
				log.warn("Kopier-Befehl konnte nicht erzeugt werden: command={} object={} error={}", c.cfg().commandId(),
						ref, e.getMessage());
			}
		}
		return result;
	}

	private static List<String> renderPerContainer(ObjectTemplate tpl, TemplateData data, List<String> containers) {
		List<String> rendered = new ArrayList<>();
		for (String name : containers) {
			String cmd = ObjectTemplates.render(tpl, data.withContainer(name)).strip();
			if (cmd.isEmpty()) {
				throw new IllegalArgumentException("befehl ist leer");
			}
			rendered.add(cmd);
		}
		return rendered;
	}

	/** Einer, wenn alle gleich sind (das Template nutzt container nicht), sonst je Container einer. */
	private static List<CopyCommand> commands(CopyCommandConfig cfg, List<String> containers, List<String> rendered) {
		String icon = cfg.icon() == null ? "" : cfg.icon();
		String color = cfg.iconColor() == null ? "" : cfg.iconColor();
		if (rendered.stream().allMatch(r -> r.equals(rendered.get(0)))) {
			return List.of(new CopyCommand(cfg.commandId(), cfg.title(), icon, color, rendered.get(0)));
		}
		List<CopyCommand> result = new ArrayList<>();
		for (int i = 0; i < rendered.size(); i++) {
			result.add(new CopyCommand(cfg.commandId() + "/" + containers.get(i),
					cfg.title() + " (" + containers.get(i) + ")", icon, color, rendered.get(i)));
		}
		return result;
	}
}
