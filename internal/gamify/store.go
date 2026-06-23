// Package gamify implements the prediction game: users (by device id) predict
// the winner of a match, predictions are graded against results into points +
// streaks, the crowd's predictions form a per-match poll, and a leaderboard
// ranks everyone. Postgres-backed; a nil *Store degrades to "off".
package gamify

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// Pick is the predicted outcome.
type Pick = string // "home" | "draw" | "away"

// Prediction is one user's pick for a match.
type Prediction struct {
	MatchID string `json:"matchId"`
	Pick    Pick   `json:"pick"`
	Graded  bool   `json:"graded"`
	Correct bool   `json:"correct"`
	Points  int    `json:"points"`
}

// Poll is the aggregate of everyone's predictions for a match.
type Poll struct {
	Home  int `json:"home"`
	Draw  int `json:"draw"`
	Away  int `json:"away"`
	Total int `json:"total"`
}

// LeaderRow is one ranked player.
type LeaderRow struct {
	Name   string `json:"name"`
	Points int    `json:"points"`
	Correct int   `json:"correct"`
	Played int    `json:"played"`
}

// Store persists predictions in Postgres.
type Store struct {
	pool *pgxpool.Pool
}

// NewStore opens a pool and ensures the table exists. Returns (nil, nil) when
// url is empty so the feature degrades to "off".
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
	_, err := s.pool.Exec(ctx, `CREATE TABLE IF NOT EXISTS predictions (
		device_id  text NOT NULL,
		name       text NOT NULL DEFAULT '',
		match_id   text NOT NULL,
		pick       text NOT NULL,
		graded     boolean NOT NULL DEFAULT false,
		correct    boolean NOT NULL DEFAULT false,
		points     integer NOT NULL DEFAULT 0,
		created_at timestamptz NOT NULL DEFAULT now(),
		PRIMARY KEY (device_id, match_id)
	)`)
	return err
}

// Upsert records/changes a pick (only while the prediction is ungraded).
func (s *Store) Upsert(ctx context.Context, deviceID, name, matchID, pick string) error {
	if s == nil || s.pool == nil {
		return nil
	}
	_, err := s.pool.Exec(ctx,
		`INSERT INTO predictions (device_id, name, match_id, pick)
		 VALUES ($1,$2,$3,$4)
		 ON CONFLICT (device_id, match_id) DO UPDATE SET
		   pick = EXCLUDED.pick,
		   name = EXCLUDED.name
		 WHERE predictions.graded = false`,
		deviceID, name, matchID, pick)
	return err
}

// ByDevice returns a user's predictions plus their points and current streak
// (consecutive correct, most-recent-first).
func (s *Store) ByDevice(ctx context.Context, deviceID string) ([]Prediction, int, int, error) {
	out := []Prediction{}
	if s == nil || s.pool == nil {
		return out, 0, 0, nil
	}
	rows, err := s.pool.Query(ctx,
		`SELECT match_id, pick, graded, correct, points
		 FROM predictions WHERE device_id=$1 ORDER BY created_at DESC`, deviceID)
	if err != nil {
		return out, 0, 0, err
	}
	defer rows.Close()
	points, streak := 0, 0
	streakOpen := true
	for rows.Next() {
		var p Prediction
		if err := rows.Scan(&p.MatchID, &p.Pick, &p.Graded, &p.Correct, &p.Points); err != nil {
			return out, 0, 0, err
		}
		points += p.Points
		if p.Graded {
			if streakOpen && p.Correct {
				streak++
			} else if streakOpen {
				streakOpen = false
			}
		}
		out = append(out, p)
	}
	return out, points, streak, rows.Err()
}

// Poll returns the aggregate pick counts for a match.
func (s *Store) Poll(ctx context.Context, matchID string) (Poll, error) {
	var p Poll
	if s == nil || s.pool == nil {
		return p, nil
	}
	err := s.pool.QueryRow(ctx,
		`SELECT
		   count(*) FILTER (WHERE pick='home'),
		   count(*) FILTER (WHERE pick='draw'),
		   count(*) FILTER (WHERE pick='away'),
		   count(*)
		 FROM predictions WHERE match_id=$1`, matchID).Scan(&p.Home, &p.Draw, &p.Away, &p.Total)
	return p, err
}

// Leaderboard returns the top players by points.
func (s *Store) Leaderboard(ctx context.Context, limit int) ([]LeaderRow, error) {
	out := []LeaderRow{}
	if s == nil || s.pool == nil {
		return out, nil
	}
	rows, err := s.pool.Query(ctx,
		`SELECT
		   coalesce(max(name),'') AS name,
		   sum(points)::int AS points,
		   count(*) FILTER (WHERE correct)::int AS correct,
		   count(*) FILTER (WHERE graded)::int AS played
		 FROM predictions
		 GROUP BY device_id
		 HAVING count(*) FILTER (WHERE graded) > 0
		 ORDER BY points DESC, correct DESC
		 LIMIT $1`, limit)
	if err != nil {
		return out, err
	}
	defer rows.Close()
	for rows.Next() {
		var r LeaderRow
		if err := rows.Scan(&r.Name, &r.Points, &r.Correct, &r.Played); err != nil {
			return out, err
		}
		if r.Name == "" {
			r.Name = "Anonymous"
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// GradeFinished grades all ungraded predictions for the given matches.
// winners maps matchID -> "home"|"draw"|"away". 10 points for a correct pick.
func (s *Store) GradeFinished(ctx context.Context, winners map[string]string) (int, error) {
	if s == nil || s.pool == nil || len(winners) == 0 {
		return 0, nil
	}
	graded := 0
	for matchID, winner := range winners {
		ct, err := s.pool.Exec(ctx,
			`UPDATE predictions
			 SET graded=true,
			     correct=(pick=$2),
			     points=CASE WHEN pick=$2 THEN 10 ELSE 0 END
			 WHERE match_id=$1 AND graded=false`, matchID, winner)
		if err != nil {
			return graded, err
		}
		graded += int(ct.RowsAffected())
	}
	return graded, nil
}

// Close releases the pool.
func (s *Store) Close() {
	if s != nil && s.pool != nil {
		s.pool.Close()
	}
}
