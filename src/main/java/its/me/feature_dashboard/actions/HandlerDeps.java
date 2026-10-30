package its.me.feature_dashboard.actions;

import io.fabric8.kubernetes.client.KubernetesClient;
import its.me.feature_dashboard.config.DashboardProperties.TeamsConfig;
import its.me.feature_dashboard.model.ClusterSource;
import its.me.feature_dashboard.pipelines.PipelineState;

/**
 * Abhängigkeiten, die Handler bei ihrer Erzeugung erhalten.
 *
 * @param pipelines merkt sich gestartete GitLab-Pipelines; null heißt: nicht verfolgen
 * @param actionName action-name der Aktion, für die der Handler erzeugt wird (setzt die Registry)
 */
public record HandlerDeps(KubernetesClient client, ClusterSource store, PipelineState pipelines, String actionName,
		TeamsConfig teams) {

	public HandlerDeps withActionName(String name) {
		return new HandlerDeps(client, store, pipelines, name, teams);
	}
}
