// Package teams defines the team domain model and a provider abstraction for
// rich team details (form, cards, goals) sourced from api-football.com.
package teams

import "context"

// Team is the basic identity of a national team.
type Team struct {
	ID      int    `json:"id"`
	Name    string `json:"name"`
	Code    string `json:"code,omitempty"`    // e.g. "ROU"
	Country string `json:"country,omitempty"`
	Logo    string `json:"logo,omitempty"`
	Founded int    `json:"founded,omitempty"`
}

// Stats is a team's season-level performance, derived from the provider's
// statistics endpoint. Zero values mean "no data yet" (e.g. tournament not
// started) and the UI should hide empty sections gracefully.
type Stats struct {
	Form          string `json:"form"`          // recent results, oldest→newest, e.g. "WWDLW"
	Formation     string `json:"formation,omitempty"` // most-used formation, e.g. "4-3-3"
	Played        int    `json:"played"`
	Wins          int    `json:"wins"`
	Draws         int    `json:"draws"`
	Losses        int    `json:"losses"`
	GoalsFor      int    `json:"goalsFor"`
	GoalsAgainst  int    `json:"goalsAgainst"`
	CleanSheets   int    `json:"cleanSheets"`
	FailedToScore int    `json:"failedToScore"`
	YellowCards   int    `json:"yellowCards"`
	RedCards      int    `json:"redCards"`
}

// Detail combines a team's identity with its statistics.
type Detail struct {
	Team
	Stats *Stats `json:"stats,omitempty"`
}

// Player is a starting-XI player from a match lineup.
type Player struct {
	Name   string `json:"name"`
	Number int    `json:"number,omitempty"`
	Pos    string `json:"pos,omitempty"`  // "G" | "D" | "M" | "F"
	Grid   string `json:"grid,omitempty"` // "row:col", row 1 = goalkeeper line
}

// Lineup is one team's confirmed starting XI for a fixture.
type Lineup struct {
	TeamID    int      `json:"teamId"`
	TeamName  string   `json:"teamName"`
	Formation string   `json:"formation"`
	Coach     string   `json:"coach,omitempty"`
	StartXI   []Player `json:"startXI"`
}

// MatchLineups holds both teams' lineups for a fixture (nil when unavailable).
type MatchLineups struct {
	Home *Lineup `json:"home"`
	Away *Lineup `json:"away"`
}

// Provider fetches team data from an external source (api-football.com).
type Provider interface {
	// Teams lists every team in the configured competition/season.
	Teams(ctx context.Context) ([]Team, error)
	// TeamDetail returns one team plus its statistics (found=false if missing).
	TeamDetail(ctx context.Context, id int) (Detail, bool, error)
	// Lineups resolves a fixture by team names + date (YYYY-MM-DD) and returns
	// both starting XIs. found=false when the fixture or lineups aren't available
	// (e.g. pre-match, or the plan/season lacks the data).
	Lineups(ctx context.Context, home, away, date string) (MatchLineups, bool, error)
}
