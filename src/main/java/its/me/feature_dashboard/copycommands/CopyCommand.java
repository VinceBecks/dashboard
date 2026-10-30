package its.me.feature_dashboard.copycommands;

import com.fasterxml.jackson.annotation.JsonPropertyOrder;

/** Fertig gerenderter Befehl, den das Frontend in die Zwischenablage kopiert. */
@JsonPropertyOrder({ "id", "title", "icon", "iconColor", "command" })
public record CopyCommand(String id, String title, String icon, String iconColor, String command) {
}
