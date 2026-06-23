// Package highlights finds YouTube highlight videos for a match via the YouTube
// Data API (server-side, so the key is never exposed to clients) and caches the
// results. Disabled (returns empty) when no API key is configured.
package highlights

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

// Video is a single highlight result.
type Video struct {
	ID    string `json:"id"`
	Title string `json:"title"`
	Thumb string `json:"thumb"`
}

const cacheTTL = 12 * time.Hour

// Client searches YouTube and caches per match.
type Client struct {
	key  string
	http *http.Client

	mu    sync.Mutex
	cache map[string]entry
}

type entry struct {
	vids []Video
	at   time.Time
}

// New builds a Client. An empty key disables the feature.
func New(key string, hc *http.Client) *Client {
	if hc == nil {
		hc = &http.Client{Timeout: 10 * time.Second}
	}
	return &Client{key: key, http: hc, cache: make(map[string]entry)}
}

// Enabled reports whether a key is configured.
func (c *Client) Enabled() bool { return c != nil && c.key != "" }

// Search returns up to ~6 embeddable highlight videos for the fixture.
func (c *Client) Search(ctx context.Context, home, away string) ([]Video, error) {
	if !c.Enabled() {
		return []Video{}, nil
	}
	cacheKey := strings.ToLower(home + "|" + away)
	c.mu.Lock()
	if e, ok := c.cache[cacheKey]; ok && time.Since(e.at) < cacheTTL {
		v := e.vids
		c.mu.Unlock()
		return v, nil
	}
	c.mu.Unlock()

	q := fmt.Sprintf("%s vs %s World Cup 2026 highlights", home, away)
	params := url.Values{}
	params.Set("part", "snippet")
	params.Set("q", q)
	params.Set("type", "video")
	params.Set("maxResults", "6")
	params.Set("order", "relevance")
	params.Set("videoEmbeddable", "true")
	params.Set("safeSearch", "none")
	params.Set("key", c.key)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet,
		"https://www.googleapis.com/youtube/v3/search?"+params.Encode(), nil)
	if err != nil {
		return nil, err
	}
	res, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("youtube api: %d", res.StatusCode)
	}

	var body struct {
		Items []struct {
			ID struct {
				VideoID string `json:"videoId"`
			} `json:"id"`
			Snippet struct {
				Title      string `json:"title"`
				Thumbnails struct {
					Medium struct {
						URL string `json:"url"`
					} `json:"medium"`
				} `json:"thumbnails"`
			} `json:"snippet"`
		} `json:"items"`
	}
	if err := json.NewDecoder(res.Body).Decode(&body); err != nil {
		return nil, err
	}

	vids := make([]Video, 0, len(body.Items))
	for _, it := range body.Items {
		if it.ID.VideoID == "" {
			continue
		}
		vids = append(vids, Video{ID: it.ID.VideoID, Title: it.Snippet.Title, Thumb: it.Snippet.Thumbnails.Medium.URL})
	}

	c.mu.Lock()
	c.cache[cacheKey] = entry{vids: vids, at: time.Now()}
	c.mu.Unlock()
	return vids, nil
}
