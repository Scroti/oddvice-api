package tips

import (
	"fmt"
	"hash/fnv"
	"time"
)

// MockProvider produces deterministic placeholder tips. The same match always
// yields the same picks (seeded from its id), so the UI is stable across
// refreshes. Swap this for a Claude/DB provider later — callers don't change.
type MockProvider struct{}

// NewMockProvider builds a MockProvider.
func NewMockProvider() *MockProvider { return &MockProvider{} }

// seed turns a match id into a stable pseudo-random number.
func seed(id string) uint64 {
	h := fnv.New64a()
	_, _ = h.Write([]byte(id))
	return h.Sum64()
}

// markets the mock rotates through, paired with plausible odds/confidence.
var safePicks = []struct {
	market    string
	selection func(home, away string) string
	odds      float64
	conf      int
}{
	{"Double Chance", func(h, _ string) string { return "1X (" + h + " or draw)" }, 1.45, 68},
	{"Over/Under 2.5", func(_, _ string) string { return "Under 3.5 goals" }, 1.40, 70},
	{"Draw No Bet", func(h, _ string) string { return h + " (DNB)" }, 1.62, 62},
	{"Double Chance", func(_, a string) string { return "X2 (draw or " + a + ")" }, 1.55, 64},
}

var valuePicks = []struct {
	market    string
	selection func(home, away string) string
	odds      float64
	conf      int
}{
	{"1X2", func(h, _ string) string { return h + " win" }, 2.10, 58},
	{"Over/Under 2.5", func(_, _ string) string { return "Over 2.5 goals" }, 1.95, 55},
	{"BTTS", func(_, _ string) string { return "Both teams to score: Yes" }, 1.85, 57},
	{"1X2", func(_, a string) string { return a + " win" }, 2.40, 52},
}

var boldPicks = []struct {
	market    string
	selection func(home, away string) string
	odds      float64
	conf      int
}{
	{"Correct Score", func(_, _ string) string { return "2-1" }, 8.50, 22},
	{"Over/Under 3.5", func(_, _ string) string { return "Over 3.5 goals" }, 3.20, 33},
	{"1X2 & BTTS", func(h, _ string) string { return h + " win & BTTS" }, 4.10, 28},
	{"Anytime Scorer", func(_, _ string) string { return "Top striker to score" }, 2.75, 38},
}

// ForMatch returns deterministic mock tips for the match.
func (p *MockProvider) ForMatch(in GenInput) (MatchTips, bool) {
	if in.MatchID == "" {
		return MatchTips{}, false
	}
	home, away := in.HomeTeam, in.AwayTeam
	if home == "" {
		home = "Home"
	}
	if away == "" {
		away = "Away"
	}
	s := seed(in.MatchID)

	safe := safePicks[s%uint64(len(safePicks))]
	value := valuePicks[(s>>8)%uint64(len(valuePicks))]
	bold := boldPicks[(s>>16)%uint64(len(boldPicks))]

	tips := []Tip{
		{
			ID:          in.MatchID + "-safe",
			Tier:        TierFree,
			Risk:        RiskSafe,
			Market:      safe.market,
			Selection:   safe.selection(home, away),
			Odds:        safe.odds,
			Confidence:  safe.conf,
			ShortReason: fmt.Sprintf("Lowest-risk angle on %s vs %s — the model's safest read of this fixture.", home, away),
		},
		{
			ID:         in.MatchID + "-value",
			Tier:       TierPremium,
			Risk:       RiskValue,
			Market:     value.market,
			Selection:  value.selection(home, away),
			Odds:       value.odds,
			Confidence: value.conf,
			Analysis:   fmt.Sprintf("The market under-rates this outcome given recent form and the matchup profile. At %.2f the implied probability sits below our estimate, leaving positive expected value on %s.", value.odds, value.selection(home, away)),
			KeyFactors: []string{
				"Recent form favours this selection",
				"Odds imply a lower probability than our model",
				"Head-to-head trend supports the pick",
			},
			StakeUnits: 2,
		},
		{
			ID:         in.MatchID + "-bold",
			Tier:       TierPremium,
			Risk:       RiskBold,
			Market:     bold.market,
			Selection:  bold.selection(home, away),
			Odds:       bold.odds,
			Confidence: bold.conf,
			Analysis:   fmt.Sprintf("A high-variance angle for a small stake. Low hit-rate by design, but the %.2f price more than compensates if %s lands.", bold.odds, bold.selection(home, away)),
			KeyFactors: []string{
				"High odds, high reward",
				"Small recommended stake",
				"Only for value-seeking bankrolls",
			},
			StakeUnits: 1,
		},
	}

	return MatchTips{
		MatchID:           in.MatchID,
		HomeTeam:          home,
		AwayTeam:          away,
		League:            in.League,
		MatchPreview:      fmt.Sprintf("%s face %s. Below are three angles on the fixture: one safe pick (free) and two deeper, higher-value picks (premium).", home, away),
		OverallConfidence: safe.conf,
		Tips:              tips,
		Source:            "mock",
		GeneratedAt:       time.Now().UTC(),
	}, true
}
