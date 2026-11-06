package api

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestCORS(t *testing.T) {
	next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })
	do := func(origins []string, method, origin string, preflight bool) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, "/api/snapshot", nil)
		if origin != "" {
			req.Header.Set("Origin", origin)
		}
		if preflight {
			req.Header.Set("Access-Control-Request-Method", "PATCH")
		}
		rec := httptest.NewRecorder()
		corsMiddleware(origins, next).ServeHTTP(rec, req)
		return rec
	}
	allowed := []string{"https://a.example.com"}

	if got := do(nil, "GET", "https://a.example.com", false).Header().Get("Access-Control-Allow-Origin"); got != "" {
		t.Errorf("ohne Konfiguration keine CORS-Header, war %q", got)
	}
	if got := do(allowed, "GET", "https://a.example.com", false).Header().Get("Access-Control-Allow-Origin"); got != "https://a.example.com" {
		t.Errorf("erlaubte Origin: %q", got)
	}
	if got := do(allowed, "GET", "https://evil.example.com", false).Header().Get("Access-Control-Allow-Origin"); got != "" {
		t.Errorf("fremde Origin darf nicht erlaubt sein: %q", got)
	}
	if got := do([]string{"*"}, "GET", "https://x.example.com", false).Header().Get("Access-Control-Allow-Origin"); got != "*" {
		t.Errorf("Wildcard: %q", got)
	}
	pre := do(allowed, "OPTIONS", "https://a.example.com", true)
	if pre.Code != http.StatusNoContent || pre.Header().Get("Access-Control-Allow-Methods") == "" {
		t.Errorf("Preflight: %d %v", pre.Code, pre.Header())
	}
	if pre := do(allowed, "OPTIONS", "https://evil.example.com", true); pre.Header().Get("Access-Control-Allow-Methods") != "" {
		t.Errorf("Preflight fremder Origin darf nicht erlaubt werden")
	}
}
