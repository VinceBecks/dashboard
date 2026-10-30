package its.me.feature_dashboard.pipelines;

import java.time.Instant;

import its.me.feature_dashboard.model.Kind;
import its.me.feature_dashboard.model.ObjectRef;

/** Laufende Pipeline, die eine Aktion auf einem Objekt gestartet hat. */
public record PipelineEntry(Kind kind, String namespace, String name, String action, long id, String project,
		String webUrl, Instant startedAt) {

	public ObjectRef ref() {
		return new ObjectRef(kind, namespace, name);
	}
}
