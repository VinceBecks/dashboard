package its.me.feature_dashboard.pipelines;

import java.time.Duration;
import java.util.concurrent.Executors;
import java.util.concurrent.ScheduledExecutorService;
import java.util.concurrent.TimeUnit;

import org.slf4j.Logger;
import org.slf4j.LoggerFactory;
import org.springframework.context.SmartLifecycle;

/** Fragt laufende Pipelines regelmäßig bei GitLab ab und entfernt beendete und unbekannte aus dem State. */
public class PipelineTracker implements SmartLifecycle {

	private static final Logger log = LoggerFactory.getLogger(PipelineTracker.class);

	private final PipelineState state;
	private final Duration interval;
	private ScheduledExecutorService executor;

	public PipelineTracker(PipelineState state, Duration interval) {
		this.state = state;
		this.interval = interval;
	}

	/** Prüft alle laufenden Pipelines einmal. */
	public void checkOnce() {
		for (PipelineEntry e : state.all()) {
			if (finished(e)) {
				state.remove(e.ref(), e.action());
			}
		}
	}

	/** Eintrag entfernen: Pipeline beendet, unbekannt oder Aktion ohne Checker. */
	private boolean finished(PipelineEntry e) {
		StatusChecker checker = state.checker(e.action());
		if (checker == null) {
			log.info("Pipeline einer nicht (mehr) verfolgten Aktion wird entfernt: action={} pipeline={}", e.action(),
					e.id());
			return true;
		}
		try {
			return PipelineState.finished(checker.pipelineStatus(e.id()));
		}
		catch (StatusChecker.PipelineNotFoundException ex) {
			return true;
		}
		catch (Exception ex) {
			log.warn("Pipeline-Status nicht abfragbar: action={} pipeline={} error={}", e.action(), e.id(),
					ex.getMessage());
			return false;
		}
	}

	@Override
	public synchronized void start() {
		executor = Executors.newSingleThreadScheduledExecutor(Thread.ofVirtual().name("pipeline-tracker").factory());
		executor.scheduleWithFixedDelay(() -> {
			try {
				checkOnce();
			}
			catch (RuntimeException ex) {
				log.warn("Pipeline-Abfrage fehlgeschlagen: {}", ex.getMessage());
			}
		}, 0, interval.toMillis(), TimeUnit.MILLISECONDS);
	}

	@Override
	public synchronized void stop() {
		if (executor != null) {
			executor.shutdownNow();
			executor = null;
		}
	}

	@Override
	public synchronized boolean isRunning() {
		return executor != null;
	}
}
