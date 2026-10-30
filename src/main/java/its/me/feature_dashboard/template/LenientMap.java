package its.me.feature_dashboard.template;

import java.util.AbstractMap;
import java.util.Map;
import java.util.Set;

/**
 * Map für Labels, Annotations und Selector in Templates: Ein fehlender Schlüssel liefert "" statt eines Fehlers (wie
 * {@code index .Labels "x"} in Go). Pebble meldet im Strict-Mode sonst jeden fehlenden Map-Schlüssel als Fehler; das
 * soll nur für {@code vars} gelten.
 */
final class LenientMap extends AbstractMap<String, String> {

	private final Map<String, String> delegate;

	LenientMap(Map<String, String> delegate) {
		this.delegate = delegate;
	}

	@Override
	public boolean containsKey(Object key) {
		return true;
	}

	@Override
	public String get(Object key) {
		return delegate.getOrDefault(key, "");
	}

	@Override
	public Set<Entry<String, String>> entrySet() {
		return delegate.entrySet();
	}
}
