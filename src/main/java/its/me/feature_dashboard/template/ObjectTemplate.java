package its.me.feature_dashboard.template;

import io.pebbletemplates.pebble.template.PebbleTemplate;

/** Ein beim Start geparstes Template. */
public record ObjectTemplate(String name, PebbleTemplate template) {
}
