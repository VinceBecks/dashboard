package its.me.feature_dashboard.model;

import java.util.Collections;
import java.util.Map;
import java.util.TreeMap;

/** Unveränderliche, nach Schlüssel sortierte Kopien; Templates iterieren Maps damit wie Go in Schlüssel-Reihenfolge. */
public final class SortedMaps {

	private SortedMaps() {
	}

	public static Map<String, String> copyOf(Map<String, String> map) {
		return map == null || map.isEmpty() ? Map.of() : Collections.unmodifiableMap(new TreeMap<>(map));
	}
}
