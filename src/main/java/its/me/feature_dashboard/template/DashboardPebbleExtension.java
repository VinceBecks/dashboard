package its.me.feature_dashboard.template;

import java.util.List;
import java.util.Map;
import java.util.function.BiFunction;
import java.util.function.Function;

import io.pebbletemplates.pebble.extension.AbstractExtension;
import io.pebbletemplates.pebble.extension.Filter;
import io.pebbletemplates.pebble.template.EvaluationContext;
import io.pebbletemplates.pebble.template.PebbleTemplate;

/**
 * Eigene Filter zusätzlich zu den eingebauten (urlencode, join, replace, …): {@code pathescape}, {@code rison},
 * {@code shellquote}, {@code trimPrefix(prefix)} und {@code extractRenewJiraTicketNumber}.
 */
class DashboardPebbleExtension extends AbstractExtension {

	@Override
	public Map<String, Filter> getFilters() {
		return Map.of(
				"pathescape", filter(TemplateFunctions::pathEscape),
				"rison", filter(TemplateFunctions::rison),
				"shellquote", filter(TemplateFunctions::shellQuote),
				"extractRenewJiraTicketNumber", filter(TemplateFunctions::extractRenewJiraTicketNumber),
				"trimPrefix", filter("prefix", TemplateFunctions::trimPrefix));
	}

	private static Filter filter(Function<String, String> fn) {
		return new Filter() {
			@Override
			public List<String> getArgumentNames() {
				return List.of();
			}

			@Override
			public Object apply(Object input, Map<String, Object> args, PebbleTemplate self, EvaluationContext context,
					int lineNumber) {
				return fn.apply(input == null ? "" : input.toString());
			}
		};
	}

	private static Filter filter(String argument, BiFunction<String, String, String> fn) {
		return new Filter() {
			@Override
			public List<String> getArgumentNames() {
				return List.of(argument);
			}

			@Override
			public Object apply(Object input, Map<String, Object> args, PebbleTemplate self, EvaluationContext context,
					int lineNumber) {
				Object arg = args.get(argument);
				return fn.apply(input == null ? "" : input.toString(), arg == null ? "" : arg.toString());
			}
		};
	}
}
