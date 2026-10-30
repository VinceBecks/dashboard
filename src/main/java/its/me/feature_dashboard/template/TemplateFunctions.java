package its.me.feature_dashboard.template;

import java.nio.charset.StandardCharsets;
import java.util.regex.Matcher;
import java.util.regex.Pattern;

/** Zusätzliche Template-Funktionen (als Pebble-Filter registriert, siehe {@link DashboardPebbleExtension}). */
public final class TemplateFunctions {

	private static final Pattern RENEW_NAMESPACE = Pattern
			.compile("^renew-(f-|if-|hf-)(story|hotfix|feat|bugfix|epic|intfix)([0-9]+).*$");

	private TemplateFunctions() {
	}

	/**
	 * Kodiert einen Pfadteil wie Go {@code url.PathEscape}: Buchstaben, Ziffern und {@code -_.~$&+:=@} bleiben, alles
	 * andere (auch {@code /} und Leerzeichen) wird als %XX kodiert.
	 */
	public static String pathEscape(String s) {
		StringBuilder sb = new StringBuilder();
		for (byte b : s.getBytes(StandardCharsets.UTF_8)) {
			char c = (char) (b & 0xff);
			if ((c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') || "-_.~$&+:=@".indexOf(c) >= 0) {
				sb.append(c);
			}
			else {
				sb.append('%').append(Character.toUpperCase(Character.forDigit((b >> 4) & 0xf, 16)))
					.append(Character.toUpperCase(Character.forDigit(b & 0xf, 16)));
			}
		}
		return sb.toString();
	}

	/** Rison-String, wie Kibana ihn in URLs erwartet: in Hochkommas, mit ! als Escape-Zeichen. */
	public static String rison(String s) {
		return "'" + s.replace("!", "!!").replace("'", "!'") + "'";
	}

	/** Setzt s für POSIX-Shells in einfache Anführungszeichen, sodass der Wert als genau ein Argument ankommt. */
	public static String shellQuote(String s) {
		return "'" + s.replace("'", "'\\''") + "'";
	}

	/** Entfernt prefix am Anfang von value; ohne dieses Präfix bleibt value unverändert. */
	public static String trimPrefix(String value, String prefix) {
		return value.startsWith(prefix) ? value.substring(prefix.length()) : value;
	}

	/**
	 * Extrahiert die JIRA-Ticketnummer aus einem Namespace-Namen der Form
	 * {@code renew-<f-|if-|hf-><story|hotfix|feat|bugfix|epic|intfix><nummer>…}, sonst "".
	 */
	public static String extractRenewJiraTicketNumber(String namespace) {
		Matcher m = RENEW_NAMESPACE.matcher(namespace);
		return m.matches() ? m.group(3) : "";
	}
}
