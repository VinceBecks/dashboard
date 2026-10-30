package its.me.feature_dashboard.actions.assignteam;

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
import its.me.feature_dashboard.actions.FormField;
import its.me.feature_dashboard.actions.HandlerDeps;
import its.me.feature_dashboard.config.DashboardProperties.Team;
import its.me.feature_dashboard.config.DashboardProperties.TeamsConfig;
import its.me.feature_dashboard.model.Kind;
import its.me.feature_dashboard.model.StoredNamespace;
import tools.jackson.databind.json.JsonMapper;

/**
 * Aktion „Team zuordnen“: setzt am Namespace das Team-Label (teams.label-key) auf die ID eines konfigurierten Teams
 * oder entfernt es (JSON-Merge-Patch, andere Labels bleiben unberührt).
 */
@Component
public class AssignTeamHandlerType implements ActionHandlerType {

	public static final String TYPE = "assign-team";
	public static final String FIELD_TEAM = "team";
	/** Auswahlwert „Kein Team“; "-" ist kein gültiger Label-Wert und kann daher keine Team-ID sein. */
	public static final String NO_TEAM = "-";

	private static final JsonMapper JSON = JsonMapper.builder().build();

	@Override
	public String type() {
		return TYPE;
	}

	@Override
	public ActionHandler create(HandlerDeps deps) {
		return new Handler(deps.client(), deps.teams());
	}

	static class Handler implements ActionHandler {

		private final KubernetesClient client;
		private final TeamsConfig teams;

		Handler(KubernetesClient client, TeamsConfig teams) {
			this.client = client;
			this.teams = teams;
		}

		/** Keine extra-handler-configs; die Teams stehen im Abschnitt teams. */
		@Override
		public List<String> configure(ExtraConfig extra) {
			if (teams == null || !teams.configured() || teams.teams().isEmpty()) {
				return List.of("der Abschnitt teams (label-key und teams) muss konfiguriert sein");
			}
			return List.of();
		}

		@Override
		public boolean isApplicable(ActionObject obj) {
			return obj.ref().kind() == Kind.NAMESPACE;
		}

		@Override
		public List<FormField> formFields(ActionObject obj) {
			List<FormField.Option> options = new ArrayList<>();
			for (Team t : teams.teams()) {
				options.add(new FormField.Option(t.teamId(), t.teamLabel()));
			}
			options.add(new FormField.Option(NO_TEAM, "Kein Team"));
			return List.of(new FormField(FIELD_TEAM, "Team", FormField.SELECT, true, "Aktuell: " + current(obj), options));
		}

		private String current(ActionObject obj) {
			String id = obj.object() instanceof StoredNamespace ns ? ns.meta().labels().get(teams.labelKey()) : null;
			if (id == null) {
				return "kein Team";
			}
			return teams.find(id).map(Team::teamLabel).orElse("unbekanntes Team \"%s\"".formatted(id));
		}

		@Override
		public ActionResult execute(ActionObject obj, Map<String, Object> inputs) {
			String id = String.valueOf(inputs.get(FIELD_TEAM));
			String name = obj.ref().name();
			Map<String, Object> labels = new HashMap<>();
			String msg;
			if (NO_TEAM.equals(id)) {
				labels.put(teams.labelKey(), null); // JSON-Merge-Patch: null entfernt das Label.
				msg = "Namespace %s ist keinem Team mehr zugeordnet".formatted(name);
			}
			else {
				labels.put(teams.labelKey(), id);
				msg = "Namespace %s gehört jetzt zu %s".formatted(name, teams.find(id).map(Team::teamLabel).orElse(id));
			}
			String patch = JSON.writeValueAsString(Map.of("metadata", Map.of("labels", labels)));
			try {
				client.namespaces().withName(name).patch(PatchContext.of(PatchType.JSON_MERGE), patch);
			}
			catch (KubernetesClientException e) {
				return ActionResult.failure("Team konnte nicht gesetzt werden: " + e.getMessage());
			}
			return ActionResult.ok(msg);
		}
	}
}
