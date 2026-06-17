// Package footballdata implements football.Provider against football-data.org,
// which covers the FIFA World Cup with real crests, scores and fixtures.
package footballdata

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/oddvice/api/internal/football"
)

// Client talks to football-data.org and maps its DTOs to the domain model.
type Client struct {
	httpClient  *http.Client
	baseURL     string
	apiKey      string
	competition string
	cacheTTL    time.Duration

	mu       sync.Mutex
	cached   []football.Match
	cachedAt time.Time
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
	ID       int    `json:"id"`
	UTCDate  string `json:"utcDate"`
	Status   string `json:"status"`
	Stage    string `json:"stage"`
	Group    string `json:"group"`
	Venue    string `json:"venue"`
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
// the API rate limit. Stale cache is served if a refresh fails.
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
			return cached, nil // serve stale on failure (e.g. rate limit)
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

// GetMatch looks up a single match by id. The bool is false when not found.
func (c *Client) GetMatch(ctx context.Context, id string) (football.Match, bool, error) {
	endpoint := fmt.Sprintf("%s/v4/matches/%s", c.baseURL, id)
	var m matchDTO
	if err := c.get(ctx, endpoint, &m); err != nil {
		return football.Match{}, false, err
	}
	if m.ID == 0 {
		return football.Match{}, false, nil
	}
	return m.toMatch(), true, nil
}

func (c *Client) get(ctx context.Context, url string, dst any) error {
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
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("provider returned status %d", resp.StatusCode)
	}
	if err := json.NewDecoder(resp.Body).Decode(dst); err != nil {
		return fmt.Errorf("decode provider response: %w", err)
	}
	return nil
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
