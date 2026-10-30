package its.me.feature_dashboard.actions.nightlyscaledown;

import java.util.ArrayList;
import java.util.HashMap;
import java.util.List;
import java.util.Map;

import org.springframework.stereotype.Component;

import io.fabric8.kubernetes.client.KubernetesClient;
import io.fabric8.kubernetes.client.KubernetesClientException;
import io.fabric8.kubernetes.client.dsl.base.PatchContext;
import io.fabric8.kubernetes.client.dsl.base.PatchType;
import its.me.feature_dashboard.actions.ActionHandler;
import its.me.feature_dashboard.actions.ActionHandlerType;
import its.me.feature_dashboard.actions.ActionObject;
import its.me.feature_dashboard.actions.ActionResult;
import its.me.feature_dashboard.actions.ExtraConfig;
import its.me.feature_dashboard.actions.HandlerDeps;
import its.me.feature_dashboard.model.Kind;
import its.me.feature_dashboard.model.StoredNamespace;
import tools.jackson.databind.json.JsonMapper;

/**
 * Handler-Typ „nightly-scale-down-exclusion“: Der CronJob nightly-scale-down skaliert jede Nacht alle
 * Feature-Umgebungen auf 0 Replicas und lässt Namespaces mit dem Label {@link #EXCLUSION_LABEL_KEY} aus. Der Handler
 * setzt dieses Label (mode mark) oder entfernt es (mode unmark). Er wird als zwei Aktionen konfiguriert, die sich
 * gegenseitig ablösen: Markieren wird nur angeboten, solange das Label fehlt, Aufheben nur, solange es gesetzt ist.
 */
@Component
public class NightlyScaleDownHandlerType implements ActionHandlerType {

	public static final String TYPE = "nightly-scale-down-exclusion";
	public static final String MODE_MARK = "mark";
	public static final String MODE_UNMARK = "unmark";

	/**
	 * Markiert einen Namespace, der nachts nicht herunterskaliert wird. Fest im Code und identisch mit dem Go-CronJob
	 * (backend/internal/actions/nightlyscaledown), nicht konfigurierbar.
	 */
	public static final String EXCLUSION_LABEL_KEY = "mycomp.de/skip-nightly-scale-down";
	public static final String EXCLUSION_LABEL_VALUE = "true";

	private static final JsonMapper JSON = JsonMapper.builder().build();

	@Override
	public String type() {
		return TYPE;
	}

	@Override
	public ActionHandler create(HandlerDeps deps) {
		return new Handler(deps.client());
	}

	record Settings(String mode) {
	}

	static class Handler implements ActionHandler {

		private final KubernetesClient client;
		private String mode;

		Handler(KubernetesClient client) {
			this.client = client;
		}

		/** Liest mode; label-key/label-value aus älteren Configs brechen den Start ab, statt still ignoriert zu werden. */
		@Override
		public List<String> configure(ExtraConfig extra) {
			List<String> errs = new ArrayList<>();
			var keys = extra.keys();
			for (String key : List.of("label-key", "label-value")) {
				if (keys.contains(key)) {
					errs.add("%s wird nicht mehr konfiguriert, das Label ist fest: %s=%s".formatted(key,
							EXCLUSION_LABEL_KEY, EXCLUSION_LABEL_VALUE));
				}
			}
			Settings s = extra.bind(Settings.class);
			mode = s.mode() == null ? "" : s.mode();
			if (!MODE_MARK.equals(mode) && !MODE_UNMARK.equals(mode)) {
				errs.add("mode muss \"%s\" oder \"%s\" sein, erhalten: \"%s\"".formatted(MODE_MARK, MODE_UNMARK, mode));
			}
			return errs;
		}

		/** mark, solange das Label mit dem festen Wert fehlt; unmark, solange es gesetzt ist. */
		@Override
		public boolean isApplicable(ActionObject obj) {
			if (obj.ref().kind() != Kind.NAMESPACE || !(obj.object() instanceof StoredNamespace ns)) {
				return false;
			}
			boolean marked = EXCLUSION_LABEL_VALUE.equals(ns.meta().labels().get(EXCLUSION_LABEL_KEY));
			return MODE_MARK.equals(mode) != marked;
		}

		@Override
		public ActionResult execute(ActionObject obj, Map<String, Object> inputs) {
			String name = obj.ref().name();
			Map<String, Object> labels = new HashMap<>();
			String msg;
			if (MODE_UNMARK.equals(mode)) {
				labels.put(EXCLUSION_LABEL_KEY, null); // JSON-Merge-Patch: null entfernt das Label.
				msg = "Namespace %s wird nachts wieder herunterskaliert".formatted(name);
			}
			else {
				labels.put(EXCLUSION_LABEL_KEY, EXCLUSION_LABEL_VALUE);
				msg = "Namespace %s wird nachts nicht mehr herunterskaliert".formatted(name);
			}
			String patch = JSON.writeValueAsString(Map.of("metadata", Map.of("labels", labels)));
			try {
				client.namespaces().withName(name).patch(PatchContext.of(PatchType.JSON_MERGE), patch);
			}
			catch (KubernetesClientException e) {
				return ActionResult.failure("Ausnahme vom nächtlichen Herunterskalieren konnte für Namespace %s nicht geändert werden (Label %s): %s"
					.formatted(name, EXCLUSION_LABEL_KEY, e.getMessage()));
			}
			return ActionResult.ok(msg);
		}
	}
}
