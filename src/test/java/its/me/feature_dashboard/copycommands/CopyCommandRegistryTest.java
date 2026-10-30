package its.me.feature_dashboard.copycommands;

import static org.assertj.core.api.Assertions.assertThat;
import static org.assertj.core.api.Assertions.assertThatThrownBy;

import java.util.List;
import java.util.Map;

import org.junit.jupiter.api.Test;

import its.me.feature_dashboard.model.Kind;
import its.me.feature_dashboard.model.ObjectRef;
import its.me.feature_dashboard.model.StoredNamespace;
import its.me.feature_dashboard.model.StoredPod;
import its.me.feature_dashboard.testsupport.ConfigFiles;
import its.me.feature_dashboard.testsupport.TestSource;

class CopyCommandRegistryTest {

	private static CopyCommandRegistry registry(String commands) {
		return new CopyCommandRegistry(ConfigFiles.bind(ConfigFiles.environment("""
				dashboard:
				  link-variables: {context: kind-demo}
				  projects:
				    - project-name: foo
				      namespace-labels: {a: b}
				  copy-commands:
				""" + commands)));
	}

	private static final String SHELL = """
			    - command-id: pod-shell
			      title: Shell
			      icon: terminal
			      projects: [foo]
			      targets:
			        pod: {command: "kubectl --context {{ vars.context }} exec -it -n {{ namespace }} {{ name }} -c {{ container }} -- sh"}
			        namespace: {command: "  kubectl config set-context --current --namespace={{ namespace }}  "}
			""";

	private static StoredPod pod(String... containers) {
		return new StoredPod(TestSource.meta("feat-1", "web-1", Map.of()), true,
				java.util.Arrays.stream(containers).map(c -> new StoredPod.Container(c, "img", List.of())).toList());
	}

	private final ObjectRef podRef = new ObjectRef(Kind.POD, "feat-1", "web-1");

	@Test
	void podWithOneContainer() {
		assertThat(registry(SHELL).forObject(podRef, pod("app"), "foo")).containsExactly(new CopyCommand("pod-shell", "Shell",
				"terminal", "", "kubectl --context kind-demo exec -it -n feat-1 web-1 -c app -- sh"));
	}

	@Test
	void podWithTwoContainersGetsOneCommandEach() {
		assertThat(registry(SHELL).forObject(podRef, pod("app", "sidecar"), "foo")).containsExactly(
				new CopyCommand("pod-shell/app", "Shell (app)", "terminal", "",
						"kubectl --context kind-demo exec -it -n feat-1 web-1 -c app -- sh"),
				new CopyCommand("pod-shell/sidecar", "Shell (sidecar)", "terminal", "",
						"kubectl --context kind-demo exec -it -n feat-1 web-1 -c sidecar -- sh"));
	}

	@Test
	void otherKindsAreTrimmedAndProjectsRespected() {
		var ns = new StoredNamespace(TestSource.meta(null, "feat-1", Map.of()), "foo");
		assertThat(registry(SHELL).forObject(ObjectRef.namespace("feat-1"), ns, "foo")).extracting(CopyCommand::command)
			.containsExactly("kubectl config set-context --current --namespace=feat-1");
		assertThat(registry(SHELL).forObject(ObjectRef.namespace("feat-1"), ns, "bar")).isEmpty();
	}

	@Test
	void invalidTemplateFailsAtStartup() {
		assertThatThrownBy(() -> registry("""
				    - command-id: broken
				      title: X
				      projects: [foo]
				      targets: {pod: {command: "{% if %}"}}
				""")).hasMessageContaining("copy-commands.broken.targets.pod.command");
	}
}
