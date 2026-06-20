// Package lineupwarm pre-fetches confirmed match lineups from the provider
// around kickoff and stores them in the provider's cache, so client requests
// are served instantly without any client-side polling.
//
// It is deliberately low-rate: it only touches matches kicking off within the
// next ~70 minutes, makes a handful of calls per match while the lineup isn't
// published yet (api-football publishes ~20-40 min pre-kickoff), and stops the
// moment a real lineup appears.
package lineupwarm

import (
	"context"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/oddvice/api/internal/football"
	"github.com/oddvice/api/internal/teams"
)

const (
	tickEvery   = 12 * time.Minute
	windowAhead = 70 * time.Minute
	keepFor     = 3 * time.Hour
)

// matchSource yields upcoming matches (kickoff in the future).
type matchSource interface {
	Upcoming(ctx context.Context, limit int) ([]football.Match, error)
}

// lineupFetcher fetches confirmed lineups by team names + date (YYYY-MM-DD).
// A fetch primes the underlying provider cache that clients read from.
type lineupFetcher interface {
	Lineups(ctx context.Context, home, away, date string) (teams.MatchLineups, bool, error)
}

// Warmer keeps recently-found matches so it doesn't keep re-fetching them.
type Warmer struct {
	matches matchSource
	lineups lineupFetcher

	mu     sync.Mutex
	warmed map[string]time.Time // match key -> kickoff (for pruning)
}

// New builds a Warmer over the football (match list) and teams (lineup) services.
func New(matches matchSource, lineups lineupFetcher) *Warmer {
	return &Warmer{matches: matches, lineups: lineups, warmed: make(map[string]time.Time)}
}

// Run blocks until ctx is cancelled, warming lineups on a slow ticker.
func (w *Warmer) Run(ctx context.Context) {
	t := time.NewTicker(tickEvery)
	defer t.Stop()
	w.tick(ctx)
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			w.tick(ctx)
		}
	}
}

func (w *Warmer) tick(ctx context.Context) {
	matches, err := w.matches.Upcoming(ctx, 0)
	if err != nil {
		slog.Warn("lineup warmer: upcoming fetch failed", "error", err)
		return
	}
	now := time.Now()
	w.prune(now)

	watching, warmedNow := 0, 0
	for _, m := range matches {
		if m.KickoffAt == nil {
			continue
		}
		ko := *m.KickoffAt
		if ko.Before(now) || ko.After(now.Add(windowAhead)) {
			continue // only the ~hour before kickoff
		}
		date := ko.UTC().Format("2006-01-02")
		key := strings.ToLower(m.HomeTeam + "|" + m.AwayTeam + "|" + date)

		w.mu.Lock()
		_, done := w.warmed[key]
		w.mu.Unlock()
		if done {
			continue
		}
		watching++

		ml, _, err := w.lineups.Lineups(ctx, m.HomeTeam, m.AwayTeam, date)
		if err != nil {
			continue
		}
		if hasXI(ml.Home) && hasXI(ml.Away) {
			w.mu.Lock()
			w.warmed[key] = ko
			w.mu.Unlock()
			warmedNow++
		}
	}
	if watching > 0 || warmedNow > 0 {
		slog.Info("lineup warmer", "watching", watching, "warmed_now", warmedNow)
	}
}

func hasXI(l *teams.Lineup) bool {
	return l != nil && len(l.StartXI) > 0
}

func (w *Warmer) prune(now time.Time) {
	w.mu.Lock()
	defer w.mu.Unlock()
	for k, ko := range w.warmed {
		if now.Sub(ko) > keepFor {
			delete(w.warmed, k)
		}
	}
}
