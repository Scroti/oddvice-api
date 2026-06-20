// Package news defines the news domain model and business logic, independent
// of any specific feed provider.
package news

import "time"

// Article is a provider-agnostic news item.
type Article struct {
	ID          string     `json:"id"` // stable id derived from the source guid/link
	Title       string     `json:"title"`
	Link        string     `json:"link"`   // URL of the original article (the source site)
	Source      string     `json:"source"` // publisher name, e.g. "Digi Sport"
	Summary     string     `json:"summary"`
	Image       string     `json:"image"` // lead image (og:image), best-effort; "" when unknown
	PublishedAt *time.Time `json:"publishedAt"`
}
