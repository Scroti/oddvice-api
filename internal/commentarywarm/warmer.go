// Package commentarywarm pre-generates AI live commentary for in-play matches
// so it is already cached when a client opens the match.
//
// Commentary is generated lazily (in the background) the first time a fixture's
// events are requested — which means a brand-new goal/card shows raw text until
// generation finishes. This warmer requests each live fixture's events on a
// short interval, so Enrich schedules generation as soon as an event appears,
// well before a user opens the match. It piggybacks on the same ~20s server-side
// events cache, so it never hits the upstream API more often than normal.
package commentarywarm

import (
	"context"
	"log/slog"
	"time"

	"github.com/oddvice/api/internal/teams"
)

// pollInterval is how often we sweep live fixtures. Kept just under the ~20s
// events cache TTL so new events are picked up promptly without extra upstream load.
const pollInterval = 15 * time.Second

// Warmer sweeps live fixtures and triggers commentary generation.
type Warmer struct {
	svc *teams.Service
}

// New builds a Warmer over the teams service.
func New(svc *teams.Service) *Warmer {
	return &Warmer{svc: svc}
}

// Run starts the sweep loop. It blocks until ctx is cancelled.
func (w *Warmer) Run(ctx context.Context) {
	ticker := time.NewTicker(pollInterval)
	defer ticker.Stop()

	w.warm(ctx) // warm immediately on start
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			w.warm(ctx)
		}
	}
}

func (w *Warmer) warm(ctx context.Context) {
	matches, err := w.svc.Live(ctx)
	if err != nil {
		slog.Warn("commentary warmer: live fetch failed", "error", err)
		return
	}
	for _, m := range matches {
		// EventsByFixture fills cached commentary and schedules background
		// generation (in every language) for any new events. The lang here only
		// affects which lines are returned to us (discarded) — generation covers
		// all languages regardless.
		if _, _, err := w.svc.EventsByFixture(ctx, m.FixtureID, "en"); err != nil {
			slog.Debug("commentary warmer: events fetch failed",
				"fixture", m.FixtureID, "error", err)
		}
	}
}
