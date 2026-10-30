package its.me.feature_dashboard.project;

import static org.assertj.core.api.Assertions.assertThat;

import java.util.List;
import java.util.Map;

import org.junit.jupiter.api.Test;

import its.me.feature_dashboard.config.DashboardProperties.Project;
import its.me.feature_dashboard.config.DashboardProperties.ServiceArea;

class ProjectsTest {

	private final Project foo = new Project("foo", Map.of("feature", "true", "project", "foo"),
			List.of(new ServiceArea("shop", "Shop", List.of(Map.of("part-of", "shop", "tier", "backend"), Map.of("part-of", "billing"))),
					new ServiceArea("infra", "Infra", List.of(Map.of("part-of", "billing")))),
			null, null);

	@Test
	void match() {
		var matcher = new ProjectMatcher(List.of(foo, new Project("all", Map.of("feature", "true"), null, null, null)));
		assertThat(matcher.match("n", Map.of("feature", "true", "project", "foo"))).contains("foo");
		assertThat(matcher.match("n", Map.of("feature", "true"))).contains("all");
		assertThat(matcher.match("n", Map.of("project", "foo"))).isEmpty();
		assertThat(matcher.match("n", Map.of("feature", "false", "project", "foo"))).isEmpty();
	}

	@Test
	void serviceAreaIds() {
		assertThat(ServiceAreas.areaIds(foo, Map.of("part-of", "shop", "tier", "backend"))).containsExactly("shop");
		assertThat(ServiceAreas.areaIds(foo, Map.of("part-of", "billing"))).containsExactly("shop", "infra");
		assertThat(ServiceAreas.areaIds(foo, Map.of("part-of", "shop"))).containsExactly("others");
		assertThat(ServiceAreas.areaIds(null, Map.of())).containsExactly("others");
		assertThat(foo.othersLabel()).isEqualTo("Andere");
	}
}
