// Package apifootball implements teams.Provider against api-football.com (v3),
// which exposes rich team statistics (form, cards, goals, clean sheets) that
// football-data.org does not.
//
// The free tier is ~100 requests/day, so the client:
//   - caches the team list and each team's detail for a long TTL;
//   - backs off (cooldown, serving stale) on HTTP 429.
package apifootball

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/oddvice/api/internal/teams"
)

var errThrottled = errors.New("api-football rate limit reached")

// Client talks to api-football.com and maps its DTOs to the domain model.
type Client struct {
	httpClient *http.Client
	baseURL    string
	apiKey     string
	league     int
	season     int
	cacheTTL   time.Duration

	mu            sync.Mutex
	teamList      []teams.Team
	teamListAt    time.Time
	details       map[int]cachedDetail
	lineups       map[string]cachedLineups
	cooldownUntil time.Time
}

type cachedDetail struct {
	detail teams.Detail
	at     time.Time
}

type cachedLineups struct {
	ml    teams.MatchLineups
	found bool
	at    time.Time
}

// New builds a Client. A nil httpClient gets a default one.
func New(baseURL, apiKey string, league, season int, cacheTTL time.Duration, httpClient *http.Client) *Client {
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 12 * time.Second}
	}
	if cacheTTL <= 0 {
		cacheTTL = 6 * time.Hour
	}
	return &Client{
		httpClient: httpClient,
		baseURL:    strings.TrimRight(baseURL, "/"),
		apiKey:     apiKey,
		league:     league,
		season:     season,
		cacheTTL:   cacheTTL,
		details:    make(map[int]cachedDetail),
		lineups:    make(map[string]cachedLineups),
	}
}

// ---- DTOs ------------------------------------------------------------------

type teamsResponse struct {
	Response []struct {
		Team struct {
			ID      int    `json:"id"`
			Name    string `json:"name"`
			Code    string `json:"code"`
			Country string `json:"country"`
			Founded int    `json:"founded"`
			Logo    string `json:"logo"`
		} `json:"team"`
	} `json:"response"`
}

type cardBracket struct {
	Total *int `json:"total"`
}

type statsResponse struct {
	Response struct {
		Team struct {
			ID   int    `json:"id"`
			Name string `json:"name"`
			Logo string `json:"logo"`
		} `json:"team"`
		Form     string `json:"form"`
		Fixtures struct {
			Played struct{ Total int `json:"total"` } `json:"played"`
			Wins   struct{ Total int `json:"total"` } `json:"wins"`
			Draws  struct{ Total int `json:"total"` } `json:"draws"`
			Loses  struct{ Total int `json:"total"` } `json:"loses"`
		} `json:"fixtures"`
		Goals struct {
			For     struct{ Total struct{ Total *int `json:"total"` } `json:"total"` } `json:"for"`
			Against struct{ Total struct{ Total *int `json:"total"` } `json:"total"` } `json:"against"`
		} `json:"goals"`
		CleanSheet    struct{ Total int `json:"total"` } `json:"clean_sheet"`
		FailedToScore struct{ Total int `json:"total"` } `json:"failed_to_score"`
		Cards         struct {
			Yellow map[string]cardBracket `json:"yellow"`
			Red    map[string]cardBracket `json:"red"`
		} `json:"cards"`
		Lineups []struct {
			Formation string `json:"formation"`
			Played    int    `json:"played"`
		} `json:"lineups"`
	} `json:"response"`
}

// ---- Provider --------------------------------------------------------------

// Teams lists every team in the configured competition/season (cached).
func (c *Client) Teams(ctx context.Context) ([]teams.Team, error) {
	c.mu.Lock()
	fresh := c.teamList != nil && time.Since(c.teamListAt) < c.cacheTTL
	cached := c.teamList
	c.mu.Unlock()
	if fresh {
		return cached, nil
	}

	endpoint := fmt.Sprintf("%s/teams?league=%d&season=%d", c.baseURL, c.league, c.season)
	var payload teamsResponse
	if err := c.get(ctx, endpoint, &payload); err != nil {
		if cached != nil {
			return cached, nil
		}
		return nil, err
	}

	list := make([]teams.Team, 0, len(payload.Response))
	for _, r := range payload.Response {
		list = append(list, teams.Team{
			ID:      r.Team.ID,
			Name:    r.Team.Name,
			Code:    r.Team.Code,
			Country: r.Team.Country,
			Logo:    r.Team.Logo,
			Founded: r.Team.Founded,
		})
	}

	c.mu.Lock()
	c.teamList, c.teamListAt = list, time.Now()
	c.mu.Unlock()
	return list, nil
}

// TeamDetail returns a team's identity plus statistics. found=false only when
// neither the team list nor the statistics endpoint yield anything.
func (c *Client) TeamDetail(ctx context.Context, id int) (teams.Detail, bool, error) {
	c.mu.Lock()
	if cd, ok := c.details[id]; ok && time.Since(cd.at) < c.cacheTTL {
		c.mu.Unlock()
		return cd.detail, true, nil
	}
	c.mu.Unlock()

	// Identity from the (cached) team list.
	var base teams.Team
	found := false
	if list, err := c.Teams(ctx); err == nil {
		for _, t := range list {
			if t.ID == id {
				base, found = t, true
				break
			}
		}
	}
	if base.ID == 0 {
		base.ID = id
	}

	// Statistics (best-effort — identity alone is still a valid detail).
	endpoint := fmt.Sprintf("%s/teams/statistics?league=%d&season=%d&team=%d",
		c.baseURL, c.league, c.season, id)
	var payload statsResponse
	stats := (*teams.Stats)(nil)
	if err := c.get(ctx, endpoint, &payload); err == nil {
		s := mapStats(payload)
		if s.Played > 0 || s.Form != "" || s.GoalsFor > 0 || s.YellowCards > 0 {
			stats = &s
			found = true
			if base.Name == "" {
				base.Name = payload.Response.Team.Name
			}
			if base.Logo == "" {
				base.Logo = payload.Response.Team.Logo
			}
		}
	}

	if !found {
		return teams.Detail{}, false, nil
	}

	detail := teams.Detail{Team: base, Stats: stats}
	c.mu.Lock()
	c.details[id] = cachedDetail{detail: detail, at: time.Now()}
	c.mu.Unlock()
	return detail, true, nil
}

type fixturesResponse struct {
	Response []struct {
		Fixture struct {
			ID int `json:"id"`
		} `json:"fixture"`
		Teams struct {
			Home struct {
				ID   int    `json:"id"`
				Name string `json:"name"`
			} `json:"home"`
			Away struct {
				ID   int    `json:"id"`
				Name string `json:"name"`
			} `json:"away"`
		} `json:"teams"`
	} `json:"response"`
}

type lineupsResponse struct {
	Response []struct {
		Team struct {
			ID   int    `json:"id"`
			Name string `json:"name"`
		} `json:"team"`
		Formation string `json:"formation"`
		StartXI   []struct {
			Player struct {
				Name   string `json:"name"`
				Number int    `json:"number"`
				Pos    string `json:"pos"`
				Grid   string `json:"grid"`
			} `json:"player"`
		} `json:"startXI"`
		Coach struct {
			Name string `json:"name"`
		} `json:"coach"`
	} `json:"response"`
}

// Lineups resolves the fixture by team names + date (YYYY-MM-DD) and returns
// both confirmed starting XIs. Cached per match; found=false when the fixture
// or its lineups aren't available (pre-match, or the plan/season lacks them).
func (c *Client) Lineups(ctx context.Context, home, away, date string) (teams.MatchLineups, bool, error) {
	home, away, date = strings.TrimSpace(home), strings.TrimSpace(away), strings.TrimSpace(date)
	if home == "" || away == "" || date == "" {
		return teams.MatchLineups{}, false, nil
	}
	key := strings.ToLower(home + "|" + away + "|" + date)

	c.mu.Lock()
	if cl, ok := c.lineups[key]; ok && time.Since(cl.at) < c.cacheTTL {
		c.mu.Unlock()
		return cl.ml, cl.found, nil
	}
	c.mu.Unlock()

	fid, ok, err := c.resolveFixture(ctx, home, away, date)
	if err != nil {
		return teams.MatchLineups{}, false, err
	}
	if !ok {
		c.cacheLineups(key, teams.MatchLineups{}, false)
		return teams.MatchLineups{}, false, nil
	}

	var payload lineupsResponse
	if err := c.get(ctx, fmt.Sprintf("%s/fixtures/lineups?fixture=%d", c.baseURL, fid), &payload); err != nil {
		return teams.MatchLineups{}, false, err
	}

	var ml teams.MatchLineups
	for _, r := range payload.Response {
		lu := &teams.Lineup{
			TeamID:    r.Team.ID,
			TeamName:  r.Team.Name,
			Formation: r.Formation,
			Coach:     r.Coach.Name,
		}
		for _, x := range r.StartXI {
			lu.StartXI = append(lu.StartXI, teams.Player{
				Name:   x.Player.Name,
				Number: x.Player.Number,
				Pos:    x.Player.Pos,
				Grid:   x.Player.Grid,
			})
		}
		switch {
		case nameMatches(r.Team.Name, home):
			ml.Home = lu
		case nameMatches(r.Team.Name, away):
			ml.Away = lu
		case ml.Home == nil:
			ml.Home = lu
		default:
			ml.Away = lu
		}
	}

	found := ml.Home != nil || ml.Away != nil
	c.cacheLineups(key, ml, found)
	return ml, found, nil
}

func (c *Client) cacheLineups(key string, ml teams.MatchLineups, found bool) {
	c.mu.Lock()
	c.lineups[key] = cachedLineups{ml: ml, found: found, at: time.Now()}
	c.mu.Unlock()
}

func (c *Client) resolveFixture(ctx context.Context, home, away, date string) (int, bool, error) {
	endpoint := fmt.Sprintf("%s/fixtures?league=%d&season=%d&date=%s",
		c.baseURL, c.league, c.season, url.QueryEscape(date))
	var payload fixturesResponse
	if err := c.get(ctx, endpoint, &payload); err != nil {
		return 0, false, err
	}
	for _, r := range payload.Response {
		h, a := r.Teams.Home.Name, r.Teams.Away.Name
		if (nameMatches(h, home) && nameMatches(a, away)) ||
			(nameMatches(h, away) && nameMatches(a, home)) {
			return r.Fixture.ID, true, nil
		}
	}
	return 0, false, nil
}

func nameMatches(a, b string) bool {
	return teams.NameMatches(a, b)
}

func mapStats(p statsResponse) teams.Stats {
	r := p.Response
	formation, maxPlayed := "", -1
	for _, l := range r.Lineups {
		if l.Formation != "" && l.Played > maxPlayed {
			formation, maxPlayed = l.Formation, l.Played
		}
	}
	return teams.Stats{
		Form:          r.Form,
		Formation:     formation,
		Played:        r.Fixtures.Played.Total,
		Wins:          r.Fixtures.Wins.Total,
		Draws:         r.Fixtures.Draws.Total,
		Losses:        r.Fixtures.Loses.Total,
		GoalsFor:      deref(r.Goals.For.Total.Total),
		GoalsAgainst:  deref(r.Goals.Against.Total.Total),
		CleanSheets:   r.CleanSheet.Total,
		FailedToScore: r.FailedToScore.Total,
		YellowCards:   sumBrackets(r.Cards.Yellow),
		RedCards:      sumBrackets(r.Cards.Red),
	}
}

func sumBrackets(m map[string]cardBracket) int {
	total := 0
	for _, b := range m {
		if b.Total != nil {
			total += *b.Total
		}
	}
	return total
}

func deref(p *int) int {
	if p == nil {
		return 0
	}
	return *p
}

// ---- HTTP ------------------------------------------------------------------

func (c *Client) inCooldown() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return time.Now().Before(c.cooldownUntil)
}

func (c *Client) setCooldown(d time.Duration) {
	c.mu.Lock()
	c.cooldownUntil = time.Now().Add(d)
	c.mu.Unlock()
}

func (c *Client) get(ctx context.Context, endpoint string, dst any) error {
	if c.apiKey == "" {
		return errors.New("api-football key not configured")
	}
	if c.inCooldown() {
		return errThrottled
	}

	if _, err := url.Parse(endpoint); err != nil {
		return fmt.Errorf("bad endpoint: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("x-apisports-key", c.apiKey)
	req.Header.Set("Accept", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("call provider: %w", err)
	}
	defer resp.Body.Close()

	// Back off on rate limiting: 429, or the daily counter hitting zero.
	remaining, remSet := atoiHeaderOK(resp.Header.Get("x-ratelimit-requests-remaining"))
	if resp.StatusCode == http.StatusTooManyRequests || (remSet && remaining <= 0) {
		c.setCooldown(5 * time.Minute)
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

func atoiHeaderOK(s string) (int, bool) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, false
	}
	n, err := strconv.Atoi(s)
	return n, err == nil
}
