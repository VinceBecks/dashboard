package its.me.feature_dashboard.template;

import static org.assertj.core.api.Assertions.assertThat;
import static org.assertj.core.api.Assertions.assertThatThrownBy;

import java.util.List;
import java.util.Map;

import org.junit.jupiter.api.Test;

import its.me.feature_dashboard.model.Kind;
import its.me.feature_dashboard.model.ObjectRef;
import its.me.feature_dashboard.model.StoredService;
import its.me.feature_dashboard.testsupport.TestSource;

class ObjectTemplatesTest {

	private static String render(String template, TemplateData data) {
		return ObjectTemplates.render(ObjectTemplates.parse("t", template), data);
	}

	private final StoredService svc = new StoredService(
			TestSource.meta("feat-1", "web", Map.of("app.kubernetes.io/name", "web")), "ClusterIP",
			Map.of("tier", "backend", "app.kubernetes.io/name", "web"), List.of(), List.of());

	private final TemplateData data = TemplateData.of(new ObjectRef(Kind.SERVICE, "feat-1", "web"), svc, "foo")
		.withVars(Map.of("base", "https://kibana"));

	@Test
	void renderWithDataAndFilters() {
		assertThat(render(
				"{{ vars.base }}/{{ project }}/{{ kind }}/{{ name }}?q={% for e in selector %}{{ e.key | replace({'.': '_'}) }}={{ e.value }};{% endfor %}",
				data)).isEqualTo("https://kibana/foo/service/web?q=app_kubernetes_io/name=web;tier=backend;");
		assertThat(render("{{ labels['app.kubernetes.io/name'] }}|{{ name | urlencode }}|{{ 'team/foo bar' | pathescape }}", data))
			.isEqualTo("web|web|team%2Ffoo%20bar");
	}

	@Test
	void missingLabelIsEmptyButMissingVarIsAnError() {
		assertThat(render("[{{ labels['nope'] }}][{{ annotations['x'] }}]", data)).isEqualTo("[][]");
		assertThatThrownBy(() -> render("{{ vars.nope }}", data)).isInstanceOf(ObjectTemplates.TemplateException.class)
			.hasMessageContaining("template t");
		assertThatThrownBy(() -> render("{{ unknown }}", data)).isInstanceOf(ObjectTemplates.TemplateException.class);
	}

	@Test
	void syntaxErrorsFailAtParse() {
		assertThatThrownBy(() -> ObjectTemplates.parse("broken", "{{ name ")).isInstanceOf(ObjectTemplates.TemplateException.class)
			.hasMessageContaining("template broken");
	}

	@Test
	void functions() {
		assertThat(TemplateFunctions.rison("it's!")).isEqualTo("'it!'s!!'");
		assertThat(TemplateFunctions.shellQuote("it's")).isEqualTo("'it'\\''s'");
		assertThat(TemplateFunctions.shellQuote("$HOME; rm")).isEqualTo("'$HOME; rm'");
		assertThat(TemplateFunctions.pathEscape("a:b@c$d&e+f=g;h,i?j")).isEqualTo("a:b@c$d&e+f=g%3Bh%2Ci%3Fj");
		assertThat(render("{{ namespace | trimPrefix(project ~ '-') }}|{{ 'x' | trimPrefix('y') }}",
				TemplateData.of(ObjectRef.namespace("myproject-foo-bar"), svc, "myproject"))).isEqualTo("foo-bar|x");
		assertThat(TemplateFunctions.extractRenewJiraTicketNumber("renew-f-feat1234-login")).isEqualTo("1234");
		assertThat(TemplateFunctions.extractRenewJiraTicketNumber("renew-hf-hotfix42")).isEqualTo("42");
		assertThat(TemplateFunctions.extractRenewJiraTicketNumber("renew-f-bugfix99")).isEqualTo("99");
		assertThat(TemplateFunctions.extractRenewJiraTicketNumber("renew-x-feat1")).isEmpty();
		assertThat(TemplateFunctions.extractRenewJiraTicketNumber("foo-renew-f-feat1")).isEmpty();
		assertThat(render("{{ namespace | extractRenewJiraTicketNumber }}",
				TemplateData.of(ObjectRef.namespace("renew-if-intfix7-x"), svc, "p"))).isEqualTo("7");
		assertThat(render("{{ services | join(',') }}", data.withServices(List.of("a", "b")))).isEqualTo("a,b");
		assertThat(render("{{ name | shellquote }} {{ 'a!' | rison }}", data)).isEqualTo("'web' 'a!!'");
	}
}
