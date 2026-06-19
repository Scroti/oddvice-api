// Package commentary turns structured match events into short, human live
// commentary lines using the Claude Code CLI (`claude -p`, Haiku model).
//
// Generation never blocks the HTTP request: a request fills what is already
// cached (in-memory, then Postgres) and returns immediately; anything missing
// is generated in the background in ALL supported languages at once, persisted
// to Postgres, and served instantly on subsequent requests. It degrades
// gracefully: if `claude` isn't installed/authenticated or anything fails,
// events are returned unchanged (the UI falls back to the raw event text).
package commentary

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"

	"github.com/oddvice/api/internal/teams"
)

// langCodes is every language we generate and persist commentary for.
var langCodes = []string{"en", "ro", "de", "fr", "es", "it", "nl", "pl", "cs"}

var langNames = map[string]string{
	"en": "English", "ro": "Romanian", "de": "German", "fr": "French",
	"es": "Spanish", "it": "Italian", "nl": "Dutch", "pl": "Polish", "cs": "Czech",
}

// Generator caches generated lines and invokes the Claude CLI for new events.
type Generator struct {
	bin     string
	model   string
	timeout time.Duration
	store   *Store // optional Postgres persistence (nil = in-memory only)

	mu       sync.Mutex
	cache    map[string]string   // "lang|signature" -> commentary line
	inflight map[string]struct{} // "fixtureID|signature" being generated
}

// New builds a Generator. Override the binary/model via CLAUDE_BIN / CLAUDE_MODEL.
// store may be nil to run without persistence.
func New(store *Store) *Generator {
	bin := os.Getenv("CLAUDE_BIN")
	if bin == "" {
		bin = "claude"
	}
	model := os.Getenv("CLAUDE_MODEL")
	if model == "" {
		model = "haiku"
	}
	return &Generator{
		bin:      bin,
		model:    model,
		timeout:  60 * time.Second,
		store:    store,
		cache:    make(map[string]string),
		inflight: make(map[string]struct{}),
	}
}

func sig(e teams.Event) string {
	return fmt.Sprintf("%d|%d|%s|%s|%s|%s", e.Minute, e.Extra, e.Type, e.Detail, e.Team, e.Player)
}

// Enrich fills the Commentary field of each event from cache (in-memory, then
// Postgres) and returns immediately. Events still missing in the requested
// language are generated in the background (in every language) and persisted,
// so later requests are instant. Missing events are left with raw text.
func (g *Generator) Enrich(ctx context.Context, fixtureID int, lang, matchLabel string, events []teams.Event) []teams.Event {
	if len(events) == 0 {
		return events
	}
	if _, ok := langNames[lang]; !ok {
		lang = "en"
	}

	// Phase 1 — in-memory cache.
	var missing []int
	g.mu.Lock()
	for i, e := range events {
		if line, ok := g.cache[lang+"|"+sig(e)]; ok {
			events[i].Commentary = line
		} else {
			missing = append(missing, i)
		}
	}
	g.mu.Unlock()

	// Phase 2 — Postgres cache for whatever is still missing.
	if len(missing) > 0 && g.store != nil {
		sigs := make([]string, len(missing))
		for n, i := range missing {
			sigs[n] = sig(events[i])
		}
		if found, err := g.store.GetMany(ctx, fixtureID, lang, sigs); err == nil && len(found) > 0 {
			var still []int
			g.mu.Lock()
			for _, i := range missing {
				if body, ok := found[sig(events[i])]; ok {
					events[i].Commentary = body
					g.cache[lang+"|"+sig(events[i])] = body
				} else {
					still = append(still, i)
				}
			}
			g.mu.Unlock()
			missing = still
		}
	}
	if len(missing) == 0 {
		return events
	}

	// Phase 3 — schedule background generation for events not already in flight.
	var todo []teams.Event
	g.mu.Lock()
	for _, i := range missing {
		key := fmt.Sprintf("%d|%s", fixtureID, sig(events[i]))
		if _, busy := g.inflight[key]; busy {
			continue
		}
		g.inflight[key] = struct{}{}
		todo = append(todo, events[i])
	}
	g.mu.Unlock()

	if len(todo) > 0 {
		go g.generateAndStore(fixtureID, matchLabel, todo)
	}
	return events // return now; missing events keep raw text until generated
}

// generateAndStore runs in the background: it generates commentary in every
// language for the given events, fills the in-memory cache, and persists to
// Postgres. It always clears the in-flight marks for its events.
func (g *Generator) generateAndStore(fixtureID int, matchLabel string, events []teams.Event) {
	defer func() {
		g.mu.Lock()
		for _, e := range events {
			delete(g.inflight, fmt.Sprintf("%d|%s", fixtureID, sig(e)))
		}
		g.mu.Unlock()
	}()

	ctx, cancel := context.WithTimeout(context.Background(), g.timeout)
	defer cancel()

	perEvent, err := g.generateAllLangs(ctx, matchLabel, events)
	if err != nil || len(perEvent) == 0 {
		return // graceful: leave raw, marks cleared by defer
	}

	g.mu.Lock()
	for i, e := range events {
		if i >= len(perEvent) {
			break
		}
		for lc, line := range perEvent[i] {
			if strings.TrimSpace(line) != "" {
				g.cache[lc+"|"+sig(e)] = line
			}
		}
	}
	g.mu.Unlock()

	if g.store != nil {
		for i, e := range events {
			if i >= len(perEvent) {
				break
			}
			_ = g.store.PutAll(ctx, fixtureID, sig(e), perEvent[i])
		}
	}
}

type promptEvent struct {
	Minute int    `json:"minute"`
	Type   string `json:"type"`
	Detail string `json:"detail"`
	Team   string `json:"team"`
	Player string `json:"player,omitempty"`
	Assist string `json:"assist,omitempty"`
}

// generateAllLangs asks the Claude CLI for commentary in every supported
// language for each event, in one call. It returns one map per event (in the
// same order) keyed by language code.
func (g *Generator) generateAllLangs(ctx context.Context, matchLabel string, events []teams.Event) ([]map[string]string, error) {
	pe := make([]promptEvent, len(events))
	for i, e := range events {
		pe[i] = promptEvent{e.Minute, e.Type, e.Detail, e.Team, e.Player, e.Assist}
	}
	eventsJSON, err := json.Marshal(pe)
	if err != nil {
		return nil, err
	}

	var langList strings.Builder
	for i, c := range langCodes {
		if i > 0 {
			langList.WriteString(", ")
		}
		fmt.Fprintf(&langList, "%s (%s)", c, langNames[c])
	}

	prompt := fmt.Sprintf(`You are a football live-commentary writer. For each event, write ONE short, vivid commentary line — present tense, energetic but factual, under ~120 characters. Use the given player and team names; do NOT invent details (no scores unless implied by the event). Goals are exciting; cards and subs are matter-of-fact. Translate each line into ALL of these languages: %s.

Match: %s
Events (JSON array, in order): %s

Output ONLY a JSON array. Element i corresponds to event i and is an OBJECT whose keys are the language codes (%s) and whose values are the commentary line in that language. No markdown, no prose, no code fences.`,
		langList.String(), matchLabel, string(eventsJSON), strings.Join(langCodes, ", "))

	cmd := exec.CommandContext(ctx, g.bin, "-p", prompt,
		"--model", g.model, "--output-format", "json", "--max-turns", "1")
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("claude cli: %w", err)
	}

	// `--output-format json` wraps the model text in a result envelope.
	var wrap struct {
		Result string `json:"result"`
	}
	if err := json.Unmarshal(out, &wrap); err != nil {
		return nil, fmt.Errorf("decode cli envelope: %w", err)
	}

	var perEvent []map[string]string
	if err := json.Unmarshal([]byte(stripFences(wrap.Result)), &perEvent); err != nil {
		return nil, fmt.Errorf("decode lines: %w", err)
	}
	return perEvent, nil
}

// stripFences removes ```json ... ``` wrappers and trims, in case the model adds them.
func stripFences(s string) string {
	s = strings.TrimSpace(s)
	if strings.HasPrefix(s, "```") {
		s = strings.TrimPrefix(s, "```json")
		s = strings.TrimPrefix(s, "```")
		s = strings.TrimSuffix(s, "```")
		s = strings.TrimSpace(s)
	}
	// Be lenient: slice to the outermost array brackets if extra text slipped in.
	if i, j := strings.Index(s, "["), strings.LastIndex(s, "]"); i >= 0 && j > i {
		s = s[i : j+1]
	}
	return s
}
