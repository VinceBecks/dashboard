package its.me.feature_dashboard.actions.nodeportservice;

import java.util.Map;

import org.springframework.stereotype.Component;

import io.fabric8.kubernetes.api.model.Service;
import io.fabric8.kubernetes.client.KubernetesClient;
import io.fabric8.kubernetes.client.KubernetesClientException;
import its.me.feature_dashboard.actions.ActionHandler;
import its.me.feature_dashboard.actions.ActionHandlerType;
import its.me.feature_dashboard.actions.ActionObject;
import its.me.feature_dashboard.actions.ActionResult;
import its.me.feature_dashboard.actions.HandlerDeps;
import its.me.feature_dashboard.model.StoredService;

/** Löscht einen NodePort-Service, den die Aktion node-port-service-on-service-base erstellt hat. */
@Component
public class DeleteNodePortServiceHandlerType implements ActionHandlerType {

	@Override
	public String type() {
		return NodePortServices.DELETE_ACTION_NAME;
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

		@Override
		public boolean isApplicable(ActionObject obj) {
			return obj.object() instanceof StoredService svc
					&& NodePortServices.createdByAction(svc.type(), svc.meta().labels());
		}

		/**
		 * Prüft am aktuellen Cluster-Objekt, dass der Service weiterhin von der Aktion stammt. Die Resource-Version als
		 * Precondition verhindert, dass ein zwischenzeitlich veränderter oder neu angelegter Service gelöscht wird.
		 */
		@Override
		public ActionResult execute(ActionObject obj, Map<String, Object> inputs) {
			var services = client.services().inNamespace(obj.ref().namespace());
			Service svc;
			try {
				svc = services.withName(obj.ref().name()).get();
			}
			catch (KubernetesClientException e) {
				return ActionResult.failure("Service %s konnte nicht gelesen werden: %s".formatted(obj.ref().name(), e.getMessage()));
			}
			if (svc == null) {
				return ActionResult.failure("Service %s existiert nicht mehr".formatted(obj.ref().name()));
			}
			String name = svc.getMetadata().getName();
			if (!NodePortServices.createdByAction(svc.getSpec().getType(), svc.getMetadata().getLabels())) {
				return ActionResult.failure("Service %s wurde nicht vom Dashboard erstellt und wird nicht gelöscht".formatted(name));
			}
			try {
				services.withName(name).lockResourceVersion(svc.getMetadata().getResourceVersion()).delete();
			}
			catch (KubernetesClientException e) {
				return ActionResult.failure("Service %s konnte nicht gelöscht werden: %s".formatted(name, e.getMessage()));
			}
			return ActionResult.ok("NodePort-Service %s wird gelöscht".formatted(name));
		}
	}
}
