package its.me.feature_dashboard.api;

import java.io.IOException;
import java.io.InputStream;

import org.springframework.core.io.ClassPathResource;
import org.springframework.http.HttpStatus;
import org.springframework.http.MediaType;
import org.springframework.http.ResponseEntity;
import org.springframework.web.bind.annotation.GetMapping;
import org.springframework.web.bind.annotation.RestController;

/** Health-Endpunkte wie im Go-Backend und die API-Beschreibung (openapi.yaml aus backend/internal/api). */
@RestController
public class HealthController {

	private final ReadinessCheck readiness;
	private final byte[] openApi;

	public HealthController(ReadinessCheck readiness) throws IOException {
		this.readiness = readiness;
		try (InputStream in = new ClassPathResource("api/openapi.yaml").getInputStream()) {
			this.openApi = in.readAllBytes();
		}
	}

	@GetMapping("/healthz")
	public ResponseEntity<Void> healthz() {
		return ResponseEntity.ok().build();
	}

	/** 200, sobald der Informer-Cache synchronisiert ist, sonst 503. */
	@GetMapping("/readyz")
	public ResponseEntity<Void> readyz() {
		return ResponseEntity.status(readiness.ready() ? HttpStatus.OK : HttpStatus.SERVICE_UNAVAILABLE).build();
	}

	@GetMapping("/api/openapi.yaml")
	public ResponseEntity<byte[]> openApi() {
		return ResponseEntity.ok().contentType(MediaType.parseMediaType("application/yaml")).body(openApi);
	}
}
