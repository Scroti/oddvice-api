package players

import (
	"context"
	"log/slog"
	"time"
)

// FetchFunc returns the full player list to index. It is provided by the caller
// (server wiring) so this package stays decoupled from the api-football client.
type FetchFunc func(ctx context.Context) ([]Player, error)

// Ingester fills the player index once on startup (if empty) and refreshes it
// on a daily cadence. It performs the upstream calls — user searches never do.
type Ingester struct {
	fetch  FetchFunc
	store  *Store
	logger *slog.Logger
}

// NewIngester builds an Ingester.
func NewIngester(fetch FetchFunc, store *Store, logger *slog.Logger) *Ingester {
	return &Ingester{fetch: fetch, store: store, logger: logger}
}

// Run blocks until ctx is cancelled. It ingests immediately when the table is
// empty, then refreshes every 24h.
func (in *Ingester) Run(ctx context.Context) {
	if in == nil || in.store == nil || in.fetch == nil {
		return
	}
	if n, err := in.store.Count(ctx); err == nil && n == 0 {
		in.ingest(ctx)
	}
	ticker := time.NewTicker(24 * time.Hour)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			in.ingest(ctx)
		}
	}
}

func (in *Ingester) ingest(ctx context.Context) {
	players, err := in.fetch(ctx)
	if err != nil {
		in.logger.Warn("player index: fetch failed", "error", err)
		return
	}
	if err := in.store.Upsert(ctx, players); err != nil {
		in.logger.Warn("player index: upsert failed", "error", err)
		return
	}
	in.logger.Info("player index refreshed", "players", len(players))
}
