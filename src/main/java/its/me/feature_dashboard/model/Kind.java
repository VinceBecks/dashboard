package its.me.feature_dashboard.model;

import java.util.Optional;

/** Die vier beobachteten Objekt-Typen; {@link #value()} ist der Name in Config und API. */
public enum Kind {
	NAMESPACE("namespace"), SERVICE("service"), INGRESS("ingress"), POD("pod");

	private final String value;

	Kind(String value) {
		this.value = value;
	}

	public String value() {
		return value;
	}

	/** Typ zu einem Namen aus Config oder API, leer bei unbekanntem Namen. */
	public static Optional<Kind> parse(String value) {
		for (Kind k : values()) {
			if (k.value.equals(value)) {
				return Optional.of(k);
			}
		}
		return Optional.empty();
	}

	@Override
	public String toString() {
		return value;
	}
}
