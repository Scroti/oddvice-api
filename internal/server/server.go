// Package server wires up the HTTP routes, handlers, and middleware.
package server

import (
	"log/slog"
	"net/http"

	"github.com/oddvice/api/internal/config"
)

// Version is the API version, overridable at build time via -ldflags.
var Version = "0.1.0"

// Server holds dependencies shared across handlers.
type Server struct {
	cfg    config.Config
	logger *slog.Logger
}

// New builds the fully-configured HTTP handler for the API.
func New(cfg config.Config, logger *slog.Logger) http.Handler {
	s := &Server{cfg: cfg, logger: logger}

	mux := http.NewServeMux()
	s.routes(mux)

	// Middleware runs outermost-first: recover -> log -> CORS -> mux.
	var handler http.Handler = mux
	handler = corsMiddleware(cfg.AllowedOrigins)(handler)
	handler = loggingMiddleware(logger)(handler)
	handler = requestIDMiddleware(handler)
	handler = recoverMiddleware(logger)(handler)
	return handler
}

// routes registers all application routes (Go 1.22+ method-aware patterns).
func (s *Server) routes(mux *http.ServeMux) {
	mux.HandleFunc("GET /healthz", s.handleHealth)
	mux.HandleFunc("GET /readyz", s.handleReady)
	mux.HandleFunc("GET /api/v1/version", s.handleVersion)
	mux.HandleFunc("GET /api/v1/ping", s.handlePing)
}
