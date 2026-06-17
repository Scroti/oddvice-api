package thesportsdb

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

const sampleResponse = `{"event":[{
  "idEvent":"2401588","strEvent":"Arsenal vs Chelsea","strLeague":"EFL Cup",
  "strSeason":"2025-2026","strHomeTeam":"Arsenal","strAwayTeam":"Chelsea",
  "intHomeScore":"1","intAwayScore":"0","strStatus":"FT",
  "dateEvent":"2026-02-03","strTime":"20:00:00",
  "strTimestamp":"2026-02-03T20:00:00","strVenue":"Emirates Stadium"
}]}`

func TestSearchMatches_MapsFields(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.Contains(r.URL.Path, "searchevents.php") {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		if got := r.URL.Query().Get("e"); got != "Arsenal_vs_Chelsea" {
			t.Errorf("expected underscored query, got %q", got)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(sampleResponse))
	}))
	defer srv.Close()

	c := New(srv.URL, "3", srv.Client())
	matches, err := c.SearchMatches(context.Background(), "Arsenal vs Chelsea")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(matches) != 1 {
		t.Fatalf("expected 1 match, got %d", len(matches))
	}

	m := matches[0]
	if m.HomeTeam != "Arsenal" || m.AwayTeam != "Chelsea" {
		t.Errorf("teams mapped wrong: %+v", m)
	}
	if m.HomeScore == nil || *m.HomeScore != 1 || m.AwayScore == nil || *m.AwayScore != 0 {
		t.Errorf("scores mapped wrong: %+v", m)
	}
	if m.KickoffAt == nil || m.KickoffAt.Year() != 2026 {
		t.Errorf("kickoff mapped wrong: %+v", m.KickoffAt)
	}
}

func TestSearchMatches_EmptyResult(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"event":null}`))
	}))
	defer srv.Close()

	c := New(srv.URL, "3", srv.Client())
	matches, err := c.SearchMatches(context.Background(), "no such match")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(matches) != 0 {
		t.Errorf("expected 0 matches, got %d", len(matches))
	}
}
