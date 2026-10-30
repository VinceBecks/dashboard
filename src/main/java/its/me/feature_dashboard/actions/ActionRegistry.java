package its.me.feature_dashboard.actions;

import java.util.ArrayList;
import java.util.HashMap;
import java.util.List;
import java.util.Map;
import java.util.Optional;
import java.util.function.IntFunction;

import org.slf4j.Logger;
import org.slf4j.LoggerFactory;

import its.me.feature_dashboard.actions.ActionExceptions.NotApplicableException;
import its.me.feature_dashboard.actions.ActionExceptions.UnknownActionException;
import its.me.feature_dashboard.actions.ActionExceptions.ValidationException;
import its.me.feature_dashboard.config.DashboardProperties.ActionHandlerConfig;

/** Kennt alle Handler-Typen und die aktivierten Aktionen; prüft Projekt, Anwendbarkeit und Eingaben. */
public class ActionRegistry {

	private static final Logger log = LoggerFactory.getLogger(ActionRegistry.class);

	private record ActiveAction(ActionHandlerConfig cfg, ActionHandler handler) {
		boolean applicable(ActionObject obj) {
			return cfg.projects().contains(obj.project()) && handler.isApplicable(obj);
		}
	}

	private final Map<String, ActionHandlerType> types = new HashMap<>();
	private final List<ActiveAction> active = new ArrayList<>();

	public ActionRegistry(List<? extends ActionHandlerType> types) {
		types.forEach(t -> this.types.put(t.type(), t));
	}

	/**
	 * Erzeugt und konfiguriert die Handler aller Aktionen, die in mindestens einem Projekt angeboten werden. Je Aktion
	 * entsteht eine eigene Instanz. Bei Aktionen mit {@code projects: []} wird nur geprüft, ob der Handler-Typ existiert.
	 *
	 * @param extra liefert die extra-handler-configs der Aktion mit diesem Index
	 * @return Fehler, die den Start abbrechen
	 */
	public List<String> activate(List<ActionHandlerConfig> cfgs, IntFunction<ExtraConfig> extra, HandlerDeps deps) {
		List<String> errors = new ArrayList<>();
		for (int i = 0; i < cfgs.size(); i++) {
			ActionHandlerConfig c = cfgs.get(i);
			ActionHandlerType type = types.get(c.handlerType());
			if (type == null) {
				errors.add("aktion \"%s\": kein ActionHandler vom Typ \"%s\" registriert".formatted(c.actionName(),
						c.handlerType()));
				continue;
			}
			if (!c.active()) {
				log.info("Aktion ist deaktiviert (projects ist leer): action={}", c.actionName());
				continue;
			}
			ActionHandler handler = type.create(deps.withActionName(c.actionName()));
			List<String> problems;
			try {
				problems = handler.configure(extra.apply(i));
			}
			catch (RuntimeException e) {
				problems = List.of("extra-handler-configs: " + e.getMessage());
			}
			if (!problems.isEmpty()) {
				problems.forEach(p -> errors.add("aktion \"%s\": %s".formatted(c.actionName(), p)));
				continue;
			}
			active.add(new ActiveAction(c, handler));
		}
		return errors;
	}

	/** Alle auf obj ausführbaren Aktionen (nie null). */
	public List<Action> actionsFor(ActionObject obj) {
		List<Action> result = new ArrayList<>();
		for (ActiveAction a : active) {
			if (a.applicable(obj)) {
				result.add(new Action(a.cfg().actionName(), nullToEmpty(a.cfg().actionTitle()),
						nullToEmpty(a.cfg().icon()), nullToEmpty(a.cfg().iconColor()), a.handler().formFields(obj)));
			}
		}
		return result;
	}

	/** action-title einer aktiven Aktion, sonst der Name selbst. */
	public String title(String name) {
		return find(name).map(a -> a.cfg().actionTitle()).filter(t -> !t.isEmpty()).orElse(name);
	}

	/** Meldet, ob die Aktion aktiviert ist. */
	public boolean has(String name) {
		return find(name).isPresent();
	}

	/** Prüft Anwendbarkeit und Eingaben und führt die Aktion aus. */
	public ActionResult execute(String name, ActionObject obj, Map<String, Object> inputs) {
		ActiveAction a = find(name).orElseThrow(UnknownActionException::new);
		if (!a.applicable(obj)) {
			throw new NotApplicableException();
		}
		Map<String, Object> in = inputs == null ? Map.of() : inputs;
		validate(a.handler().formFields(obj), in);
		return a.handler().execute(obj, in);
	}

	private Optional<ActiveAction> find(String name) {
		return active.stream().filter(a -> a.cfg().actionName().equals(name)).findFirst();
	}

	static void validate(List<FormField> fields, Map<String, Object> inputs) {
		List<String> problems = new ArrayList<>();
		for (FormField f : fields == null ? List.<FormField>of() : fields) {
			Object v = inputs.get(f.id());
			if (v == null || "".equals(v)) {
				if (f.isRequired()) {
					problems.add(f.id() + ": Pflichtfeld");
				}
				continue;
			}
			String msg = checkType(f, v);
			if (msg != null) {
				problems.add(f.id() + ": " + msg);
			}
		}
		if (!problems.isEmpty()) {
			throw new ValidationException(problems);
		}
	}

	private static String checkType(FormField f, Object v) {
		List<FormField.Option> options = f.options() == null ? List.of() : f.options();
		java.util.function.Predicate<Object> allowed = s -> s instanceof String str
				&& options.stream().anyMatch(o -> o.value().equals(str));
		return switch (f.type()) {
			case FormField.TEXT -> v instanceof String ? null : "Text erwartet";
			case FormField.NUMBER -> v instanceof Number ? null : "Zahl erwartet";
			case FormField.SELECT -> allowed.test(v) ? null : "ungültige Auswahl";
			case FormField.MULTI_SELECT -> {
				if (!(v instanceof List<?> list)) {
					yield "Liste erwartet";
				}
				if (f.isRequired() && list.isEmpty()) {
					yield "Pflichtfeld";
				}
				yield list.stream().allMatch(allowed) ? null : "ungültige Auswahl";
			}
			default -> null;
		};
	}

	private static String nullToEmpty(String s) {
		return s == null ? "" : s;
	}
}
