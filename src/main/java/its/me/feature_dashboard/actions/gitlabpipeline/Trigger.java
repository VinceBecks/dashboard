package its.me.feature_dashboard.actions.gitlabpipeline;

import java.io.IOException;
import java.net.URI;
import java.net.URLEncoder;
import java.net.http.HttpClient;
import java.net.http.HttpRequest;
import java.net.http.HttpResponse;
import java.nio.charset.StandardCharsets;
import java.time.Duration;
import java.time.Instant;
import java.time.temporal.ChronoUnit;
import java.util.ArrayList;
import java.util.LinkedHashMap;
import java.util.List;
import java.util.Map;
import java.util.StringJoiner;
import java.util.TreeMap;

import its.me.feature_dashboard.actions.ActionResult;
import its.me.feature_dashboard.pipelines.PipelineEntry;
import its.me.feature_dashboard.pipelines.PipelineState;
import its.me.feature_dashboard.pipelines.StatusChecker;
import its.me.feature_dashboard.template.ObjectTemplate;
import its.me.feature_dashboard.template.ObjectTemplates;
import its.me.feature_dashboard.template.TemplateData;
import its.me.feature_dashboard.template.TemplateFunctions;
import tools.jackson.databind.JsonNode;
import tools.jackson.databind.json.JsonMapper;

/**
 * Startet GitLab-Pipelines über die Pipeline-Trigger-API und fragt ihren Status ab. Wiederverwendbar für alle
 * Handler-Typen, die Pipelines starten.
 */
public class Trigger implements StatusChecker {

	static final Duration DEFAULT_TIMEOUT = Duration.ofSeconds(10);
	private static final int MAX_MESSAGE_LENGTH = 300;
	private static final JsonMapper JSON = JsonMapper.builder().build();

	private final HttpClient client;
	private final Duration timeout;
	private final String projectUrl;
	private final String triggerUrl;
	private final String project;
	private final String token;
	private final String statusToken;
	private final ObjectTemplate ref;
	private final Map<String, ObjectTemplate> variables;

	private PipelineState pipelines;
	private String action;

	private Trigger(TriggerConfig c, String projectUrl, ObjectTemplate ref, Map<String, ObjectTemplate> variables) {
		this.timeout = c.timeout() == null ? DEFAULT_TIMEOUT : c.timeout();
		this.client = HttpClient.newBuilder().connectTimeout(timeout).build();
		this.projectUrl = projectUrl;
		this.triggerUrl = projectUrl + "/trigger/pipeline";
		this.project = c.project();
		this.token = c.token();
		this.statusToken = c.statusToken();
		this.ref = ref;
		this.variables = variables;
	}

	/** Pipeline, die GitLab gestartet hat; id ist 0, wenn die Antwort nicht auswertbar war. */
	public record Pipeline(long id, String webUrl) {
	}

	/** Prüft die Angaben und parst die Templates; problems sammelt alle Fehler. */
	public static Trigger create(TriggerConfig c, List<String> problems) {
		List<String> errs = new ArrayList<>();
		String projectUrl = null;
		URI base = null;
		try {
			base = c.gitlabUrl() == null ? null : new URI(c.gitlabUrl());
		}
		catch (Exception e) {
			// unten gemeldet
		}
		if (base == null || !("http".equals(base.getScheme()) || "https".equals(base.getScheme())) || base.getHost() == null) {
			errs.add("gitlab-url muss eine absolute http(s)-URL sein");
		}
		else {
			projectUrl = c.gitlabUrl().replaceAll("/+$", "") + "/api/v4/projects/"
					+ TemplateFunctions.pathEscape(c.project() == null ? "" : c.project());
		}
		if (c.project() == null || c.project().isEmpty()) {
			errs.add("project fehlt");
		}
		if (c.token() == null || c.token().isEmpty()) {
			errs.add("token fehlt (Trigger-Token als Platzhalter, z. B. token: ${GITLAB_TRIGGER_TOKEN_FOO})");
		}
		else if (unresolved(c.token())) {
			errs.add("token: Platzhalter %s nicht auflösbar (Umgebungsvariable mit dem Trigger-Token fehlt)".formatted(c.token()));
		}
		if (c.statusToken() != null && unresolved(c.statusToken())) {
			errs.add("status-token: Platzhalter %s nicht auflösbar (Umgebungsvariable mit dem Lese-Token fehlt)"
				.formatted(c.statusToken()));
		}
		if (c.timeout() != null && (c.timeout().isZero() || c.timeout().isNegative())) {
			errs.add("timeout \"%s\" ist ungültig".formatted(c.timeout()));
		}
		ObjectTemplate ref = null;
		if (c.ref() == null || c.ref().isEmpty()) {
			errs.add("ref fehlt");
		}
		else {
			try {
				ref = ObjectTemplates.parse("ref", c.ref());
			}
			catch (ObjectTemplates.TemplateException e) {
				errs.add(e.getMessage());
			}
		}
		Map<String, ObjectTemplate> variables = new LinkedHashMap<>();
		(c.variables() == null ? Map.<String, String>of() : new TreeMap<>(c.variables())).forEach((key, value) -> {
			try {
				variables.put(key, ObjectTemplates.parse("variables." + key, value));
			}
			catch (ObjectTemplates.TemplateException e) {
				errs.add(e.getMessage());
			}
		});
		problems.addAll(errs);
		return errs.isEmpty() ? new Trigger(c, projectUrl, ref, variables) : null;
	}

	/**
	 * Merkt sich gestartete Pipelines in state, wenn ein status-token gesetzt ist; ohne bleibt die Pipeline
	 * unverfolgt.
	 */
	public void track(PipelineState state, String action) {
		if (state == null || statusToken == null || statusToken.isEmpty()) {
			return;
		}
		this.pipelines = state;
		this.action = action;
		state.registerChecker(action, this);
	}

	/** Rendert ref und Variablen mit data und startet die Pipeline; bei aktivem track wird sie gemerkt. */
	public ActionResult run(TemplateData data) {
		String renderedRef;
		Map<String, String> vars = new LinkedHashMap<>();
		try {
			renderedRef = ObjectTemplates.render(ref, data);
			for (var e : variables.entrySet()) {
				vars.put(e.getKey(), ObjectTemplates.render(e.getValue(), data));
			}
		}
		catch (ObjectTemplates.TemplateException e) {
			return ActionResult.failure(e.getMessage());
		}
		Pipeline pipeline;
		try {
			pipeline = start(renderedRef, vars, Map.of());
		}
		catch (TriggerException e) {
			return ActionResult.failure(e.getMessage());
		}
		if (pipeline.id() == 0) {
			return ActionResult.ok("Pipeline für ref %s gestartet".formatted(renderedRef));
		}
		if (pipelines != null) {
			pipelines.set(new PipelineEntry(its.me.feature_dashboard.model.Kind.parse(data.kind()).orElseThrow(),
					data.namespace(), data.name(), action, pipeline.id(), project, pipeline.webUrl(),
					Instant.now().truncatedTo(ChronoUnit.SECONDS)));
		}
		return ActionResult.ok("Pipeline #%d gestartet: %s".formatted(pipeline.id(), pipeline.webUrl()));
	}

	/**
	 * Startet die Pipeline für ref mit fertigen Werten: variables als CI/CD-Variablen ({@code variables[KEY]}), inputs
	 * als Pipeline-Inputs ({@code inputs[KEY]}). Merkt sich nichts.
	 */
	public Pipeline start(String ref, Map<String, String> variables, Map<String, String> inputs) {
		StringJoiner form = new StringJoiner("&");
		form.add(param("token", token));
		form.add(param("ref", ref));
		new TreeMap<>(variables).forEach((k, v) -> form.add(param("variables[" + k + "]", v)));
		new TreeMap<>(inputs).forEach((k, v) -> form.add(param("inputs[" + k + "]", v)));
		HttpRequest request = HttpRequest.newBuilder(URI.create(triggerUrl))
			.timeout(timeout)
			.header("Content-Type", "application/x-www-form-urlencoded")
			.POST(HttpRequest.BodyPublishers.ofString(form.toString()))
			.build();
		HttpResponse<String> res;
		try {
			res = client.send(request, HttpResponse.BodyHandlers.ofString());
		}
		catch (IOException e) {
			// Die Meldung enthält höchstens die URL; das Token steht im Body und taucht nicht auf.
			throw new TriggerException("GitLab nicht erreichbar: " + e.getMessage());
		}
		catch (InterruptedException e) {
			Thread.currentThread().interrupt();
			throw new TriggerException("GitLab nicht erreichbar: unterbrochen");
		}
		if (res.statusCode() != 201 && res.statusCode() != 200) {
			throw new TriggerException("GitLab antwortete mit %d: %s".formatted(res.statusCode(), gitlabMessage(res.body())));
		}
		try {
			JsonNode body = JSON.readTree(res.body());
			return new Pipeline(body.path("id").asLong(0), body.path("web_url").asString(""));
		}
		catch (RuntimeException e) {
			return new Pipeline(0, "");
		}
	}

	@Override
	public String pipelineStatus(long id) throws Exception {
		HttpRequest request = HttpRequest.newBuilder(URI.create(projectUrl + "/pipelines/" + id))
			.timeout(timeout)
			.header("PRIVATE-TOKEN", statusToken)
			.GET()
			.build();
		HttpResponse<String> res = client.send(request, HttpResponse.BodyHandlers.ofString());
		if (res.statusCode() == 404) {
			throw new PipelineNotFoundException("Pipeline " + id + " unbekannt");
		}
		if (res.statusCode() != 200) {
			throw new IOException("GitLab antwortete mit %d: %s".formatted(res.statusCode(), gitlabMessage(res.body())));
		}
		String status = JSON.readTree(res.body()).path("status").asString("");
		if (status.isEmpty()) {
			throw new IOException("unerwartete Antwort von GitLab: " + gitlabMessage(res.body()));
		}
		return status;
	}

	/** Spring lässt nicht auflösbare Platzhalter beim Binden stehen; ein solches Token darf nicht genutzt werden. */
	private static boolean unresolved(String value) {
		return value.contains("${");
	}

	private static String param(String key, String value) {
		return URLEncoder.encode(key, StandardCharsets.UTF_8) + "=" + URLEncoder.encode(value, StandardCharsets.UTF_8);
	}

	/** Meldung aus einer GitLab-Fehlerantwort ({@code message} bzw. {@code error}), gekürzt. */
	static String gitlabMessage(String body) {
		String msg = body == null ? "" : body.strip();
		try {
			JsonNode node = JSON.readTree(msg);
			if (node.has("message")) {
				msg = text(node.get("message"));
			}
			else if (node.has("error")) {
				msg = text(node.get("error"));
			}
		}
		catch (RuntimeException e) {
			// kein JSON: Rohtext
		}
		return msg.length() > MAX_MESSAGE_LENGTH ? msg.substring(0, MAX_MESSAGE_LENGTH) + "…" : msg;
	}

	private static String text(JsonNode node) {
		return node.isString() ? node.asString() : node.toString();
	}

	/** Pipeline konnte nicht gestartet werden; die Meldung ist für den Nutzer formuliert. */
	public static class TriggerException extends RuntimeException {
		public TriggerException(String message) {
			super(message);
		}
	}
}
