// Package commentary turns structured match events into short, human live
// commentary lines using the Claude Code CLI (`claude -p`, Haiku model).
//
// It runs once per new event (cached per language), and degrades gracefully:
// if `claude` isn't installed/authenticated or anything fails, events are
// returned unchanged (the UI falls back to the raw event text).
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

// Generator caches generated lines and invokes the Claude CLI for new events.
type Generator struct {
	bin     string
	model   string
	timeout time.Duration

	mu    sync.Mutex
	cache map[string]string // "lang|signature" -> commentary line
}

// New builds a Generator. Override the binary/model via CLAUDE_BIN / CLAUDE_MODEL.
func New() *Generator {
	bin := os.Getenv("CLAUDE_BIN")
	if bin == "" {
		bin = "claude"
	}
	model := os.Getenv("CLAUDE_MODEL")
	if model == "" {
		model = "haiku"
	}
	return &Generator{
		bin:     bin,
		model:   model,
		timeout: 30 * time.Second,
		cache:   make(map[string]string),
	}
}

var langNames = map[string]string{
	"en": "English", "ro": "Romanian", "de": "German", "fr": "French",
	"es": "Spanish", "it": "Italian", "nl": "Dutch", "pl": "Polish", "cs": "Czech",
}

func sig(e teams.Event) string {
	return fmt.Sprintf("%d|%d|%s|%s|%s|%s", e.Minute, e.Extra, e.Type, e.Detail, e.Team, e.Player)
}

// Enrich fills the Commentary field of each event (from cache, or by generating
// new ones in a single Claude call). Returns the same slice, possibly enriched.
func (g *Generator) Enrich(ctx context.Context, lang, matchLabel string, events []teams.Event) []teams.Event {
	if len(events) == 0 {
		return events
	}
	if _, ok := langNames[lang]; !ok {
		lang = "en"
	}

	var todoIdx []int
	g.mu.Lock()
	for i, e := range events {
		if line, ok := g.cache[lang+"|"+sig(e)]; ok {
			events[i].Commentary = line
		} else {
			todoIdx = append(todoIdx, i)
		}
	}
	g.mu.Unlock()
	if len(todoIdx) == 0 {
		return events
	}

	todo := make([]teams.Event, 0, len(todoIdx))
	for _, i := range todoIdx {
		todo = append(todo, events[i])
	}

	lines, err := g.generate(ctx, lang, matchLabel, todo)
	if err != nil || len(lines) == 0 {
		return events // graceful: leave raw
	}

	g.mu.Lock()
	for n, i := range todoIdx {
		if n < len(lines) && strings.TrimSpace(lines[n]) != "" {
			events[i].Commentary = lines[n]
			g.cache[lang+"|"+sig(events[i])] = lines[n]
		}
	}
	g.mu.Unlock()
	return events
}

type promptEvent struct {
	Minute int    `json:"minute"`
	Type   string `json:"type"`
	Detail string `json:"detail"`
	Team   string `json:"team"`
	Player string `json:"player,omitempty"`
	Assist string `json:"assist,omitempty"`
}

func (g *Generator) generate(ctx context.Context, lang, matchLabel string, events []teams.Event) ([]string, error) {
	pe := make([]promptEvent, len(events))
	for i, e := range events {
		pe[i] = promptEvent{e.Minute, e.Type, e.Detail, e.Team, e.Player, e.Assist}
	}
	eventsJSON, err := json.Marshal(pe)
	if err != nil {
		return nil, err
	}

	prompt := fmt.Sprintf(`You are a football live-commentary writer. For each event, write ONE short, vivid commentary line in %s, present tense, energetic but factual, under ~120 characters. Use the given player and team names; do NOT invent details (no scores unless implied by the event). Goals are exciting; cards and subs are matter-of-fact.

Match: %s
Events (JSON array, in order): %s

Output ONLY a JSON array of strings — one line per event, in the same order. No markdown, no prose, no code fences.`,
		langNames[lang], matchLabel, string(eventsJSON))

	cctx, cancel := context.WithTimeout(ctx, g.timeout)
	defer cancel()
	cmd := exec.CommandContext(cctx, g.bin, "-p", prompt,
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

	var lines []string
	if err := json.Unmarshal([]byte(stripFences(wrap.Result)), &lines); err != nil {
		return nil, fmt.Errorf("decode lines: %w", err)
	}
	return lines, nil
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
