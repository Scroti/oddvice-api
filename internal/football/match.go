// Package football defines the football domain model and business logic,
// independent of any specific data provider.
package football

import "time"

// Match is a provider-agnostic representation of a football fixture.
type Match struct {
	ID        string     `json:"id"`
	Name      string     `json:"name"`
	League    string     `json:"league"`
	Season    string     `json:"season"`
	HomeTeam  string     `json:"homeTeam"`
	AwayTeam  string     `json:"awayTeam"`
	HomeScore *int       `json:"homeScore"` // nil when not yet played
	AwayScore *int       `json:"awayScore"` // nil when not yet played
	Status    string     `json:"status"`
	Venue     string     `json:"venue,omitempty"`
	KickoffAt *time.Time `json:"kickoffAt"` // nil when unknown
	Thumbnail string     `json:"thumbnail,omitempty"`
	HomeBadge string     `json:"homeBadge,omitempty"`
	AwayBadge string     `json:"awayBadge,omitempty"`
	Video     string     `json:"video,omitempty"` // highlights URL (e.g. YouTube)
}
