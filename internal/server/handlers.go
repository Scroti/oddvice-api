package server

import (
	"net/http"
	"time"

	"github.com/oddvice/api/internal/httpx"
)

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	httpx.WriteJSON(w, http.StatusOK, map[string]string{
		"status":  "ok",
		"service": "oddvice-api",
		"time":    time.Now().UTC().Format(time.RFC3339),
	})
}

// handleReady is a readiness probe. Extend it to check DB/cache connectivity.
func (s *Server) handleReady(w http.ResponseWriter, r *http.Request) {
	httpx.WriteJSON(w, http.StatusOK, map[string]string{"status": "ready"})
}

func (s *Server) handleVersion(w http.ResponseWriter, r *http.Request) {
	httpx.WriteJSON(w, http.StatusOK, map[string]string{
		"service": "oddvice-api",
		"version": Version,
		"env":     s.cfg.Env,
	})
}

func (s *Server) handlePing(w http.ResponseWriter, r *http.Request) {
	httpx.WriteJSON(w, http.StatusOK, map[string]string{"message": "pong"})
}
