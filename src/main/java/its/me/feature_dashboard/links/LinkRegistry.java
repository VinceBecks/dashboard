package its.me.feature_dashboard.links;

import java.net.URI;
import java.net.URISyntaxException;
import java.util.ArrayList;
import java.util.EnumMap;
import java.util.HashMap;
import java.util.List;
import java.util.Map;
import java.util.regex.Pattern;

import org.slf4j.Logger;
import org.slf4j.LoggerFactory;

import its.me.feature_dashboard.config.DashboardProperties;
import its.me.feature_dashboard.config.DashboardProperties.LinkConfig;
import its.me.feature_dashboard.model.Kind;
import its.me.feature_dashboard.model.ObjectRef;
import its.me.feature_dashboard.model.Stored;
import its.me.feature_dashboard.template.ObjectTemplate;
import its.me.feature_dashboard.template.ObjectTemplates;
import its.me.feature_dashboard.template.TemplateData;

/** Hält die beim Start geparsten Link-Templates und rendert die Links je Objekt. */
public class LinkRegistry {

	private static final Logger log = LoggerFactory.getLogger(LinkRegistry.class);

	/** Erlaubt, lange URL-Templates umzubrechen (YAML-Blockstil |-): Zeilenumbrüche samt Einrückung entfallen. */
	private static final Pattern LINE_BREAKS = Pattern.compile("[ \\t]*\\r?\\n[ \\t]*");

	private record Compiled(LinkConfig cfg, Map<Kind, ObjectTemplate> urls) {
	}

	private final List<Compiled> links = new ArrayList<>();
	private final Map<String, String> globalVars;
	private final Map<String, Map<String, String>> projectVars = new HashMap<>();

	/** Parst alle Link-Templates; Fehler brechen den Start ab. */
	public LinkRegistry(DashboardProperties cfg) {
		this.globalVars = cfg.linkVariables();
		cfg.projects().forEach(p -> projectVars.put(p.projectName(), p.linkVariablesOverrides()));
		List<String> errors = new ArrayList<>();
		for (LinkConfig l : cfg.links()) {
			if (l.projects() != null && l.projects().isEmpty()) {
				log.warn("Link ist in keinem Projekt verfügbar (projects ist leer): link={}", l.linkId());
			}
			Map<Kind, ObjectTemplate> urls = new EnumMap<>(Kind.class);
			l.targets().forEach((kind, target) -> {
				try {
					urls.put(Kind.parse(kind).orElseThrow(), ObjectTemplates.parse(
							"links.%s.targets.%s.url".formatted(l.linkId(), kind),
							LINE_BREAKS.matcher(target.url()).replaceAll("")));
				}
				catch (ObjectTemplates.TemplateException e) {
					errors.add(e.getMessage());
				}
			});
			links.add(new Compiled(l, urls));
		}
		if (!errors.isEmpty()) {
			throw new IllegalStateException(String.join("\n", errors));
		}
	}

	/** Links zu einem Objekt (nie null); nicht renderbare werden weggelassen und geloggt. */
	public List<Link> forObject(ObjectRef ref, Stored obj, String project) {
		List<Link> result = new ArrayList<>();
		for (Compiled c : links) {
			ObjectTemplate tpl = c.urls().get(ref.kind());
			if (tpl == null || c.cfg().projects() == null || !c.cfg().projects().contains(project)) {
				continue;
			}
			try {
				String url = render(tpl, ref, obj, project);
				result.add(new Link(c.cfg().linkId(), c.cfg().title(), nullToEmpty(c.cfg().icon()),
						nullToEmpty(c.cfg().iconColor()), url));
			}
			catch (RuntimeException e) {
				log.warn("Link konnte nicht erzeugt werden: link={} object={} error={}", c.cfg().linkId(), ref,
						e.getMessage());
			}
		}
		return result;
	}

	private String render(ObjectTemplate tpl, ObjectRef ref, Stored obj, String project) {
		Map<String, String> vars = new HashMap<>(globalVars);
		vars.putAll(projectVars.getOrDefault(project, Map.of()));
		String raw = ObjectTemplates.render(tpl, TemplateData.of(ref, obj, project).withVars(vars));
		URI uri;
		try {
			uri = new URI(raw);
		}
		catch (URISyntaxException e) {
			throw new IllegalArgumentException("ungültige URL. Raw String wert ist '%s': %s".formatted(raw, e.getMessage()));
		}
		if (!("http".equals(uri.getScheme()) || "https".equals(uri.getScheme())) || uri.getHost() == null
				|| uri.getHost().isEmpty()) {
			throw new IllegalArgumentException("nur absolute http(s)-URLs sind erlaubt, erhalten: \"%s\"".formatted(raw));
		}
		return raw;
	}

	static String nullToEmpty(String s) {
		return s == null ? "" : s;
	}
}
