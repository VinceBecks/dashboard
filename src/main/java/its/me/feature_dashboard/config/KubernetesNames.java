package its.me.feature_dashboard.config;

import java.util.regex.Pattern;

/**
 * Prüfungen für Kubernetes-Namen, entsprechend {@code k8s.io/apimachinery/pkg/util/validation}.
 */
public final class KubernetesNames {

	private static final String NAME_PART = "([A-Za-z0-9][-A-Za-z0-9_.]*)?[A-Za-z0-9]";
	private static final Pattern NAME_PART_PATTERN = Pattern.compile(NAME_PART);
	private static final Pattern DNS1123_SUBDOMAIN = Pattern
			.compile("[a-z0-9]([-a-z0-9]*[a-z0-9])?(\\.[a-z0-9]([-a-z0-9]*[a-z0-9])?)*");
	private static final Pattern DNS1035_LABEL = Pattern.compile("[a-z]([-a-z0-9]*[a-z0-9])?");
	private static final Pattern IPV4 = Pattern
			.compile("((25[0-5]|2[0-4]\\d|1\\d\\d|[1-9]?\\d)\\.){3}(25[0-5]|2[0-4]\\d|1\\d\\d|[1-9]?\\d)");

	private KubernetesNames() {
	}

	/** Gültiger Label-Schlüssel ({@code [prefix/]name}). */
	public static boolean isQualifiedName(String value) {
		if (value == null || value.isEmpty()) {
			return false;
		}
		String name = value;
		int slash = value.indexOf('/');
		if (slash >= 0) {
			String prefix = value.substring(0, slash);
			name = value.substring(slash + 1);
			if (prefix.isEmpty() || !isDns1123Subdomain(prefix)) {
				return false;
			}
		}
		return !name.isEmpty() && name.length() <= 63 && NAME_PART_PATTERN.matcher(name).matches();
	}

	/** Gültiger Label-Wert (leer erlaubt). */
	public static boolean isValidLabelValue(String value) {
		if (value == null || value.isEmpty()) {
			return true;
		}
		return value.length() <= 63 && NAME_PART_PATTERN.matcher(value).matches();
	}

	public static boolean isDns1123Subdomain(String value) {
		return value != null && value.length() <= 253 && DNS1123_SUBDOMAIN.matcher(value).matches();
	}

	/** Gültiger Service-Name. */
	public static boolean isDns1035Label(String value) {
		return value != null && value.length() <= 63 && DNS1035_LABEL.matcher(value).matches();
	}

	/** IPv4- oder IPv6-Literal, ohne DNS-Auflösung. */
	public static boolean isIpLiteral(String value) {
		if (value == null || value.isEmpty()) {
			return false;
		}
		if (IPV4.matcher(value).matches()) {
			return true;
		}
		if (!value.contains(":") || !value.matches("[0-9A-Fa-f:.]+")) {
			return false;
		}
		try {
			// Für Literale mit ':' löst InetAddress nicht per DNS auf, sondern parst nur.
			return java.net.InetAddress.getByName(value) instanceof java.net.Inet6Address;
		}
		catch (java.net.UnknownHostException e) {
			return false;
		}
	}
}
