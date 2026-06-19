package teams

import (
	"context"
	"strings"
)

// Commentator turns match events into human commentary lines (implemented by
// the commentary package). Optional — nil disables AI commentary.
type Commentator interface {
	Enrich(ctx context.Context, fixtureID int, lang, matchLabel string, events []Event) []Event
}

// Service holds team business logic over a Provider.
type Service struct {
	provider    Provider
	commentator Commentator
}

// NewService builds a Service backed by the given Provider (commentator optional).
func NewService(provider Provider, commentator Commentator) *Service {
	return &Service{provider: provider, commentator: commentator}
}

// All returns every team in the competition.
func (s *Service) All(ctx context.Context) ([]Team, error) {
	return s.provider.Teams(ctx)
}

// Get returns one team's full detail by id.
func (s *Service) Get(ctx context.Context, id int) (Detail, bool, error) {
	return s.provider.TeamDetail(ctx, id)
}

// Lineups returns both starting XIs for a fixture identified by team names + date.
func (s *Service) Lineups(ctx context.Context, home, away, date string) (MatchLineups, bool, error) {
	return s.provider.Lineups(ctx, home, away, date)
}

// MatchStats returns per-team match statistics for a fixture (names + date).
func (s *Service) MatchStats(ctx context.Context, home, away, date string) (MatchStats, bool, error) {
	return s.provider.MatchStats(ctx, home, away, date)
}

// Live returns all in-play fixtures of the competition.
func (s *Service) Live(ctx context.Context) ([]LiveMatch, error) {
	return s.provider.LiveMatches(ctx)
}

// SearchPlayers finds players by name for the profile-avatar picker.
func (s *Service) SearchPlayers(ctx context.Context, q string) ([]PlayerHit, error) {
	return s.provider.SearchPlayers(ctx, q)
}

// Events returns the match timeline for a fixture (names + date).
func (s *Service) Events(ctx context.Context, home, away, date string) ([]Event, bool, error) {
	return s.provider.Events(ctx, home, away, date)
}

// EventsByFixture returns the timeline for a known api-football fixture id,
// enriched with AI commentary lines (in lang) when a commentator is configured.
func (s *Service) EventsByFixture(ctx context.Context, fixtureID int, lang string) ([]Event, bool, error) {
	events, found, err := s.provider.EventsByFixture(ctx, fixtureID)
	if err != nil || !found || s.commentator == nil {
		return events, found, err
	}
	return s.commentator.Enrich(ctx, fixtureID, lang, matchLabel(events), events), found, nil
}

// matchLabel derives "TeamA vs TeamB" from the events' distinct team names.
func matchLabel(events []Event) string {
	var names []string
	for _, e := range events {
		if e.Team == "" {
			continue
		}
		seen := false
		for _, n := range names {
			if n == e.Team {
				seen = true
				break
			}
		}
		if !seen {
			names = append(names, e.Team)
		}
		if len(names) == 2 {
			break
		}
	}
	return strings.Join(names, " vs ")
}

// ByName resolves a team detail from a team name (used to enrich match views,
// where names come from a different provider). Match is case-insensitive,
// preferring an exact name match and falling back to a substring match.
func (s *Service) ByName(ctx context.Context, name string) (Detail, bool, error) {
	q := Normalize(name)
	if q == "" {
		return Detail{}, false, nil
	}
	list, err := s.provider.Teams(ctx)
	if err != nil {
		return Detail{}, false, err
	}
	var fallback *Team
	for i := range list {
		ln := Normalize(list[i].Name)
		if ln == q {
			return s.provider.TeamDetail(ctx, list[i].ID)
		}
		if fallback == nil && (strings.Contains(ln, q) || strings.Contains(q, ln)) {
			fallback = &list[i]
		}
	}
	if fallback != nil {
		return s.provider.TeamDetail(ctx, fallback.ID)
	}
	return Detail{}, false, nil
}
