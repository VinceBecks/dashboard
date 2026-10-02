package api

import (
	"io/fs"
	"net/http"
	"os"
	"path"
	"strings"
)

// staticHandler liefert den Frontend-Build aus. Unbekannte Pfade außerhalb von /api/ erhalten die index.html,
// damit das clientseitige Routing (z. B. /projects/foo/namespaces/bar) beim direkten Aufruf funktioniert.
func staticHandler(dir string) http.Handler {
	root := os.DirFS(dir)
	files := http.FileServerFS(root)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/api/") {
			http.NotFound(w, r)
			return
		}
		name := strings.TrimPrefix(path.Clean(r.URL.Path), "/")
		if name != "" {
			if info, err := fs.Stat(root, name); err == nil && !info.IsDir() {
				if strings.HasPrefix(name, "assets/") {
					// Dateinamen unter assets/ enthalten einen Content-Hash.
					w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
				}
				files.ServeHTTP(w, r)
				return
			}
		}
		w.Header().Set("Cache-Control", "no-cache")
		http.ServeFileFS(w, r, root, "index.html")
	})
}
