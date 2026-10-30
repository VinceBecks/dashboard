package its.me.feature_dashboard.links;

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

class LinkRegistryTest {

	private static LinkRegistry registry(String links) {
		return new LinkRegistry(ConfigFiles.bind(ConfigFiles.environment("""
				dashboard:
				  link-variables:
				    "[kibana]": https://kibana.example
				  projects:
				    - project-name: foo
				      namespace-labels: {a: b}
				    - project-name: bar
				      namespace-labels: {a: c}
				      link-variables-overrides:
				        "[kibana]": https://other.example
				  links:
				""" + links)));
	}

	private static final StoredPod POD = TestSource.pod("feat-1", "web-1", true, Map.of());

	@Test
	void kibanaLinkPerKindWithLineBreaksAndOverrides() {
		var r = registry("""
				    - link-id: kibana-logs
				      title: Logs
				      icon: scroll-text
				      icon-color: "#00bfb3"
				      projects: [foo, bar]
				      targets:
				        pod:
				          url: |-
				            {{ vars.kibana }}/app/discover#/
				              ?_a=(query:{{ ('kubernetes.pod.name:' ~ name) | rison | urlencode }})
				""");
		var ref = new ObjectRef(Kind.POD, "feat-1", "web-1");
		assertThat(r.forObject(ref, POD, "foo")).containsExactly(new Link("kibana-logs", "Logs", "scroll-text", "#00bfb3",
				"https://kibana.example/app/discover#/?_a=(query:%27kubernetes.pod.name%3Aweb-1%27)"));
		assertThat(r.forObject(ref, POD, "bar").get(0).url()).startsWith("https://other.example/");
		// Typ ohne Target und Projekt ohne Link: kein Link
		assertThat(r.forObject(ObjectRef.namespace("feat-1"),
				new StoredNamespace(TestSource.meta(null, "feat-1", Map.of()), "foo"), "foo")).isEmpty();
		assertThat(r.forObject(ref, POD, "baz")).isEmpty();
	}

	@Test
	void brokenLinksAreOmitted() {
		var r = registry("""
				    - link-id: missing-var
				      title: X
				      projects: [foo]
				      targets: {pod: {url: "{{ vars.nope }}/x"}}
				    - link-id: relative
				      title: X
				      projects: [foo]
				      targets: {pod: {url: "/relative/{{ name }}"}}
				    - link-id: ok
				      title: X
				      projects: [foo]
				      targets: {pod: {url: "https://x.example/{{ name | pathescape }}"}}
				""");
		assertThat(r.forObject(new ObjectRef(Kind.POD, "feat-1", "web-1"), POD, "foo")).extracting(Link::id)
			.containsExactly("ok");
	}

	@Test
	void invalidTemplateFailsAtStartup() {
		assertThatThrownBy(() -> registry("""
				    - link-id: broken
				      title: X
				      projects: [foo]
				      targets: {pod: {url: "{{ name "}}
				""")).hasMessageContaining("links.broken.targets.pod.url");
	}

	@Test
	void noLinksGivesEmptyList() {
		assertThat(registry("    []\n").forObject(new ObjectRef(Kind.POD, "feat-1", "web-1"), POD, "foo"))
			.isEqualTo(List.of());
	}
}
