// Package tips defines betting-advice models and a provider abstraction.
//
// Tips are AI-generated analysis for a match. The structure mirrors the
// freemium product: each match gets up to 3 tips — one free "safe" pick and
// two premium picks ("value" and "bold"). For now a mock provider supplies
// deterministic placeholder data; later a Claude-backed provider (generated
// once per match and persisted) will drop in behind the same interface.
package tips

import "time"

// Tier gates a tip behind the free or premium plan.
type Tier string

const (
	TierFree    Tier = "free"
	TierPremium Tier = "premium"
)

// Risk classifies a pick's risk/reward profile.
type Risk string

const (
	RiskSafe  Risk = "safe"  // lower odds, higher probability (the free pick)
	RiskValue Risk = "value" // best risk/reward edge (premium)
	RiskBold  Risk = "bold"  // higher odds, higher risk (premium)
)

// Tip is a single betting recommendation for a match.
type Tip struct {
	ID          string   `json:"id"`
	Tier        Tier     `json:"tier"`
	Risk        Risk     `json:"risk"`
	Market      string   `json:"market"`      // e.g. "1X2", "Over/Under 2.5", "BTTS"
	Selection   string   `json:"selection"`   // e.g. "Home win", "Over 2.5"
	Odds        float64  `json:"odds"`        // decimal odds
	Confidence  int      `json:"confidence"`  // 0-100, calibrated
	ShortReason string   `json:"shortReason"` // free: one line
	Analysis    string   `json:"analysis,omitempty"`    // premium: 2-4 sentences
	KeyFactors  []string `json:"keyFactors,omitempty"`  // premium: bullet points
	StakeUnits  int      `json:"stakeUnits,omitempty"`  // premium: 1-5 units
}

// MatchTips is the full analysis bundle for one match.
type MatchTips struct {
	MatchID           string    `json:"matchId"`
	HomeTeam          string    `json:"homeTeam"`
	AwayTeam          string    `json:"awayTeam"`
	League            string    `json:"league"`
	MatchPreview      string    `json:"matchPreview"`
	OverallConfidence int       `json:"overallConfidence"`
	Tips              []Tip     `json:"tips"`
	Source            string    `json:"source"` // "mock" now, "claude" later
	GeneratedAt       time.Time `json:"generatedAt"`
}

// GenInput carries the match facts a provider needs to produce (or look up)
// tips. The mock provider uses the names to render realistic copy; a future
// DB-backed provider can ignore them and read its persisted row by MatchID.
type GenInput struct {
	MatchID  string
	HomeTeam string
	AwayTeam string
	League   string
}

// Provider supplies tips for matches. The mock provider implements it today;
// a Claude/DB-backed provider implements it later without touching callers.
type Provider interface {
	// ForMatch returns the tips bundle for a single match (found=false if none).
	ForMatch(in GenInput) (MatchTips, bool)
}
