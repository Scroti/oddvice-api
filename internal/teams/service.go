package teams

import (
	"context"
	"strings"
)

// Service holds team business logic over a Provider.
type Service struct {
	provider Provider
}

// NewService builds a Service backed by the given Provider.
func NewService(provider Provider) *Service {
	return &Service{provider: provider}
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

// Events returns the match timeline for a fixture (names + date).
func (s *Service) Events(ctx context.Context, home, away, date string) ([]Event, bool, error) {
	return s.provider.Events(ctx, home, away, date)
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
