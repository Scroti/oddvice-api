// Package thesportsdb implements football.Provider against TheSportsDB's free
// API (https://www.thesportsdb.com), which needs no key for the test key "3".
package thesportsdb

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/oddvice/api/internal/football"
)

// Client talks to TheSportsDB and maps its DTOs to the football domain model.
type Client struct {
	httpClient *http.Client
	baseURL    string
	apiKey     string
}

// New builds a Client. If httpClient is nil a default one is used.
func New(baseURL, apiKey string, httpClient *http.Client) *Client {
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 10 * time.Second}
	}
	return &Client{
		httpClient: httpClient,
		baseURL:    strings.TrimRight(baseURL, "/"),
		apiKey:     apiKey,
	}
}

// eventDTO mirrors the relevant fields of a TheSportsDB event. Numeric values
// arrive as strings, so we parse them defensively.
type eventDTO struct {
	IDEvent      string `json:"idEvent"`
	StrEvent     string `json:"strEvent"`
	StrLeague    string `json:"strLeague"`
	StrSeason    string `json:"strSeason"`
	StrHomeTeam  string `json:"strHomeTeam"`
	StrAwayTeam  string `json:"strAwayTeam"`
	IntHomeScore string `json:"intHomeScore"`
	IntAwayScore string `json:"intAwayScore"`
	StrStatus    string `json:"strStatus"`
	DateEvent    string `json:"dateEvent"`
	StrTime      string `json:"strTime"`
	StrTimestamp string `json:"strTimestamp"`
	StrVenue     string `json:"strVenue"`
	StrThumb     string `json:"strThumb"`
}

type searchEventsResponse struct {
	Event []eventDTO `json:"event"`
}

// SearchMatches searches events by name (e.g. "Arsenal vs Chelsea").
func (c *Client) SearchMatches(ctx context.Context, query string) ([]football.Match, error) {
	// TheSportsDB expects spaces as underscores in the event-name search.
	e := url.QueryEscape(strings.ReplaceAll(strings.TrimSpace(query), " ", "_"))
	endpoint := fmt.Sprintf("%s/api/v1/json/%s/searchevents.php?e=%s", c.baseURL, c.apiKey, e)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Accept", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("call provider: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("provider returned status %d", resp.StatusCode)
	}

	var payload searchEventsResponse
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return nil, fmt.Errorf("decode provider response: %w", err)
	}

	matches := make([]football.Match, 0, len(payload.Event))
	for _, e := range payload.Event {
		matches = append(matches, e.toMatch())
	}
	return matches, nil
}

func (e eventDTO) toMatch() football.Match {
	return football.Match{
		ID:        e.IDEvent,
		Name:      e.StrEvent,
		League:    e.StrLeague,
		Season:    e.StrSeason,
		HomeTeam:  e.StrHomeTeam,
		AwayTeam:  e.StrAwayTeam,
		HomeScore: parseScore(e.IntHomeScore),
		AwayScore: parseScore(e.IntAwayScore),
		Status:    e.StrStatus,
		Venue:     e.StrVenue,
		KickoffAt: parseKickoff(e.StrTimestamp, e.DateEvent, e.StrTime),
		Thumbnail: e.StrThumb,
	}
}

// parseScore turns a possibly-empty score string into *int (nil if absent).
func parseScore(s string) *int {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil
	}
	n, err := strconv.Atoi(s)
	if err != nil {
		return nil
	}
	return &n
}

// parseKickoff derives the kickoff time from the timestamp, falling back to the
// separate date/time fields. TheSportsDB times have no zone; we treat them UTC.
func parseKickoff(timestamp, date, clock string) *time.Time {
	timestamp = strings.TrimSpace(timestamp)
	if timestamp != "" {
		for _, layout := range []string{time.RFC3339, "2006-01-02T15:04:05"} {
			if t, err := time.Parse(layout, timestamp); err == nil {
				utc := t.UTC()
				return &utc
			}
		}
	}
	date, clock = strings.TrimSpace(date), strings.TrimSpace(clock)
	if date != "" && clock != "" {
		if t, err := time.Parse("2006-01-02 15:04:05", date+" "+clock); err == nil {
			return &t
		}
	}
	return nil
}
