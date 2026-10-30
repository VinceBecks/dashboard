package its.me.feature_dashboard.config;

import java.util.List;

import org.springframework.boot.autoconfigure.condition.ConditionalOnMissingBean;
import org.springframework.boot.context.properties.EnableConfigurationProperties;
import org.springframework.boot.context.properties.bind.Binder;
import org.springframework.context.annotation.Bean;
import org.springframework.context.annotation.Configuration;
import org.springframework.core.env.Environment;

import io.fabric8.kubernetes.client.KubernetesClient;
import io.fabric8.kubernetes.client.KubernetesClientBuilder;
import its.me.feature_dashboard.actions.ActionHandlerType;
import its.me.feature_dashboard.actions.ActionRegistry;
import its.me.feature_dashboard.actions.ExtraConfig;
import its.me.feature_dashboard.actions.HandlerDeps;
import its.me.feature_dashboard.api.EventHub;
import its.me.feature_dashboard.api.NamespaceDtoBuilder;
import its.me.feature_dashboard.api.ReadinessCheck;
import its.me.feature_dashboard.copycommands.CopyCommandRegistry;
import its.me.feature_dashboard.kube.Cluster;
import its.me.feature_dashboard.links.LinkRegistry;
import its.me.feature_dashboard.model.ClusterSource;
import its.me.feature_dashboard.pipelines.PipelineState;
import its.me.feature_dashboard.pipelines.PipelineTracker;
import its.me.feature_dashboard.project.ProjectMatcher;
import tools.jackson.databind.ObjectMapper;

/**
 * Verdrahtung des Dashboards. Die Konfiguration wird beim Start vollständig geprüft; Fehler (auch in Templates und
 * extra-handler-configs) brechen den Start ab.
 */
@Configuration(proxyBeanMethods = false)
@EnableConfigurationProperties(DashboardProperties.class)
public class DashboardConfiguration {

	private final DashboardProperties properties;

	public DashboardConfiguration(DashboardProperties properties) {
		DashboardConfigValidator.validateOrThrow(properties);
		this.properties = properties;
	}

	/** Kubernetes-Client: im Cluster die In-Cluster-Konfiguration, lokal KUBECONFIG bzw. ~/.kube/config. */
	@Bean(destroyMethod = "close")
	@ConditionalOnMissingBean
	KubernetesClient kubernetesClient() {
		return new KubernetesClientBuilder().build();
	}

	@Bean
	Cluster cluster(KubernetesClient client) {
		return new Cluster(client, new ProjectMatcher(properties.projects()));
	}

	@Bean
	@ConditionalOnMissingBean
	ReadinessCheck readinessCheck(Cluster cluster) {
		return cluster::isReady;
	}

	/** Laufende Pipelines, nur im Speicher. */
	@Bean
	PipelineState pipelineState() {
		return new PipelineState();
	}

	@Bean
	PipelineTracker pipelineTracker(PipelineState state) {
		return new PipelineTracker(state, properties.pipelines().pollInterval());
	}

	@Bean
	LinkRegistry linkRegistry() {
		return new LinkRegistry(properties);
	}

	@Bean
	CopyCommandRegistry copyCommandRegistry() {
		return new CopyCommandRegistry(properties);
	}

	/** Aktiviert die Aktionen; extra-handler-configs bindet jeder Handler aus dashboard.action-handlers[i]. */
	@Bean
	ActionRegistry actionRegistry(List<ActionHandlerType> types, KubernetesClient client, ClusterSource source,
			PipelineState pipelines, Environment environment) {
		ActionRegistry registry = new ActionRegistry(types);
		Binder binder = Binder.get(environment);
		List<String> errors = registry.activate(properties.actionHandlers(),
				i -> new ExtraConfig(binder, "dashboard.action-handlers[%d].extra-handler-configs".formatted(i)),
				new HandlerDeps(client, source, pipelines, null, properties.teams()));
		if (!errors.isEmpty()) {
			throw new DashboardConfigValidator.InvalidConfigException(errors);
		}
		return registry;
	}

	@Bean
	NamespaceDtoBuilder namespaceDtoBuilder(ActionRegistry registry, LinkRegistry links, CopyCommandRegistry copyCommands,
			PipelineState pipelines) {
		return new NamespaceDtoBuilder(registry, links, copyCommands, pipelines, properties.cluster().dnsName(),
				properties.projects(), properties.teams());
	}

	@Bean
	EventHub eventHub(ClusterSource source, PipelineState pipelines, NamespaceDtoBuilder builder, ObjectMapper json) {
		return new EventHub(source, pipelines, builder::build, properties.projects(), properties.teams(), json);
	}
}
