// Package recap generates a short post-match AI recap (via the Claude CLI) for
// finished matches, in all supported languages, persists them in Postgres, and
// serves them for the feed. Mirrors the commentary package. Degrades to "off"
// when DATABASE_URL is unset or the CLI is unavailable.
package recap

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// Recap is a finished match's recap in one language.
type Recap struct {
	MatchID   string `json:"matchId"`
	Home      string `json:"home"`
	Away      string `json:"away"`
	HomeScore int    `json:"homeScore"`
	AwayScore int    `json:"awayScore"`
	HomeBadge string `json:"homeBadge"`
	AwayBadge string `json:"awayBadge"`
	League    string `json:"league"`
	Body      string `json:"body"`
}

// Store persists recaps in Postgres.
type Store struct {
	pool *pgxpool.Pool
}

// NewStore opens a pool and ensures the table exists. (nil, nil) when url empty.
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
	s := &Store{pool: pool}
	if err := s.migrate(cctx); err != nil {
		pool.Close()
		return nil, err
	}
	return s, nil
}

func (s *Store) migrate(ctx context.Context) error {
	if _, err := s.pool.Exec(ctx, `CREATE TABLE IF NOT EXISTS recaps (
		match_id   text NOT NULL,
		lang       text NOT NULL,
		home       text NOT NULL,
		away       text NOT NULL,
		home_score integer NOT NULL,
		away_score integer NOT NULL,
		league     text NOT NULL DEFAULT '',
		body       text NOT NULL,
		created_at timestamptz NOT NULL DEFAULT now(),
		PRIMARY KEY (match_id, lang)
	)`); err != nil {
		return err
	}
	if _, err := s.pool.Exec(ctx, `ALTER TABLE recaps ADD COLUMN IF NOT EXISTS home_badge text NOT NULL DEFAULT ''`); err != nil {
		return err
	}
	_, err := s.pool.Exec(ctx, `ALTER TABLE recaps ADD COLUMN IF NOT EXISTS away_badge text NOT NULL DEFAULT ''`)
	return err
}

// Put upserts a recap for (matchID, lang).
func (s *Store) Put(ctx context.Context, r Recap, lang string) error {
	if s == nil || s.pool == nil {
		return nil
	}
	_, err := s.pool.Exec(ctx,
		`INSERT INTO recaps (match_id, lang, home, away, home_score, away_score, league, body, home_badge, away_badge)
		 VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)
		 ON CONFLICT (match_id, lang) DO UPDATE SET body=EXCLUDED.body, home_badge=EXCLUDED.home_badge, away_badge=EXCLUDED.away_badge`,
		r.MatchID, lang, r.Home, r.Away, r.HomeScore, r.AwayScore, r.League, r.Body, r.HomeBadge, r.AwayBadge)
	return err
}

// Recent returns the latest recaps in the given language.
func (s *Store) Recent(ctx context.Context, lang string, limit int) ([]Recap, error) {
	out := []Recap{}
	if s == nil || s.pool == nil {
		return out, nil
	}
	rows, err := s.pool.Query(ctx,
		`SELECT match_id, home, away, home_score, away_score, league, body, home_badge, away_badge
		 FROM recaps WHERE lang=$1 ORDER BY created_at DESC LIMIT $2`, lang, limit)
	if err != nil {
		return out, err
	}
	defer rows.Close()
	for rows.Next() {
		var r Recap
		if err := rows.Scan(&r.MatchID, &r.Home, &r.Away, &r.HomeScore, &r.AwayScore, &r.League, &r.Body, &r.HomeBadge, &r.AwayBadge); err != nil {
			return out, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// HaveMatchIDs returns the set of match ids that already have a recap.
func (s *Store) HaveMatchIDs(ctx context.Context, ids []string) (map[string]bool, error) {
	have := map[string]bool{}
	if s == nil || s.pool == nil || len(ids) == 0 {
		return have, nil
	}
	rows, err := s.pool.Query(ctx, `SELECT DISTINCT match_id FROM recaps WHERE match_id = ANY($1)`, ids)
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

// Close releases the pool.
func (s *Store) Close() {
	if s != nil && s.pool != nil {
		s.pool.Close()
	}
}
