package its.me.feature_dashboard.model;

import java.util.List;

public record StoredIngress(Meta meta, List<Rule> rules) implements Stored {

	public StoredIngress {
		rules = List.copyOf(rules);
	}

	public record Rule(String host, List<Path> paths) {
		public Rule {
			paths = List.copyOf(paths);
		}
	}

	public record Path(String pathType, String path, String backendServiceName) {
	}
}
