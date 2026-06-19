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

// Player is a player from a match lineup (starter or substitute).
type Player struct {
	ID     int    `json:"id,omitempty"`
	Name   string `json:"name"`
	Number int    `json:"number,omitempty"`
	Pos    string `json:"pos,omitempty"`   // "G" | "D" | "M" | "F"
	Grid   string `json:"grid,omitempty"`  // "row:col", row 1 = goalkeeper line
	Photo  string `json:"photo,omitempty"` // headshot URL
}

// Lineup is one team's confirmed XI for a fixture.
type Lineup struct {
	TeamID      int      `json:"teamId"`
	TeamName    string   `json:"teamName"`
	Formation   string   `json:"formation"`
	Coach       string   `json:"coach,omitempty"`
	CoachPhoto  string   `json:"coachPhoto,omitempty"`
	StartXI     []Player `json:"startXI"`
	Substitutes []Player `json:"substitutes,omitempty"`
}

// MatchLineups holds both teams' lineups for a fixture (nil when unavailable).
type MatchLineups struct {
	Home *Lineup `json:"home"`
	Away *Lineup `json:"away"`
}

// StatLine is one match statistic compared across both teams (values are raw
// strings as the provider gives them, e.g. "42%", "8", "0.48").
type StatLine struct {
	Type string `json:"type"`
	Home string `json:"home"`
	Away string `json:"away"`
}

// MatchStats is the per-match team statistics (possession, shots, passes…).
type MatchStats struct {
	Lines []StatLine `json:"lines"`
}

// LiveMatch is a currently in-play fixture for the live scoreboard.
type LiveMatch struct {
	FixtureID int    `json:"fixtureId"`
	Home      string `json:"home"`
	Away      string `json:"away"`
	HomeLogo  string `json:"homeLogo,omitempty"`
	AwayLogo  string `json:"awayLogo,omitempty"`
	HomeGoals int    `json:"homeGoals"`
	AwayGoals int    `json:"awayGoals"`
	Elapsed   int    `json:"elapsed"` // minutes played
	Status    string `json:"status"`  // short code: 1H, HT, 2H, ET, P, …
}

// Event is one match timeline entry (goal, card, substitution, VAR).
type Event struct {
	Minute int    `json:"minute"`
	Extra  int    `json:"extra,omitempty"`
	Team   string `json:"team"`
	Type   string `json:"type"`   // "Goal" | "Card" | "subst" | "Var"
	Detail string `json:"detail"` // "Normal Goal", "Yellow Card", "Substitution 1", …
	Player string `json:"player,omitempty"`
	Assist string `json:"assist,omitempty"` // assister, or (for subs) the player off
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
	// MatchStats returns per-team match statistics for the fixture (found=false
	// when unavailable, e.g. before kickoff).
	MatchStats(ctx context.Context, home, away, date string) (MatchStats, bool, error)
	// LiveMatches returns all currently in-play fixtures of the competition.
	LiveMatches(ctx context.Context) ([]LiveMatch, error)
	// Events returns the match timeline (goals, cards, subs) for the fixture.
	Events(ctx context.Context, home, away, date string) ([]Event, bool, error)
	// EventsByFixture returns the timeline for a known api-football fixture id.
	EventsByFixture(ctx context.Context, fixtureID int) ([]Event, bool, error)
}
