package aitips

import (
	"context"
	"time"

	"github.com/oddvice/api/internal/tips"
)

// Provider implements tips.Provider over the generated-tips store, falling back
// to another provider (the mock) for matches not yet generated.
type Provider struct {
	store    *Store
	fallback tips.Provider
	ctx      context.Context
}

// NewProvider builds a Provider. ctx is the server lifetime context (used for
// the quick DB lookups, since tips.Provider.ForMatch carries no context).
func NewProvider(ctx context.Context, store *Store, fallback tips.Provider) *Provider {
	return &Provider{store: store, fallback: fallback, ctx: ctx}
}

// ForMatch returns a generated bundle if present, else the fallback's.
func (p *Provider) ForMatch(in tips.GenInput) (tips.MatchTips, bool) {
	if p.store != nil {
		cctx, cancel := context.WithTimeout(p.ctx, 5*time.Second)
		b, ok, _ := p.store.Get(cctx, in.MatchID)
		cancel()
		if ok && len(b.Tips) > 0 {
			return b.ToMatchTips(), true
		}
	}
	if p.fallback != nil {
		return p.fallback.ForMatch(in)
	}
	return tips.MatchTips{}, false
}
