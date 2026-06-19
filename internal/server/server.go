// Package server wires up the HTTP routes, handlers, and middleware.
package server

import (
	"context"
	"log/slog"
	"net/http"

	"github.com/oddvice/api/internal/commentary"
	"github.com/oddvice/api/internal/config"
	"github.com/oddvice/api/internal/football"
	"github.com/oddvice/api/internal/football/footballdata"
	"github.com/oddvice/api/internal/news"
	"github.com/oddvice/api/internal/news/googlenews"
	"github.com/oddvice/api/internal/push"
	"github.com/oddvice/api/internal/teams"
	"github.com/oddvice/api/internal/teams/apifootball"
	"github.com/oddvice/api/internal/tips"
)

// Version is the API version, overridable at build time via -ldflags.
var Version = "0.1.0"

// Server holds dependencies shared across the generic handlers.
type Server struct {
	cfg    config.Config
	logger *slog.Logger
}

// New builds the fully-configured HTTP handler for the API.
// ctx is used for background goroutines (e.g. the push goal watcher) and
// should be cancelled when the server is shutting down.
func New(ctx context.Context, cfg config.Config, logger *slog.Logger) http.Handler {
	s := &Server{cfg: cfg, logger: logger}

	mux := http.NewServeMux()
	s.routes(mux)
	registerFeatures(ctx, mux, cfg, logger)

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
func registerFeatures(ctx context.Context, mux *http.ServeMux, cfg config.Config, logger *slog.Logger) {
	// Football (football-data.org) — shared with the tips feature.
	footballClient := &http.Client{Timeout: cfg.Football.Timeout}
	footballProvider := footballdata.New(
		cfg.Football.BaseURL,
		cfg.Football.APIKey,
		cfg.Football.Competition,
		cfg.Football.CacheTTL,
		footballClient,
	)
	footballService := football.NewService(footballProvider)
	football.NewHandler(footballService, logger).Register(mux)

	// Teams (api-football.com) — rich details: form, cards, goals.
	teamsClient := &http.Client{Timeout: cfg.Teams.Timeout}
	teamsProvider := apifootball.New(
		cfg.Teams.BaseURL,
		cfg.Teams.APIKey,
		cfg.Teams.League,
		cfg.Teams.Season,
		cfg.Teams.CacheTTL,
		teamsClient,
	)
	teamsSvc := teams.NewService(teamsProvider, commentary.New())
	teams.NewHandler(teamsSvc, logger).Register(mux)

	// Tips (mock now, Claude/DB-backed later) — built over the football service.
	tipsService := tips.NewService(tips.NewMockProvider(), footballService)
	tips.NewHandler(tipsService, logger).Register(mux)

	// News
	newsClient := &http.Client{Timeout: cfg.News.Timeout}
	newsProvider := googlenews.New(cfg.News.Limit, newsClient)
	news.NewHandler(news.NewService(newsProvider), logger).Register(mux)

	// Push — Web Push goal notifications. Routes are always registered so the
	// web app can discover the public key; subscribe returns 503 when unconfigured.
	pushStore, err := push.NewStore(cfg.Push.StorePath)
	if err != nil {
		logger.Error("push store init failed; push disabled", "error", err)
		pushStore, _ = push.NewStore("/tmp/push-subs-fallback.json")
	}
	pushSender := push.NewSender(cfg.Push.Public, cfg.Push.Private, cfg.Push.Subject)
	push.NewHandler(pushStore, pushSender, cfg.Push.Public).Register(mux)

	if cfg.Push.Configured() {
		watcher := push.NewWatcher(teamsSvc, pushStore, pushSender)
		go watcher.Run(ctx)
		logger.Info("push watcher started")
	} else {
		logger.Info("VAPID not configured; push watcher disabled")
	}
}
