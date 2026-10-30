package its.me.feature_dashboard.links;

import com.fasterxml.jackson.annotation.JsonPropertyOrder;

/** Fertig gerenderter Link für das Frontend. */
@JsonPropertyOrder({ "id", "title", "icon", "iconColor", "url" })
public record Link(String id, String title, String icon, String iconColor, String url) {
}
