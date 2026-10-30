package its.me.feature_dashboard.actions.gitlabpipeline;

import java.time.Duration;
import java.util.Map;

/**
 * GitLab-Angaben eines Pipeline-Triggers (gemeinsam für gitlab-pipeline und deploy-missing-services). Tokens stehen
 * nie direkt in der Konfiguration, sondern als Platzhalter, z. B. {@code token: ${GITLAB_TRIGGER_TOKEN_FOO}}.
 *
 * @param ref Branch/Tag, Template erlaubt
 * @param statusToken optional; Token mit read_api, mit dem laufende Pipelines verfolgt werden
 * @param timeout HTTP-Timeout, Standard 10s
 * @param variables CI/CD-Variablen, Werte sind Templates
 */
public record TriggerConfig(String gitlabUrl, String project, String ref, String token, String statusToken,
		Duration timeout, Map<String, String> variables) {
}
