package its.me.feature_dashboard.api;

import static org.assertj.core.api.Assertions.assertThat;
import static org.assertj.core.api.Assertions.assertThatThrownBy;

import java.time.Duration;
import java.time.Instant;
import java.util.ArrayList;
import java.util.List;
import java.util.Map;
import java.util.Optional;

import org.junit.jupiter.api.Test;

import its.me.feature_dashboard.actions.ActionHandler;
import its.me.feature_dashboard.actions.ActionHandlerType;
import its.me.feature_dashboard.actions.ActionObject;
import its.me.feature_dashboard.actions.ActionRegistry;
import its.me.feature_dashboard.actions.ActionResult;
import its.me.feature_dashboard.actions.ExtraConfig;
import its.me.feature_dashboard.actions.FormField;
import its.me.feature_dashboard.actions.HandlerDeps;
import its.me.feature_dashboard.api.EventHub.Event;
import its.me.feature_dashboard.api.EventHub.Subscription;
import its.me.feature_dashboard.config.DashboardProperties;
import its.me.feature_dashboard.config.DashboardProperties.ActionHandlerConfig;
import its.me.feature_dashboard.config.DashboardProperties.Project;
import its.me.feature_dashboard.config.DashboardProperties.Team;
import its.me.feature_dashboard.config.DashboardProperties.TeamsConfig;
import its.me.feature_dashboard.copycommands.CopyCommandRegistry;
import its.me.feature_dashboard.links.LinkRegistry;
import its.me.feature_dashboard.model.Kind;
import its.me.feature_dashboard.model.StoredIngress;
import its.me.feature_dashboard.model.StoredService;
import its.me.feature_dashboard.pipelines.PipelineEntry;
import its.me.feature_dashboard.pipelines.PipelineState;
import its.me.feature_dashboard.model.ObjectRef;
import its.me.feature_dashboard.testsupport.ConfigFiles;
import its.me.feature_dashboard.testsupport.TestSource;
import tools.jackson.databind.json.JsonMapper;

/**
 * Vertragstests: Snapshot und Events müssen dem Go-Backend entsprechen (erwartete Strings aus
 * backend/internal/api/server_test.go).
 */
class EventHubTest {

	/** Aktion „restart“ auf Pods mit Pflichtfeld „reason“ (wie podAction im Go-Test). */
	static class PodAction implements ActionHandlerType {
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
					return ActionResult.ok("ok");
				}
			};
		}
	}

	final TestSource source = new TestSource();
	final PipelineState pipelines = new PipelineState();
	EventHub hub;

	EventHubTest() {
		source.namespace("feat-1", "foo")
			.put(TestSource.service("feat-1", "web", "ClusterIP", Map.of("app", "web")))
			.put(TestSource.pod("feat-1", "web-1", false, Map.of("app", "web")));
		hub = hub("""
				dashboard:
				  cluster: {dns-name: nodes.example}
				  projects:
				    - project-name: foo
				      namespace-labels: {a: b}
				    - project-name: bar
				      namespace-labels: {a: c}
				""", null);
	}

	private EventHub hub(String yaml, TeamsConfig teams) {
		DashboardProperties p = ConfigFiles.bind(ConfigFiles.environment(yaml));
		ActionRegistry registry = new ActionRegistry(List.of(new PodAction()));
		registry.activate(List.of(new ActionHandlerConfig("restart", null, "Neustart", "rotate-ccw", null, List.of("foo"))),
				i -> ExtraConfig.of(Map.of()), new HandlerDeps(null, source, pipelines, null, null));
		TeamsConfig t = teams == null ? p.teams() : teams;
		NamespaceDtoBuilder builder = new NamespaceDtoBuilder(registry, new LinkRegistry(p), new CopyCommandRegistry(p),
				pipelines, p.cluster().dnsName(), p.projects(), t);
		return new EventHub(source, pipelines, builder::build, p.projects(), t, JsonMapper.builder().build());
	}

	private List<String> sync(Subscription sub) throws InterruptedException {
		hub.syncNow();
		List<String> got = new ArrayList<>();
		for (Optional<Event> ev = sub.poll(Duration.ZERO); ev.isPresent(); ev = sub.poll(Duration.ZERO)) {
			got.add(ev.get().type() + " " + ev.get().data());
		}
		return got;
	}

	@Test
	void snapshot() {
		source.put(new StoredService(TestSource.meta("feat-1", "shop-node-port", Map.of()), "NodePort", Map.of(),
				List.of(TestSource.port("http", 80, "80", 30080), TestSource.port("intern", 9090, "9090", 0),
						TestSource.port("admin", 8081, "8081", 30081)),
				List.of()))
			.put(TestSource.service("feat-1", "lb", "LoadBalancer", Map.of()))
			.put(TestSource.service("feat-1", "worker", "ClusterIP", Map.of()))
			.put(new StoredIngress(TestSource.meta("feat-1", "web", Map.of()),
					List.of(new StoredIngress.Rule("feat-1.example",
							List.of(new StoredIngress.Path("", "/", ""), new StoredIngress.Path("", "/api", ""),
									new StoredIngress.Path("", "/", ""))),
							new StoredIngress.Rule("", List.of(new StoredIngress.Path("", "/ohne-host", ""))))));
		String raw = hub.snapshot();
		assertThat(raw).startsWith("{\"teams\":[],\"projects\":[{\"name\":\"foo\",\"serviceAreas\":[{\"id\":\"others\",\"label\":\"Andere\"}],\"namespaces\":[")
			.endsWith(",{\"name\":\"bar\",\"serviceAreas\":[{\"id\":\"others\",\"label\":\"Andere\"}],\"namespaces\":[]}]}")
			.contains(
					"{\"kind\":\"namespace\",\"name\":\"feat-1\",\"parents\":{\"project\":\"foo\"},\"actions\":[],\"links\":[],\"copyCommands\":[],\"pipelines\":[],\"services\":[",
					"{\"kind\":\"pod\",\"name\":\"web-1\",\"parents\":{\"project\":\"foo\",\"namespace\":\"feat-1\",\"service\":\"web\"},\"ready\":false,\"containers\":[],\"actions\":[{\"id\":\"restart\"",
					"\"formFields\":[{\"id\":\"reason\"",
					"{\"kind\":\"ingress\",\"name\":\"web\",\"parents\":{\"project\":\"foo\",\"namespace\":\"feat-1\"},\"urls\":[\"https://feat-1.example/\",\"https://feat-1.example/api\"],\"actions\":[],\"links\":[],\"copyCommands\":[],\"pipelines\":[]}",
					"\"pods\":[]",
					"\"nodePortServices\":[{\"kind\":\"node-port-service\",\"name\":\"shop-node-port\",\"parents\":{\"project\":\"foo\",\"namespace\":\"feat-1\"},\"addresses\":[\"nodes.example:30080\",\"nodes.example:30081\"],\"actions\":[],\"links\":[],\"copyCommands\":[],\"pipelines\":[]}]")
			.doesNotContain("labels")
			.doesNotContain("rules")
			.doesNotContain("\"name\":\"lb\"");
	}

	@Test
	void events() throws Exception {
		Subscription sub = hub.subscribe();
		assertThat(sync(sub)).isEmpty();

		// Pod wird ready → upsert für Service (running) und Pod, Parent zuerst.
		source.put(TestSource.pod("feat-1", "web-1", true, Map.of("app", "web")));
		assertThat(sync(sub)).containsExactly(
				"upsert {\"kind\":\"service\",\"name\":\"web\",\"parents\":{\"project\":\"foo\",\"namespace\":\"feat-1\"},\"running\":true,\"areas\":[\"others\"],\"actions\":[],\"links\":[],\"copyCommands\":[],\"pipelines\":[]}",
				"upsert {\"kind\":\"pod\",\"name\":\"web-1\",\"parents\":{\"project\":\"foo\",\"namespace\":\"feat-1\",\"service\":\"web\"},\"ready\":true,\"containers\":[],\"actions\":[{\"id\":\"restart\",\"title\":\"Neustart\",\"icon\":\"rotate-ccw\",\"iconColor\":\"\",\"formFields\":[{\"id\":\"reason\",\"label\":\"Grund\",\"type\":\"text\",\"isRequired\":true}]}],\"links\":[],\"copyCommands\":[],\"pipelines\":[]}");

		// NodePort-Service angelegt und wieder gelöscht.
		source.put(new StoredService(TestSource.meta("feat-1", "web-node-port", Map.of()), "NodePort", Map.of(),
				List.of(TestSource.port("", 80, "80", 30080)), List.of()));
		assertThat(sync(sub)).containsExactly(
				"upsert {\"kind\":\"node-port-service\",\"name\":\"web-node-port\",\"parents\":{\"project\":\"foo\",\"namespace\":\"feat-1\"},\"addresses\":[\"nodes.example:30080\"],\"actions\":[],\"links\":[],\"copyCommands\":[],\"pipelines\":[]}");
		source.remove(StoredService.class, "feat-1", "web-node-port");
		assertThat(sync(sub)).containsExactly(
				"delete {\"kind\":\"node-port-service\",\"name\":\"web-node-port\",\"parents\":{\"project\":\"foo\",\"namespace\":\"feat-1\"}}");

		// Service gelöscht → nur delete für den Service, der Pod fällt implizit mit weg.
		source.remove(StoredService.class, "feat-1", "web");
		assertThat(sync(sub)).containsExactly(
				"delete {\"kind\":\"service\",\"name\":\"web\",\"parents\":{\"project\":\"foo\",\"namespace\":\"feat-1\"}}");

		// Projektwechsel → delete im alten, upsert im neuen Projekt.
		source.namespace("feat-1", "bar");
		List<String> got = sync(sub);
		assertThat(got).hasSize(2);
		assertThat(got.get(0)).isEqualTo("delete {\"kind\":\"namespace\",\"name\":\"feat-1\",\"parents\":{\"project\":\"foo\"}}");
		assertThat(got.get(1)).startsWith("upsert {\"kind\":\"namespace\",\"name\":\"feat-1\",\"parents\":{\"project\":\"bar\"}");

		// Namespace entfernt → ein delete.
		source.removeNamespace("feat-1");
		assertThat(sync(sub)).containsExactly("delete {\"kind\":\"namespace\",\"name\":\"feat-1\",\"parents\":{\"project\":\"bar\"}}");
	}

	@Test
	void slowSubscriberIsDropped() throws Exception {
		Subscription sub = hub.subscribe();
		for (int i = 0; i <= EventHub.SUBSCRIBER_BUFFER; i++) {
			source.put(TestSource.pod("feat-1", "web-1", i % 2 == 0, Map.of("app", "web")));
			hub.syncNow();
		}
		assertThatThrownBy(() -> sub.poll(Duration.ZERO)).isInstanceOf(EventHub.ClosedException.class);
	}

	@Test
	void pipelinesAndTeams() throws Exception {
		hub = hub("""
				dashboard:
				  cluster: {dns-name: nodes.example}
				  projects:
				    - project-name: foo
				      namespace-labels: {a: b}
				""", new TeamsConfig("mycomp.de/team", List.of(new Team("checkout", "Team Checkout", "#2e7d32"))));
		Subscription sub = hub.subscribe();
		assertThat(sub.snapshot()).startsWith("{\"teams\":[{\"id\":\"checkout\",\"label\":\"Team Checkout\",\"teamColor\":\"#2e7d32\"}],")
			.doesNotContain("\"team\":");

		pipelines.set(new PipelineEntry(Kind.NAMESPACE, "feat-1", "feat-1", "restart", 4711, "p",
				"https://gitlab.example/p/-/pipelines/4711", Instant.parse("2026-10-06T14:00:00Z")));
		List<String> got = sync(sub);
		assertThat(got).singleElement().asString().startsWith("upsert {\"kind\":\"namespace\",\"name\":\"feat-1\"")
			.contains("\"pipelines\":[{\"action\":\"restart\",\"title\":\"Neustart\",\"url\":\"https://gitlab.example/p/-/pipelines/4711\",\"startedAt\":\"2026-10-06T14:00:00Z\"}]");
		pipelines.remove(ObjectRef.namespace("feat-1"), "restart");
		assertThat(sync(sub)).singleElement().asString().contains("\"pipelines\":[]");

		source.namespace("feat-1", "foo", Map.of("mycomp.de/team", "checkout"));
		assertThat(sync(sub)).singleElement().asString()
			.startsWith("upsert {\"kind\":\"namespace\",\"name\":\"feat-1\",\"parents\":{\"project\":\"foo\"},\"team\":\"checkout\",");
		source.namespace("feat-1", "foo", Map.of("mycomp.de/team", "unbekannt"));
		assertThat(sync(sub)).singleElement().asString().doesNotContain("\"team\":");
	}

	@Test
	void serviceAreasInSnapshotAndServices() {
		hub = hub("""
				dashboard:
				  cluster: {dns-name: nodes.example}
				  projects:
				    - project-name: foo
				      namespace-labels: {a: b}
				      others-area-label: Sonstige
				      service-areas:
				        - area-name: shop
				          area-label: Shop
				          label-combinations: [{"[app.kubernetes.io/part-of]": shop}]
				""", null);
		source.put(new StoredService(TestSource.meta("feat-1", "cart", Map.of("app.kubernetes.io/part-of", "shop")),
				"ClusterIP", Map.of(), List.of(), List.of()));
		assertThat(hub.snapshot())
			.contains("\"serviceAreas\":[{\"id\":\"shop\",\"label\":\"Shop\"},{\"id\":\"others\",\"label\":\"Sonstige\"}]")
			.contains("\"name\":\"cart\",\"parents\":{\"project\":\"foo\",\"namespace\":\"feat-1\"},\"running\":false,\"areas\":[\"shop\"]");
	}

	@Test
	void fullResyncWithoutChangesSendsNothing() throws Exception {
		Subscription sub = hub.subscribe();
		source.namespace("feat-1", "foo"); // neue Revision, gleicher Inhalt
		assertThat(sync(sub)).isEmpty();
	}
}
