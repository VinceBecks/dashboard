package its.me.feature_dashboard.config;

import static org.assertj.core.api.Assertions.assertThat;

import java.time.Duration;
import java.util.List;
import java.util.Map;

import org.junit.jupiter.api.Test;
import org.springframework.boot.context.properties.bind.Binder;
import org.springframework.core.env.StandardEnvironment;

import its.me.feature_dashboard.actions.ActionRegistry;
import its.me.feature_dashboard.actions.ExtraConfig;
import its.me.feature_dashboard.actions.HandlerDeps;
import its.me.feature_dashboard.actions.assignteam.AssignTeamHandlerType;
import its.me.feature_dashboard.actions.deletepod.DeletePodHandlerType;
import its.me.feature_dashboard.actions.gitlabpipeline.GitlabPipelineHandlerType;
import its.me.feature_dashboard.actions.missingservices.MissingServicesHandlerType;
import its.me.feature_dashboard.actions.nightlyscaledown.NightlyScaleDownHandlerType;
import its.me.feature_dashboard.actions.nodeportservice.CreateNodePortServiceHandlerType;
import its.me.feature_dashboard.actions.nodeportservice.DeleteNodePortServiceHandlerType;
import its.me.feature_dashboard.actions.restartservice.RestartServiceHandlerType;
import its.me.feature_dashboard.links.LinkRegistry;
import its.me.feature_dashboard.copycommands.CopyCommandRegistry;
import its.me.feature_dashboard.testsupport.ConfigFiles;

class DashboardPropertiesBindingTest {

	@Test
	void exampleConfigBindsAndIsValid() {
		DashboardProperties p = ConfigFiles.bind(ConfigFiles.exampleEnvironment());
		assertThat(DashboardConfigValidator.validate(p)).isEmpty();

		var foo = p.projects().get(0);
		assertThat(foo.namespaceLabels()).containsEntry("mycomp.de/is-feature-environment", "true")
			.containsEntry("mycomp.de/project-name", "foo-projekt");
		assertThat(foo.serviceAreas().get(1).labelCombinations()).containsExactly(
				Map.of("app.kubernetes.io/part-of", "rabbitmq"), Map.of("app.kubernetes.io/part-of", "database"));
		assertThat(p.linkVariables()).containsEntry("demoUrl", "http://localhost:8080");
		assertThat(p.projects().get(2).linkVariablesOverrides()).containsEntry("something", "isNotHere");
		assertThat(p.teams().labelKey()).isEqualTo("mycomp.de/team");
		assertThat(p.cluster().dnsName()).isEqualTo("demo-cluster-control-plane");
		assertThat(p.pipelines().pollInterval()).isEqualTo(Duration.ofSeconds(15));
		assertThat(p.links().get(0).targets()).containsOnlyKeys("namespace", "service");
	}

	@Test
	void exampleConfigActivatesAllActions() {
		StandardEnvironment env = ConfigFiles.exampleEnvironment();
		DashboardProperties p = ConfigFiles.bind(env);
		ActionRegistry registry = new ActionRegistry(List.of(new DeletePodHandlerType(),
				new CreateNodePortServiceHandlerType(), new DeleteNodePortServiceHandlerType(),
				new RestartServiceHandlerType(), new GitlabPipelineHandlerType(), new MissingServicesHandlerType(),
				new AssignTeamHandlerType(), new NightlyScaleDownHandlerType()));
		Binder binder = Binder.get(env);
		List<String> errors = registry.activate(p.actionHandlers(),
				i -> new ExtraConfig(binder, "dashboard.action-handlers[%d].extra-handler-configs".formatted(i)),
				new HandlerDeps(null, null, null, null, p.teams()));
		assertThat(errors).isEmpty();
		for (var a : p.actionHandlers()) {
			assertThat(registry.has(a.actionName())).as(a.actionName()).isTrue();
		}
		new LinkRegistry(p);
		new CopyCommandRegistry(p);
	}

	@Test
	void templateVariableKeysKeepTheirCase() {
		StandardEnvironment env = ConfigFiles.exampleEnvironment();
		ExtraConfig extra = new ExtraConfig(Binder.get(env), "dashboard.action-handlers[8].extra-handler-configs");
		assertThat(extra.keys()).contains("gitlab-url", "token", "variables", "applies-to");
		@SuppressWarnings("unchecked")
		Map<String, Object> variables = (Map<String, Object>) Binder.get(env)
			.bind("dashboard.action-handlers[8].extra-handler-configs.variables", Map.class)
			.get();
		assertThat(variables).containsKeys("NAMESPACE", "ORGANIZATION_UNIT", "FEATURE");
	}

	@Test
	void keysWithoutSpecialCharactersKeepTheirCaseWithoutBrackets() {
		DashboardProperties p = ConfigFiles.bind(ConfigFiles.environment("""
				dashboard:
				  link-variables:
				    demoUrl: https://x
				    KIBANA_URL: https://k
				"""));
		assertThat(p.linkVariables()).containsOnlyKeys("demoUrl", "KIBANA_URL");
	}

	@Test
	void escapedPlaceholderStaysLiteral() {
		DashboardProperties p = ConfigFiles.bind(ConfigFiles.environment("""
				dashboard:
				  copy-commands:
				    - command-id: home
				      targets:
				        pod: {command: 'echo \\${HOME}'}
				"""));
		assertThat(p.copyCommands().get(0).targets().get("pod").command()).isEqualTo("echo ${HOME}");
	}

	@Test
	void missingProjectsDifferFromEmptyList() {
		DashboardProperties p = ConfigFiles.bind(ConfigFiles.environment("""
				dashboard:
				  action-handlers:
				    - action-name: a
				    - action-name: b
				      projects: []
				"""));
		assertThat(p.actionHandlers().get(0).projects()).isNull();
		assertThat(p.actionHandlers().get(1).projects()).isEmpty();
	}

	@Test
	void unresolvedTokenPlaceholderIsAnError() {
		StandardEnvironment env = ConfigFiles.environment(new org.springframework.core.io.FileSystemResource(
				"config-example/application.yml"), Map.of());
		DashboardProperties p = ConfigFiles.bind(env);
		ActionRegistry registry = new ActionRegistry(List.of(new GitlabPipelineHandlerType(),
				new MissingServicesHandlerType(), new DeletePodHandlerType(), new CreateNodePortServiceHandlerType(),
				new DeleteNodePortServiceHandlerType(), new RestartServiceHandlerType(), new AssignTeamHandlerType(),
				new NightlyScaleDownHandlerType()));
		Binder binder = Binder.get(env);
		List<String> errors = registry.activate(p.actionHandlers(),
				i -> new ExtraConfig(binder, "dashboard.action-handlers[%d].extra-handler-configs".formatted(i)),
				new HandlerDeps(null, null, null, null, p.teams()));
		assertThat(errors).anySatisfy(e -> assertThat(e).contains("deploy-missing-services").contains("GITLAB_TRIGGER_TOKEN_FOO"));
	}
}
