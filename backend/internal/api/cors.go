package api

import (
	"net/http"
	"slices"
)

// corsMiddleware erlaubt Browser-Aufrufe von den angegebenen Origins ("*" = alle). Ohne Origins wird der Handler
// unverändert zurückgegeben: keine CORS-Header, Browser blockieren fremde Origins. Die API kennt keine
// Authentifizierung und keine Cookies, daher werden nie Credentials erlaubt.
func corsMiddleware(origins []string, next http.Handler) http.Handler {
	if len(origins) == 0 {
		return next
	}
	any := slices.Contains(origins, "*")
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origin := r.Header.Get("Origin")
		if origin == "" {
			next.ServeHTTP(w, r)
			return
		}
		h := w.Header()
		if !any {
			h.Add("Vary", "Origin")
		}
		if !any && !slices.Contains(origins, origin) {
			next.ServeHTTP(w, r)
			return
		}
		if any {
			h.Set("Access-Control-Allow-Origin", "*")
		} else {
			h.Set("Access-Control-Allow-Origin", origin)
		}
		if r.Method == http.MethodOptions && r.Header.Get("Access-Control-Request-Method") != "" {
			h.Add("Vary", "Access-Control-Request-Method")
			h.Add("Vary", "Access-Control-Request-Headers")
			h.Set("Access-Control-Allow-Methods", "GET, PATCH, OPTIONS")
			h.Set("Access-Control-Allow-Headers", "Content-Type")
			h.Set("Access-Control-Max-Age", "600")
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}
