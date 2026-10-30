package its.me.feature_dashboard.model;

import java.util.Map;

/**
 * Quelle mit einer Revision je Namespace (Cluster-Cache, laufende Pipelines). Revisionen steigen streng monoton;
 * eine geänderte Revision heißt: Der Namespace muss neu aufgebaut werden.
 */
public interface RevisionSource {

	/** Revision je Namespace (Kopie). */
	Map<String, Long> namespaceRevisions();

	/** Meldet jede Änderung an listener (koaleszierend; welcher Namespace, zeigen die Revisionen). */
	void onChange(Runnable listener);
}
