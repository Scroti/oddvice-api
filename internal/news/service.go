package news

import "context"

// Provider fetches news articles from an external feed. Implementations live in
// sub-packages (e.g. googlenews) and are the only thing that changes when the
// feed source changes.
type Provider interface {
	Latest(ctx context.Context) ([]Article, error)
}

// Service holds the news business logic.
type Service struct {
	provider Provider
}

// NewService builds a Service backed by the given Provider.
func NewService(provider Provider) *Service {
	return &Service{provider: provider}
}

// Latest returns the most recent articles.
func (s *Service) Latest(ctx context.Context) ([]Article, error) {
	return s.provider.Latest(ctx)
}

// GetByID returns a single article by id. The bool is false when not found.
func (s *Service) GetByID(ctx context.Context, id string) (Article, bool, error) {
	articles, err := s.provider.Latest(ctx)
	if err != nil {
		return Article{}, false, err
	}
	for _, a := range articles {
		if a.ID == id {
			return a, true, nil
		}
	}
	return Article{}, false, nil
}
