package its.me.feature_dashboard.actions;

import its.me.feature_dashboard.model.ObjectRef;
import its.me.feature_dashboard.model.Stored;

/** Objekt, auf dem eine Aktion angeboten bzw. ausgeführt wird; project ist das Dashboard-Projekt seines Namespaces. */
public record ActionObject(ObjectRef ref, Stored object, String project) {
}
