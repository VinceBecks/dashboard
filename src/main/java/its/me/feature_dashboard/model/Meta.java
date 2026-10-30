package its.me.feature_dashboard.model;

import java.util.Map;

/** Gemeinsame Metadaten aller Stored-Objects. Labels und Annotations sind Kopien, nie null. */
public record Meta(String name, String namespace, Map<String, String> labels, Map<String, String> annotations) {

	public Meta {
		labels = SortedMaps.copyOf(labels);
		annotations = SortedMaps.copyOf(annotations);
	}
}
