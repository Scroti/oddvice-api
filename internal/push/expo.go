package push

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// ExpoStore persists Expo push tokens (for the native apps) in Postgres.
// A nil *ExpoStore is a safe no-op (feature off without DATABASE_URL).
type ExpoStore struct {
	pool *pgxpool.Pool
}

// NewExpoStore opens a pool and ensures the table exists. (nil,nil) if url empty.
func NewExpoStore(ctx context.Context, url string) (*ExpoStore, error) {
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
	if _, err := pool.Exec(cctx,
		`CREATE TABLE IF NOT EXISTS expo_tokens (token text PRIMARY KEY, created_at timestamptz NOT NULL DEFAULT now())`); err != nil {
		pool.Close()
		return nil, err
	}
	return &ExpoStore{pool: pool}, nil
}

// Add records a token (idempotent).
func (s *ExpoStore) Add(ctx context.Context, token string) error {
	if s == nil || s.pool == nil {
		return nil
	}
	_, err := s.pool.Exec(ctx, `INSERT INTO expo_tokens (token) VALUES ($1) ON CONFLICT (token) DO NOTHING`, token)
	return err
}

// Remove deletes a token (e.g. when Expo reports it invalid).
func (s *ExpoStore) Remove(ctx context.Context, token string) {
	if s == nil || s.pool == nil {
		return
	}
	_, _ = s.pool.Exec(ctx, `DELETE FROM expo_tokens WHERE token=$1`, token)
}

// All returns every registered token.
func (s *ExpoStore) All(ctx context.Context) ([]string, error) {
	out := []string{}
	if s == nil || s.pool == nil {
		return out, nil
	}
	rows, err := s.pool.Query(ctx, `SELECT token FROM expo_tokens`)
	if err != nil {
		return out, err
	}
	defer rows.Close()
	for rows.Next() {
		var t string
		if err := rows.Scan(&t); err != nil {
			return out, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

type expoMessage struct {
	To        string            `json:"to"`
	Title     string            `json:"title"`
	Body      string            `json:"body"`
	Sound     string            `json:"sound,omitempty"`
	ChannelID string            `json:"channelId,omitempty"`
	Data      map[string]string `json:"data,omitempty"`
}

// SendExpo delivers a notification to the given tokens via the Expo push API
// (which routes to FCM/APNs). Best-effort; errors are ignored per token batch.
func SendExpo(ctx context.Context, hc *http.Client, tokens []string, title, body, url string) {
	if len(tokens) == 0 {
		return
	}
	if hc == nil {
		hc = &http.Client{Timeout: 12 * time.Second}
	}
	for i := 0; i < len(tokens); i += 100 {
		end := i + 100
		if end > len(tokens) {
			end = len(tokens)
		}
		msgs := make([]expoMessage, 0, end-i)
		for _, tok := range tokens[i:end] {
			msgs = append(msgs, expoMessage{
				To: tok, Title: title, Body: body, Sound: "default", ChannelID: "goals",
				Data: map[string]string{"url": url},
			})
		}
		payload, err := json.Marshal(msgs)
		if err != nil {
			continue
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://exp.host/--/api/v2/push/send", bytes.NewReader(payload))
		if err != nil {
			continue
		}
		req.Header.Set("Content-Type", "application/json")
		if res, err := hc.Do(req); err == nil {
			res.Body.Close()
		}
	}
}
