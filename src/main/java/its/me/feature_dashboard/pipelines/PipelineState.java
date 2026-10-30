package its.me.feature_dashboard.pipelines;

import java.util.ArrayList;
import java.util.Comparator;
import java.util.HashMap;
import java.util.List;
import java.util.Map;
import java.util.concurrent.CopyOnWriteArrayList;

import its.me.feature_dashboard.model.ObjectRef;
import its.me.feature_dashboard.model.RevisionSource;

/**
 * Laufende Pipelines, nur im Speicher (nach einem Neustart leer). Je Objekt und Aktion gibt es höchstens einen
 * Eintrag. Änderungen erhöhen die Revision des Namespaces und lösen Events aus wie Änderungen im Cluster.
 */
public class PipelineState implements RevisionSource {

	private record Key(ObjectRef ref, String action) {
	}

	private final Map<Key, PipelineEntry> entries = new HashMap<>();
	private final Map<String, Long> revisions = new HashMap<>();
	private final Map<String, StatusChecker> checkers = new HashMap<>();
	private final List<Runnable> listeners = new CopyOnWriteArrayList<>();
	private long lastRevision;

	/** Merkt sich die zuletzt gestartete Pipeline einer Aktion auf einem Objekt (ersetzt einen älteren Eintrag). */
	public void set(PipelineEntry e) {
		synchronized (this) {
			Key k = new Key(e.ref(), e.action());
			if (e.equals(entries.get(k))) {
				return;
			}
			entries.put(k, e);
			touch(e.namespace());
		}
		notifyListeners();
	}

	/** Entfernt den Eintrag einer Aktion auf einem Objekt, z. B. weil die Pipeline beendet ist. */
	public void remove(ObjectRef ref, String action) {
		synchronized (this) {
			if (entries.remove(new Key(ref, action)) == null) {
				return;
			}
			touch(ref.namespace());
		}
		notifyListeners();
	}

	/** Laufende Pipelines eines Objekts, sortiert nach Aktion. */
	public synchronized List<PipelineEntry> forRef(ObjectRef ref) {
		return entries.entrySet()
			.stream()
			.filter(e -> e.getKey().ref().equals(ref))
			.map(Map.Entry::getValue)
			.sorted(Comparator.comparing(PipelineEntry::action))
			.toList();
	}

	public synchronized List<PipelineEntry> all() {
		return new ArrayList<>(entries.values());
	}

	/** Meldet, wie der Status der Pipelines einer Aktion abgefragt wird. Nur Aktionen mit Checker legen Einträge an. */
	public synchronized void registerChecker(String action, StatusChecker checker) {
		checkers.put(action, checker);
	}

	synchronized StatusChecker checker(String action) {
		return checkers.get(action);
	}

	@Override
	public synchronized Map<String, Long> namespaceRevisions() {
		return new HashMap<>(revisions);
	}

	@Override
	public void onChange(Runnable listener) {
		listeners.add(listener);
	}

	private void touch(String namespace) {
		revisions.put(namespace, ++lastRevision);
	}

	private void notifyListeners() {
		listeners.forEach(Runnable::run);
	}

	/** Meldet, ob eine Pipeline mit diesem GitLab-Status beendet ist. */
	public static boolean finished(String status) {
		return switch (status) {
			case "success", "failed", "canceled", "skipped" -> true;
			default -> false;
		};
	}
}
