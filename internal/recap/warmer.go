package recap

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

var langCodes = []string{"en", "ro", "de", "fr", "es", "it", "nl", "pl", "cs"}

var langNames = map[string]string{
	"en": "English", "ro": "Romanian", "de": "German", "fr": "French",
	"es": "Spanish", "it": "Italian", "nl": "Dutch", "pl": "Polish", "cs": "Czech",
}

// Finished is a completed match the warmer can recap.
type Finished struct {
	ID        string
	Home      string
	Away      string
	HomeScore int
	AwayScore int
	HomeBadge string
	AwayBadge string
	League    string
	KickoffAt time.Time
}

// settleDelay is how long after kickoff a match is considered "finished + ~30
// min". A match runs ~2h wall-clock (halves + half-time + stoppage), so we wait
// ~2h30 before writing the recap, per the "30 min after full time" rule.
const settleDelay = 2*time.Hour + 30*time.Minute

// dailyCap is the most recaps we generate per calendar day.
const dailyCap = 4

// ResultsFn returns the currently-finished matches.
type ResultsFn func(ctx context.Context) ([]Finished, error)

// Warmer generates + persists recaps for finished matches not yet recapped.
type Warmer struct {
	store   *Store
	results ResultsFn
	bin     string
	model   string
	timeout time.Duration
	perCycle int
	log     *slog.Logger
}

// NewWarmer builds a Warmer. CLAUDE_BIN / CLAUDE_MODEL override the CLI.
func NewWarmer(store *Store, results ResultsFn, log *slog.Logger) *Warmer {
	bin := os.Getenv("CLAUDE_BIN")
	if bin == "" {
		bin = "claude"
	}
	model := os.Getenv("CLAUDE_MODEL")
	if model == "" {
		model = "haiku"
	}
	return &Warmer{store: store, results: results, bin: bin, model: model, timeout: 90 * time.Second, perCycle: 4, log: log}
}

const warmInterval = 5 * time.Minute

// Run warms on start, then every warmInterval.
func (w *Warmer) Run(ctx context.Context) {
	w.warm(ctx)
	ticker := time.NewTicker(warmInterval)
	defer ticker.Stop()
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
	// Respect the daily cap.
	today, err := w.store.CountTodayMatches(ctx)
	if err != nil {
		w.log.Warn("recap warmer: daily-count failed", "error", err)
		return
	}
	if today >= dailyCap {
		return
	}

	finished, err := w.results(ctx)
	if err != nil {
		w.log.Warn("recap warmer: results fetch failed", "error", err)
		return
	}
	if len(finished) == 0 {
		return
	}
	ids := make([]string, len(finished))
	for i, f := range finished {
		ids[i] = f.ID
	}
	have, err := w.store.HaveMatchIDs(ctx, ids)
	if err != nil {
		w.log.Warn("recap warmer: have-check failed", "error", err)
		return
	}
	budget := dailyCap - today // how many more we may write today
	done := 0
	for _, f := range finished {
		if done >= budget || done >= w.perCycle {
			break
		}
		if have[f.ID] {
			continue
		}
		// Only ~30 min after full time (kickoff + ~2h30).
		if f.KickoffAt.IsZero() || time.Since(f.KickoffAt) < settleDelay {
			continue
		}
		perLang, err := w.generate(ctx, f)
		if err != nil || len(perLang) == 0 {
			continue // graceful: try again next cycle
		}
		for lang, body := range perLang {
			if strings.TrimSpace(body) == "" {
				continue
			}
			_ = w.store.Put(ctx, Recap{
				MatchID: f.ID, Home: f.Home, Away: f.Away,
				HomeScore: f.HomeScore, AwayScore: f.AwayScore,
				HomeBadge: f.HomeBadge, AwayBadge: f.AwayBadge,
				League: f.League, Body: body,
			}, lang)
		}
		done++
		w.log.Info("recap: generated", "match", f.ID, "score", fmt.Sprintf("%d-%d", f.HomeScore, f.AwayScore))
	}
}

// generate asks the Claude CLI for the recap in every language, in one call.
func (w *Warmer) generate(ctx context.Context, f Finished) (map[string]string, error) {
	cctx, cancel := context.WithTimeout(ctx, w.timeout)
	defer cancel()

	var langList strings.Builder
	for i, c := range langCodes {
		if i > 0 {
			langList.WriteString(", ")
		}
		fmt.Fprintf(&langList, "%s (%s)", c, langNames[c])
	}

	prompt := fmt.Sprintf(`You are a football writer. Write a punchy 2-sentence recap of this FINISHED 2026 World Cup match. Be factual: use ONLY the teams, score and competition given — do NOT invent goalscorers, minutes or events. Energetic but accurate.

Match: %s %d-%d %s (%s)

Translate the recap into ALL of these languages: %s.

Output ONLY a JSON object whose keys are the language codes (%s) and whose values are the recap in that language. No markdown, no code fences, no extra text.`,
		f.Home, f.HomeScore, f.AwayScore, f.Away, f.League, langList.String(), strings.Join(langCodes, ", "))

	cmd := exec.CommandContext(cctx, w.bin, "-p", prompt, "--model", w.model, "--output-format", "json", "--max-turns", "1")
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("claude cli: %w", err)
	}
	var wrap struct {
		Result string `json:"result"`
	}
	if err := json.Unmarshal(out, &wrap); err != nil {
		return nil, fmt.Errorf("decode cli envelope: %w", err)
	}
	var perLang map[string]string
	if err := json.Unmarshal([]byte(stripFences(wrap.Result)), &perLang); err != nil {
		return nil, fmt.Errorf("decode recap json: %w", err)
	}
	return perLang, nil
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
