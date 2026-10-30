package its.me.feature_dashboard.actions;

import static org.assertj.core.api.Assertions.assertThat;
import static org.assertj.core.api.Assertions.assertThatThrownBy;

import java.util.ArrayList;
import java.util.HashMap;
import java.util.List;
import java.util.Map;

import org.junit.jupiter.api.AfterEach;
import org.junit.jupiter.api.BeforeEach;
import org.junit.jupiter.api.Test;

import its.me.feature_dashboard.actions.gitlabpipeline.GitlabPipelineHandlerType;
import its.me.feature_dashboard.actions.gitlabpipeline.Trigger;
import its.me.feature_dashboard.actions.gitlabpipeline.TriggerConfig;
import its.me.feature_dashboard.actions.missingservices.MissingServicesHandlerType;
import its.me.feature_dashboard.config.DashboardProperties.ActionHandlerConfig;
import its.me.feature_dashboard.model.Kind;
import its.me.feature_dashboard.model.ObjectRef;
import its.me.feature_dashboard.model.StoredNamespace;
import its.me.feature_dashboard.pipelines.PipelineEntry;
import its.me.feature_dashboard.pipelines.PipelineState;
import its.me.feature_dashboard.pipelines.PipelineTracker;
import its.me.feature_dashboard.testsupport.FakeGitLab;
import its.me.feature_dashboard.testsupport.TestSource;

class GitlabHandlersTest {

	private FakeGitLab gitlab;
	private final TestSource store = new TestSource();
	private final PipelineState pipelines = new PipelineState();

	@BeforeEach
	void start() throws Exception {
		gitlab = new FakeGitLab();
		store.namespace("foo-projekt-login", "foo-projekt", Map.of("app", "shop"));
	}

	@AfterEach
	void stop() {
		gitlab.close();
	}

	private Map<String, Object> gitlabConfig() {
		Map<String, Object> cfg = new HashMap<>();
		cfg.put("gitlab-url", gitlab.url());
		cfg.put("project", "team/foo-deploy");
		cfg.put("ref", "feature/{{ namespace }}");
		cfg.put("token", "glptt-secret");
		cfg.put("applies-to", List.of("namespace", "service"));
		cfg.put("variables", Map.of("NAMESPACE", "{{ namespace }}", "APP", "{{ labels['app'] }}",
				"FEATURE", "{{ namespace | trimPrefix(project ~ '-') }}", "REPLICAS", 2));
		return cfg;
	}

	private ActionRegistry registry(ActionHandlerType type, Map<String, Object> extra, List<String> errors) {
		ActionRegistry r = new ActionRegistry(List.of(type));
		errors.addAll(r.activate(List.of(new ActionHandlerConfig("deploy", type.type(), "Deployen", "", "", List.of("foo-projekt"))),
				i -> ExtraConfig.of(extra), new HandlerDeps(null, store, pipelines, null, null)));
		return r;
	}

	private ActionObject namespace() {
		StoredNamespace ns = (StoredNamespace) store.get(ObjectRef.namespace("foo-projekt-login")).orElseThrow();
		return new ActionObject(ObjectRef.namespace("foo-projekt-login"), ns, "foo-projekt");
	}

	@Test
	void gitlabPipelineTriggersWithRenderedValues() {
		List<String> errors = new ArrayList<>();
		ActionRegistry r = registry(new GitlabPipelineHandlerType(), gitlabConfig(), errors);
		assertThat(errors).isEmpty();
		var result = r.execute("deploy", namespace(), Map.of());
		assertThat(result).isEqualTo(ActionResult.ok("Pipeline #4711 gestartet: https://gitlab.example/p/-/pipelines/4711"));
		assertThat(gitlab.lastPath).isEqualTo("/api/v4/projects/team%2Ffoo-deploy/trigger/pipeline");
		assertThat(gitlab.lastForm).containsEntry("token", "glptt-secret")
			.containsEntry("ref", "feature/foo-projekt-login")
			.containsEntry("variables[NAMESPACE]", "foo-projekt-login")
			.containsEntry("variables[APP]", "shop")
			.containsEntry("variables[FEATURE]", "login")
			.containsEntry("variables[REPLICAS]", "2");
		// ohne status-token wird nichts gemerkt
		assertThat(pipelines.all()).isEmpty();
		// applies-to: nicht an Pods
		assertThat(r.actionsFor(new ActionObject(new ObjectRef(Kind.POD, "foo-projekt-login", "p"),
				TestSource.pod("foo-projekt-login", "p", true, Map.of()), "foo-projekt"))).isEmpty();
	}

	@Test
	void statusTokenTracksPipelineUntilFinished() {
		Map<String, Object> cfg = gitlabConfig();
		cfg.put("status-token", "glpat-read");
		List<String> errors = new ArrayList<>();
		ActionRegistry r = registry(new GitlabPipelineHandlerType(), cfg, errors);
		r.execute("deploy", namespace(), Map.of());
		List<PipelineEntry> running = pipelines.forRef(ObjectRef.namespace("foo-projekt-login"));
		assertThat(running).singleElement().satisfies(e -> {
			assertThat(e.id()).isEqualTo(4711);
			assertThat(e.action()).isEqualTo("deploy");
			assertThat(e.webUrl()).isEqualTo("https://gitlab.example/p/-/pipelines/4711");
		});
		PipelineTracker tracker = new PipelineTracker(pipelines, java.time.Duration.ofSeconds(5));
		gitlab.pipelineStatus(4711, "running");
		tracker.checkOnce();
		assertThat(pipelines.all()).hasSize(1);
		assertThat(gitlab.lastPrivateToken).isEqualTo("glpat-read");
		gitlab.pipelineStatus(4711, "success");
		tracker.checkOnce();
		assertThat(pipelines.all()).isEmpty();
	}

	@Test
	void gitlabErrorsAndTemplateErrorsBecomeFailures() {
		gitlab.respondWith(400, "{\"message\":{\"base\":[\"Reference not found\"]}}");
		List<String> errors = new ArrayList<>();
		ActionRegistry r = registry(new GitlabPipelineHandlerType(), gitlabConfig(), errors);
		var result = r.execute("deploy", namespace(), Map.of());
		assertThat(result.success()).isFalse();
		assertThat(result.message()).isEqualTo("GitLab antwortete mit 400: {\"base\":[\"Reference not found\"]}");

		Map<String, Object> cfg = gitlabConfig();
		cfg.put("variables", Map.of("X", "{{ vars.nope }}"));
		ActionRegistry broken = registry(new GitlabPipelineHandlerType(), cfg, errors);
		assertThat(broken.execute("deploy", namespace(), Map.of()).message()).startsWith("template variables.X");
	}

	@Test
	void configureErrors() {
		List<String> errors = new ArrayList<>();
		registry(new GitlabPipelineHandlerType(), Map.of("gitlab-url", "gitlab.example", "token-env", "X", "projects", List.of("a"),
				"applies-to", List.of("deployment"), "ref", "{{ broken"), errors);
		assertThat(errors).contains("aktion \"deploy\": gitlab-url muss eine absolute http(s)-URL sein",
				"aktion \"deploy\": project fehlt",
				"aktion \"deploy\": applies-to: ungültiger Objekt-Typ \"deployment\"",
				"aktion \"deploy\": projects gehört nicht in extra-handler-configs, sondern direkt an die Aktion (projects: [...])");
		assertThat(errors).anySatisfy(e -> assertThat(e).contains("token-env wird nicht mehr unterstützt"));
		assertThat(errors).anySatisfy(e -> assertThat(e).contains("token fehlt"));
		assertThat(errors).anySatisfy(e -> assertThat(e).contains("template ref"));
	}

	@Test
	void missingServices() {
		store.put(TestSource.service("foo-projekt-login", "foo-backend", "ClusterIP", Map.of("app", "b")));
		Map<String, Object> cfg = gitlabConfig();
		cfg.remove("applies-to");
		cfg.put("variables", Map.of("SERVICES", "{{ services | join(',') }}"));
		cfg.put("services", List.of(Map.of("name", "foo-backend", "label", "Foo Backend"), Map.of("name", "foo-frontend"),
				Map.of("name", "foo-worker", "label", "Worker")));
		List<String> errors = new ArrayList<>();
		ActionRegistry r = registry(new MissingServicesHandlerType(), cfg, errors);
		assertThat(errors).isEmpty();

		FormField field = r.actionsFor(namespace()).get(0).formFields().get(0);
		assertThat(field.description()).isEqualTo("Fehlende Services, die per Pipeline deployt werden");
		assertThat(field.options()).containsExactly(new FormField.Option("foo-frontend", "foo-frontend"),
				new FormField.Option("foo-worker", "Worker"));

		assertThatThrownBy(() -> r.execute("deploy", namespace(), Map.of("services", List.of("foo-backend"))))
			.isInstanceOf(ActionExceptions.ValidationException.class);
		var result = r.execute("deploy", namespace(), Map.of("services", List.of("foo-worker", "foo-frontend")));
		assertThat(result.success()).isTrue();
		assertThat(gitlab.lastForm).containsEntry("variables[SERVICES]", "foo-worker,foo-frontend");
	}

	@Test
	void triggerStartSendsInputs() {
		List<String> problems = new ArrayList<>();
		Trigger t = Trigger.create(new TriggerConfig(gitlab.url(), "team/scale-down", "main", "secret", null, null, null), problems);
		assertThat(problems).isEmpty();
		var p = t.start("main", Map.of(), Map.of("EXCLUDE_NAMESPACES", "a,b"));
		assertThat(p).isEqualTo(new Trigger.Pipeline(4711, "https://gitlab.example/p/-/pipelines/4711"));
		assertThat(gitlab.lastForm).containsEntry("inputs[EXCLUDE_NAMESPACES]", "a,b").containsEntry("ref", "main");
		assertThat(gitlab.lastForm.keySet()).noneMatch(k -> k.startsWith("variables["));
	}
}
