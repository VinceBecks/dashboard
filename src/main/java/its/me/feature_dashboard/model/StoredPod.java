package its.me.feature_dashboard.model;

import java.util.List;

/** Pod mit Ready-Zustand (keine Phase) und Containern. */
public record StoredPod(Meta meta, boolean ready, List<Container> containers) implements Stored {

	public StoredPod {
		containers = List.copyOf(containers);
	}

	public record Container(String name, String image, List<ContainerPort> ports) {
		public Container {
			ports = List.copyOf(ports);
		}
	}

	public record ContainerPort(String name, int containerPort, String protocol) {
	}
}
