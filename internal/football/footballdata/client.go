// Package footballdata implements football.Provider against football-data.org,
// which covers the FIFA World Cup with real crests, scores and fixtures.
//
// The free tier is rate-limited (≈10 requests/minute), so the client:
//   - caches the full match list and serves every view (list/upcoming/results/
//     search/detail) from it, making ~1 upstream call per cache window;
//   - honours the API's throttling headers (X-Requests-Available-Minute,
//     X-RequestCounter-Reset) and backs off (serving stale) when low or 429'd.
package footballdata

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/oddvice/api/internal/football"
)

// errThrottled is returned when we're in a rate-limit cooldown window.
var errThrottled = errors.New("football-data.org rate limit reached")

// Client talks to football-data.org and maps its DTOs to the domain model.
type Client struct {
	httpClient  *http.Client
	baseURL     string
	apiKey      string
	competition string
	cacheTTL    time.Duration

	mu            sync.Mutex
	cached        []football.Match
	cachedAt      time.Time
	standings     []football.Group
	standingsAt   time.Time
	cooldownUntil time.Time // don't call upstream before this (rate limit)
}

// New builds a Client. A nil httpClient gets a default one.
func New(baseURL, apiKey, competition string, cacheTTL time.Duration, httpClient *http.Client) *Client {
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 12 * time.Second}
	}
	if cacheTTL <= 0 {
		cacheTTL = 2 * time.Minute
	}
	return &Client{
		httpClient:  httpClient,
		baseURL:     strings.TrimRight(baseURL, "/"),
		apiKey:      apiKey,
		competition: competition,
		cacheTTL:    cacheTTL,
	}
}

type matchDTO struct {
	ID       int     `json:"id"`
	UTCDate  string  `json:"utcDate"`
	Status   string  `json:"status"`
	Stage    string  `json:"stage"`
	Group    string  `json:"group"`
	Venue    string  `json:"venue"`
	HomeTeam teamDTO `json:"homeTeam"`
	AwayTeam teamDTO `json:"awayTeam"`
	Score    struct {
		FullTime struct {
			Home *int `json:"home"`
			Away *int `json:"away"`
		} `json:"fullTime"`
	} `json:"score"`
}

type teamDTO struct {
	Name  string `json:"name"`
	Crest string `json:"crest"`
}

type matchesResponse struct {
	Matches []matchDTO `json:"matches"`
}

// Matches returns all matches of the configured competition, cached to respect
// the API rate limit. Stale cache is served if a refresh fails or is throttled.
func (c *Client) Matches(ctx context.Context) ([]football.Match, error) {
	c.mu.Lock()
	fresh := c.cached != nil && time.Since(c.cachedAt) < c.cacheTTL
	cached := c.cached
	c.mu.Unlock()
	if fresh {
		return cached, nil
	}

	endpoint := fmt.Sprintf("%s/v4/competitions/%s/matches", c.baseURL, c.competition)
	var payload matchesResponse
	if err := c.get(ctx, endpoint, &payload); err != nil {
		if cached != nil {
			return cached, nil // serve stale on failure / throttle
		}
		return nil, err
	}

	matches := make([]football.Match, 0, len(payload.Matches))
	for _, m := range payload.Matches {
		matches = append(matches, m.toMatch())
	}

	c.mu.Lock()
	c.cached, c.cachedAt = matches, time.Now()
	c.mu.Unlock()
	return matches, nil
}

// GetMatch returns a single match. It first looks in the cached competition
// list (no extra upstream call); only a truly unknown id triggers a direct
// lookup. The bool is false when not found.
func (c *Client) GetMatch(ctx context.Context, id string) (football.Match, bool, error) {
	matches, listErr := c.Matches(ctx)
	if listErr == nil {
		for _, m := range matches {
			if m.ID == id {
				return m, true, nil
			}
		}
	}

	// Fallback: direct lookup for an id outside the cached competition.
	var dto matchDTO
	if err := c.get(ctx, fmt.Sprintf("%s/v4/matches/%s", c.baseURL, id), &dto); err != nil {
		if listErr != nil {
			return football.Match{}, false, err
		}
		return football.Match{}, false, nil // list was fine, id just not found
	}
	if dto.ID == 0 {
		return football.Match{}, false, nil
	}
	return dto.toMatch(), true, nil
}

type standingsResponse struct {
	Standings []struct {
		Type  string `json:"type"`
		Group string `json:"group"`
		Table []struct {
			Position       int     `json:"position"`
			Team           teamDTO `json:"team"`
			PlayedGames    int     `json:"playedGames"`
			Won            int     `json:"won"`
			Draw           int     `json:"draw"`
			Lost           int     `json:"lost"`
			Points         int     `json:"points"`
			GoalDifference int     `json:"goalDifference"`
		} `json:"table"`
	} `json:"standings"`
}

// Standings returns the group tables, cached like the match list.
func (c *Client) Standings(ctx context.Context) ([]football.Group, error) {
	c.mu.Lock()
	fresh := c.standings != nil && time.Since(c.standingsAt) < c.cacheTTL
	cached := c.standings
	c.mu.Unlock()
	if fresh {
		return cached, nil
	}

	endpoint := fmt.Sprintf("%s/v4/competitions/%s/standings", c.baseURL, c.competition)
	var payload standingsResponse
	if err := c.get(ctx, endpoint, &payload); err != nil {
		if cached != nil {
			return cached, nil
		}
		return nil, err
	}

	groups := make([]football.Group, 0, len(payload.Standings))
	for _, s := range payload.Standings {
		if s.Type != "TOTAL" || s.Group == "" {
			continue
		}
		table := make([]football.Standing, 0, len(s.Table))
		for _, row := range s.Table {
			table = append(table, football.Standing{
				Position:       row.Position,
				Team:           row.Team.Name,
				Crest:          row.Team.Crest,
				Played:         row.PlayedGames,
				Won:            row.Won,
				Draw:           row.Draw,
				Lost:           row.Lost,
				GoalDifference: row.GoalDifference,
				Points:         row.Points,
			})
		}
		groups = append(groups, football.Group{
			Name:  strings.Replace(s.Group, "Group ", "Grupa ", 1),
			Table: table,
		})
	}

	c.mu.Lock()
	c.standings, c.standingsAt = groups, time.Now()
	c.mu.Unlock()
	return groups, nil
}

func (c *Client) inCooldown() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return time.Now().Before(c.cooldownUntil)
}

func (c *Client) setCooldown(until time.Time) {
	c.mu.Lock()
	c.cooldownUntil = until
	c.mu.Unlock()
}

func (c *Client) get(ctx context.Context, url string, dst any) error {
	if c.inCooldown() {
		return errThrottled
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("X-Auth-Token", c.apiKey)
	req.Header.Set("Accept", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("call provider: %w", err)
	}
	defer resp.Body.Close()

	// Honour the throttling headers: if we're out of requests (or got 429),
	// pause upstream calls until the counter resets.
	reset := atoiHeader(resp.Header.Get("X-RequestCounter-Reset"))
	avail, availSet := atoiHeaderOK(resp.Header.Get("X-Requests-Available-Minute"))
	if resp.StatusCode == http.StatusTooManyRequests || (availSet && avail <= 0) {
		wait := time.Duration(reset) * time.Second
		if wait <= 0 {
			wait = time.Minute
		}
		c.setCooldown(time.Now().Add(wait))
	}
	if resp.StatusCode == http.StatusTooManyRequests {
		return errThrottled
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("provider returned status %d", resp.StatusCode)
	}
	if err := json.NewDecoder(resp.Body).Decode(dst); err != nil {
		return fmt.Errorf("decode provider response: %w", err)
	}
	return nil
}

func atoiHeader(s string) int {
	n, _ := strconv.Atoi(strings.TrimSpace(s))
	return n
}

func atoiHeaderOK(s string) (int, bool) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, false
	}
	n, err := strconv.Atoi(s)
	return n, err == nil
}

func (m matchDTO) toMatch() football.Match {
	return football.Match{
		ID:        fmt.Sprintf("%d", m.ID),
		Name:      m.HomeTeam.Name + " vs " + m.AwayTeam.Name,
		League:    stageLabel(m.Stage, m.Group),
		HomeTeam:  m.HomeTeam.Name,
		AwayTeam:  m.AwayTeam.Name,
		HomeScore: m.Score.FullTime.Home,
		AwayScore: m.Score.FullTime.Away,
		Status:    normalizeStatus(m.Status),
		Venue:     m.Venue,
		KickoffAt: parseDate(m.UTCDate),
		HomeBadge: m.HomeTeam.Crest,
		AwayBadge: m.AwayTeam.Crest,
	}
}

// normalizeStatus maps football-data statuses to short domain codes.
func normalizeStatus(s string) string {
	switch s {
	case "FINISHED":
		return "FT"
	case "IN_PLAY", "PAUSED":
		return "LIVE"
	default: // SCHEDULED, TIMED, POSTPONED, etc.
		return ""
	}
}

// stageLabel turns stage/group codes into a friendly Romanian label.
func stageLabel(stage, group string) string {
	switch stage {
	case "GROUP_STAGE":
		if group != "" {
			return "Grupa " + strings.TrimPrefix(group, "GROUP_")
		}
		return "Faza grupelor"
	case "LAST_16":
		return "Optimi"
	case "QUARTER_FINALS":
		return "Sferturi"
	case "SEMI_FINALS":
		return "Semifinale"
	case "THIRD_PLACE":
		return "Finala mică"
	case "FINAL":
		return "Finală"
	default:
		label := strings.ReplaceAll(strings.ToLower(stage), "_", " ")
		if label == "" {
			return ""
		}
		return strings.ToUpper(label[:1]) + label[1:]
	}
}

func parseDate(s string) *time.Time {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil
	}
	if t, err := time.Parse(time.RFC3339, s); err == nil {
		utc := t.UTC()
		return &utc
	}
	return nil
}
