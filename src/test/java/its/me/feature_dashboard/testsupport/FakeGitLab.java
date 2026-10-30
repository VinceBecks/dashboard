package its.me.feature_dashboard.testsupport;

import java.io.IOException;
import java.net.InetSocketAddress;
import java.net.URLDecoder;
import java.nio.charset.StandardCharsets;
import java.util.LinkedHashMap;
import java.util.Map;
import java.util.concurrent.ConcurrentHashMap;

import com.sun.net.httpserver.HttpServer;

/** Fake-GitLab für Trigger-Requests (POST) und Status-Abfragen (GET); merkt sich den letzten Trigger-Request. */
public class FakeGitLab implements AutoCloseable {

	private final HttpServer server;
	private volatile int status = 201;
	private volatile String body = "{\"id\": 4711, \"web_url\": \"https://gitlab.example/p/-/pipelines/4711\"}";
	private final Map<Long, String> pipelineStatus = new ConcurrentHashMap<>();
	public volatile String lastPath;
	public volatile Map<String, String> lastForm = Map.of();
	public volatile String lastPrivateToken;

	public FakeGitLab() throws IOException {
		server = HttpServer.create(new InetSocketAddress("127.0.0.1", 0), 0);
		server.createContext("/", exchange -> {
			if ("POST".equals(exchange.getRequestMethod())) {
				lastPath = exchange.getRequestURI().getRawPath();
				lastForm = parse(new String(exchange.getRequestBody().readAllBytes(), StandardCharsets.UTF_8));
				respond(exchange, status, body);
				return;
			}
			lastPrivateToken = exchange.getRequestHeaders().getFirst("PRIVATE-TOKEN");
			String path = exchange.getRequestURI().getPath();
			long id = Long.parseLong(path.substring(path.lastIndexOf('/') + 1));
			String s = pipelineStatus.get(id);
			if (s == null) {
				respond(exchange, 404, "{\"message\":\"404 Not found\"}");
			}
			else {
				respond(exchange, 200, "{\"id\":" + id + ",\"status\":\"" + s + "\"}");
			}
		});
		server.start();
	}

	private static void respond(com.sun.net.httpserver.HttpExchange exchange, int status, String body) throws IOException {
		byte[] bytes = body.getBytes(StandardCharsets.UTF_8);
		exchange.sendResponseHeaders(status, bytes.length);
		exchange.getResponseBody().write(bytes);
		exchange.close();
	}

	private static Map<String, String> parse(String form) {
		Map<String, String> result = new LinkedHashMap<>();
		for (String pair : form.split("&")) {
			if (pair.isEmpty()) {
				continue;
			}
			int eq = pair.indexOf('=');
			result.put(URLDecoder.decode(pair.substring(0, eq), StandardCharsets.UTF_8),
					URLDecoder.decode(pair.substring(eq + 1), StandardCharsets.UTF_8));
		}
		return result;
	}

	public String url() {
		return "http://127.0.0.1:" + server.getAddress().getPort();
	}

	public FakeGitLab respondWith(int status, String body) {
		this.status = status;
		this.body = body;
		return this;
	}

	public FakeGitLab pipelineStatus(long id, String status) {
		pipelineStatus.put(id, status);
		return this;
	}

	@Override
	public void close() {
		server.stop(0);
	}
}
