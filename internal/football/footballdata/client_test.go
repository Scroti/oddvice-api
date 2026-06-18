package footballdata

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

const sampleMatches = `{"matches":[
  {"id":537327,"utcDate":"2026-06-11T19:00:00Z","status":"FINISHED","stage":"GROUP_STAGE","group":"GROUP_A",
   "homeTeam":{"name":"Mexico","crest":"https://x/mex.png"},"awayTeam":{"name":"South Africa","crest":"https://x/rsa.png"},
   "score":{"fullTime":{"home":2,"away":0}}},
  {"id":537400,"utcDate":"2026-07-19T19:00:00Z","status":"TIMED","stage":"FINAL","group":null,
   "homeTeam":{"name":"TBD","crest":""},"awayTeam":{"name":"TBD","crest":""},
   "score":{"fullTime":{"home":null,"away":null}}}
]}`

func TestMatches_MapsAndCaches(t *testing.T) {
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Auth-Token") == "" {
			t.Error("expected auth token header")
		}
		calls++
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(sampleMatches))
	}))
	defer srv.Close()

	c := New(srv.URL, "test-key", "WC", 0, srv.Client())
	matches, err := c.Matches(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(matches) != 2 {
		t.Fatalf("expected 2 matches, got %d", len(matches))
	}

	m := matches[0]
	if m.HomeTeam != "Mexico" || m.League != "Group A" {
		t.Errorf("mapping wrong: %+v", m)
	}
	if !m.Played() || *m.HomeScore != 2 {
		t.Errorf("expected finished 2-0, got %+v", m)
	}
	if m.Status != "FT" {
		t.Errorf("expected FT status, got %q", m.Status)
	}
	if m.HomeBadge == "" {
		t.Error("expected crest url")
	}

	if matches[1].League != "Final" || matches[1].Played() {
		t.Errorf("final mapping wrong: %+v", matches[1])
	}

	// Second call should hit the cache (no extra upstream request).
	_, _ = c.Matches(context.Background())
	if calls != 1 {
		t.Errorf("expected 1 upstream call (cached), got %d", calls)
	}
}
