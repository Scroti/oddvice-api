package push

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"github.com/oddvice/api/internal/teams"
)

const pollInterval = 30 * time.Second

// score holds the last-seen goal count for a live fixture.
type score struct {
	home int
	away int
}

// Watcher polls live matches and sends goal notifications to all subscribers
// (Web Push) and native devices (Expo push).
type Watcher struct {
	svc    *teams.Service
	store  *Store
	sender *Sender
	expo   *ExpoStore
}

// NewWatcher constructs a Watcher. expo may be nil (native push then skipped).
func NewWatcher(svc *teams.Service, store *Store, sender *Sender, expo *ExpoStore) *Watcher {
	return &Watcher{svc: svc, store: store, sender: sender, expo: expo}
}

// Run starts the polling loop. It blocks until ctx is cancelled.
func (w *Watcher) Run(ctx context.Context) {
	last := make(map[int]score)
	ticker := time.NewTicker(pollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			w.poll(ctx, last)
		}
	}
}

func (w *Watcher) poll(ctx context.Context, last map[int]score) {
	matches, err := w.svc.Live(ctx)
	if err != nil {
		slog.Warn("push watcher: live fetch failed", "error", err)
		return
	}

	// Track which fixture IDs are currently live so we can forget stale ones.
	seen := make(map[int]struct{}, len(matches))
	for _, m := range matches {
		seen[m.FixtureID] = struct{}{}

		prev, known := last[m.FixtureID]
		cur := score{home: m.HomeGoals, away: m.AwayGoals}

		if !known {
			// First time we observe this fixture — record without notifying.
			last[m.FixtureID] = cur
			continue
		}

		// Detect score increases and notify.
		if m.HomeGoals > prev.home {
			w.notify(ctx, m, "home", m.HomeGoals-prev.home)
		}
		if m.AwayGoals > prev.away {
			w.notify(ctx, m, "away", m.AwayGoals-prev.away)
		}

		last[m.FixtureID] = cur
	}

	// Forget fixtures that are no longer live.
	for id := range last {
		if _, live := seen[id]; !live {
			delete(last, id)
		}
	}
}

func (w *Watcher) notify(ctx context.Context, m teams.LiveMatch, side string, _ int) {
	var scorer string
	switch side {
	case "home":
		scorer = m.Home
	default:
		scorer = m.Away
	}

	title := fmt.Sprintf("GOAL! %s %d-%d %s", m.Home, m.HomeGoals, m.AwayGoals, m.Away)
	body := fmt.Sprintf("%s scored · %d'", scorer, m.Elapsed)

	payload, err := json.Marshal(map[string]string{
		"title": title,
		"body":  body,
		"url":   "/",
	})
	if err != nil {
		slog.Error("push watcher: failed to marshal payload", "error", err)
		return
	}

	subs := w.store.All()
	for _, sub := range subs {
		w.sender.Send(ctx, w.store, sub, payload)
	}

	// Native devices via Expo push.
	if w.expo != nil {
		if tokens, err := w.expo.All(ctx); err == nil {
			SendExpo(ctx, nil, tokens, title, body, "/")
		}
	}
	slog.Info("push: goal notification sent", "fixture", m.FixtureID, "title", title, "subscribers", len(subs))
}
