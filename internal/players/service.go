package players

import (
	"context"
	"strings"
)

// Service answers player name searches from the indexed store.
type Service struct {
	store *Store
}

// NewService builds a Service over the given store (may be nil → empty results).
func NewService(store *Store) *Service {
	return &Service{store: store}
}

// Search returns matching players (max 30). Queries shorter than 3 runes return
// nothing, to avoid huge result sets.
func (s *Service) Search(ctx context.Context, q string) ([]Player, error) {
	q = strings.TrimSpace(q)
	if len([]rune(q)) < 3 {
		return []Player{}, nil
	}
	return s.store.Search(ctx, q, 30)
}
