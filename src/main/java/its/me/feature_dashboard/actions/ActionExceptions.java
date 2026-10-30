package its.me.feature_dashboard.actions;

import java.util.List;

/** Fehler beim Ausführen einer Aktion, die die API in Statuscodes übersetzt. */
public final class ActionExceptions {

	private ActionExceptions() {
	}

	/** Aktion existiert nicht oder ist nicht aktiviert (404). */
	public static class UnknownActionException extends RuntimeException {
		public UnknownActionException() {
			super("unbekannte oder deaktivierte Aktion");
		}
	}

	/** Aktion ist auf dem Objekt nicht ausführbar (400). */
	public static class NotApplicableException extends RuntimeException {
		public NotApplicableException() {
			super("aktion ist auf diesem Objekt nicht ausführbar");
		}
	}

	/** Ungültige Formular-Eingaben (400 mit problems). */
	public static class ValidationException extends RuntimeException {

		private final List<String> problems;

		public ValidationException(List<String> problems) {
			super("ungültige Eingaben: " + problems);
			this.problems = List.copyOf(problems);
		}

		public List<String> problems() {
			return problems;
		}
	}
}
