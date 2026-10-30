package its.me.feature_dashboard.actions;

import static org.assertj.core.api.Assertions.assertThat;
import static org.assertj.core.api.Assertions.assertThatThrownBy;

import java.util.ArrayList;
import java.util.List;
import java.util.Map;

import org.junit.jupiter.api.Test;

import its.me.feature_dashboard.config.DashboardProperties.ActionHandlerConfig;
import its.me.feature_dashboard.model.Kind;
import its.me.feature_dashboard.model.ObjectRef;
import its.me.feature_dashboard.testsupport.TestSource;

class ActionRegistryTest {

	/** Handler-Typ für Tests: Aktion auf Pods mit konfigurierbaren Feldern. */
	static class FakeType implements ActionHandlerType {
		final List<String> configuredWith = new ArrayList<>();
		final List<Map<String, Object>> executed = new ArrayList<>();
		List<FormField> fields = List.of();

		@Override
		public String type() {
			return "fake";
		}

		@Override
		public ActionHandler create(HandlerDeps deps) {
			return new ActionHandler() {
				@Override
				public List<String> configure(ExtraConfig extra) {
					configuredWith.add(deps.actionName() + ":" + extra.keys());
					return extra.keys().contains("broken") ? List.of("kaputt") : List.of();
				}

				@Override
				public boolean isApplicable(ActionObject obj) {
					return obj.ref().kind() == Kind.POD;
				}

				@Override
				public List<FormField> formFields(ActionObject obj) {
					return fields;
				}

				@Override
				public ActionResult execute(ActionObject obj, Map<String, Object> inputs) {
					executed.add(inputs);
					return ActionResult.ok("ok");
				}
			};
		}
	}

	private static ActionHandlerConfig cfg(String name, String handler, List<String> projects) {
		return new ActionHandlerConfig(name, handler, "Titel " + name, "icon", "red", projects);
	}

	private final ActionObject pod = new ActionObject(new ObjectRef(Kind.POD, "feat-1", "web-1"),
			TestSource.pod("feat-1", "web-1", true, Map.of()), "foo");

	@Test
	void activationRespectsTypesProjectsAndConfigErrors() {
		FakeType type = new FakeType();
		ActionRegistry r = new ActionRegistry(List.of(type));
		List<String> errors = r.activate(List.of(cfg("a", "fake", List.of("foo")), cfg("b", "fake", List.of("bar")),
				cfg("off", "fake", List.of()), cfg("unknown", "nope", List.of("foo")), cfg("bad", "fake", List.of("foo"))),
				i -> ExtraConfig.of(i == 4 ? Map.of("broken", true) : Map.of()), new HandlerDeps(null, null, null, null, null));
		assertThat(errors).containsExactly("aktion \"unknown\": kein ActionHandler vom Typ \"nope\" registriert",
				"aktion \"bad\": kaputt");
		assertThat(type.configuredWith).containsExactly("a:[]", "b:[]", "bad:[broken]");
		assertThat(r.has("a")).isTrue();
		assertThat(r.has("off")).isFalse();
		assertThat(r.actionsFor(pod)).containsExactly(new Action("a", "Titel a", "icon", "red", List.of()));
		assertThat(r.title("a")).isEqualTo("Titel a");
		assertThat(r.title("x")).isEqualTo("x");
	}

	@Test
	void executeValidatesInputs() {
		FakeType type = new FakeType();
		type.fields = List.of(new FormField("reason", "Grund", FormField.TEXT, true, null, null),
				new FormField("count", "Anzahl", FormField.NUMBER, false, null, null),
				new FormField("color", "Farbe", FormField.SELECT, false, null, List.of(new FormField.Option("red", "Rot"))),
				new FormField("tags", "Tags", FormField.MULTI_SELECT, false, null, List.of(new FormField.Option("a", "A"))));
		ActionRegistry r = new ActionRegistry(List.of(type));
		r.activate(List.of(cfg("a", "fake", List.of("foo"))), i -> ExtraConfig.of(Map.of()),
				new HandlerDeps(null, null, null, null, null));

		assertThatThrownBy(() -> r.execute("a", pod, Map.of("count", "x", "color", "blue", "tags", List.of("b"))))
			.isInstanceOfSatisfying(ActionExceptions.ValidationException.class, e -> assertThat(e.problems())
				.containsExactly("reason: Pflichtfeld", "count: Zahl erwartet", "color: ungültige Auswahl", "tags: ungültige Auswahl"));
		assertThat(r.execute("a", pod, Map.of("reason", "x", "count", 2, "color", "red", "tags", List.of("a"))).success()).isTrue();
		assertThatThrownBy(() -> r.execute("nope", pod, Map.of())).isInstanceOf(ActionExceptions.UnknownActionException.class);
		ActionObject ns = new ActionObject(ObjectRef.namespace("feat-1"), pod.object(), "foo");
		assertThatThrownBy(() -> r.execute("a", ns, Map.of())).isInstanceOf(ActionExceptions.NotApplicableException.class);
		ActionObject otherProject = new ActionObject(pod.ref(), pod.object(), "bar");
		assertThatThrownBy(() -> r.execute("a", otherProject, Map.of())).isInstanceOf(ActionExceptions.NotApplicableException.class);
	}
}
