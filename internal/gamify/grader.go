package gamify

import (
	"context"
	"log/slog"
	"time"
)

// ResultsFn returns finished matches as a map of matchID -> "home"|"draw"|"away".
type ResultsFn func(ctx context.Context) (map[string]string, error)

// Grader periodically grades ungraded predictions against finished results.
type Grader struct {
	store   *Store
	results ResultsFn
	log     *slog.Logger
}

// NewGrader builds a Grader.
func NewGrader(store *Store, results ResultsFn, log *slog.Logger) *Grader {
	return &Grader{store: store, results: results, log: log}
}

const gradeInterval = 90 * time.Second

// Run grades on start and then every gradeInterval until ctx is cancelled.
func (g *Grader) Run(ctx context.Context) {
	g.grade(ctx)
	ticker := time.NewTicker(gradeInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			g.grade(ctx)
		}
	}
}

func (g *Grader) grade(ctx context.Context) {
	winners, err := g.results(ctx)
	if err != nil {
		g.log.Warn("gamify grader: results fetch failed", "error", err)
		return
	}
	n, err := g.store.GradeFinished(ctx, winners)
	if err != nil {
		g.log.Warn("gamify grader: grade failed", "error", err)
		return
	}
	if n > 0 {
		g.log.Info("gamify: graded predictions", "count", n)
	}
}
