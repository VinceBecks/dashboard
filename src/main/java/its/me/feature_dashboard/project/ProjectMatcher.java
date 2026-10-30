package its.me.feature_dashboard.project;

import java.util.List;
import java.util.Map;
import java.util.Optional;

import org.slf4j.Logger;
import org.slf4j.LoggerFactory;

import its.me.feature_dashboard.config.DashboardProperties.Project;

/** Ordnet Namespaces anhand ihrer Labels einem Projekt zu. */
public class ProjectMatcher {

	private static final Logger log = LoggerFactory.getLogger(ProjectMatcher.class);

	private final List<Project> projects;

	public ProjectMatcher(List<Project> projects) {
		this.projects = List.copyOf(projects);
	}

	/** Erstes Projekt (in Config-Reihenfolge), dessen namespace-labels alle gesetzt sind. */
	public Optional<String> match(String namespace, Map<String, String> labels) {
		String match = null;
		for (Project p : projects) {
			if (!hasAll(labels, p.namespaceLabels())) {
				continue;
			}
			if (match != null) {
				log.warn("Namespace passt zu mehreren Projekten, erstes gewinnt: namespace={} project={} ignored={}",
						namespace, match, p.projectName());
				continue;
			}
			match = p.projectName();
		}
		return Optional.ofNullable(match);
	}

	static boolean hasAll(Map<String, String> labels, Map<String, String> required) {
		for (Map.Entry<String, String> e : required.entrySet()) {
			if (labels == null || !e.getValue().equals(labels.get(e.getKey()))) {
				return false;
			}
		}
		return true;
	}
}
