package news

import "context"

// Provider fetches localized news articles from an external feed. Implementations
// live in sub-packages (e.g. googlenews) and are the only thing that changes when
// the feed source changes. lang is an app locale (e.g. "en", "fr").
type Provider interface {
	Latest(ctx context.Context, lang string) ([]Article, error)
}

// Service holds the news business logic.
type Service struct {
	provider Provider
}

// NewService builds a Service backed by the given Provider.
func NewService(provider Provider) *Service {
	return &Service{provider: provider}
}

// Latest returns the most recent articles for the given language.
func (s *Service) Latest(ctx context.Context, lang string) ([]Article, error) {
	return s.provider.Latest(ctx, lang)
}

// GetByID returns a single article by id, within the given language's feed.
// The bool is false when not found.
func (s *Service) GetByID(ctx context.Context, id, lang string) (Article, bool, error) {
	articles, err := s.provider.Latest(ctx, lang)
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
