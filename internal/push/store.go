// Package push implements Web Push subscription storage, sending, and HTTP handlers.
package push

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"

	webpush "github.com/SherClockHolmes/webpush-go"
)

// Store is a mutex-guarded, file-backed set of push subscriptions deduplicated
// by endpoint.
type Store struct {
	mu   sync.Mutex
	path string
	subs []webpush.Subscription
}

// NewStore loads (or initialises) the subscription list at path, creating any
// missing parent directories. A missing or empty file is not an error.
func NewStore(path string) (*Store, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, err
	}

	s := &Store{path: path}

	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return s, nil
		}
		return nil, err
	}
	if len(data) == 0 {
		return s, nil
	}
	if err := json.Unmarshal(data, &s.subs); err != nil {
		// Corrupted file — start fresh rather than crashing.
		s.subs = nil
	}
	return s, nil
}

// Add appends sub if its endpoint is not already stored, then persists.
func (s *Store) Add(sub webpush.Subscription) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, existing := range s.subs {
		if existing.Endpoint == sub.Endpoint {
			return
		}
	}
	s.subs = append(s.subs, sub)
	_ = s.save()
}

// Remove deletes the subscription with the given endpoint and persists.
func (s *Store) Remove(endpoint string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	n := 0
	for _, sub := range s.subs {
		if sub.Endpoint != endpoint {
			s.subs[n] = sub
			n++
		}
	}
	s.subs = s.subs[:n]
	_ = s.save()
}

// All returns a shallow copy of all stored subscriptions.
func (s *Store) All() []webpush.Subscription {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]webpush.Subscription, len(s.subs))
	copy(out, s.subs)
	return out
}

// save writes the current list to disk; caller must hold s.mu.
func (s *Store) save() error {
	data, err := json.MarshalIndent(s.subs, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(s.path, data, 0o644)
}
