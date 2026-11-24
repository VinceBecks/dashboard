package api

import (
	"errors"
	"net/http"
)

func (s *Server) corsAllowed(origin string) bool {
	return origin != "" && s.corsOrigins[origin]
}

func (s *Server) writeCORSHeaders(w http.ResponseWriter, origin string) bool {
	if origin == "" {
		return false
	}
	w.Header().Add("Vary", "Origin")
	if !s.corsAllowed(origin) {
		return false
	}
	w.Header().Set("Access-Control-Allow-Origin", origin)
	return true
}

func (s *Server) withCORS(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		s.writeCORSHeaders(w, r.Header.Get("Origin"))
		next(w, r)
	}
}

func (s *Server) handleSnapshotPreflight(w http.ResponseWriter, r *http.Request) {
	allowed := s.writeCORSHeaders(w, r.Header.Get("Origin"))
	if !allowed {
		writeError(w, http.StatusMethodNotAllowed, errors.New("preflight für diesen origin nicht erlaubt"))
		return
	}
	h := w.Header()
	h.Set("Access-Control-Allow-Methods", "GET, OPTIONS")
	h.Set("Access-Control-Max-Age", "600")
	w.WriteHeader(http.StatusNoContent)
}
