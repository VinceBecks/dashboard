package its.me.feature_dashboard.actions;

import java.util.LinkedHashMap;
import java.util.List;
import java.util.Map;
import java.util.Set;

import org.springframework.boot.context.properties.bind.Bindable;
import org.springframework.boot.context.properties.bind.Binder;
import org.springframework.boot.context.properties.source.MapConfigurationPropertySource;

/**
 * Die {@code extra-handler-configs} einer Aktion. Jeder Handler bindet sie typisiert (Records mit kebab-case-Feldern,
 * Spring Relaxed Binding) und kann alte, nicht mehr unterstützte Schlüssel erkennen.
 */
public final class ExtraConfig {

	private final Binder binder;
	private final String prefix;

	public ExtraConfig(Binder binder, String prefix) {
		this.binder = binder;
		this.prefix = prefix;
	}

	/** extra-handler-configs aus einer Map (für Tests); Listen und verschachtelte Maps werden wie YAML gebunden. */
	public static ExtraConfig of(Map<String, ?> values) {
		Map<String, Object> flat = new LinkedHashMap<>();
		flatten("extra", values, flat);
		return new ExtraConfig(new Binder(new MapConfigurationPropertySource(flat)), "extra");
	}

	private static void flatten(String prefix, Object value, Map<String, Object> out) {
		if (value instanceof Map<?, ?> map) {
			map.forEach((k, v) -> flatten(prefix + "." + k, v, out));
		}
		else if (value instanceof List<?> list) {
			for (int i = 0; i < list.size(); i++) {
				flatten(prefix + "[" + i + "]", list.get(i), out);
			}
		}
		else if (value != null) {
			out.put(prefix, value.toString());
		}
	}

	/** Bindet die Konfiguration an type; fehlt sie ganz, wird ein leerer Wert gebunden. */
	public <T> T bind(Class<T> type) {
		return binder.bindOrCreate(prefix, Bindable.of(type));
	}

	/** Schlüssel der obersten Ebene, z. B. um veraltete Angaben zu erkennen. */
	public Set<String> keys() {
		return binder.bind(prefix, Bindable.mapOf(String.class, Object.class)).map(Map::keySet).orElse(Set.of());
	}
}
