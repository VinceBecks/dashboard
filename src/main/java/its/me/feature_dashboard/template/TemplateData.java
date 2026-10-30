package its.me.feature_dashboard.template;

import java.util.HashMap;
import java.util.LinkedHashMap;
import java.util.List;
import java.util.Map;
import java.util.TreeMap;

import its.me.feature_dashboard.model.ObjectRef;
import its.me.feature_dashboard.model.Stored;
import its.me.feature_dashboard.model.StoredService;

/**
 * Werte, die in Templates zur Verfügung stehen, z. B. {@code {{ namespace }}}, {@code {{ labels['app'] }}} oder
 * {@code {{ vars.kibanaUrl }}}.
 *
 * @param selector nur bei Services gefüllt
 * @param vars setzt der Aufrufer (link-variables bei Links und Kopier-Befehlen)
 * @param services setzt der Aufrufer (gewählte Services bei deploy-missing-services), sonst leer
 * @param container setzt der Aufrufer bei Kopier-Befehlen für Pods (je Container ein Befehl), sonst leer
 */
public record TemplateData(String kind, String name, String namespace, String project, Map<String, String> labels,
		Map<String, String> annotations, Map<String, String> selector, Map<String, String> vars, List<String> services,
		String container) {

	/** Template-Daten zu einem Objekt. */
	public static TemplateData of(ObjectRef ref, Stored obj, String project) {
		Map<String, String> selector = obj instanceof StoredService svc ? svc.selector() : Map.of();
		return new TemplateData(ref.kind().value(), ref.name(), ref.namespace(), project, obj.meta().labels(),
				obj.meta().annotations(), selector, Map.of(), List.of(), "");
	}

	public TemplateData withVars(Map<String, String> vars) {
		return new TemplateData(kind, name, namespace, project, labels, annotations, selector, vars, services, container);
	}

	public TemplateData withServices(List<String> services) {
		return new TemplateData(kind, name, namespace, project, labels, annotations, selector, vars,
				List.copyOf(services), container);
	}

	public TemplateData withContainer(String container) {
		return new TemplateData(kind, name, namespace, project, labels, annotations, selector, vars, services, container);
	}

	/** Kontext für Pebble. */
	Map<String, Object> toContext() {
		Map<String, Object> ctx = new HashMap<>();
		ctx.put("kind", kind);
		ctx.put("name", name);
		ctx.put("namespace", namespace);
		ctx.put("project", project);
		ctx.put("labels", new LenientMap(labels));
		ctx.put("annotations", new LenientMap(annotations));
		ctx.put("selector", new LenientMap(selector));
		ctx.put("vars", new LinkedHashMap<>(new TreeMap<>(vars)));
		ctx.put("services", services);
		ctx.put("container", container);
		return ctx;
	}
}
