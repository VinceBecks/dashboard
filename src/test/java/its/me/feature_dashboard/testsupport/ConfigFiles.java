package its.me.feature_dashboard.testsupport;

import java.io.IOException;
import java.util.Map;

import org.springframework.boot.context.properties.bind.Binder;
import org.springframework.boot.env.YamlPropertySourceLoader;
import org.springframework.core.env.MapPropertySource;
import org.springframework.core.env.StandardEnvironment;
import org.springframework.core.io.ByteArrayResource;
import org.springframework.core.io.FileSystemResource;
import org.springframework.core.io.Resource;

import its.me.feature_dashboard.config.DashboardProperties;

/** Lädt YAML wie Spring Boot und bindet dashboard.* (für Tests ohne Spring-Kontext). */
public final class ConfigFiles {

	private ConfigFiles() {
	}

	public static StandardEnvironment environment(Resource yaml, Map<String, Object> env) {
		StandardEnvironment environment = new StandardEnvironment();
		try {
			new YamlPropertySourceLoader().load("test", yaml).forEach(ps -> environment.getPropertySources().addLast(ps));
		}
		catch (IOException e) {
			throw new IllegalStateException(e);
		}
		environment.getPropertySources().addFirst(new MapPropertySource("env", env));
		return environment;
	}

	public static StandardEnvironment exampleEnvironment() {
		return environment(new FileSystemResource("config-example/application.yml"),
				Map.of("GITLAB_TRIGGER_TOKEN_FOO", "secret"));
	}

	public static StandardEnvironment environment(String yaml) {
		return environment(new ByteArrayResource(yaml.getBytes()), Map.of());
	}

	public static DashboardProperties bind(StandardEnvironment environment) {
		return Binder.get(environment).bindOrCreate("dashboard", DashboardProperties.class);
	}
}
