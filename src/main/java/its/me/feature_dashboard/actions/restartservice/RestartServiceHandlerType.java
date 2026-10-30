package its.me.feature_dashboard.actions.restartservice;

import java.util.ArrayList;
import java.util.List;
import java.util.Map;

import org.springframework.stereotype.Component;

import io.fabric8.kubernetes.api.model.OwnerReference;
import io.fabric8.kubernetes.api.model.Pod;
import io.fabric8.kubernetes.client.KubernetesClient;
import io.fabric8.kubernetes.client.KubernetesClientException;
import its.me.feature_dashboard.actions.ActionHandler;
import its.me.feature_dashboard.actions.ActionHandlerType;
import its.me.feature_dashboard.actions.ActionObject;
import its.me.feature_dashboard.actions.ActionResult;
import its.me.feature_dashboard.actions.HandlerDeps;
import its.me.feature_dashboard.actions.nodeportservice.NodePortServices;
import its.me.feature_dashboard.model.StoredService;

/** Löscht alle Pods eines Services, damit ihr Controller sie neu startet. */
@Component
public class RestartServiceHandlerType implements ActionHandlerType {

	public static final String TYPE = "restart-service-replicas";

	@Override
	public String type() {
		return TYPE;
	}

	@Override
	public ActionHandler create(HandlerDeps deps) {
		return new Handler(deps.client());
	}

	static class Handler implements ActionHandler {

		private final KubernetesClient client;

		Handler(KubernetesClient client) {
			this.client = client;
		}

		/**
		 * Gilt für Services mit Selector und mindestens einem Replica. Ausgenommen sind NodePort-Services, die die
		 * Aktion node-port-service-on-service-base erstellt hat.
		 */
		@Override
		public boolean isApplicable(ActionObject obj) {
			if (!(obj.object() instanceof StoredService svc) || svc.selector().isEmpty() || svc.replicas().isEmpty()) {
				return false;
			}
			return !NodePortServices.CREATE_ACTION_NAME.equals(svc.meta().labels().get(NodePortServices.LABEL_CREATED_BY_ACTION));
		}

		/**
		 * Löscht alle aktuell selektierten Pods, frisch aus dem Cluster gelesen. Pods ohne Controller werden
		 * übersprungen, da sie nach dem Löschen nicht neu entstehen würden.
		 */
		@Override
		public ActionResult execute(ActionObject obj, Map<String, Object> inputs) {
			if (!(obj.object() instanceof StoredService svc) || svc.selector().isEmpty()) {
				// Ein leerer Selector würde alle Pods des Namespaces treffen.
				return ActionResult.failure("Service hat keinen Selector");
			}
			var podsClient = client.pods().inNamespace(obj.ref().namespace());
			List<Pod> pods;
			try {
				pods = podsClient.withLabels(svc.selector()).list().getItems();
			}
			catch (KubernetesClientException e) {
				return ActionResult.failure("Pods konnten nicht gelesen werden: " + e.getMessage());
			}
			List<String> deleted = new ArrayList<>();
			List<String> skipped = new ArrayList<>();
			List<String> failed = new ArrayList<>();
			for (Pod pod : pods) {
				String name = pod.getMetadata().getName();
				if (pod.getMetadata().getDeletionTimestamp() != null) {
					continue; // wird bereits gelöscht
				}
				if (!hasController(pod)) {
					skipped.add(name);
					continue;
				}
				try {
					podsClient.withName(name).delete();
					deleted.add(name);
				}
				catch (KubernetesClientException e) {
					failed.add("%s (%s)".formatted(name, e.getMessage()));
				}
			}
			List<String> msg = new ArrayList<>();
			if (!deleted.isEmpty()) {
				msg.add("%d Pod(s) gelöscht, sie werden neu gestartet: %s".formatted(deleted.size(), String.join(", ", deleted)));
			}
			if (!skipped.isEmpty()) {
				msg.add("Übersprungen, da ohne Controller (würden nicht neu gestartet): " + String.join(", ", skipped));
			}
			if (!failed.isEmpty()) {
				msg.add("Fehlgeschlagen: " + String.join(", ", failed));
			}
			if (msg.isEmpty()) {
				msg.add("Keine Pods zum Neustarten gefunden");
			}
			return new ActionResult(failed.isEmpty() && !deleted.isEmpty(), String.join(". ", msg));
		}

		private static boolean hasController(Pod pod) {
			List<OwnerReference> refs = pod.getMetadata().getOwnerReferences();
			return refs != null && refs.stream().anyMatch(r -> Boolean.TRUE.equals(r.getController()));
		}
	}
}
