package its.me.feature_dashboard.config;

import static org.assertj.core.api.Assertions.assertThat;

import java.util.List;

import org.junit.jupiter.api.Test;

import its.me.feature_dashboard.testsupport.ConfigFiles;

class DashboardConfigValidatorTest {

	private static List<String> errors(String yaml) {
		return DashboardConfigValidator.validate(ConfigFiles.bind(ConfigFiles.environment(yaml)));
	}

	private static final String BASE = """
			dashboard:
			  cluster: {dns-name: nodes.example}
			  projects:
			    - project-name: foo
			      namespace-labels: {a: b}
			""";

	@Test
	void validMinimalConfig() {
		assertThat(errors(BASE)).isEmpty();
	}

	@Test
	void projectsMustBeUniqueAndHaveLabels() {
		assertThat(errors("""
				dashboard:
				  cluster: {dns-name: nodes.example}
				  projects:
				    - project-name: a
				      namespace-labels: {x: y}
				    - project-name: a
				    - namespace-labels: {x: y}
				""")).containsExactlyInAnyOrder("projects[1]: project-name \"a\" ist nicht eindeutig",
						"projects[1]: namespace-labels dürfen nicht leer sein", "projects[2]: project-name fehlt");
	}

	@Test
	void clusterDnsName() {
		assertThat(errors("dashboard:\n  projects: []\n")).containsExactly(
				"cluster.dns-name fehlt (DNS-Name oder IP der Nodes für die NodePort-Adressen)");
		assertThat(errors("dashboard:\n  cluster: {dns-name: 10.0.0.1}\n")).isEmpty();
		assertThat(errors("dashboard:\n  cluster: {dns-name: '::1'}\n")).isEmpty();
		assertThat(errors("dashboard:\n  cluster: {dns-name: 'https://nodes:80'}\n")).hasSize(1);
	}

	@Test
	void actionProjectsAreRequiredAndMustBeKnown() {
		assertThat(errors(BASE + """
				  action-handlers:
				    - action-name: x
				      projects: [foo]
				    - action-name: y
				      projects: []
				""")).isEmpty();
		assertThat(errors(BASE + """
				  action-handlers:
				    - action-name: x
				    - action-name: x
				      projects: [nope]
				""")).containsExactly("action-handlers[0]: projects fehlt (leere Liste [] heißt: in keinem Projekt)",
						"action-handlers[1]: action-name \"x\" ist nicht eindeutig",
						"action-handlers[1]: unbekanntes Projekt \"nope\"");
	}

	@Test
	void serviceAreas() {
		assertThat(errors(BASE.replace("namespace-labels: {a: b}", """
				namespace-labels: {a: b}
				      service-areas:
				        - area-name: others
				          area-label: X
				          label-combinations: [{a: b}]
				        - area-name: shop
				          label-combinations: [{}]
				        - area-name: shop
				          area-label: Shop
				"""))).containsExactlyInAnyOrder("projects[0].service-areas[0]: area-name \"others\" ist reserviert",
						"projects[0].service-areas[1]: area-label fehlt",
						"projects[0].service-areas[1].label-combinations[0]: leere Kombination würde alle Services treffen",
						"projects[0].service-areas[2]: area-name \"shop\" ist nicht eindeutig",
						"projects[0].service-areas[2]: label-combinations dürfen nicht leer sein");
	}

	@Test
	void linksAndCopyCommands() {
		assertThat(errors(BASE + """
				  links:
				    - link-id: l
				      title: L
				      projects: [foo]
				      targets:
				        pod: {url: "https://x"}
				        deployment: {url: "https://x"}
				    - link-id: l
				      projects: [foo]
				  copy-commands:
				    - command-id: c
				      title: C
				      targets:
				        pod: {}
				""")).containsExactlyInAnyOrder("links[0]: targets: ungültiger Objekt-Typ \"deployment\"",
						"links[1]: link-id \"l\" ist nicht eindeutig", "links[1]: title fehlt",
						"links[1]: targets dürfen nicht leer sein",
						"copy-commands[0]: projects fehlt (leere Liste [] heißt: in keinem Projekt)",
						"copy-commands[0]: targets.pod: command fehlt (Typ ohne Befehl einfach weglassen)");
	}

	@Test
	void teamsAndPipelines() {
		assertThat(errors(BASE + """
				  pipelines: {poll-interval: 1s}
				  teams:
				    label-key: "a b"
				    teams:
				      - team-id: checkout
				      - team-id: checkout
				        team-label: X
				      - team-id: "nicht gültig!"
				        team-label: Y
				""")).containsExactlyInAnyOrder("pipelines.poll-interval \"PT1S\" muss mindestens PT5S sein",
						"teams.label-key \"a b\" ist kein gültiger Label-Schlüssel", "teams.teams[0]: team-label fehlt",
						"teams.teams[1]: team-id \"checkout\" ist nicht eindeutig",
						"teams.teams[2]: team-id \"nicht gültig!\" ist kein gültiger Label-Wert");
	}

	@Test
	void kubernetesNames() {
		assertThat(KubernetesNames.isQualifiedName("mycomp.de/team")).isTrue();
		assertThat(KubernetesNames.isQualifiedName("app")).isTrue();
		assertThat(KubernetesNames.isQualifiedName("a b")).isFalse();
		assertThat(KubernetesNames.isQualifiedName("/x")).isFalse();
		assertThat(KubernetesNames.isValidLabelValue("")).isTrue();
		assertThat(KubernetesNames.isValidLabelValue("-x")).isFalse();
		assertThat(KubernetesNames.isDns1035Label("web-node-port")).isTrue();
		assertThat(KubernetesNames.isDns1035Label("1web")).isFalse();
	}
}
