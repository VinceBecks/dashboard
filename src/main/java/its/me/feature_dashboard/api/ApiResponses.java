package its.me.feature_dashboard.api;

import java.util.LinkedHashMap;
import java.util.List;
import java.util.Map;

import org.springframework.http.HttpStatus;
import org.springframework.http.MediaType;
import org.springframework.http.ResponseEntity;

/** Fehler-Bodies der API: immer {@code {"error": …}}, bei Validierungsfehlern zusätzlich {@code problems}. */
final class ApiResponses {

	private ApiResponses() {
	}

	static ResponseEntity<Object> error(HttpStatus status, String message) {
		return ResponseEntity.status(status).contentType(MediaType.APPLICATION_JSON).body(Map.of("error", message));
	}

	static ResponseEntity<Object> validation(String message, List<String> problems) {
		Map<String, Object> body = new LinkedHashMap<>();
		body.put("error", message);
		body.put("problems", problems);
		return ResponseEntity.status(HttpStatus.BAD_REQUEST).contentType(MediaType.APPLICATION_JSON).body(body);
	}
}
