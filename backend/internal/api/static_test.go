package api

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"feature-dashboard/internal/actions"
	"feature-dashboard/internal/testsource"
)

func TestStatic(t *testing.T) {
	dir := t.TempDir()
	must := func(err error) {
		if err != nil {
			t.Fatal(err)
		}
	}
	must(os.WriteFile(filepath.Join(dir, "index.html"), []byte("<html>index</html>"), 0o644))
	must(os.MkdirAll(filepath.Join(dir, "assets"), 0o755))
	must(os.WriteFile(filepath.Join(dir, "assets", "app-123.js"), []byte("console.log(1)"), 0o644))

	h := NewServer(testsource.New(), actions.NewRegistry(), nil, func() bool { return true }).WithStaticDir(dir).Handler()

	if rec := do(h, "GET", "/assets/app-123.js", ""); rec.Code != 200 || !strings.Contains(rec.Body.String(), "console.log") ||
		!strings.Contains(rec.Header().Get("Cache-Control"), "immutable") {
		t.Errorf("Asset: %d %q", rec.Code, rec.Header().Get("Cache-Control"))
	}
	for _, p := range []string{"/", "/projects/foo/namespaces/bar"} {
		if rec := do(h, "GET", p, ""); rec.Code != 200 || !strings.Contains(rec.Body.String(), "index") {
			t.Errorf("%s: status %d, body %q", p, rec.Code, rec.Body)
		}
	}
	if rec := do(h, "GET", "/api/unknown", ""); rec.Code != 404 {
		t.Errorf("/api/unknown: status %d", rec.Code)
	}
	if rec := do(h, "GET", "/api/openapi.yaml", ""); rec.Code != 200 || !strings.Contains(rec.Body.String(), "openapi") {
		t.Errorf("/api/openapi.yaml: status %d", rec.Code)
	}
}
