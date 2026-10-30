package its.me.feature_dashboard.template;

import java.io.IOException;
import java.io.StringWriter;

import io.pebbletemplates.pebble.PebbleEngine;
import io.pebbletemplates.pebble.error.PebbleException;
import io.pebbletemplates.pebble.loader.StringLoader;

/**
 * Pebble-Templates mit den Daten eines Stored-Objects (Links, Kopier-Befehle, GitLab-ref und -Variablen). Fehlende
 * Variablen und fehlende {@code vars}-Schlüssel sind beim Rendern ein Fehler; fehlende Labels, Annotations und
 * Selector-Einträge liefern "". Kein HTML-Escaping.
 */
public final class ObjectTemplates {

	private static final PebbleEngine ENGINE = new PebbleEngine.Builder()
		.loader(new StringLoader())
		.strictVariables(true)
		.autoEscaping(false)
		.newLineTrimming(false)
		.cacheActive(false)
		.extension(new DashboardPebbleExtension())
		.build();

	private ObjectTemplates() {
	}

	/** Parst ein Template; Syntaxfehler brechen den Start ab. */
	public static ObjectTemplate parse(String name, String text) {
		try {
			return new ObjectTemplate(name, ENGINE.getLiteralTemplate(text));
		}
		catch (PebbleException e) {
			throw new TemplateException("template %s: %s".formatted(name, e.getMessage()), e);
		}
	}

	/** Rendert ein Template mit den Daten eines Objekts. */
	public static String render(ObjectTemplate template, TemplateData data) {
		StringWriter out = new StringWriter();
		try {
			template.template().evaluate(out, data.toContext());
		}
		catch (PebbleException | IOException e) {
			throw new TemplateException("template %s: %s".formatted(template.name(), e.getMessage()), e);
		}
		return out.toString();
	}

	/** Fehler beim Parsen oder Rendern eines Templates. */
	public static class TemplateException extends RuntimeException {
		public TemplateException(String message, Throwable cause) {
			super(message, cause);
		}
	}
}
