// Package aitips is a Claude-backed, self-improving tips provider. It generates
// betting-advice bundles per upcoming match, persists them, grades each pick
// against the final score, and feeds the running track record (hits/misses)
// back into the generation prompt so the model calibrates from its own
// mistakes. Implements tips.Provider; falls back to the mock when empty.
package aitips

import (
	"context"
	"encoding/json"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/oddvice/api/internal/tips"
)

// genTip is a generated pick with a machine-gradeable key (kept out of the
// public tips.Tip, which is what clients see).
type genTip struct {
	Tier        string   `json:"tier"`
	Risk        string   `json:"risk"`
	Market      string   `json:"market"`
	Selection   string   `json:"selection"`
	GradeKey    string   `json:"gradeKey"`
	Odds        float64  `json:"odds"`
	Confidence  int      `json:"confidence"`
	ShortReason string   `json:"shortReason"`
	Analysis    string   `json:"analysis,omitempty"`
	KeyFactors  []string `json:"keyFactors,omitempty"`
	StakeUnits  int      `json:"stakeUnits,omitempty"`
}

// Bundle is a generated match-tips bundle (with grade keys), persisted as JSON.
type Bundle struct {
	MatchID           string    `json:"matchId"`
	HomeTeam          string    `json:"homeTeam"`
	AwayTeam          string    `json:"awayTeam"`
	League            string    `json:"league"`
	MatchPreview      string    `json:"matchPreview"`
	OverallConfidence int       `json:"overallConfidence"`
	Tips              []genTip  `json:"tips"`
	GeneratedAt       time.Time `json:"generatedAt"`
}

// ToMatchTips maps to the public tips bundle clients receive.
func (b Bundle) ToMatchTips() tips.MatchTips {
	out := tips.MatchTips{
		MatchID:           b.MatchID,
		HomeTeam:          b.HomeTeam,
		AwayTeam:          b.AwayTeam,
		League:            b.League,
		MatchPreview:      b.MatchPreview,
		OverallConfidence: b.OverallConfidence,
		Source:            "claude",
		GeneratedAt:       b.GeneratedAt,
	}
	for i, t := range b.Tips {
		out.Tips = append(out.Tips, tips.Tip{
			ID:          b.MatchID + "-" + t.Risk,
			Tier:        tips.Tier(t.Tier),
			Risk:        tips.Risk(t.Risk),
			Market:      t.Market,
			Selection:   t.Selection,
			Odds:        t.Odds,
			Confidence:  t.Confidence,
			ShortReason: t.ShortReason,
			Analysis:    t.Analysis,
			KeyFactors:  t.KeyFactors,
			StakeUnits:  t.StakeUnits,
		})
		_ = i
	}
	return out
}

// Store persists generated bundles + their grades in Postgres.
type Store struct {
	pool *pgxpool.Pool
}

// NewStore opens a pool + migrates. (nil,nil) when url empty.
func NewStore(ctx context.Context, url string) (*Store, error) {
	if url == "" {
		return nil, nil
	}
	cctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	pool, err := pgxpool.New(cctx, url)
	if err != nil {
		return nil, err
	}
	if err := pool.Ping(cctx); err != nil {
		pool.Close()
		return nil, err
	}
	if _, err := pool.Exec(cctx, `CREATE TABLE IF NOT EXISTS ai_tips (
		match_id     text PRIMARY KEY,
		payload      jsonb NOT NULL,
		kickoff      timestamptz,
		graded       boolean NOT NULL DEFAULT false,
		hits         integer NOT NULL DEFAULT 0,
		total        integer NOT NULL DEFAULT 0,
		generated_at timestamptz NOT NULL DEFAULT now()
	)`); err != nil {
		pool.Close()
		return nil, err
	}
	return &Store{pool: pool}, nil
}

// Get returns a stored bundle for a match.
func (s *Store) Get(ctx context.Context, matchID string) (Bundle, bool, error) {
	var b Bundle
	if s == nil || s.pool == nil {
		return b, false, nil
	}
	var raw []byte
	err := s.pool.QueryRow(ctx, `SELECT payload FROM ai_tips WHERE match_id=$1`, matchID).Scan(&raw)
	if err != nil {
		return b, false, nil // not found / error → treat as absent
	}
	if err := json.Unmarshal(raw, &b); err != nil {
		return b, false, nil
	}
	return b, true, nil
}

// Put upserts a generated bundle.
func (s *Store) Put(ctx context.Context, b Bundle, kickoff time.Time) error {
	if s == nil || s.pool == nil {
		return nil
	}
	raw, err := json.Marshal(b)
	if err != nil {
		return err
	}
	_, err = s.pool.Exec(ctx,
		`INSERT INTO ai_tips (match_id, payload, kickoff) VALUES ($1,$2,$3)
		 ON CONFLICT (match_id) DO UPDATE SET payload=EXCLUDED.payload, kickoff=EXCLUDED.kickoff`,
		b.MatchID, raw, kickoff)
	return err
}

// HaveIDs returns the set of match ids that already have a bundle.
func (s *Store) HaveIDs(ctx context.Context, ids []string) (map[string]bool, error) {
	have := map[string]bool{}
	if s == nil || s.pool == nil || len(ids) == 0 {
		return have, nil
	}
	rows, err := s.pool.Query(ctx, `SELECT match_id FROM ai_tips WHERE match_id = ANY($1)`, ids)
	if err != nil {
		return have, err
	}
	defer rows.Close()
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return have, err
		}
		have[id] = true
	}
	return have, rows.Err()
}

// Ungraded returns stored, not-yet-graded bundles (for grading vs results).
func (s *Store) Ungraded(ctx context.Context) ([]Bundle, error) {
	out := []Bundle{}
	if s == nil || s.pool == nil {
		return out, nil
	}
	rows, err := s.pool.Query(ctx, `SELECT payload FROM ai_tips WHERE graded=false`)
	if err != nil {
		return out, err
	}
	defer rows.Close()
	for rows.Next() {
		var raw []byte
		if err := rows.Scan(&raw); err != nil {
			return out, err
		}
		var b Bundle
		if json.Unmarshal(raw, &b) == nil {
			out = append(out, b)
		}
	}
	return out, rows.Err()
}

// MarkGraded records the grade for a bundle.
func (s *Store) MarkGraded(ctx context.Context, matchID string, hits, total int) error {
	if s == nil || s.pool == nil {
		return nil
	}
	_, err := s.pool.Exec(ctx, `UPDATE ai_tips SET graded=true, hits=$2, total=$3 WHERE match_id=$1`, matchID, hits, total)
	return err
}

// Record is the model's running track record, fed back into generation.
type Record struct {
	Hits   int
	Total  int
	Misses []string // short descriptions of recent missed picks
}

// TrackRecord aggregates graded results + a few recent misses.
func (s *Store) TrackRecord(ctx context.Context) (Record, error) {
	var rec Record
	if s == nil || s.pool == nil {
		return rec, nil
	}
	_ = s.pool.QueryRow(ctx,
		`SELECT coalesce(sum(hits),0), coalesce(sum(total),0) FROM ai_tips WHERE graded`).Scan(&rec.Hits, &rec.Total)

	// Recent misses: look at the latest graded bundles and list picks that lost.
	rows, err := s.pool.Query(ctx,
		`SELECT payload, hits, total FROM ai_tips WHERE graded AND total>0 ORDER BY kickoff DESC NULLS LAST LIMIT 8`)
	if err != nil {
		return rec, nil
	}
	defer rows.Close()
	for rows.Next() {
		var raw []byte
		var hits, total int
		if rows.Scan(&raw, &hits, &total) != nil {
			continue
		}
		if hits >= total {
			continue // all hit — nothing to learn here
		}
		var b Bundle
		if json.Unmarshal(raw, &b) != nil {
			continue
		}
		for _, t := range b.Tips {
			if t.GradeKey == "" {
				continue
			}
			rec.Misses = append(rec.Misses, b.HomeTeam+" vs "+b.AwayTeam+": "+t.Selection+" ("+t.Market+")")
			if len(rec.Misses) >= 8 {
				return rec, nil
			}
		}
	}
	return rec, nil
}

// Close releases the pool.
func (s *Store) Close() {
	if s != nil && s.pool != nil {
		s.pool.Close()
	}
}
