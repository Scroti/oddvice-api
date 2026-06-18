package football

import (
	"context"
	"sort"
	"strings"
)

// Provider fetches football data from an external source (e.g. football-data.org).
type Provider interface {
	// Matches returns all matches of the configured competition.
	Matches(ctx context.Context) ([]Match, error)
	// GetMatch returns a single match by id (found=false when missing).
	GetMatch(ctx context.Context, id string) (Match, bool, error)
	// Standings returns the group/league tables.
	Standings(ctx context.Context) ([]Group, error)
}

// Service holds the football business logic, deriving views from the match list.
type Service struct {
	provider Provider
}

// NewService builds a Service backed by the given Provider.
func NewService(provider Provider) *Service {
	return &Service{provider: provider}
}

// All returns every match, sorted by kickoff time (ascending).
func (s *Service) All(ctx context.Context) ([]Match, error) {
	matches, err := s.provider.Matches(ctx)
	if err != nil {
		return nil, err
	}
	sortByKickoff(matches, true)
	return matches, nil
}

// Search filters matches whose team names or stage contain the query.
func (s *Service) Search(ctx context.Context, query string) ([]Match, error) {
	q := strings.ToLower(strings.TrimSpace(query))
	all, err := s.All(ctx)
	if err != nil {
		return nil, err
	}
	if q == "" {
		return all, nil
	}
	out := make([]Match, 0)
	for _, m := range all {
		hay := strings.ToLower(m.HomeTeam + " " + m.AwayTeam + " " + m.League)
		if strings.Contains(hay, q) {
			out = append(out, m)
		}
	}
	return out, nil
}

// Upcoming returns not-yet-finished matches, soonest first.
func (s *Service) Upcoming(ctx context.Context, limit int) ([]Match, error) {
	all, err := s.All(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]Match, 0)
	for _, m := range all {
		if !m.Played() {
			out = append(out, m)
		}
	}
	return capped(out, limit), nil
}

// Results returns finished matches, most recent first.
func (s *Service) Results(ctx context.Context, limit int) ([]Match, error) {
	all, err := s.All(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]Match, 0)
	for _, m := range all {
		if m.Played() {
			out = append(out, m)
		}
	}
	sortByKickoff(out, false) // most recent first
	return capped(out, limit), nil
}

// GetMatch returns a single match by id.
func (s *Service) GetMatch(ctx context.Context, id string) (Match, bool, error) {
	return s.provider.GetMatch(ctx, strings.TrimSpace(id))
}

// Standings returns the group tables.
func (s *Service) Standings(ctx context.Context) ([]Group, error) {
	return s.provider.Standings(ctx)
}

func sortByKickoff(matches []Match, asc bool) {
	sort.SliceStable(matches, func(i, j int) bool {
		ti, tj := matches[i].KickoffAt, matches[j].KickoffAt
		if ti == nil {
			return false
		}
		if tj == nil {
			return true
		}
		if asc {
			return ti.Before(*tj)
		}
		return ti.After(*tj)
	})
}

func capped(matches []Match, limit int) []Match {
	if limit > 0 && len(matches) > limit {
		return matches[:limit]
	}
	return matches
}
