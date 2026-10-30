package its.me.feature_dashboard.converter;

import static org.assertj.core.api.Assertions.assertThat;

import java.util.List;
import java.util.Map;

import org.junit.jupiter.api.Test;

import io.fabric8.kubernetes.api.model.IntOrString;
import io.fabric8.kubernetes.api.model.NamespaceBuilder;
import io.fabric8.kubernetes.api.model.Pod;
import io.fabric8.kubernetes.api.model.PodBuilder;
import io.fabric8.kubernetes.api.model.ServiceBuilder;
import io.fabric8.kubernetes.api.model.networking.v1.IngressBuilder;
import its.me.feature_dashboard.model.NamespaceView;
import its.me.feature_dashboard.model.StoredService;

class ConverterTest {

	private static Pod pod(String name, boolean ready, Map<String, String> labels) {
		return new PodBuilder().withNewMetadata()
			.withName(name)
			.withNamespace("feat-1")
			.withLabels(labels)
			.endMetadata()
			.withNewSpec()
			.addNewContainer()
			.withName("app")
			.withImage("web:1")
			.addNewPort()
			.withName("http")
			.withContainerPort(8080)
			.endPort()
			.endContainer()
			.endSpec()
			.withNewStatus()
			.addNewCondition()
			.withType("Ready")
			.withStatus(ready ? "True" : "False")
			.endCondition()
			.endStatus()
			.build();
	}

	@Test
	void namespaceViewResolvesServicesAndSorts() {
		var ns = new NamespaceBuilder().withNewMetadata().withName("feat-1").addToLabels("a", "b").endMetadata().build();
		var svc = new ServiceBuilder().withNewMetadata()
			.withName("web")
			.withNamespace("feat-1")
			.endMetadata()
			.withNewSpec()
			.withSelector(Map.of("app", "web"))
			.addNewPort()
			.withName("http")
			.withPort(80)
			.withTargetPort(new IntOrString("http"))
			.endPort()
			.addNewPort()
			.withName("metrics")
			.withPort(9090)
			.endPort()
			.addNewPort()
			.withName("missing")
			.withPort(81)
			.withTargetPort(new IntOrString("nope"))
			.endPort()
			.endSpec()
			.build();
		var ingress = new IngressBuilder().withNewMetadata()
			.withName("web")
			.withNamespace("feat-1")
			.endMetadata()
			.withNewSpec()
			.addNewRule()
			.withHost("feat-1.example")
			.withNewHttp()
			.addNewPath()
			.withPath("/api")
			.withPathType("Prefix")
			.withNewBackend()
			.withNewService()
			.withName("web")
			.endService()
			.endBackend()
			.endPath()
			.endHttp()
			.endRule()
			.endSpec()
			.build();

		NamespaceView view = Converter.namespaceView(ns, "foo",
				List.of(pod("web-2", false, Map.of("app", "web")), pod("web-1", true, Map.of("app", "web")),
						pod("other", true, Map.of("app", "other"))),
				List.of(svc), List.of(ingress));

		assertThat(view.namespace().project()).isEqualTo("foo");
		assertThat(view.pods()).extracting(p -> p.name()).containsExactly("other", "web-1", "web-2");
		assertThat(view.pods().get(1).ready()).isTrue();
		assertThat(view.pods().get(1).containers().get(0).ports().get(0).containerPort()).isEqualTo(8080);
		StoredService s = view.services().get(0);
		assertThat(s.type()).isEqualTo("ClusterIP");
		assertThat(s.replicas()).containsExactly("web-1", "web-2");
		assertThat(s.ports()).extracting(StoredService.ServicePort::targetPort).containsExactly("http", "9090", "nope");
		assertThat(s.ports()).extracting(StoredService.ServicePort::resolvedTargetPort).containsExactly(8080, 9090, null);
		assertThat(view.ingresses().get(0).rules().get(0).paths().get(0).backendServiceName()).isEqualTo("web");
	}

	@Test
	void serviceWithoutSelectorHasNoReplicas() {
		var svc = new ServiceBuilder().withNewMetadata().withName("db").withNamespace("feat-1").endMetadata()
			.withNewSpec().withType("NodePort").addNewPort().withPort(5432).withNodePort(30001).endPort().endSpec().build();
		StoredService s = Converter.resolveService(Converter.service(svc), List.of(Converter.pod(pod("x", true, Map.of()))));
		assertThat(s.replicas()).isEmpty();
		assertThat(s.ports().get(0).nodePort()).isEqualTo(30001);
		assertThat(s.ports().get(0).targetPort()).isEqualTo("5432");
	}
}
