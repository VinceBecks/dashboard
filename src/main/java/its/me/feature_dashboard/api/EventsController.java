package its.me.feature_dashboard.api;

import java.io.IOException;
import java.time.Duration;
import java.util.Optional;

import org.springframework.http.HttpStatus;
import org.springframework.http.MediaType;
import org.springframework.http.ResponseEntity;
import org.springframework.web.bind.annotation.ExceptionHandler;
import org.springframework.web.bind.annotation.GetMapping;
import org.springframework.web.bind.annotation.RestController;
import org.springframework.web.servlet.mvc.method.annotation.SseEmitter;

import its.me.feature_dashboard.api.EventHub.Event;
import its.me.feature_dashboard.api.EventHub.Subscription;

/**
 * {@code GET /api/events}: zuerst ein snapshot-Event mit allen Projekten, danach upsert- und delete-Events. Ein
 * Reconnect erhält immer einen neuen Snapshot; Last-Event-ID wird ignoriert. {@code GET /api/snapshot}: derselbe
 * Snapshot einmalig als JSON.
 */
@RestController
public class EventsController {

	/** Hält Event-Streams über Proxys hinweg offen. */
	static final Duration PING_INTERVAL = Duration.ofSeconds(25);

	private final EventHub hub;
	private final ReadinessCheck readiness;

	public EventsController(EventHub hub, ReadinessCheck readiness) {
		this.hub = hub;
		this.readiness = readiness;
	}

	@GetMapping(path = "/api/events", produces = MediaType.TEXT_EVENT_STREAM_VALUE)
	public ResponseEntity<SseEmitter> events() {
		if (!readiness.ready()) {
			throw new UnavailableException("informer-Cache ist noch nicht synchronisiert");
		}
		Subscription sub;
		try {
			sub = hub.subscribe();
		}
		catch (EventHub.HubClosedException e) {
			throw new UnavailableException(e.getMessage());
		}
		SseEmitter emitter = new SseEmitter(0L);
		emitter.onCompletion(sub::cancel);
		emitter.onTimeout(sub::cancel);
		emitter.onError(e -> sub.cancel());
		Thread.ofVirtual().name("sse-client").start(() -> stream(sub, emitter));
		return ResponseEntity.ok().header("Cache-Control", "no-cache").header("X-Accel-Buffering", "no").body(emitter);
	}

	/** Schickt den Snapshot und danach alle Events des Abos; alle 25 Sekunden ein Ping-Kommentar. */
	private static void stream(Subscription sub, SseEmitter emitter) {
		int id = 0;
		try {
			send(emitter, ++id, EventHub.EVENT_SNAPSHOT, sub.snapshot());
			while (true) {
				Optional<Event> ev = sub.poll(PING_INTERVAL);
				if (ev.isPresent()) {
					send(emitter, ++id, ev.get().type(), ev.get().data());
				}
				else {
					emitter.send(SseEmitter.event().comment(" ping"));
				}
			}
		}
		catch (EventHub.ClosedException e) {
			// Client zu langsam oder Server fährt herunter: Stream endet, der Browser verbindet sich neu.
			emitter.complete();
		}
		catch (IOException | IllegalStateException e) {
			// Client getrennt.
		}
		catch (InterruptedException e) {
			Thread.currentThread().interrupt();
			emitter.complete();
		}
		finally {
			sub.cancel();
		}
	}

	private static void send(SseEmitter emitter, int id, String type, String data) throws IOException {
		emitter.send(SseEmitter.event().id(Integer.toString(id)).name(type).data(data, MediaType.TEXT_PLAIN));
	}

	@GetMapping("/api/snapshot")
	public ResponseEntity<Object> snapshot() {
		if (!readiness.ready()) {
			return ApiResponses.error(HttpStatus.SERVICE_UNAVAILABLE, "informer-Cache ist noch nicht synchronisiert");
		}
		String snapshot;
		try {
			snapshot = hub.snapshot();
		}
		catch (EventHub.HubClosedException e) {
			return ApiResponses.error(HttpStatus.SERVICE_UNAVAILABLE, e.getMessage());
		}
		catch (RuntimeException e) {
			return ApiResponses.error(HttpStatus.INTERNAL_SERVER_ERROR, e.getMessage());
		}
		return ResponseEntity.ok().contentType(MediaType.APPLICATION_JSON).header("Cache-Control", "no-cache").body(snapshot);
	}

	/** 503 mit dem üblichen Fehler-Body, solange der Cache nicht synchron ist oder der Server herunterfährt. */
	@ExceptionHandler(UnavailableException.class)
	ResponseEntity<Object> unavailable(UnavailableException e) {
		return ApiResponses.error(HttpStatus.SERVICE_UNAVAILABLE, e.getMessage());
	}

	static class UnavailableException extends RuntimeException {
		UnavailableException(String message) {
			super(message);
		}
	}
}
