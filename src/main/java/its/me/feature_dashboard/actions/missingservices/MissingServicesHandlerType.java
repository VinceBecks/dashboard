package its.me.feature_dashboard.actions.missingservices;

import java.time.Duration;
import java.util.ArrayList;
import java.util.HashSet;
import java.util.List;
import java.util.Map;
import java.util.Set;

import org.springframework.stereotype.Component;

import its.me.feature_dashboard.actions.ActionHandler;
import its.me.feature_dashboard.actions.ActionHandlerType;
import its.me.feature_dashboard.actions.ActionObject;
import its.me.feature_dashboard.actions.ActionResult;
import its.me.feature_dashboard.actions.ExtraConfig;
import its.me.feature_dashboard.actions.FormField;
import its.me.feature_dashboard.actions.HandlerDeps;
import its.me.feature_dashboard.actions.gitlabpipeline.GitlabPipelineHandlerType;
import its.me.feature_dashboard.actions.gitlabpipeline.Trigger;
import its.me.feature_dashboard.actions.gitlabpipeline.TriggerConfig;
import its.me.feature_dashboard.model.Kind;
import its.me.feature_dashboard.model.StoredService;
import its.me.feature_dashboard.template.TemplateData;

/**
 * Bietet am Namespace die fehlenden konfigurierten Services (Abgleich per Name) zur Mehrfachauswahl an und startet
 * für die Auswahl eine GitLab-Pipeline; die gewählten Services stehen im Template als {@code services}.
 */
@Component
public class MissingServicesHandlerType implements ActionHandlerType {

	public static final String TYPE = "deploy-missing-services";
	public static final String FIELD_SERVICES = "services";

	@Override
	public String type() {
		return TYPE;
	}

	@Override
	public ActionHandler create(HandlerDeps deps) {
		return new Handler(deps);
	}

	record ServiceEntry(String name, String label) {
	}

	record Settings(String gitlabUrl, String project, String ref, String token, String statusToken, Duration timeout,
			Map<String, String> variables, List<ServiceEntry> services) {
	}

	static class Handler implements ActionHandler {

		private final HandlerDeps deps;
		private List<ServiceEntry> services = List.of();
		private Trigger trigger;

		Handler(HandlerDeps deps) {
			this.deps = deps;
		}

		@Override
		public List<String> configure(ExtraConfig extra) {
			List<String> errs = new ArrayList<>(GitlabPipelineHandlerType.legacyKeys(extra));
			Settings s = extra.bind(Settings.class);
			List<ServiceEntry> entries = new ArrayList<>();
			if (s.services() == null || s.services().isEmpty()) {
				errs.add("services fehlt");
			}
			else {
				Set<String> seen = new HashSet<>();
				for (int i = 0; i < s.services().size(); i++) {
					ServiceEntry e = s.services().get(i);
					if (e.name() == null || e.name().isEmpty()) {
						errs.add("services[%d]: name fehlt".formatted(i));
						continue;
					}
					if (!seen.add(e.name())) {
						errs.add("services: \"%s\" ist doppelt".formatted(e.name()));
					}
					entries.add(new ServiceEntry(e.name(), e.label() == null || e.label().isEmpty() ? e.name() : e.label()));
				}
			}
			services = entries;
			trigger = Trigger.create(new TriggerConfig(s.gitlabUrl(), s.project(), s.ref(), s.token(), s.statusToken(),
					s.timeout(), s.variables()), errs);
			if (trigger != null) {
				trigger.track(deps.pipelines(), deps.actionName());
			}
			return errs;
		}

		/** Gilt für alle Namespaces, auch wenn kein Service fehlt (das Dropdown ist dann leer). */
		@Override
		public boolean isApplicable(ActionObject obj) {
			return obj.ref().kind() == Kind.NAMESPACE;
		}

		@Override
		public List<FormField> formFields(ActionObject obj) {
			List<ServiceEntry> missing = missing(obj.ref().namespace());
			String description = missing.isEmpty() ? "Alle konfigurierten Services sind bereits deployt"
					: "Fehlende Services, die per Pipeline deployt werden";
			List<FormField.Option> options = missing.stream().map(e -> new FormField.Option(e.name(), e.label())).toList();
			return List.of(new FormField(FIELD_SERVICES, "Services", FormField.MULTI_SELECT, true, description, options));
		}

		/** Startet die Pipeline für die gewählten Services, die weiterhin fehlen. */
		@Override
		public ActionResult execute(ActionObject obj, Map<String, Object> inputs) {
			List<ServiceEntry> missing = missing(obj.ref().namespace());
			List<String> selected = new ArrayList<>();
			if (inputs.get(FIELD_SERVICES) instanceof List<?> raw) {
				for (Object v : raw) {
					if (!(v instanceof String name) || selected.contains(name)) {
						continue;
					}
					if (missing.stream().noneMatch(e -> e.name().equals(name))) {
						return ActionResult.failure("Service %s fehlt nicht (mehr) im Namespace".formatted(name));
					}
					selected.add(name);
				}
			}
			if (selected.isEmpty()) {
				return ActionResult.failure("Keine fehlenden Services ausgewählt");
			}
			return trigger.run(TemplateData.of(obj.ref(), obj.object(), obj.project()).withServices(selected));
		}

		/** Konfigurierte Services, die im Namespace nicht existieren, in Config-Reihenfolge. */
		private List<ServiceEntry> missing(String namespace) {
			Set<String> deployed = new HashSet<>();
			deps.store().services(namespace).stream().map(StoredService::name).forEach(deployed::add);
			return services.stream().filter(e -> !deployed.contains(e.name())).toList();
		}
	}
}
