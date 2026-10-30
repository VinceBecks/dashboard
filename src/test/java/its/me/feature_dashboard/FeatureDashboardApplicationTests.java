package its.me.feature_dashboard;

import static org.assertj.core.api.Assertions.assertThat;
import static org.awaitility.Awaitility.await;

import java.io.BufferedReader;
import java.io.InputStream;
import java.io.InputStreamReader;
import java.net.URI;
import java.net.http.HttpClient;
import java.net.http.HttpRequest;
import java.net.http.HttpResponse;
import java.nio.charset.StandardCharsets;
import java.time.Duration;
import java.util.ArrayList;
import java.util.List;
import java.util.Map;
import java.util.concurrent.BlockingQueue;
import java.util.concurrent.LinkedBlockingQueue;
import java.util.concurrent.TimeUnit;

import org.junit.jupiter.api.Test;
import org.springframework.boot.test.context.SpringBootTest;
import org.springframework.boot.test.context.TestConfiguration;
import org.springframework.boot.test.web.server.LocalServerPort;
import org.springframework.context.annotation.Bean;
import org.springframework.context.annotation.Primary;

import io.fabric8.kubernetes.api.model.NamespaceBuilder;
import io.fabric8.kubernetes.api.model.PodBuilder;
import io.fabric8.kubernetes.client.KubernetesClient;
import io.fabric8.kubernetes.client.server.mock.EnableKubernetesMockClient;

/** Startet die ganze Anwendung gegen den fabric8-Mock-Server und prüft die HTTP-API von außen. */
@EnableKubernetesMockClient(crud = true)
@SpringBootTest(webEnvironment = SpringBootTest.WebEnvironment.RANDOM_PORT,
		properties = "spring.config.additional-location=classpath:test-dashboard.yml")
class FeatureDashboardApplicationTests {

	static KubernetesClient client;

	@TestConfiguration
	static class MockCluster {
		@Bean
		@Primary
		KubernetesClient mockKubernetesClient() {
			return client;
		}
	}

	@LocalServerPort
	int port;

	private final HttpClient http = HttpClient.newBuilder().version(HttpClient.Version.HTTP_1_1).build();

	private HttpResponse<String> send(String method, String path, String body) throws Exception {
		HttpRequest.Builder b = HttpRequest.newBuilder(URI.create("http://localhost:" + port + path));
		b = body == null ? b.method(method, HttpRequest.BodyPublishers.noBody())
				: b.header("Content-Type", "application/json").method(method, HttpRequest.BodyPublishers.ofString(body));
		return http.send(b.build(), HttpResponse.BodyHandlers.ofString());
	}

	/** Liest den SSE-Stream in einem eigenen Thread; Events kommen mit Timeout aus der Queue. */
	private static BlockingQueue<List<String>> events(InputStream body) {
		BlockingQueue<List<String>> events = new LinkedBlockingQueue<>();
		Thread.ofVirtual().start(() -> {
			try (BufferedReader reader = new BufferedReader(new InputStreamReader(body, StandardCharsets.UTF_8))) {
				List<String> lines = new ArrayList<>();
				for (String line = reader.readLine(); line != null; line = reader.readLine()) {
					if (!line.isEmpty()) {
						// SSE: ein Leerzeichen nach dem Doppelpunkt ist optional ("data:x" == "data: x").
						int colon = line.indexOf(':');
						lines.add(colon > 0 && line.startsWith(" ", colon + 1)
								? line.substring(0, colon + 1) + line.substring(colon + 2) : line);
					}
					else if (!lines.isEmpty() && !lines.get(0).startsWith(":")) {
						events.add(lines);
						lines = new ArrayList<>();
					}
					else {
						lines = new ArrayList<>();
					}
				}
			}
			catch (Exception e) {
				// Stream beendet
			}
		});
		return events;
	}

	private static List<String> next(BlockingQueue<List<String>> events) throws InterruptedException {
		List<String> ev = events.poll(15, TimeUnit.SECONDS);
		assertThat(ev).as("Event innerhalb von 15 s").isNotNull();
		return ev;
	}

	@Test
	void dashboardEndToEnd() throws Exception {
		client.namespaces().resource(new NamespaceBuilder().withNewMetadata().withName("feat-1")
			.addToLabels("mycomp.de/project-name", "foo").endMetadata().build()).create();
		await().atMost(Duration.ofSeconds(15)).until(() -> send("GET", "/readyz", null).statusCode() == 200);
		assertThat(send("GET", "/healthz", null).statusCode()).isEqualTo(200);

		await().atMost(Duration.ofSeconds(10)).until(() -> send("GET", "/api/snapshot", null).body().contains("\"name\":\"feat-1\""));
		String snapshot = send("GET", "/api/snapshot", null).body();
		assertThat(snapshot).startsWith("{\"teams\":[{\"id\":\"checkout\",\"label\":\"Team Checkout\",\"teamColor\":\"\"}],\"projects\":[{\"name\":\"foo\"")
			.contains("{\"kind\":\"namespace\",\"name\":\"feat-1\",\"parents\":{\"project\":\"foo\"},\"actions\":[{\"id\":\"exclude-from-nightly-scale-down\",\"title\":\"Nachts nicht herunterskalieren\",\"icon\":\"sun\",\"iconColor\":\"\"}],\"links\":[{\"id\":\"logs\",\"title\":\"Logs\",\"icon\":\"\",\"iconColor\":\"\",\"url\":\"https://kibana.example/feat-1\"}],\"copyCommands\":[],\"pipelines\":[],\"services\":[],\"nodePortServices\":[],\"ingresses\":[]}");

		// SSE: zuerst der Snapshot, danach Änderungen als upsert.
		// Eigener Client für den Stream: Er hält seine Verbindung offen, die übrigen Requests laufen getrennt davon.
		HttpClient streamClient = HttpClient.newBuilder().version(HttpClient.Version.HTTP_1_1).build();
		HttpResponse<InputStream> stream = streamClient.send(
				HttpRequest.newBuilder(URI.create("http://localhost:" + port + "/api/events")).build(),
				HttpResponse.BodyHandlers.ofInputStream());
		assertThat(stream.statusCode()).isEqualTo(200);
		assertThat(stream.headers().firstValue("Content-Type")).hasValueSatisfying(v -> assertThat(v).startsWith("text/event-stream"));
		BlockingQueue<List<String>> events = events(stream.body());
		{
			List<String> first = next(events);
			assertThat(first.get(0)).isEqualTo("id:1");
			assertThat(first.get(1)).isEqualTo("event:snapshot");
			assertThat(first.get(2)).startsWith("data:{\"teams\":");

			// Aktion: Namespace vom nächtlichen Herunterskalieren ausnehmen.
			var result = send("PATCH", "/api/actions/exclude-from-nightly-scale-down",
					"{\"object\":{\"kind\":\"namespace\",\"name\":\"feat-1\"}}");
			assertThat(result.statusCode()).isEqualTo(200);
			assertThat(result.body()).isEqualTo("{\"success\":true,\"message\":\"Namespace feat-1 wird nachts nicht mehr herunterskaliert\"}");
			assertThat(client.namespaces().withName("feat-1").get().getMetadata().getLabels())
				.containsEntry("mycomp.de/skip-nightly-scale-down", "true");

			// Die Änderung kommt über den Informer: upsert mit der jeweils anderen Aktion.
			List<String> next = next(events);
			assertThat(next.get(0)).isEqualTo("id:2");
			assertThat(next.get(1)).isEqualTo("event:upsert");
			assertThat(next.get(2)).contains("\"actions\":[{\"id\":\"include-in-nightly-scale-down\"");
		}

		// Ein Pod ohne Service erscheint nicht, er hängt nur unter Services.
		client.pods().inNamespace("feat-1").resource(new PodBuilder().withNewMetadata().withName("lonely")
			.withNamespace("feat-1").endMetadata().build()).create();

		assertThat(send("PATCH", "/api/actions/nope", "{}").statusCode()).isEqualTo(404);
		assertThat(send("PATCH", "/api/actions/delete-pod", "{\"object\":{\"kind\":\"pod\",\"namespace\":\"feat-1\",\"name\":\"x\"}}")
			.statusCode()).isEqualTo(404);

		var openapi = send("GET", "/api/openapi.yaml", null);
		assertThat(openapi.statusCode()).isEqualTo(200);
		assertThat(openapi.body()).startsWith("openapi:");
		assertThat(send("GET", "/api/swagger", null).statusCode()).isBetween(200, 399);
	}
}
