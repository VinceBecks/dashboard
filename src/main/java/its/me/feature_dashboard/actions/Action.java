package its.me.feature_dashboard.actions;

import java.util.List;

import com.fasterxml.jackson.annotation.JsonInclude;
import com.fasterxml.jackson.annotation.JsonPropertyOrder;

/** Beschreibung einer auf einem Objekt ausführbaren Aktion für das Frontend. */
@JsonPropertyOrder({ "id", "title", "icon", "iconColor", "formFields" })
public record Action(String id, String title, String icon, String iconColor,
		@JsonInclude(JsonInclude.Include.NON_EMPTY) List<FormField> formFields) {
}
