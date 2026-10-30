package its.me.feature_dashboard.actions.gitlabpipeline;

import java.time.Duration;
import java.util.ArrayList;
import java.util.List;
import java.util.Map;

import org.springframework.stereotype.Component;

import its.me.feature_dashboard.actions.ActionHandler;
import its.me.feature_dashboard.actions.ActionHandlerType;
import its.me.feature_dashboard.actions.ActionObject;
import its.me.feature_dashboard.actions.ActionResult;
import its.me.feature_dashboard.actions.ExtraConfig;
import its.me.feature_dashboard.actions.HandlerDeps;
import its.me.feature_dashboard.model.Kind;
import its.me.feature_dashboard.template.TemplateData;

/** Generischer Handler-Typ: startet eine GitLab-Pipeline über die Trigger-API; beliebig viele Aktionen möglich. */
@Component
public class GitlabPipelineHandlerType implements ActionHandlerType {

	public static final String TYPE = "gitlab-pipeline";

	@Override
	public String type() {
		return TYPE;
	}

	@Override
	public ActionHandler create(HandlerDeps deps) {
		return new Handler(deps);
	}

	/** extra-handler-configs; projects nur, um alte Configs zu erkennen. */
	record Settings(String gitlabUrl, String project, String ref, String token, String statusToken, Duration timeout,
			Map<String, String> variables, List<String> appliesTo, List<String> projects) {

		TriggerConfig trigger() {
			return new TriggerConfig(gitlabUrl, project, ref, token, statusToken, timeout, variables);
		}
	}

	/** Meldungen zu Schlüsseln, die es nicht mehr gibt (gemeinsam mit deploy-missing-services). */
	public static List<String> legacyKeys(ExtraConfig extra) {
		List<String> errs = new ArrayList<>();
		var keys = extra.keys();
		if (keys.contains("token-env")) {
			errs.add("token-env wird nicht mehr unterstützt: Token als Platzhalter angeben, z. B. token: ${GITLAB_TRIGGER_TOKEN_FOO}");
		}
		if (keys.contains("status-token-env")) {
			errs.add("status-token-env wird nicht mehr unterstützt: status-token: ${GITLAB_READ_TOKEN_FOO}");
		}
		return errs;
	}

	static class Handler implements ActionHandler {

		private final HandlerDeps deps;
		private Trigger trigger;
		private List<Kind> appliesTo = List.of();

		Handler(HandlerDeps deps) {
			this.deps = deps;
		}

		@Override
		public List<String> configure(ExtraConfig extra) {
			List<String> errs = new ArrayList<>(legacyKeys(extra));
			Settings s = extra.bind(Settings.class);
			List<Kind> kinds = new ArrayList<>();
			if (s.appliesTo() == null || s.appliesTo().isEmpty()) {
				errs.add("applies-to fehlt");
			}
			else {
				for (String k : s.appliesTo()) {
					Kind.parse(k).ifPresentOrElse(kinds::add,
							() -> errs.add("applies-to: ungültiger Objekt-Typ \"%s\"".formatted(k)));
				}
			}
			appliesTo = kinds;
			if (s.projects() != null) {
				errs.add("projects gehört nicht in extra-handler-configs, sondern direkt an die Aktion (projects: [...])");
			}
			trigger = Trigger.create(s.trigger(), errs);
			if (trigger != null) {
				trigger.track(deps.pipelines(), deps.actionName());
			}
			return errs;
		}

		/** Prüft den Objekt-Typ (applies-to). Die Projekte prüft bereits die Registry. */
		@Override
		public boolean isApplicable(ActionObject obj) {
			return appliesTo.contains(obj.ref().kind());
		}

		@Override
		public ActionResult execute(ActionObject obj, Map<String, Object> inputs) {
			return trigger.run(TemplateData.of(obj.ref(), obj.object(), obj.project()));
		}
	}
}
