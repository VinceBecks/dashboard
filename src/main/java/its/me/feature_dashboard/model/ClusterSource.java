package its.me.feature_dashboard.model;

import java.util.List;
import java.util.Optional;

/**
 * Lesender Zugriff auf die Projekt-Namespaces und ihre Objekte, im Betrieb der Informer-Cache ({@code kube.Cluster}),
 * in Tests eine In-Memory-Quelle. Alle Werte sind Kopien.
 */
public interface ClusterSource extends RevisionSource {

	/** Alle konvertierten Objekte eines Projekt-Namespaces samt Revision; leer, wenn er fehlt oder zu keinem Projekt gehört. */
	Optional<RevisedNamespaceView> namespace(String name);

	/** Ein einzelnes Objekt eines Projekt-Namespaces; Services mit aufgelösten Replicas und targetPorts. */
	default Optional<Stored> get(ObjectRef ref) {
		return namespace(ref.namespace()).flatMap(r -> {
			NamespaceView view = r.view();
			return switch (ref.kind()) {
				case NAMESPACE -> ref.name().equals(ref.namespace()) ? Optional.of(view.namespace()) : Optional.empty();
				case SERVICE -> view.services().stream().filter(s -> s.name().equals(ref.name())).<Stored>map(s -> s).findFirst();
				case INGRESS -> view.ingresses().stream().filter(s -> s.name().equals(ref.name())).<Stored>map(s -> s).findFirst();
				case POD -> view.pods().stream().filter(s -> s.name().equals(ref.name())).<Stored>map(s -> s).findFirst();
			};
		});
	}

	/** Alle Services eines Projekt-Namespaces (leer, wenn er unbekannt ist). */
	default List<StoredService> services(String namespace) {
		return namespace(namespace).map(r -> r.view().services()).orElse(List.of());
	}

	/** NamespaceView mit der Revision, aus der sie gebaut wurde. */
	record RevisedNamespaceView(NamespaceView view, long revision) {
	}
}
