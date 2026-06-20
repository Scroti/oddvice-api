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
	stats         map[string]cachedStats
	live          []teams.LiveMatch
	liveAt        time.Time
	events        map[string]cachedEvents
	fixtureIDs    map[string]int            // "home|away|date" -> fixture id (stable)
	players       map[string]cachedPlayers  // normalized query -> player search results
	cooldownUntil time.Time
}

// liveTTL keeps live data fresh without hammering the API (it updates ~every 15s).
const liveTTL = 20 * time.Second

// lineupRetryTTL caps how long a "no lineup yet" result is cached, so the open
// match screen and the lineup warmer pick up lineups the moment they're
// published (~20-40 min pre-kickoff) instead of being stuck on the long
// not-found cache. Found lineups keep the normal long TTL.
const lineupRetryTTL = 5 * time.Minute

// playerSearchTTL caches a player-name search result (used by the avatar picker).
const playerSearchTTL = 30 * time.Minute

type cachedPlayers struct {
	players []teams.PlayerHit
	at      time.Time
}

type cachedEvents struct {
	events []teams.Event
	found  bool
	at     time.Time
}

type cachedStats struct {
	ms    teams.MatchStats
	found bool
	at    time.Time
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
		stats:      make(map[string]cachedStats),
		events:     make(map[string]cachedEvents),
		fixtureIDs: make(map[string]int),
		players:    make(map[string]cachedPlayers),
	}
}

// playerPhoto builds the headshot URL for an api-football player id.
func playerPhoto(id int) string {
	if id == 0 {
		return ""
	}
	return fmt.Sprintf("https://media.api-sports.io/football/players/%d.png", id)
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

type lineupPlayer struct {
	Player struct {
		ID     int    `json:"id"`
		Name   string `json:"name"`
		Number int    `json:"number"`
		Pos    string `json:"pos"`
		Grid   string `json:"grid"`
	} `json:"player"`
}

type lineupsResponse struct {
	Response []struct {
		Team struct {
			ID   int    `json:"id"`
			Name string `json:"name"`
		} `json:"team"`
		Formation   string         `json:"formation"`
		StartXI     []lineupPlayer `json:"startXI"`
		Substitutes []lineupPlayer `json:"substitutes"`
		Coach       struct {
			Name  string `json:"name"`
			Photo string `json:"photo"`
		} `json:"coach"`
	} `json:"response"`
}

func mapPlayers(in []lineupPlayer) []teams.Player {
	out := make([]teams.Player, 0, len(in))
	for _, e := range in {
		p := e.Player
		out = append(out, teams.Player{
			ID:     p.ID,
			Name:   p.Name,
			Number: p.Number,
			Pos:    p.Pos,
			Grid:   p.Grid,
			Photo:  playerPhoto(p.ID),
		})
	}
	return out
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
	if cl, ok := c.lineups[key]; ok {
		ttl := c.cacheTTL
		if !cl.found {
			ttl = lineupRetryTTL
		}
		if time.Since(cl.at) < ttl {
			c.mu.Unlock()
			return cl.ml, cl.found, nil
		}
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
			TeamID:      r.Team.ID,
			TeamName:    r.Team.Name,
			Formation:   r.Formation,
			Coach:       r.Coach.Name,
			CoachPhoto:  r.Coach.Photo,
			StartXI:     mapPlayers(r.StartXI),
			Substitutes: mapPlayers(r.Substitutes),
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
	key := strings.ToLower(home + "|" + away + "|" + date)
	c.mu.Lock()
	if id, ok := c.fixtureIDs[key]; ok {
		c.mu.Unlock()
		return id, true, nil
	}
	c.mu.Unlock()

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
			c.mu.Lock()
			c.fixtureIDs[key] = r.Fixture.ID
			c.mu.Unlock()
			return r.Fixture.ID, true, nil
		}
	}
	return 0, false, nil
}

type statisticsResponse struct {
	Response []struct {
		Team struct {
			ID   int    `json:"id"`
			Name string `json:"name"`
		} `json:"team"`
		Statistics []struct {
			Type  string          `json:"type"`
			Value json.RawMessage `json:"value"`
		} `json:"statistics"`
	} `json:"response"`
}

// rawToString renders a JSON value (number, "42%", or null) as a plain string.
func rawToString(r json.RawMessage) string {
	s := strings.TrimSpace(string(r))
	if s == "" || s == "null" {
		return ""
	}
	if len(s) >= 2 && s[0] == '"' {
		var v string
		if err := json.Unmarshal(r, &v); err == nil {
			return v
		}
	}
	return s
}

// MatchStats returns per-team match statistics, paired home vs away.
func (c *Client) MatchStats(ctx context.Context, home, away, date string) (teams.MatchStats, bool, error) {
	home, away, date = strings.TrimSpace(home), strings.TrimSpace(away), strings.TrimSpace(date)
	if home == "" || away == "" || date == "" {
		return teams.MatchStats{}, false, nil
	}
	key := strings.ToLower(home + "|" + away + "|" + date)

	c.mu.Lock()
	if cs, ok := c.stats[key]; ok && time.Since(cs.at) < c.cacheTTL {
		c.mu.Unlock()
		return cs.ms, cs.found, nil
	}
	c.mu.Unlock()

	fid, ok, err := c.resolveFixture(ctx, home, away, date)
	if err != nil {
		return teams.MatchStats{}, false, err
	}
	if !ok {
		c.cacheStats(key, teams.MatchStats{}, false)
		return teams.MatchStats{}, false, nil
	}

	var payload statisticsResponse
	if err := c.get(ctx, fmt.Sprintf("%s/fixtures/statistics?fixture=%d", c.baseURL, fid), &payload); err != nil {
		return teams.MatchStats{}, false, err
	}
	if len(payload.Response) < 2 {
		c.cacheStats(key, teams.MatchStats{}, false)
		return teams.MatchStats{}, false, nil
	}

	// Figure out which entry is home vs away.
	hi, ai := 0, 1
	if nameMatches(payload.Response[1].Team.Name, home) || nameMatches(payload.Response[0].Team.Name, away) {
		hi, ai = 1, 0
	}
	awayByType := make(map[string]string)
	for _, s := range payload.Response[ai].Statistics {
		awayByType[s.Type] = rawToString(s.Value)
	}
	ms := teams.MatchStats{}
	for _, s := range payload.Response[hi].Statistics {
		ms.Lines = append(ms.Lines, teams.StatLine{
			Type: s.Type,
			Home: rawToString(s.Value),
			Away: awayByType[s.Type],
		})
	}

	found := len(ms.Lines) > 0
	c.cacheStats(key, ms, found)
	return ms, found, nil
}

func (c *Client) cacheStats(key string, ms teams.MatchStats, found bool) {
	c.mu.Lock()
	c.stats[key] = cachedStats{ms: ms, found: found, at: time.Now()}
	c.mu.Unlock()
}

type liveFixturesResponse struct {
	Response []struct {
		Fixture struct {
			ID     int `json:"id"`
			Status struct {
				Short   string `json:"short"`
				Elapsed int    `json:"elapsed"`
			} `json:"status"`
		} `json:"fixture"`
		Teams struct {
			Home struct {
				Name string `json:"name"`
				Logo string `json:"logo"`
			} `json:"home"`
			Away struct {
				Name string `json:"name"`
				Logo string `json:"logo"`
			} `json:"away"`
		} `json:"teams"`
		Goals struct {
			Home *int `json:"home"`
			Away *int `json:"away"`
		} `json:"goals"`
	} `json:"response"`
}

// LiveMatches returns all currently in-play fixtures (cached ~20s).
func (c *Client) LiveMatches(ctx context.Context) ([]teams.LiveMatch, error) {
	c.mu.Lock()
	if c.live != nil && time.Since(c.liveAt) < liveTTL {
		out := c.live
		c.mu.Unlock()
		return out, nil
	}
	cached := c.live
	c.mu.Unlock()

	endpoint := fmt.Sprintf("%s/fixtures?live=all&league=%d&season=%d", c.baseURL, c.league, c.season)
	var payload liveFixturesResponse
	if err := c.get(ctx, endpoint, &payload); err != nil {
		if cached != nil {
			return cached, nil
		}
		return nil, err
	}

	out := make([]teams.LiveMatch, 0, len(payload.Response))
	for _, r := range payload.Response {
		out = append(out, teams.LiveMatch{
			FixtureID: r.Fixture.ID,
			Home:      r.Teams.Home.Name,
			Away:      r.Teams.Away.Name,
			HomeLogo:  r.Teams.Home.Logo,
			AwayLogo:  r.Teams.Away.Logo,
			HomeGoals: deref(r.Goals.Home),
			AwayGoals: deref(r.Goals.Away),
			Elapsed:   r.Fixture.Status.Elapsed,
			Status:    r.Fixture.Status.Short,
		})
	}

	c.mu.Lock()
	c.live, c.liveAt = out, time.Now()
	c.mu.Unlock()
	return out, nil
}

type eventsResponse struct {
	Response []struct {
		Time struct {
			Elapsed int  `json:"elapsed"`
			Extra   *int `json:"extra"`
		} `json:"time"`
		Team   struct{ Name string `json:"name"` } `json:"team"`
		Player struct{ Name string `json:"name"` } `json:"player"`
		Assist struct{ Name string `json:"name"` } `json:"assist"`
		Type   string `json:"type"`
		Detail string `json:"detail"`
	} `json:"response"`
}

// Events returns the match timeline for the fixture (cached ~20s for live).
func (c *Client) Events(ctx context.Context, home, away, date string) ([]teams.Event, bool, error) {
	home, away, date = strings.TrimSpace(home), strings.TrimSpace(away), strings.TrimSpace(date)
	if home == "" || away == "" || date == "" {
		return nil, false, nil
	}
	key := strings.ToLower(home + "|" + away + "|" + date)

	c.mu.Lock()
	if ce, ok := c.events[key]; ok && time.Since(ce.at) < liveTTL {
		c.mu.Unlock()
		return ce.events, ce.found, nil
	}
	c.mu.Unlock()

	fid, ok, err := c.resolveFixture(ctx, home, away, date)
	if err != nil {
		return nil, false, err
	}
	if !ok {
		c.mu.Lock()
		c.events[key] = cachedEvents{found: false, at: time.Now()}
		c.mu.Unlock()
		return nil, false, nil
	}

	out, found, err := c.fetchEvents(ctx, fid)
	if err != nil {
		return nil, false, err
	}
	c.mu.Lock()
	c.events[key] = cachedEvents{events: out, found: found, at: time.Now()}
	c.mu.Unlock()
	return out, found, nil
}

func (c *Client) fetchEvents(ctx context.Context, fid int) ([]teams.Event, bool, error) {
	var payload eventsResponse
	if err := c.get(ctx, fmt.Sprintf("%s/fixtures/events?fixture=%d", c.baseURL, fid), &payload); err != nil {
		return nil, false, err
	}
	out := make([]teams.Event, 0, len(payload.Response))
	for _, e := range payload.Response {
		out = append(out, teams.Event{
			Minute: e.Time.Elapsed,
			Extra:  deref(e.Time.Extra),
			Team:   e.Team.Name,
			Type:   e.Type,
			Detail: e.Detail,
			Player: e.Player.Name,
			Assist: e.Assist.Name,
		})
	}
	return out, len(out) > 0, nil
}

// EventsByFixture returns the timeline for a known fixture id (cached ~20s).
func (c *Client) EventsByFixture(ctx context.Context, fid int) ([]teams.Event, bool, error) {
	if fid <= 0 {
		return nil, false, nil
	}
	key := "fid:" + strconv.Itoa(fid)
	c.mu.Lock()
	if ce, ok := c.events[key]; ok && time.Since(ce.at) < liveTTL {
		c.mu.Unlock()
		return ce.events, ce.found, nil
	}
	c.mu.Unlock()

	out, found, err := c.fetchEvents(ctx, fid)
	if err != nil {
		return nil, false, err
	}
	c.mu.Lock()
	c.events[key] = cachedEvents{events: out, found: found, at: time.Now()}
	c.mu.Unlock()
	return out, found, nil
}

type playerProfilesResponse struct {
	Response []struct {
		Player struct {
			ID          int    `json:"id"`
			Name        string `json:"name"`
			Firstname   string `json:"firstname"`
			Lastname    string `json:"lastname"`
			Photo       string `json:"photo"`
			Nationality string `json:"nationality"`
		} `json:"player"`
	} `json:"response"`
}

// SearchPlayers finds players by (partial) name. It tries /players/profiles
// first, then falls back to the competition /players search. Always graceful:
// returns an empty slice (never an error) for short/empty queries or on any
// provider failure, so the handler can serve HTTP 200.
func (c *Client) SearchPlayers(ctx context.Context, q string) ([]teams.PlayerHit, error) {
	q = strings.TrimSpace(q)
	if len([]rune(q)) < 3 {
		return []teams.PlayerHit{}, nil
	}
	key := strings.ToLower(q)

	c.mu.Lock()
	if cp, ok := c.players[key]; ok && time.Since(cp.at) < playerSearchTTL {
		out := cp.players
		c.mu.Unlock()
		return out, nil
	}
	c.mu.Unlock()

	hits := c.fetchPlayerProfiles(ctx, q)
	if len(hits) == 0 {
		hits = c.fetchPlayersFallback(ctx, q)
	}
	if hits == nil {
		hits = []teams.PlayerHit{}
	}

	// Only cache real results so a transient throttle doesn't stick as "none".
	if len(hits) > 0 {
		c.mu.Lock()
		c.players[key] = cachedPlayers{players: hits, at: time.Now()}
		c.mu.Unlock()
	}
	return hits, nil
}

func (c *Client) fetchPlayerProfiles(ctx context.Context, q string) []teams.PlayerHit {
	endpoint := fmt.Sprintf("%s/players/profiles?search=%s", c.baseURL, url.QueryEscape(q))
	var payload playerProfilesResponse
	if err := c.get(ctx, endpoint, &payload); err != nil {
		return nil
	}
	return mapPlayerHits(payload)
}

func (c *Client) fetchPlayersFallback(ctx context.Context, q string) []teams.PlayerHit {
	endpoint := fmt.Sprintf("%s/players?search=%s&league=%d&season=%d",
		c.baseURL, url.QueryEscape(q), c.league, c.season)
	var payload playerProfilesResponse
	if err := c.get(ctx, endpoint, &payload); err != nil {
		return nil
	}
	return mapPlayerHits(payload)
}

func mapPlayerHits(p playerProfilesResponse) []teams.PlayerHit {
	out := make([]teams.PlayerHit, 0, len(p.Response))
	seen := make(map[int]struct{})
	for _, r := range p.Response {
		pl := r.Player
		if pl.ID == 0 {
			continue
		}
		if _, dup := seen[pl.ID]; dup {
			continue
		}
		seen[pl.ID] = struct{}{}
		name := strings.TrimSpace(pl.Name)
		if name == "" {
			name = strings.TrimSpace(pl.Firstname + " " + pl.Lastname)
		}
		photo := pl.Photo
		if photo == "" {
			photo = playerPhoto(pl.ID)
		}
		out = append(out, teams.PlayerHit{
			ID:          pl.ID,
			Name:        name,
			Photo:       photo,
			Nationality: pl.Nationality,
		})
	}
	return out
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

// ---- Squads (build the searchable player index) ----------------------------

// SquadPlayer is one player in a team's current squad, used ONCE by the player
// index ingester (not per user search). Photo falls back to the headshot URL.
type SquadPlayer struct {
	ID       int
	Name     string
	Photo    string
	Position string
}

type squadsResponse struct {
	Response []struct {
		Team struct {
			ID   int    `json:"id"`
			Name string `json:"name"`
		} `json:"team"`
		Players []struct {
			ID       int    `json:"id"`
			Name     string `json:"name"`
			Number   int    `json:"number"`
			Position string `json:"position"`
			Photo    string `json:"photo"`
		} `json:"players"`
	} `json:"response"`
}

// Squad returns a team's current squad in a single api-football call. The player
// index is ingested from these once (and refreshed daily), so user-facing name
// searches hit Postgres and make zero api-football calls.
func (c *Client) Squad(ctx context.Context, teamID int) ([]SquadPlayer, error) {
	endpoint := fmt.Sprintf("%s/players/squads?team=%d", c.baseURL, teamID)
	var payload squadsResponse
	if err := c.get(ctx, endpoint, &payload); err != nil {
		return nil, err
	}
	if len(payload.Response) == 0 {
		return nil, nil
	}
	in := payload.Response[0].Players
	out := make([]SquadPlayer, 0, len(in))
	for _, p := range in {
		if p.ID == 0 {
			continue
		}
		photo := p.Photo
		if photo == "" {
			photo = playerPhoto(p.ID)
		}
		out = append(out, SquadPlayer{ID: p.ID, Name: p.Name, Photo: photo, Position: p.Position})
	}
	return out, nil
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
