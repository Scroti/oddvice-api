package aitips

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"strings"
	"time"
)

// MatchCtx is the context the generator needs for an upcoming fixture.
type MatchCtx struct {
	ID      string
	Home    string
	Away    string
	League  string
	Kickoff time.Time
}

// UpcomingFn returns upcoming fixtures (soonest first). ResultsFn returns
// finished matches as matchID -> [homeGoals, awayGoals].
type UpcomingFn func(ctx context.Context) ([]MatchCtx, error)
type ResultsFn func(ctx context.Context) (map[string][2]int, error)

// Warmer generates tips for upcoming matches and grades finished ones, feeding
// the track record back into generation so the model learns from its misses.
type Warmer struct {
	store    *Store
	upcoming UpcomingFn
	results  ResultsFn
	bin      string
	model    string
	timeout  time.Duration
	log      *slog.Logger
}

// NewWarmer builds a Warmer. CLAUDE_BIN / CLAUDE_MODEL override the CLI.
func NewWarmer(store *Store, upcoming UpcomingFn, results ResultsFn, log *slog.Logger) *Warmer {
	bin := os.Getenv("CLAUDE_BIN")
	if bin == "" {
		bin = "claude"
	}
	model := os.Getenv("CLAUDE_MODEL")
	if model == "" {
		model = "haiku"
	}
	return &Warmer{store: store, upcoming: upcoming, results: results, bin: bin, model: model, timeout: 90 * time.Second, log: log}
}

const (
	warmInterval = 10 * time.Minute
	genPerCycle  = 3
	genHorizon   = 48 * time.Hour // only generate for matches within ~2 days
)

// Run grades + generates on start, then every warmInterval.
func (w *Warmer) Run(ctx context.Context) {
	w.tick(ctx)
	ticker := time.NewTicker(warmInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			w.tick(ctx)
		}
	}
}

func (w *Warmer) tick(ctx context.Context) {
	w.grade(ctx)
	w.generate(ctx)
}

// grade scores every ungraded bundle whose match has finished.
func (w *Warmer) grade(ctx context.Context) {
	results, err := w.results(ctx)
	if err != nil {
		return
	}
	ungraded, err := w.store.Ungraded(ctx)
	if err != nil {
		return
	}
	for _, b := range ungraded {
		sc, ok := results[b.MatchID]
		if !ok {
			continue // not finished yet
		}
		hits, total := 0, 0
		for _, t := range b.Tips {
			if t.GradeKey == "" {
				continue
			}
			total++
			if gradeHit(t.GradeKey, sc[0], sc[1]) {
				hits++
			}
		}
		_ = w.store.MarkGraded(ctx, b.MatchID, hits, total)
	}
}

// generate creates tips for soon-upcoming matches that don't have any yet.
func (w *Warmer) generate(ctx context.Context) {
	ups, err := w.upcoming(ctx)
	if err != nil || len(ups) == 0 {
		return
	}
	ids := make([]string, len(ups))
	for i, m := range ups {
		ids[i] = m.ID
	}
	have, err := w.store.HaveIDs(ctx, ids)
	if err != nil {
		return
	}
	rec, _ := w.store.TrackRecord(ctx)

	done := 0
	now := time.Now()
	for _, m := range ups {
		if done >= genPerCycle {
			break
		}
		if have[m.ID] {
			continue
		}
		if m.Kickoff.IsZero() || m.Kickoff.Before(now) || m.Kickoff.Sub(now) > genHorizon {
			continue
		}
		b, err := w.gen(ctx, m, rec)
		if err != nil || len(b.Tips) == 0 {
			continue
		}
		b.MatchID = m.ID
		b.HomeTeam = m.Home
		b.AwayTeam = m.Away
		b.League = m.League
		b.GeneratedAt = now
		if err := w.store.Put(ctx, b, m.Kickoff); err == nil {
			done++
			w.log.Info("aitips: generated", "match", m.ID, "record", fmt.Sprintf("%d/%d", rec.Hits, rec.Total))
		}
	}
}

// gradeHit grades a constrained market key against the final score.
func gradeHit(key string, h, a int) bool {
	total := h + a
	switch key {
	case "1x2_home":
		return h > a
	case "1x2_draw":
		return h == a
	case "1x2_away":
		return a > h
	case "dc_1x":
		return h >= a
	case "dc_x2":
		return a >= h
	case "dc_12":
		return h != a
	case "btts_yes":
		return h > 0 && a > 0
	case "btts_no":
		return !(h > 0 && a > 0)
	case "over25":
		return total >= 3
	case "under25":
		return total <= 2
	case "home_and_btts":
		return h > a && h > 0 && a > 0
	case "away_and_btts":
		return a > h && h > 0 && a > 0
	default:
		return false
	}
}

func (w *Warmer) gen(ctx context.Context, m MatchCtx, rec Record) (Bundle, error) {
	cctx, cancel := context.WithTimeout(ctx, w.timeout)
	defer cancel()

	pct := 0
	if rec.Total > 0 {
		pct = rec.Hits * 100 / rec.Total
	}
	misses := "none yet"
	if len(rec.Misses) > 0 {
		misses = "- " + strings.Join(rec.Misses, "\n- ")
	}

	prompt := fmt.Sprintf(`You are a football betting analyst for an ADVICE app (18+, responsible gambling — analysis only, never a guarantee). For this UPCOMING 2026 World Cup match produce EXACTLY 3 tips: one "safe" (tier free), one "value" (tier premium), one "bold" (tier premium).

Match: %s vs %s (%s)

Every tip MUST use one of these gradeKey values, with matching human market/selection text (so it can be graded against the final score):
- 1x2_home / 1x2_draw / 1x2_away  → market "1X2", selection "%s win" / "Draw" / "%s win"
- dc_1x / dc_x2 / dc_12            → market "Double Chance", selection "1X (%s or draw)" / "X2 (draw or %s)" / "12 (no draw)"
- btts_yes / btts_no              → market "BTTS", selection "Both teams to score: Yes/No"
- over25 / under25               → market "Over/Under 2.5", selection "Over 2.5 goals" / "Under 2.5 goals"
- home_and_btts / away_and_btts  → market "Result & BTTS", selection "%s win & BTTS" / "%s win & BTTS"

YOUR TRACK RECORD: %d/%d graded picks correct (%d%%).
Recent MISSES — learn from these, do NOT repeat overconfident picks of the same kind:
%s

Calibrate confidence HONESTLY given that record (lower it if your record is weak). Give realistic decimal odds. Premium tips also need: analysis (2-3 sentences), 2-3 keyFactors, stakeUnits (1-5).

Output ONLY this JSON (no markdown/fences):
{"matchPreview":"1-2 sentences","overallConfidence":0-100,"tips":[{"tier":"free","risk":"safe","market":"","selection":"","gradeKey":"","odds":1.5,"confidence":0,"shortReason":""},{"tier":"premium","risk":"value","market":"","selection":"","gradeKey":"","odds":2.0,"confidence":0,"shortReason":"","analysis":"","keyFactors":[""],"stakeUnits":2},{"tier":"premium","risk":"bold","market":"","selection":"","gradeKey":"","odds":3.5,"confidence":0,"shortReason":"","analysis":"","keyFactors":[""],"stakeUnits":1}]}`,
		m.Home, m.Away, m.League, m.Home, m.Away, m.Home, m.Away, m.Home, m.Away, rec.Hits, rec.Total, pct, misses)

	cmd := exec.CommandContext(cctx, w.bin, "-p", prompt, "--model", w.model, "--output-format", "json", "--max-turns", "1")
	out, err := cmd.Output()
	if err != nil {
		return Bundle{}, fmt.Errorf("claude cli: %w", err)
	}
	var wrap struct {
		Result string `json:"result"`
	}
	if err := json.Unmarshal(out, &wrap); err != nil {
		return Bundle{}, err
	}
	var parsed struct {
		MatchPreview      string   `json:"matchPreview"`
		OverallConfidence int      `json:"overallConfidence"`
		Tips              []genTip `json:"tips"`
	}
	if err := json.Unmarshal([]byte(stripFences(wrap.Result)), &parsed); err != nil {
		return Bundle{}, err
	}
	return Bundle{MatchPreview: parsed.MatchPreview, OverallConfidence: parsed.OverallConfidence, Tips: parsed.Tips}, nil
}

func stripFences(s string) string {
	s = strings.TrimSpace(s)
	if strings.HasPrefix(s, "```") {
		s = strings.TrimPrefix(s, "```json")
		s = strings.TrimPrefix(s, "```")
		s = strings.TrimSuffix(s, "```")
		s = strings.TrimSpace(s)
	}
	if i, j := strings.Index(s, "{"), strings.LastIndex(s, "}"); i >= 0 && j > i {
		s = s[i : j+1]
	}
	return s
}
