package its.me.feature_dashboard.kube;

import static org.assertj.core.api.Assertions.assertThat;
import static org.awaitility.Awaitility.await;

import java.time.Duration;
import java.util.List;
import java.util.Map;

import org.junit.jupiter.api.AfterEach;
import org.junit.jupiter.api.Test;

import io.fabric8.kubernetes.api.model.ManagedFieldsEntryBuilder;
import io.fabric8.kubernetes.api.model.NamespaceBuilder;
import io.fabric8.kubernetes.api.model.Pod;
import io.fabric8.kubernetes.api.model.PodBuilder;
import io.fabric8.kubernetes.api.model.ServiceBuilder;
import io.fabric8.kubernetes.client.KubernetesClient;
import io.fabric8.kubernetes.client.server.mock.EnableKubernetesMockClient;
import its.me.feature_dashboard.config.DashboardProperties.Project;
import its.me.feature_dashboard.model.Kind;
import its.me.feature_dashboard.model.ObjectRef;
import its.me.feature_dashboard.project.ProjectMatcher;

@EnableKubernetesMockClient(crud = true)
class ClusterTest {

	KubernetesClient client;
	Cluster cluster;

	@AfterEach
	void stop() {
		if (cluster != null) {
			cluster.stop();
		}
	}

	private void namespace(String name, Map<String, String> labels) {
		client.namespaces().resource(new NamespaceBuilder().withNewMetadata().withName(name).withLabels(labels)
			.endMetadata().build()).createOr(r -> r.update());
	}

	private Pod pod(String ns, String name) {
		return new PodBuilder().withNewMetadata()
			.withName(name)
			.withNamespace(ns)
			.addToLabels("app", "web")
			.withManagedFields(new ManagedFieldsEntryBuilder().withManager("kubectl").build())
			.endMetadata()
			.withNewSpec()
			.addNewContainer()
			.withName("app")
			.withImage("web:1")
			.withCommand("sh")
			.addNewPort()
			.withName("http")
			.withContainerPort(8080)
			.endPort()
			.endContainer()
			.withNodeName("node-1")
			.endSpec()
			.withNewStatus()
			.withPhase("Running")
			.addNewCondition()
			.withType("Ready")
			.withStatus("True")
			.withMessage("ok")
			.endCondition()
			.addNewCondition()
			.withType("Initialized")
			.withStatus("True")
			.endCondition()
			.endStatus()
			.build();
	}

	@Test
	void revisionsFollowProjectNamespaces() {
		namespace("feat-1", Map.of("project", "foo"));
		namespace("other", Map.of());
		client.pods().inNamespace("feat-1").resource(pod("feat-1", "web-1")).create();
		client.services().inNamespace("feat-1").resource(new ServiceBuilder().withNewMetadata().withName("web")
			.withNamespace("feat-1").endMetadata().withNewSpec().withSelector(Map.of("app", "web")).addNewPort()
			.withPort(80).withNewTargetPort("http").endPort().endSpec().build()).create();

		cluster = new Cluster(client, new ProjectMatcher(List.of(new Project("foo", Map.of("project", "foo"), null, null, null))));
		cluster.start();
		await().atMost(Duration.ofSeconds(10)).until(cluster::isReady);

		assertThat(cluster.namespaceRevisions()).containsOnlyKeys("feat-1");
		var view = cluster.namespace("feat-1").orElseThrow().view();
		assertThat(view.namespace().project()).isEqualTo("foo");
		assertThat(view.services().get(0).replicas()).containsExactly("web-1");
		assertThat(view.services().get(0).ports().get(0).resolvedTargetPort()).isEqualTo(8080);
		assertThat(view.pods().get(0).ready()).isTrue();
		assertThat(cluster.get(new ObjectRef(Kind.POD, "feat-1", "web-1"))).isPresent();
		assertThat(cluster.namespace("other")).isEmpty();

		// Änderung in einem Projekt-Namespace erhöht seine Revision; andere Namespaces nicht.
		long before = cluster.namespaceRevisions().get("feat-1");
		client.pods().inNamespace("other").resource(pod("other", "x")).create();
		client.pods().inNamespace("feat-1").resource(pod("feat-1", "web-2")).create();
		await().atMost(Duration.ofSeconds(10)).until(() -> cluster.namespace("feat-1").orElseThrow().view().pods().size() == 2);
		assertThat(cluster.namespaceRevisions().get("feat-1")).isGreaterThan(before);
		assertThat(cluster.namespaceRevisions()).containsOnlyKeys("feat-1");

		// Projekt-Labels entfernt → Namespace fällt heraus; wieder gesetzt → wieder drin.
		namespace("feat-1", Map.of());
		await().atMost(Duration.ofSeconds(10)).until(() -> cluster.namespaceRevisions().isEmpty());
		namespace("other", Map.of("project", "foo"));
		await().atMost(Duration.ofSeconds(10)).until(() -> cluster.namespaceRevisions().containsKey("other"));
	}

	@Test
	void trimKeepsWhatTheConverterReads() {
		Pod trimmed = TrimmingItemStore.trim(pod("feat-1", "web-1"));
		assertThat(trimmed.getMetadata().getManagedFields()).isNull();
		assertThat(trimmed.getMetadata().getLabels()).containsEntry("app", "web");
		assertThat(trimmed.getSpec().getNodeName()).isNull();
		assertThat(trimmed.getSpec().getContainers()).singleElement().satisfies(c -> {
			assertThat(c.getName()).isEqualTo("app");
			assertThat(c.getImage()).isEqualTo("web:1");
			assertThat(c.getCommand()).isEmpty();
			assertThat(c.getPorts().get(0).getContainerPort()).isEqualTo(8080);
		});
		assertThat(trimmed.getStatus().getPhase()).isEqualTo("Running");
		assertThat(trimmed.getStatus().getConditions()).singleElement().satisfies(c -> {
			assertThat(c.getType()).isEqualTo("Ready");
			assertThat(c.getStatus()).isEqualTo("True");
			assertThat(c.getMessage()).isNull();
		});
	}
}
