package its.me.feature_dashboard.project;

import java.util.ArrayList;
import java.util.List;
import java.util.Map;

import its.me.feature_dashboard.config.DashboardProperties;
import its.me.feature_dashboard.config.DashboardProperties.Project;
import its.me.feature_dashboard.config.DashboardProperties.ServiceArea;

/** Zuordnung von Services zu den Service-Bereichen eines Projekts. */
public final class ServiceAreas {

	private ServiceAreas() {
	}

	/**
	 * IDs aller Bereiche des Projekts, zu denen ein Service mit diesen Labels gehört, in Config-Reihenfolge. Passt
	 * keiner, gehört er zu „others“.
	 */
	public static List<String> areaIds(Project project, Map<String, String> labels) {
		List<String> ids = new ArrayList<>();
		if (project != null) {
			for (ServiceArea a : project.serviceAreas()) {
				if (matchesAny(labels, a.labelCombinations())) {
					ids.add(a.areaName());
				}
			}
		}
		return ids.isEmpty() ? List.of(DashboardProperties.OTHERS_AREA_NAME) : ids;
	}

	private static boolean matchesAny(Map<String, String> labels, List<Map<String, String>> combinations) {
		for (Map<String, String> c : combinations) {
			if (!c.isEmpty() && ProjectMatcher.hasAll(labels, c)) {
				return true;
			}
		}
		return false;
	}
}
