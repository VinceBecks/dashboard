package its.me.feature_dashboard.pipelines;

import static org.assertj.core.api.Assertions.assertThat;

import java.time.Duration;
import java.time.Instant;
import java.util.Map;
import java.util.concurrent.atomic.AtomicInteger;

import org.junit.jupiter.api.Test;

import its.me.feature_dashboard.model.Kind;
import its.me.feature_dashboard.model.ObjectRef;

class PipelinesTest {

	private static PipelineEntry entry(String action, long id) {
		return new PipelineEntry(Kind.NAMESPACE, "feat-1", "feat-1", action, id, "team/p", "https://gitlab/p/" + id,
				Instant.parse("2026-10-06T14:00:00Z"));
	}

	@Test
	void setForRemoveAndRevisions() {
		PipelineState state = new PipelineState();
		AtomicInteger signals = new AtomicInteger();
		state.onChange(signals::incrementAndGet);
		state.set(entry("deploy", 1));
		state.set(entry("build", 2));
		state.set(entry("build", 2)); // unverändert: kein Signal
		assertThat(signals.get()).isEqualTo(2);
		assertThat(state.forRef(ObjectRef.namespace("feat-1"))).extracting(PipelineEntry::action).containsExactly("build", "deploy");
		long revision = state.namespaceRevisions().get("feat-1");
		state.remove(ObjectRef.namespace("feat-1"), "build");
		assertThat(state.namespaceRevisions().get("feat-1")).isGreaterThan(revision);
		state.remove(ObjectRef.namespace("feat-1"), "build");
		assertThat(signals.get()).isEqualTo(3);
	}

	@Test
	void trackerRemovesFinishedUnknownAndUntracked() {
		PipelineState state = new PipelineState();
		Map<Long, String> statuses = Map.of(1L, "running", 2L, "success", 3L, "error");
		state.registerChecker("deploy", id -> {
			if (id == 5) {
				throw new StatusChecker.PipelineNotFoundException("weg");
			}
			if (!statuses.containsKey(id)) {
				throw new java.io.IOException("GitLab nicht erreichbar");
			}
			return statuses.get(id);
		});
		for (long id = 1; id <= 6; id++) {
			state.set(new PipelineEntry(Kind.POD, "feat-1", "p" + id, "deploy", id, "p", "u", Instant.EPOCH));
		}
		state.set(new PipelineEntry(Kind.POD, "feat-1", "x", "untracked", 9, "p", "u", Instant.EPOCH));
		new PipelineTracker(state, Duration.ofSeconds(5)).checkOnce();
		// 1 läuft, 3 unbekannter Status, 4 und 6 nicht abfragbar: bleiben; 2 beendet, 5 unbekannt, untracked: entfernt
		assertThat(state.all()).extracting(PipelineEntry::id).containsExactlyInAnyOrder(1L, 3L, 4L, 6L);
		assertThat(PipelineState.finished("failed")).isTrue();
		assertThat(PipelineState.finished("running")).isFalse();
	}
}
