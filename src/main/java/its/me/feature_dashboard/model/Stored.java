package its.me.feature_dashboard.model;

/**
 * Die reduzierte, konvertierte Form eines Kubernetes-Objekts (Stored-Object), beim Lesen aus dem Informer-Cache
 * erzeugt.
 */
public sealed interface Stored permits StoredNamespace, StoredService, StoredIngress, StoredPod {

	Meta meta();

	default String name() {
		return meta().name();
	}
}
