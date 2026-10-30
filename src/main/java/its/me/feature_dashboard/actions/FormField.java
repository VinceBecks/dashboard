package its.me.feature_dashboard.actions;

import java.util.List;

import com.fasterxml.jackson.annotation.JsonInclude;
import com.fasterxml.jackson.annotation.JsonProperty;
import com.fasterxml.jackson.annotation.JsonPropertyOrder;

/** Zusätzliche Eingabe, die eine Aktion vom Nutzer benötigt. */
@JsonPropertyOrder({ "id", "label", "type", "isRequired", "description", "options" })
public record FormField(String id, String label, String type, @JsonProperty("isRequired") boolean isRequired,
		@JsonInclude(JsonInclude.Include.NON_EMPTY) String description,
		@JsonInclude(JsonInclude.Include.NON_EMPTY) List<Option> options) {

	public static final String TEXT = "text";
	public static final String SELECT = "select";
	public static final String MULTI_SELECT = "multi-select";
	public static final String NUMBER = "number";

	/** Auswahlwert für select- und multi-select-Felder. */
	@JsonPropertyOrder({ "value", "label" })
	public record Option(String value, String label) {
	}
}
