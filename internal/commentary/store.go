package commentary

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Store persists generated commentary lines in Postgres so they are produced
// once (in all languages) and served instantly thereafter. It is optional: a
// nil *Store behaves as "no persistence" and every method is a safe no-op.
type Store struct {
	pool *pgxpool.Pool
}

// NewStore opens a connection pool to the given DATABASE_URL. When url is empty
// it returns (nil, nil) so the caller can run without persistence. A non-nil
// error means the URL was set but unusable.
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
	return &Store{pool: pool}, nil
}

// GetMany returns the cached commentary bodies for the given event signatures
// in one language, keyed by signature. Missing signatures are simply absent.
func (s *Store) GetMany(ctx context.Context, fixtureID int, lang string, sigs []string) (map[string]string, error) {
	out := make(map[string]string, len(sigs))
	if s == nil || s.pool == nil || len(sigs) == 0 {
		return out, nil
	}
	rows, err := s.pool.Query(ctx,
		`SELECT event_sig, body FROM commentary WHERE fixture_id=$1 AND lang=$2 AND event_sig = ANY($3)`,
		fixtureID, lang, sigs)
	if err != nil {
		return out, err
	}
	defer rows.Close()
	for rows.Next() {
		var sig, body string
		if err := rows.Scan(&sig, &body); err != nil {
			return out, err
		}
		out[sig] = body
	}
	return out, rows.Err()
}

// PutAll upserts every language variant of one event's commentary in a single
// batch. byLang maps language code -> commentary line.
func (s *Store) PutAll(ctx context.Context, fixtureID int, sig string, byLang map[string]string) error {
	if s == nil || s.pool == nil || len(byLang) == 0 {
		return nil
	}
	batch := &pgx.Batch{}
	for lang, body := range byLang {
		batch.Queue(
			`INSERT INTO commentary (fixture_id, event_sig, lang, body) VALUES ($1,$2,$3,$4)
			 ON CONFLICT (fixture_id, event_sig, lang) DO UPDATE SET body = EXCLUDED.body`,
			fixtureID, sig, lang, body)
	}
	br := s.pool.SendBatch(ctx, batch)
	defer br.Close()
	for range byLang {
		if _, err := br.Exec(); err != nil {
			return err
		}
	}
	return nil
}

// Close releases the connection pool.
func (s *Store) Close() {
	if s != nil && s.pool != nil {
		s.pool.Close()
	}
}
