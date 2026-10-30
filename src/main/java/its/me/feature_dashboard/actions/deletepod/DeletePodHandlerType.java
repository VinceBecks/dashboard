package its.me.feature_dashboard.actions.deletepod;

import java.util.List;
import java.util.Map;

import org.springframework.stereotype.Component;

import io.fabric8.kubernetes.client.KubernetesClient;
import io.fabric8.kubernetes.client.KubernetesClientException;
import its.me.feature_dashboard.actions.ActionHandler;
import its.me.feature_dashboard.actions.ActionHandlerType;
import its.me.feature_dashboard.actions.ActionObject;
import its.me.feature_dashboard.actions.ActionResult;
import its.me.feature_dashboard.actions.ExtraConfig;
import its.me.feature_dashboard.actions.HandlerDeps;
import its.me.feature_dashboard.model.Kind;

/** Aktion „Pod löschen“; mit {@code force-delete: true} ohne Grace-Period. */
@Component
public class DeletePodHandlerType implements ActionHandlerType {

	public static final String TYPE = "delete-pod";

	@Override
	public String type() {
		return TYPE;
	}

	@Override
	public ActionHandler create(HandlerDeps deps) {
		return new Handler(deps.client());
	}

	record Settings(Boolean forceDelete) {
	}

	static class Handler implements ActionHandler {

		private final KubernetesClient client;
		private boolean forceDelete;

		Handler(KubernetesClient client) {
			this.client = client;
		}

		@Override
		public List<String> configure(ExtraConfig extra) {
			Settings s = extra.bind(Settings.class);
			forceDelete = s != null && Boolean.TRUE.equals(s.forceDelete());
			return List.of();
		}

		@Override
		public boolean isApplicable(ActionObject obj) {
			return obj.ref().kind() == Kind.POD;
		}

		@Override
		public ActionResult execute(ActionObject obj, Map<String, Object> inputs) {
			var pod = client.pods().inNamespace(obj.ref().namespace()).withName(obj.ref().name());
			try {
				if (forceDelete) {
					pod.withGracePeriod(0).delete();
				}
				else {
					pod.delete();
				}
			}
			catch (KubernetesClientException e) {
				return ActionResult.failure("Pod %s konnte nicht gelöscht werden: %s".formatted(obj.ref().name(), e.getMessage()));
			}
			return ActionResult.ok("Pod %s wird gelöscht".formatted(obj.ref().name()));
		}
	}
}
