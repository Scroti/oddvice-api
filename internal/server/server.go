// Package server wires up the HTTP routes, handlers, and middleware.
package server

import (
	"log/slog"
	"net/http"

	"github.com/oddvice/api/internal/config"
	"github.com/oddvice/api/internal/football"
	"github.com/oddvice/api/internal/football/footballdata"
	"github.com/oddvice/api/internal/news"
	"github.com/oddvice/api/internal/news/googlenews"
)

// Version is the API version, overridable at build time via -ldflags.
var Version = "0.1.0"

// Server holds dependencies shared across the generic handlers.
type Server struct {
	cfg    config.Config
	logger *slog.Logger
}

// New builds the fully-configured HTTP handler for the API.
func New(cfg config.Config, logger *slog.Logger) http.Handler {
	s := &Server{cfg: cfg, logger: logger}

	mux := http.NewServeMux()
	s.routes(mux)
	registerFeatures(mux, cfg, logger)

	// Middleware runs outermost-first: recover -> request-id -> log -> CORS.
	var handler http.Handler = mux
	handler = corsMiddleware(cfg.AllowedOrigins)(handler)
	handler = loggingMiddleware(logger)(handler)
	handler = requestIDMiddleware(handler)
	handler = recoverMiddleware(logger)(handler)
	return handler
}

// routes registers the generic/system routes (Go 1.22+ method patterns).
func (s *Server) routes(mux *http.ServeMux) {
	mux.HandleFunc("GET /healthz", s.handleHealth)
	mux.HandleFunc("GET /readyz", s.handleReady)
	mux.HandleFunc("GET /api/v1/version", s.handleVersion)
	mux.HandleFunc("GET /api/v1/ping", s.handlePing)
}

// registerFeatures builds and mounts the feature modules.
func registerFeatures(mux *http.ServeMux, cfg config.Config, logger *slog.Logger) {
	// Football (football-data.org)
	footballClient := &http.Client{Timeout: cfg.Football.Timeout}
	footballProvider := footballdata.New(
		cfg.Football.BaseURL,
		cfg.Football.APIKey,
		cfg.Football.Competition,
		cfg.Football.CacheTTL,
		footballClient,
	)
	football.NewHandler(football.NewService(footballProvider), logger).Register(mux)

	// News
	newsClient := &http.Client{Timeout: cfg.News.Timeout}
	newsProvider := googlenews.New(cfg.News.FeedURL, cfg.News.Limit, newsClient)
	news.NewHandler(news.NewService(newsProvider), logger).Register(mux)
}
