package its.me.feature_dashboard.model;

import java.util.List;

/** Alle konvertierten Objekte eines Projekt-Namespaces, je Typ nach Name sortiert. */
public record NamespaceView(StoredNamespace namespace, List<StoredService> services, List<StoredIngress> ingresses,
		List<StoredPod> pods) {
}
