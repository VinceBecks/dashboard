package its.me.feature_dashboard.actions;

import com.fasterxml.jackson.annotation.JsonPropertyOrder;

/** Ergebnis einer Aktion; message ist für den Nutzer formuliert. */
@JsonPropertyOrder({ "success", "message" })
public record ActionResult(boolean success, String message) {

	public static ActionResult ok(String message) {
		return new ActionResult(true, message);
	}

	public static ActionResult failure(String message) {
		return new ActionResult(false, message);
	}
}
