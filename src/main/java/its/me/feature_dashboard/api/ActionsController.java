package its.me.feature_dashboard.api;

import java.nio.charset.StandardCharsets;
import java.util.Map;
import java.util.Optional;

import org.slf4j.Logger;
import org.slf4j.LoggerFactory;
import org.springframework.http.HttpStatus;
import org.springframework.http.MediaType;
import org.springframework.http.ResponseEntity;
import org.springframework.web.bind.annotation.PatchMapping;
import org.springframework.web.bind.annotation.PathVariable;
import org.springframework.web.bind.annotation.RequestBody;
import org.springframework.web.bind.annotation.RestController;

import its.me.feature_dashboard.actions.ActionExceptions;
import its.me.feature_dashboard.actions.ActionObject;
import its.me.feature_dashboard.actions.ActionRegistry;
import its.me.feature_dashboard.actions.ActionResult;
import its.me.feature_dashboard.model.ClusterSource;
import its.me.feature_dashboard.model.Kind;
import its.me.feature_dashboard.model.ObjectRef;
import its.me.feature_dashboard.model.Stored;
import its.me.feature_dashboard.model.StoredNamespace;
import tools.jackson.core.type.TypeReference;
import tools.jackson.databind.JsonNode;
import tools.jackson.databind.ObjectMapper;

/**
 * {@code PATCH /api/actions/{action}}: führt eine Aktion auf einem Objekt aus. 200 bei Erfolg; 400 bei ungültigem
 * Body, nicht anwendbarer Aktion oder ungültigen Eingaben (mit problems); 404 bei unbekannter Aktion oder unbekanntem
 * Objekt; 422, wenn der Handler die Aktion nicht ausführen konnte (nie 200); 500 bei internen Fehlern.
 */
@RestController
public class ActionsController {

	private static final Logger log = LoggerFactory.getLogger(ActionsController.class);
	private static final int MAX_BODY = 1 << 20;

	private final ActionRegistry registry;
	private final ClusterSource source;
	private final ObjectMapper json;

	public ActionsController(ActionRegistry registry, ClusterSource source, ObjectMapper json) {
		this.registry = registry;
		this.source = source;
		this.json = json;
	}

	@PatchMapping(path = "/api/actions/{action}", produces = MediaType.APPLICATION_JSON_VALUE)
	public ResponseEntity<Object> execute(@PathVariable("action") String name,
			@RequestBody(required = false) byte[] rawBody) {
		if (!registry.has(name)) {
			return ApiResponses.error(HttpStatus.NOT_FOUND, new ActionExceptions.UnknownActionException().getMessage());
		}
		ObjectRef ref;
		Map<String, Object> inputs;
		try {
			if (rawBody == null || rawBody.length > MAX_BODY) {
				throw new IllegalArgumentException();
			}
			JsonNode body = json.readTree(new String(rawBody, StandardCharsets.UTF_8));
			JsonNode object = body.path("object");
			String kind = object.path("kind").asString("");
			String namespace = object.path("namespace").asString("");
			String objectName = object.path("name").asString("");
			Optional<Kind> parsed = Kind.parse(kind);
			if (parsed.isEmpty()) {
				return ApiResponses.error(HttpStatus.BAD_REQUEST, "ungültiger Objekt-Typ");
			}
			if (parsed.get() == Kind.NAMESPACE && namespace.isEmpty()) {
				namespace = objectName;
			}
			ref = new ObjectRef(parsed.get(), namespace, objectName);
			JsonNode in = body.path("inputs");
			inputs = in.isObject() ? json.convertValue(in, new TypeReference<Map<String, Object>>() {
			}) : Map.of();
		}
		catch (RuntimeException e) {
			return ApiResponses.error(HttpStatus.BAD_REQUEST, "ungültiger Request-Body");
		}
		Optional<ActionObject> obj = load(ref);
		if (obj.isEmpty()) {
			return ApiResponses.error(HttpStatus.NOT_FOUND, "objekt nicht gefunden");
		}
		ActionResult result;
		try {
			result = registry.execute(name, obj.get(), inputs);
		}
		catch (ActionExceptions.ValidationException e) {
			return ApiResponses.validation(e.getMessage(), e.problems());
		}
		catch (ActionExceptions.NotApplicableException e) {
			return ApiResponses.error(HttpStatus.BAD_REQUEST, e.getMessage());
		}
		catch (RuntimeException e) {
			log.error("Aktion fehlgeschlagen: action={} object={}", name, ref, e);
			return ApiResponses.error(HttpStatus.INTERNAL_SERVER_ERROR, String.valueOf(e.getMessage()));
		}
		if (!result.success()) {
			// Der Request war gültig, die Aktion ließ sich aber nicht ausführen (z. B. Objekt inzwischen verändert,
			// Kubernetes oder GitLab lehnt ab). Die Meldung des Handlers ist für den Nutzer formuliert.
			String msg = result.message() == null || result.message().isEmpty() ? "Aktion konnte nicht ausgeführt werden"
					: result.message();
			log.warn("Aktion fehlgeschlagen: action={} object={} project={} message={}", name, ref, obj.get().project(),
					result.message());
			return ApiResponses.error(HttpStatus.UNPROCESSABLE_CONTENT, msg);
		}
		log.info("Aktion ausgeführt: action={} object={} message={}", name, ref, result.message());
		return ResponseEntity.ok().contentType(MediaType.APPLICATION_JSON).body(json.writeValueAsString(result));
	}

	/** Liest das referenzierte Objekt und das Projekt seines Namespaces. */
	private Optional<ActionObject> load(ObjectRef ref) {
		Optional<Stored> obj = source.get(ref);
		if (obj.isEmpty()) {
			return Optional.empty();
		}
		return source.get(ObjectRef.namespace(ref.namespace()))
			.map(ns -> new ActionObject(ref, obj.get(), ((StoredNamespace) ns).project()));
	}
}
