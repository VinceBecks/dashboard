package its.me.feature_dashboard.actions;

import static org.assertj.core.api.Assertions.assertThat;

import java.util.List;
import java.util.Map;

import org.junit.jupiter.api.Test;

import io.fabric8.kubernetes.api.model.IntOrString;
import io.fabric8.kubernetes.api.model.NamespaceBuilder;
import io.fabric8.kubernetes.api.model.Pod;
import io.fabric8.kubernetes.api.model.PodBuilder;
import io.fabric8.kubernetes.api.model.Service;
import io.fabric8.kubernetes.api.model.ServiceBuilder;
import io.fabric8.kubernetes.client.KubernetesClient;
import io.fabric8.kubernetes.client.server.mock.EnableKubernetesMockClient;
import its.me.feature_dashboard.actions.assignteam.AssignTeamHandlerType;
import its.me.feature_dashboard.actions.deletepod.DeletePodHandlerType;
import its.me.feature_dashboard.actions.nightlyscaledown.NightlyScaleDownHandlerType;
import its.me.feature_dashboard.actions.nodeportservice.CreateNodePortServiceHandlerType;
import its.me.feature_dashboard.actions.nodeportservice.DeleteNodePortServiceHandlerType;
import its.me.feature_dashboard.actions.nodeportservice.NodePortServices;
import its.me.feature_dashboard.actions.restartservice.RestartServiceHandlerType;
import its.me.feature_dashboard.config.DashboardProperties.ActionHandlerConfig;
import its.me.feature_dashboard.config.DashboardProperties.Team;
import its.me.feature_dashboard.config.DashboardProperties.TeamsConfig;
import its.me.feature_dashboard.converter.Converter;
import its.me.feature_dashboard.model.Kind;
import its.me.feature_dashboard.model.ObjectRef;
import its.me.feature_dashboard.model.Stored;
import its.me.feature_dashboard.model.StoredService;
import its.me.feature_dashboard.testsupport.TestSource;

/** Handler, die den Cluster verändern, gegen den fabric8-Mock-Server (CRUD-Modus). */
@EnableKubernetesMockClient(crud = true)
class KubernetesHandlersTest {

	KubernetesClient client;

	private final TestSource store = new TestSource();
	private final TeamsConfig teams = new TeamsConfig("mycomp.de/team",
			List.of(new Team("checkout", "Team Checkout", null), new Team("search", "Team Suche", null)));

	private ActionRegistry registry(ActionHandlerType type, String name, Map<String, ?> extra) {
		ActionRegistry r = new ActionRegistry(List.of(type));
		List<String> errors = r.activate(List.of(new ActionHandlerConfig(name, type.type(), "Titel", "", "", List.of("foo"))),
				i -> ExtraConfig.of(extra), new HandlerDeps(client, store, null, null, teams));
		assertThat(errors).isEmpty();
		return r;
	}

	private ActionObject obj(Kind kind, String name, Stored object) {
		return new ActionObject(new ObjectRef(kind, "feat-1", name), object, "foo");
	}

	private void namespace(Map<String, String> labels) {
		client.namespaces().resource(new NamespaceBuilder().withNewMetadata().withName("feat-1").withLabels(labels)
			.endMetadata().build()).create();
		store.namespace("feat-1", "foo", labels);
	}

	private Map<String, String> namespaceLabels() {
		return client.namespaces().withName("feat-1").get().getMetadata().getLabels();
	}

	private Pod pod(String name, boolean controller) {
		var b = new PodBuilder().withNewMetadata().withName(name).withNamespace("feat-1").addToLabels("app", "web");
		if (controller) {
			b = b.addNewOwnerReference().withApiVersion("apps/v1").withKind("ReplicaSet").withName("web-rs")
				.withUid("u1").withController(true).endOwnerReference();
		}
		return client.pods().inNamespace("feat-1").resource(b.endMetadata().build()).create();
	}

	@Test
	void deletePod() {
		pod("web-1", true);
		var r = registry(new DeletePodHandlerType(), "delete-pod", Map.of("force-delete", true));
		var result = r.execute("delete-pod", obj(Kind.POD, "web-1", TestSource.pod("feat-1", "web-1", true, Map.of())), Map.of());
		assertThat(result).isEqualTo(ActionResult.ok("Pod web-1 wird gelöscht"));
		assertThat(client.pods().inNamespace("feat-1").withName("web-1").get()).isNull();
	}

	@Test
	void restartServiceDeletesOnlyPodsWithController() {
		pod("web-1", true);
		pod("web-2", false);
		var svc = Converter.resolveService(TestSource.service("feat-1", "web", "ClusterIP", Map.of("app", "web")),
				List.of(TestSource.pod("feat-1", "web-1", true, Map.of("app", "web"))));
		var r = registry(new RestartServiceHandlerType(), "restart", Map.of());
		var result = r.execute("restart", obj(Kind.SERVICE, "web", svc), Map.of());
		assertThat(result.success()).isTrue();
		assertThat(result.message()).isEqualTo("1 Pod(s) gelöscht, sie werden neu gestartet: web-1. "
				+ "Übersprungen, da ohne Controller (würden nicht neu gestartet): web-2");
		assertThat(client.pods().inNamespace("feat-1").list().getItems()).extracting(p -> p.getMetadata().getName())
			.containsExactly("web-2");
		// ohne Replicas nicht angeboten
		var empty = TestSource.service("feat-1", "web", "ClusterIP", Map.of("app", "web"));
		assertThat(r.actionsFor(obj(Kind.SERVICE, "web", empty))).isEmpty();
	}

	@Test
	void createThenDeleteNodePortService() {
		Service base = client.services().inNamespace("feat-1").resource(new ServiceBuilder().withNewMetadata()
			.withName("web").withNamespace("feat-1").endMetadata().withNewSpec().withType("ClusterIP")
			.withSelector(Map.of("app", "web")).addNewPort().withName("http").withPort(80)
			.withTargetPort(new IntOrString("http")).endPort().addNewPort().withName("metrics").withPort(9090).endPort()
			.endSpec().build()).create();
		StoredService stored = Converter.service(base);
		store.namespace("feat-1", "foo").put(stored);

		var create = registry(new CreateNodePortServiceHandlerType(), NodePortServices.CREATE_ACTION_NAME, Map.of());
		var actions = create.actionsFor(obj(Kind.SERVICE, "web", stored));
		assertThat(actions.get(0).formFields().get(0).options()).extracting(FormField.Option::value, FormField.Option::label)
			.containsExactly(org.assertj.core.groups.Tuple.tuple("http", "http (http)"),
					org.assertj.core.groups.Tuple.tuple("metrics", "9090 (metrics)"));

		var result = create.execute(NodePortServices.CREATE_ACTION_NAME, obj(Kind.SERVICE, "web", stored),
				Map.of("ports", List.of("http")));
		assertThat(result.success()).as(result.message()).isTrue();
		Service created = client.services().inNamespace("feat-1").withName("web-node-port").get();
		assertThat(created.getSpec().getType()).isEqualTo("NodePort");
		assertThat(created.getSpec().getPorts()).hasSize(1);
		assertThat(created.getMetadata().getLabels()).containsEntry(NodePortServices.LABEL_CREATED_BY_ACTION,
				NodePortServices.CREATE_ACTION_NAME);
		assertThat(created.getMetadata().getOwnerReferences().get(0).getUid()).isEqualTo(base.getMetadata().getUid());

		// Den gibt es jetzt: erneutes Erstellen wird nicht mehr angeboten
		store.put(Converter.service(created));
		assertThat(create.actionsFor(obj(Kind.SERVICE, "web", stored))).isEmpty();

		var delete = registry(new DeleteNodePortServiceHandlerType(), NodePortServices.DELETE_ACTION_NAME, Map.of());
		StoredService createdStored = Converter.service(created);
		assertThat(delete.actionsFor(obj(Kind.SERVICE, "web", stored))).isEmpty();
		assertThat(delete.execute(NodePortServices.DELETE_ACTION_NAME, obj(Kind.SERVICE, "web-node-port", createdStored),
				Map.of())).isEqualTo(ActionResult.ok("NodePort-Service web-node-port wird gelöscht"));
		assertThat(client.services().inNamespace("feat-1").withName("web-node-port").get()).isNull();
		assertThat(delete.execute(NodePortServices.DELETE_ACTION_NAME, obj(Kind.SERVICE, "web-node-port", createdStored),
				Map.of())).isEqualTo(ActionResult.failure("Service web-node-port existiert nicht mehr"));
	}

	@Test
	void assignAndRemoveTeam() {
		namespace(Map.of("mycomp.de/project-name", "foo"));
		var r = registry(new AssignTeamHandlerType(), "assign-team", Map.of());
		var ns = obj(Kind.NAMESPACE, "feat-1", store.namespace("feat-1").orElseThrow().view().namespace());
		FormField field = r.actionsFor(ns).get(0).formFields().get(0);
		assertThat(field.description()).isEqualTo("Aktuell: kein Team");
		assertThat(field.options()).extracting(FormField.Option::value).containsExactly("checkout", "search", "-");

		assertThat(r.execute("assign-team", ns, Map.of("team", "checkout")))
			.isEqualTo(ActionResult.ok("Namespace feat-1 gehört jetzt zu Team Checkout"));
		assertThat(namespaceLabels()).containsEntry("mycomp.de/team", "checkout").containsEntry("mycomp.de/project-name", "foo");
		assertThat(r.execute("assign-team", ns, Map.of("team", "-")).success()).isTrue();
		// Der Mock-Server lässt den Schlüssel mit null stehen; ein echter API-Server entfernt ihn (JSON-Merge-Patch).
		assertThat(namespaceLabels().get("mycomp.de/team")).isNull();
		assertThat(namespaceLabels()).containsEntry("mycomp.de/project-name", "foo");
	}

	@Test
	void assignTeamNeedsTeams() {
		ActionRegistry r = new ActionRegistry(List.of(new AssignTeamHandlerType()));
		assertThat(r.activate(List.of(new ActionHandlerConfig("assign-team", null, "", "", "", List.of("foo"))),
				i -> ExtraConfig.of(Map.of()), new HandlerDeps(client, store, null, null, new TeamsConfig(null, null))))
			.containsExactly("aktion \"assign-team\": der Abschnitt teams (label-key und teams) muss konfiguriert sein");
	}

	@Test
	void nightlyScaleDownMarkAndUnmark() {
		namespace(Map.of("mycomp.de/project-name", "foo"));
		ActionRegistry r = new ActionRegistry(List.of(new NightlyScaleDownHandlerType()));
		HandlerDeps deps = new HandlerDeps(client, store, null, null, teams);
		assertThat(r.activate(List.of(
				new ActionHandlerConfig("exclude", NightlyScaleDownHandlerType.TYPE, "", "", "", List.of("foo")),
				new ActionHandlerConfig("include", NightlyScaleDownHandlerType.TYPE, "", "", "", List.of("foo"))),
				i -> ExtraConfig.of(Map.of("mode", i == 0 ? "mark" : "unmark")), deps)).isEmpty();

		var unmarked = obj(Kind.NAMESPACE, "feat-1", store.namespace("feat-1").orElseThrow().view().namespace());
		assertThat(r.actionsFor(unmarked)).extracting(Action::id).containsExactly("exclude");
		assertThat(r.execute("exclude", unmarked, Map.of()))
			.isEqualTo(ActionResult.ok("Namespace feat-1 wird nachts nicht mehr herunterskaliert"));
		assertThat(namespaceLabels()).containsEntry(NightlyScaleDownHandlerType.EXCLUSION_LABEL_KEY, "true");

		store.namespace("feat-1", "foo", namespaceLabels());
		var marked = obj(Kind.NAMESPACE, "feat-1", store.namespace("feat-1").orElseThrow().view().namespace());
		assertThat(r.actionsFor(marked)).extracting(Action::id).containsExactly("include");
		assertThat(r.execute("include", marked, Map.of()).success()).isTrue();
		assertThat(namespaceLabels().get(NightlyScaleDownHandlerType.EXCLUSION_LABEL_KEY)).isNull();
	}

	@Test
	void nightlyScaleDownRejectsOldConfig() {
		ActionRegistry r = new ActionRegistry(List.of(new NightlyScaleDownHandlerType()));
		assertThat(r.activate(List.of(new ActionHandlerConfig("x", NightlyScaleDownHandlerType.TYPE, "", "", "", List.of("foo"))),
				i -> ExtraConfig.of(Map.of("label-key", "a/b", "mode", "toggle")), new HandlerDeps(client, store, null, null, teams)))
			.containsExactlyInAnyOrder(
					"aktion \"x\": label-key wird nicht mehr konfiguriert, das Label ist fest: mycomp.de/skip-nightly-scale-down=true",
					"aktion \"x\": mode muss \"mark\" oder \"unmark\" sein, erhalten: \"toggle\"");
	}
}
