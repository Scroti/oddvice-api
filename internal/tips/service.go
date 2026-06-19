package tips

import (
	"context"
	"strings"
	"time"

	"github.com/oddvice/api/internal/football"
)

// Service derives tip views, resolving match facts from the football service
// and delegating generation/lookup to a Provider.
type Service struct {
	provider Provider
	football *football.Service
}

// NewService wires a Service to a tips Provider and the football service.
func NewService(provider Provider, fb *football.Service) *Service {
	return &Service{provider: provider, football: fb}
}

// ForMatch returns the tips bundle for a single match. found=false when the
// match doesn't exist or the provider has nothing for it.
func (s *Service) ForMatch(ctx context.Context, matchID string) (MatchTips, bool, error) {
	matchID = strings.TrimSpace(matchID)
	m, ok, err := s.football.GetMatch(ctx, matchID)
	if err != nil {
		return MatchTips{}, false, err
	}
	if !ok {
		return MatchTips{}, false, nil
	}
	bundle, has := s.provider.ForMatch(GenInput{
		MatchID:  m.ID,
		HomeTeam: m.HomeTeam,
		AwayTeam: m.AwayTeam,
		League:   m.League,
	})
	return bundle, has, nil
}

// List returns tip bundles for the current matchday only — the fixtures on the
// day of the soonest upcoming match (today's matches, or the next day that has
// matches if there are none today). Days are compared in the server's local
// timezone, which on a local install is the user's. limit caps the result as a
// safety net (a matchday rarely exceeds it).
func (s *Service) List(ctx context.Context, limit int) ([]MatchTips, error) {
	matches, err := s.football.Upcoming(ctx, 0) // all upcoming, soonest first
	if err != nil {
		return nil, err
	}

	loc := time.Local
	target := ""
	out := make([]MatchTips, 0)
	for _, m := range matches {
		if m.KickoffAt == nil {
			continue
		}
		day := m.KickoffAt.In(loc).Format("2006-01-02")
		if target == "" {
			target = day // matchday = the soonest upcoming match's day
		}
		if day != target {
			break // sorted ascending — we've passed the matchday
		}
		if limit > 0 && len(out) >= limit {
			break
		}
		if bundle, has := s.provider.ForMatch(GenInput{
			MatchID:  m.ID,
			HomeTeam: m.HomeTeam,
			AwayTeam: m.AwayTeam,
			League:   m.League,
		}); has {
			out = append(out, bundle)
		}
	}
	return out, nil
}
