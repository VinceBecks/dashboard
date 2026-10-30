package its.me.feature_dashboard.api;

import static org.springframework.test.web.servlet.request.MockMvcRequestBuilders.patch;
import static org.springframework.test.web.servlet.result.MockMvcResultMatchers.content;
import static org.springframework.test.web.servlet.result.MockMvcResultMatchers.status;

import java.util.List;
import java.util.Map;

import org.junit.jupiter.api.Test;
import org.springframework.http.MediaType;
import org.springframework.test.web.servlet.MockMvc;
import org.springframework.test.web.servlet.setup.MockMvcBuilders;

import its.me.feature_dashboard.actions.ActionHandler;
import its.me.feature_dashboard.actions.ActionHandlerType;
import its.me.feature_dashboard.actions.ActionObject;
import its.me.feature_dashboard.actions.ActionRegistry;
import its.me.feature_dashboard.actions.ActionResult;
import its.me.feature_dashboard.actions.ExtraConfig;
import its.me.feature_dashboard.actions.FormField;
import its.me.feature_dashboard.actions.HandlerDeps;
import its.me.feature_dashboard.config.DashboardProperties.ActionHandlerConfig;
import its.me.feature_dashboard.model.Kind;
import its.me.feature_dashboard.testsupport.TestSource;
import tools.jackson.databind.json.JsonMapper;

/** Statuscodes und Bodies von PATCH /api/actions/{action} (wie TestAction im Go-Backend). */
class ActionsControllerTest {

	private String fail;

	private final MockMvc mvc;

	ActionsControllerTest() {
		TestSource source = new TestSource();
		source.namespace("feat-1", "foo").put(TestSource.pod("feat-1", "web-1", true, Map.of()));
		ActionHandlerType restart = new ActionHandlerType() {
			@Override
			public String type() {
				return "restart";
			}

			@Override
			public ActionHandler create(HandlerDeps deps) {
				return new ActionHandler() {
					@Override
					public boolean isApplicable(ActionObject obj) {
						return obj.ref().kind() == Kind.POD;
					}

					@Override
					public List<FormField> formFields(ActionObject obj) {
						return List.of(new FormField("reason", "Grund", FormField.TEXT, true, null, null));
					}

					@Override
					public ActionResult execute(ActionObject obj, Map<String, Object> inputs) {
						if ("boom".equals(inputs.get("reason"))) {
							throw new IllegalStateException("kaputt");
						}
						return fail == null ? ActionResult.ok("ok") : ActionResult.failure(fail);
					}
				};
			}
		};
		ActionRegistry registry = new ActionRegistry(List.of(restart));
		registry.activate(List.of(new ActionHandlerConfig("restart", null, "Neustart", "", "", List.of("foo"))),
				i -> ExtraConfig.of(Map.of()), new HandlerDeps(null, source, null, null, null));
		mvc = MockMvcBuilders.standaloneSetup(new ActionsController(registry, source, JsonMapper.builder().build())).build();
	}

	private org.springframework.test.web.servlet.ResultActions call(String action, String body) throws Exception {
		return mvc.perform(patch("/api/actions/" + action).contentType(MediaType.APPLICATION_JSON).content(body));
	}

	private static final String POD = "{\"object\":{\"kind\":\"pod\",\"namespace\":\"feat-1\",\"name\":\"web-1\"},\"inputs\":{\"reason\":\"test\"}}";

	@Test
	void statusCodes() throws Exception {
		call("nope", POD).andExpect(status().isNotFound())
			.andExpect(content().json("{\"error\":\"unbekannte oder deaktivierte Aktion\"}"));
		call("restart", "{").andExpect(status().isBadRequest()).andExpect(content().json("{\"error\":\"ungültiger Request-Body\"}"));
		call("restart", "{\"object\":{\"kind\":\"pod\",\"namespace\":\"feat-1\",\"name\":\"x\"}}").andExpect(status().isNotFound())
			.andExpect(content().json("{\"error\":\"objekt nicht gefunden\"}"));
		call("restart", "{\"object\":{\"kind\":\"deployment\",\"namespace\":\"feat-1\",\"name\":\"x\"}}")
			.andExpect(status().isBadRequest())
			.andExpect(content().json("{\"error\":\"ungültiger Objekt-Typ\"}"));
		call("restart", "{\"object\":{\"kind\":\"namespace\",\"name\":\"feat-1\"}}").andExpect(status().isBadRequest())
			.andExpect(content().json("{\"error\":\"aktion ist auf diesem Objekt nicht ausführbar\"}"));
		call("restart", "{\"object\":{\"kind\":\"pod\",\"namespace\":\"feat-1\",\"name\":\"web-1\"}}")
			.andExpect(status().isBadRequest())
			.andExpect(content().json("{\"error\":\"ungültige Eingaben: [reason: Pflichtfeld]\",\"problems\":[\"reason: Pflichtfeld\"]}"));
		call("restart", POD).andExpect(status().isOk())
			.andExpect(content().string("{\"success\":true,\"message\":\"ok\"}"));
	}

	@Test
	void failedActionIsNever200() throws Exception {
		fail = "Pod web-1 konnte nicht gelöscht werden";
		call("restart", POD).andExpect(status().isUnprocessableContent())
			.andExpect(content().json("{\"error\":\"Pod web-1 konnte nicht gelöscht werden\"}"));
		fail = "";
		call("restart", POD).andExpect(status().isUnprocessableContent())
			.andExpect(content().json("{\"error\":\"Aktion konnte nicht ausgeführt werden\"}"));
		fail = null;
		call("restart", POD.replace("test", "boom")).andExpect(status().isInternalServerError())
			.andExpect(content().json("{\"error\":\"kaputt\"}"));
	}
}
