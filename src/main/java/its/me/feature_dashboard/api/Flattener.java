package its.me.feature_dashboard.api;

import java.util.ArrayList;
import java.util.List;

import its.me.feature_dashboard.api.Dtos.NamespaceDto;
import its.me.feature_dashboard.api.Dtos.ObjectHeader;
import tools.jackson.databind.ObjectMapper;

/** Zerlegt einen Namespace-Teilbaum in einzelne Objekte ohne Kind-Listen, Parent vor Kind, als JSON. */
final class Flattener {

	/** Objekt des Teilbaums, so wie es in einem upsert-Event verschickt wird. */
	record FlatObject(String key, String parentKey, ObjectHeader header, String data) {
	}

	private Flattener() {
	}

	/** Identifiziert ein Objekt eindeutig über Typ, Parents und Name. */
	static String key(ObjectHeader h) {
		return String.join("\u0000", h.kind(), h.parents().project(), nullToEmpty(h.parents().namespace()),
				nullToEmpty(h.parents().service()), h.name());
	}

	static List<FlatObject> flatten(NamespaceDto ns, ObjectMapper json) {
		List<FlatObject> result = new ArrayList<>();
		String nsKey = add(result, ns.header(), "", ns.withoutChildren(), json);
		for (var svc : ns.services()) {
			String svcKey = add(result, svc.header(), nsKey, svc.withoutChildren(), json);
			for (var pod : svc.pods()) {
				add(result, pod.header(), svcKey, pod, json);
			}
		}
		for (var np : ns.nodePortServices()) {
			add(result, np.header(), nsKey, np, json);
		}
		for (var ing : ns.ingresses()) {
			add(result, ing.header(), nsKey, ing, json);
		}
		return result;
	}

	private static String add(List<FlatObject> result, ObjectHeader header, String parentKey, Object value,
			ObjectMapper json) {
		String key = key(header);
		result.add(new FlatObject(key, parentKey, header, json.writeValueAsString(value)));
		return key;
	}

	private static String nullToEmpty(String s) {
		return s == null ? "" : s;
	}
}
