// Package players provides a Postgres-backed, name-searchable index of World
// Cup players (for the profile-avatar picker). The index is ingested once from
// api-football squads and refreshed daily, so user searches never call the
// upstream provider.
package players

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Player is one indexed footballer.
type Player struct {
	ID          int    `json:"id"`
	Name        string `json:"name"`
	Photo       string `json:"photo,omitempty"`
	Team        string `json:"team,omitempty"`
	Position    string `json:"position,omitempty"`
	Nationality string `json:"nationality,omitempty"`
}

// Store persists the player index in Postgres. A nil *Store is a safe no-op so
// the feature degrades to "off" when DATABASE_URL is unset.
type Store struct {
	pool *pgxpool.Pool
}

// NewStore opens a pool and ensures the table exists. Returns (nil, nil) when
// url is empty so the caller can run without the feature.
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
	_, err := s.pool.Exec(ctx, `CREATE TABLE IF NOT EXISTS players (
		id integer PRIMARY KEY,
		name text NOT NULL,
		photo text,
		team text,
		position text,
		nationality text,
		updated_at timestamptz DEFAULT now()
	)`)
	return err
}

// Count returns how many players are indexed (0 for a nil store).
func (s *Store) Count(ctx context.Context) (int, error) {
	if s == nil || s.pool == nil {
		return 0, nil
	}
	var n int
	if err := s.pool.QueryRow(ctx, `SELECT count(*) FROM players`).Scan(&n); err != nil {
		return 0, err
	}
	return n, nil
}

// Search returns up to limit players whose name matches q (case-insensitive
// substring), ordered by name. Empty slice for a nil store.
func (s *Store) Search(ctx context.Context, q string, limit int) ([]Player, error) {
	out := []Player{}
	if s == nil || s.pool == nil {
		return out, nil
	}
	rows, err := s.pool.Query(ctx,
		`SELECT id, name, coalesce(photo,''), coalesce(team,''), coalesce(position,''), coalesce(nationality,'')
		 FROM players WHERE name ILIKE '%'||$1||'%' ORDER BY name LIMIT $2`, q, limit)
	if err != nil {
		return out, err
	}
	defer rows.Close()
	for rows.Next() {
		var p Player
		if err := rows.Scan(&p.ID, &p.Name, &p.Photo, &p.Team, &p.Position, &p.Nationality); err != nil {
			return out, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// Upsert inserts/updates a batch of players keyed by id.
func (s *Store) Upsert(ctx context.Context, players []Player) error {
	if s == nil || s.pool == nil || len(players) == 0 {
		return nil
	}
	batch := &pgx.Batch{}
	for _, p := range players {
		batch.Queue(
			`INSERT INTO players (id, name, photo, team, position, nationality, updated_at)
			 VALUES ($1,$2,$3,$4,$5,$6, now())
			 ON CONFLICT (id) DO UPDATE SET
			   name=EXCLUDED.name, photo=EXCLUDED.photo, team=EXCLUDED.team,
			   position=EXCLUDED.position, nationality=EXCLUDED.nationality, updated_at=now()`,
			p.ID, p.Name, p.Photo, p.Team, p.Position, p.Nationality)
	}
	br := s.pool.SendBatch(ctx, batch)
	defer br.Close()
	for range players {
		if _, err := br.Exec(); err != nil {
			return err
		}
	}
	return nil
}

// Close releases the pool.
func (s *Store) Close() {
	if s != nil && s.pool != nil {
		s.pool.Close()
	}
}
