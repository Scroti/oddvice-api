package football

import (
	"context"
	"errors"
	"strings"
)

// ErrEmptyQuery is returned when a search is attempted with a blank query.
var ErrEmptyQuery = errors.New("query must not be empty")

// Provider fetches football data from an external source. Implementations live
// in sub-packages (e.g. thesportsdb); swapping providers means implementing
// this interface and nothing else.
type Provider interface {
	SearchMatches(ctx context.Context, query string) ([]Match, error)
}

// Service holds the football business logic.
type Service struct {
	provider Provider
}

// NewService builds a Service backed by the given Provider.
func NewService(provider Provider) *Service {
	return &Service{provider: provider}
}

// SearchMatches validates the query and delegates to the provider.
func (s *Service) SearchMatches(ctx context.Context, query string) ([]Match, error) {
	query = strings.TrimSpace(query)
	if query == "" {
		return nil, ErrEmptyQuery
	}
	return s.provider.SearchMatches(ctx, query)
}
