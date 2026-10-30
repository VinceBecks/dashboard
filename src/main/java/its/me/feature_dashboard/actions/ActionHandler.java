package its.me.feature_dashboard.actions;

import java.util.List;
import java.util.Map;

/**
 * Setzt genau eine Aktion um. Er verändert nur den Cluster (oder ein externes System); das Dashboard aktualisiert sich
 * ausschließlich über die Informer-Events.
 */
public interface ActionHandler {

	/** Übernimmt die handler-spezifischen extra-handler-configs; Rückgabe sind Fehler, die den Start abbrechen. */
	default List<String> configure(ExtraConfig extra) {
		return List.of();
	}

	/** Entscheidet, ob die Aktion auf obj ausführbar ist. Die Projekte prüft bereits die Registry. */
	boolean isApplicable(ActionObject obj);

	/** Benötigte Eingaben; null oder leer, wenn keine nötig sind. */
	default List<FormField> formFields(ActionObject obj) {
		return null;
	}

	/** Führt die Aktion aus. inputs sind bereits gegen formFields validiert. */
	ActionResult execute(ActionObject obj, Map<String, Object> inputs);
}
