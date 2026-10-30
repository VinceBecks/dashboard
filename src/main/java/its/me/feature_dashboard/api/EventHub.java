package its.me.feature_dashboard.api;

import java.time.Duration;
import java.util.ArrayList;
import java.util.HashMap;
import java.util.HashSet;
import java.util.LinkedHashMap;
import java.util.List;
import java.util.Map;
import java.util.Optional;
import java.util.Set;
import java.util.TreeSet;
import java.util.concurrent.ArrayBlockingQueue;
import java.util.concurrent.BlockingQueue;
import java.util.concurrent.TimeUnit;
import java.util.function.Function;

import org.slf4j.Logger;
import org.slf4j.LoggerFactory;
import org.springframework.context.SmartLifecycle;

import its.me.feature_dashboard.api.Dtos.AreaDto;
import its.me.feature_dashboard.api.Dtos.NamespaceDto;
import its.me.feature_dashboard.api.Dtos.TeamDto;
import its.me.feature_dashboard.api.Flattener.FlatObject;
import its.me.feature_dashboard.config.DashboardProperties;
import its.me.feature_dashboard.config.DashboardProperties.Project;
import its.me.feature_dashboard.config.DashboardProperties.TeamsConfig;
import its.me.feature_dashboard.model.ClusterSource;
import its.me.feature_dashboard.model.NamespaceView;
import its.me.feature_dashboard.model.RevisionSource;
import tools.jackson.databind.ObjectMapper;

/**
 * Vergleicht die Datenquelle nach jeder Änderung mit dem zuletzt verschickten Stand und verteilt die Unterschiede als
 * upsert-/delete-Events an alle verbundenen Clients. Der Hub hält nur JSON der zuletzt verschickten DTOs, keine Kopie
 * der Kubernetes-Objekte. Snapshot und Abo entstehen unter demselben Lock wie das Verteilen der Events, daher geht
 * dazwischen nichts verloren.
 */
public class EventHub implements SmartLifecycle {

	private static final Logger log = LoggerFactory.getLogger(EventHub.class);

	public static final String EVENT_SNAPSHOT = "snapshot";
	public static final String EVENT_UPSERT = "upsert";
	public static final String EVENT_DELETE = "delete";

	/** So viele Events darf ein Client im Rückstand sein; danach wird er getrennt und verbindet sich neu. */
	static final int SUBSCRIBER_BUFFER = 256;

	/** Sicherheitsnetz gegen verpasste Signale: So oft baut der Hub alle Namespaces neu auf. */
	static final Duration FULL_RESYNC_INTERVAL = Duration.ofHours(1);

	/** Server-Sent Event; data ist einzeiliges JSON. */
	public record Event(String type, String data) {
	}

	/** Zuletzt verschickter Stand eines Namespaces, nur als JSON. */
	private record NsState(long revision, long extraRevision, String project, String tree, List<FlatObject> flat) {
	}

	private final ClusterSource source;
	private final RevisionSource extra;
	private final Function<NamespaceView, NamespaceDto> build;
	private final List<Project> projects;
	private final TeamsConfig teams;
	private final ObjectMapper json;

	private final Object lock = new Object();
	private final Map<String, NsState> namespaces = new HashMap<>();
	private final Set<Subscription> subscribers = new HashSet<>();
	private boolean closed;

	private final BlockingQueue<Boolean> changed = new ArrayBlockingQueue<>(1);
	private Thread runner;

	/**
	 * @param extra Daten außerhalb des Clusters, die in die Objekte einfließen (laufende Pipelines); null heißt keine
	 */
	public EventHub(ClusterSource source, RevisionSource extra, Function<NamespaceView, NamespaceDto> build,
			List<Project> projects, TeamsConfig teams, ObjectMapper json) {
		this.source = source;
		this.extra = extra;
		this.build = build;
		this.projects = List.copyOf(projects);
		this.teams = teams;
		this.json = json;
		source.onChange(this::signal);
		if (extra != null) {
			extra.onChange(this::signal);
		}
	}

	private void signal() {
		changed.offer(Boolean.TRUE);
	}

	/** Gleicht bei jeder Änderung ab, bis der Hub gestoppt wird; danach werden alle Clients getrennt. */
	@Override
	public synchronized void start() {
		closed = false;
		runner = Thread.ofVirtual().name("event-hub").start(this::run);
	}

	private void run() {
		long nextFullResync = System.nanoTime() + FULL_RESYNC_INTERVAL.toNanos();
		while (!Thread.currentThread().isInterrupted()) {
			boolean full;
			try {
				long wait = Math.max(0, nextFullResync - System.nanoTime());
				full = changed.poll(wait, TimeUnit.NANOSECONDS) == null;
			}
			catch (InterruptedException e) {
				return;
			}
			if (full) {
				nextFullResync = System.nanoTime() + FULL_RESYNC_INTERVAL.toNanos();
			}
			try {
				synchronized (lock) {
					if (full) {
						namespaces.replaceAll((name, ns) -> new NsState(-1, -1, ns.project(), ns.tree(), ns.flat()));
					}
					syncLocked();
				}
			}
			catch (RuntimeException e) {
				log.error("Abgleich fehlgeschlagen", e);
			}
		}
	}

	@Override
	public synchronized void stop() {
		if (runner != null) {
			runner.interrupt();
			runner = null;
		}
		synchronized (lock) {
			closed = true;
			new ArrayList<>(subscribers).forEach(this::removeLocked);
		}
	}

	@Override
	public synchronized boolean isRunning() {
		return runner != null;
	}

	/** Gleicht sofort ab (für Tests; im Betrieb läuft der Abgleich nach jedem Signal). */
	void syncNow() {
		synchronized (lock) {
			syncLocked();
		}
	}

	/** Registriert einen Client und liefert den aktuellen Snapshot. */
	public Subscription subscribe() {
		synchronized (lock) {
			String snapshot = snapshotJsonLocked();
			Subscription sub = new Subscription(snapshot, this);
			subscribers.add(sub);
			return sub;
		}
	}

	/** Gleicht ab und liefert den Stand als JSON, ohne einen Client zu registrieren. */
	public String snapshot() {
		synchronized (lock) {
			return snapshotJsonLocked();
		}
	}

	private String snapshotJsonLocked() {
		if (closed) {
			throw new HubClosedException();
		}
		syncLocked();
		return snapshotLocked();
	}

	void cancel(Subscription sub) {
		synchronized (lock) {
			removeLocked(sub);
		}
	}

	private void removeLocked(Subscription sub) {
		if (subscribers.remove(sub)) {
			sub.close();
		}
	}

	/** Snapshot: alle Projekte (Reihenfolge der Config) mit ihren Namespaces (nach Name). */
	private String snapshotLocked() {
		Map<String, List<String>> byProject = new HashMap<>();
		for (String name : new TreeSet<>(namespaces.keySet())) {
			NsState ns = namespaces.get(name);
			byProject.computeIfAbsent(ns.project(), p -> new ArrayList<>()).add(ns.tree());
		}
		List<TeamDto> teamDtos = teams.teams()
			.stream()
			.map(t -> new TeamDto(t.teamId(), t.teamLabel(), t.teamColor() == null ? "" : t.teamColor()))
			.toList();
		StringBuilder sb = new StringBuilder("{\"teams\":").append(json.writeValueAsString(teamDtos)).append(",\"projects\":[");
		for (int i = 0; i < projects.size(); i++) {
			Project p = projects.get(i);
			if (i > 0) {
				sb.append(',');
			}
			Map<String, Object> head = new LinkedHashMap<>();
			head.put("name", p.projectName());
			head.put("serviceAreas", serviceAreas(p));
			String headJson = json.writeValueAsString(head);
			sb.append(headJson, 0, headJson.length() - 1)
				.append(",\"namespaces\":[")
				.append(String.join(",", byProject.getOrDefault(p.projectName(), List.of())))
				.append("]}");
		}
		return sb.append("]}").toString();
	}

	/** Bereiche eines Projekts in Config-Reihenfolge, „others“ zuletzt. */
	static List<AreaDto> serviceAreas(Project p) {
		List<AreaDto> areas = new ArrayList<>();
		p.serviceAreas().forEach(a -> areas.add(new AreaDto(a.areaName(), a.areaLabel())));
		areas.add(new AreaDto(DashboardProperties.OTHERS_AREA_NAME, p.othersLabel()));
		return areas;
	}

	/** Gleicht alle Namespaces ab, deren Revision sich geändert hat, und verteilt die Unterschiede. */
	private void syncLocked() {
		Map<String, Long> revisions = source.namespaceRevisions();
		Map<String, Long> extraRevisions = extra == null ? Map.of() : extra.namespaceRevisions();
		List<Event> events = new ArrayList<>();
		for (String name : new TreeSet<>(namespaces.keySet())) {
			if (!revisions.containsKey(name)) {
				events.addAll(diff(namespaces.get(name).flat(), List.of()));
				namespaces.remove(name);
			}
		}
		for (String name : new TreeSet<>(revisions.keySet())) {
			NsState old = namespaces.get(name);
			long extraRevision = extraRevisions.getOrDefault(name, 0L);
			if (old != null && old.revision() == revisions.get(name) && old.extraRevision() == extraRevision) {
				continue;
			}
			Optional<NsState> next;
			try {
				next = load(name, extraRevision);
			}
			catch (RuntimeException e) {
				log.error("Namespace für Events nicht aufbereitet: namespace={}", name, e);
				continue;
			}
			if (next.isEmpty()) {
				continue; // zwischen den Revisionen und dem Lesen entfernt; der nächste Abgleich bestätigt das
			}
			events.addAll(diff(old == null ? List.of() : old.flat(), next.get().flat()));
			namespaces.put(name, next.get());
		}
		for (Event ev : events) {
			for (Subscription sub : new ArrayList<>(subscribers)) {
				if (!sub.offer(ev)) {
					log.warn("Client zu langsam, Event-Stream wird beendet");
					removeLocked(sub);
				}
			}
		}
	}

	private Optional<NsState> load(String name, long extraRevision) {
		return source.namespace(name).map(r -> {
			NamespaceDto tree = build.apply(r.view());
			return new NsState(r.revision(), extraRevision, r.view().namespace().project(),
					json.writeValueAsString(tree), Flattener.flatten(tree, json));
		});
	}

	/** Events, die old in next überführen: zuerst alle delete-, danach alle upsert-Events. */
	List<Event> diff(List<FlatObject> old, List<FlatObject> next) {
		List<Event> events = new ArrayList<>(deleteEvents(old, next));
		events.addAll(upsertEvents(old, next));
		return events;
	}

	/**
	 * delete für jedes Objekt aus old, das in next fehlt, Kind vor Parent. Objekte, deren Parent ebenfalls gelöscht
	 * wird, entfallen, da der Client Kinder mit dem Parent entfernt.
	 */
	private List<Event> deleteEvents(List<FlatObject> old, List<FlatObject> next) {
		Set<String> remaining = new HashSet<>();
		next.forEach(o -> remaining.add(o.key()));
		Set<String> removed = new HashSet<>();
		old.stream().filter(o -> !remaining.contains(o.key())).forEach(o -> removed.add(o.key()));
		List<Event> events = new ArrayList<>();
		for (int i = old.size() - 1; i >= 0; i--) {
			FlatObject o = old.get(i);
			if (!removed.contains(o.key()) || removed.contains(o.parentKey())) {
				continue;
			}
			events.add(new Event(EVENT_DELETE, json.writeValueAsString(o.header())));
		}
		return events;
	}

	/** upsert für jedes neue oder geänderte Objekt aus next, Parent vor Kind. */
	private static List<Event> upsertEvents(List<FlatObject> old, List<FlatObject> next) {
		Map<String, String> before = new HashMap<>();
		old.forEach(o -> before.put(o.key(), o.data()));
		List<Event> events = new ArrayList<>();
		for (FlatObject o : next) {
			if (o.data().equals(before.get(o.key()))) {
				continue;
			}
			events.add(new Event(EVENT_UPSERT, o.data()));
		}
		return events;
	}

	/** Verbundener Client. Wird er getrennt (zu langsam oder Hub beendet), liefert {@link #take} null. */
	public static final class Subscription {

		private static final Event CLOSED = new Event("", "");

		private final String snapshot;
		private final EventHub hub;
		private final BlockingQueue<Event> events = new ArrayBlockingQueue<>(SUBSCRIBER_BUFFER + 1);
		private volatile boolean closed;

		private Subscription(String snapshot, EventHub hub) {
			this.snapshot = snapshot;
			this.hub = hub;
		}

		public String snapshot() {
			return snapshot;
		}

		private boolean offer(Event ev) {
			return events.size() < SUBSCRIBER_BUFFER && events.offer(ev);
		}

		/** Trennt den Client sofort; noch nicht abgeholte Events entfallen (er erhält beim Reconnect einen Snapshot). */
		private void close() {
			closed = true;
			events.clear();
			events.offer(CLOSED);
		}

		/**
		 * Nächstes Event; {@link Optional#empty()} nach timeout ohne Event. Wirft {@link ClosedException}, wenn der
		 * Client getrennt wurde.
		 */
		public Optional<Event> poll(Duration timeout) throws InterruptedException {
			Event ev = events.poll(timeout.toMillis(), TimeUnit.MILLISECONDS);
			if (ev == CLOSED || (ev == null && closed)) {
				throw new ClosedException();
			}
			return Optional.ofNullable(ev);
		}

		/** Meldet den Client ab. */
		public void cancel() {
			hub.cancel(this);
		}
	}

	/** Der Client wurde getrennt. */
	public static class ClosedException extends RuntimeException {
		public ClosedException() {
			super("event-stream beendet");
		}
	}

	/** Der Hub ist beendet (Server fährt herunter). */
	public static class HubClosedException extends RuntimeException {
		public HubClosedException() {
			super("event-hub ist beendet");
		}
	}
}
