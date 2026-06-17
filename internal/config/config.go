// Package config loads runtime configuration from environment variables.
package config

import (
	"os"
	"strconv"
	"strings"
	"time"
)

// Config holds the server's runtime configuration.
type Config struct {
	Env            string   // "development" | "production"
	Host           string   // bind host, e.g. "0.0.0.0"
	Port           string   // bind port, e.g. "8080"
	AllowedOrigins []string // CORS allow-list; ["*"] allows any origin
	Football       Football // football data provider settings
	News           News     // news feed provider settings
}

// Football configures the football-data.org provider.
type Football struct {
	APIKey      string        // football-data.org API token
	BaseURL     string        // API base URL
	Competition string        // competition code, e.g. "WC" (FIFA World Cup)
	Timeout     time.Duration // per-request timeout
	CacheTTL    time.Duration // how long to cache the match list (rate limits)
}

// News configures the external news feed provider.
type News struct {
	FeedURL string        // RSS feed URL
	Limit   int           // max articles returned (0 = no cap)
	Timeout time.Duration // per-request timeout for upstream calls
}

// defaultNewsFeed is a free, keyless Google News RSS search for World Cup 2026.
const defaultNewsFeed = "https://news.google.com/rss/search?q=Cupa%20Mondiala%202026&hl=ro&gl=RO&ceid=RO:ro"

// Load reads configuration from the environment, applying sensible defaults.
func Load() Config {
	return Config{
		Env:            getenv("APP_ENV", "development"),
		Host:           getenv("HOST", "0.0.0.0"),
		Port:           getenv("PORT", "8080"),
		AllowedOrigins: splitAndTrim(getenv("CORS_ALLOWED_ORIGINS", "http://localhost:3000")),
		Football: Football{
			APIKey:      getenv("FOOTBALL_DATA_API_KEY", ""),
			BaseURL:     getenv("FOOTBALL_DATA_BASE_URL", "https://api.football-data.org"),
			Competition: getenv("FOOTBALL_COMPETITION", "WC"),
			Timeout:     getduration("FOOTBALL_TIMEOUT_SECONDS", 12*time.Second),
			CacheTTL:    getduration("FOOTBALL_CACHE_SECONDS", 120*time.Second),
		},
		News: News{
			FeedURL: getenv("NEWS_FEED_URL", defaultNewsFeed),
			Limit:   getint("NEWS_LIMIT", 30),
			Timeout: getduration("NEWS_TIMEOUT_SECONDS", 10*time.Second),
		},
	}
}

// Addr returns the host:port string for http.Server.
func (c Config) Addr() string {
	return c.Host + ":" + c.Port
}

// IsProduction reports whether the app runs in production mode.
func (c Config) IsProduction() bool {
	return c.Env == "production"
}

func getenv(key, fallback string) string {
	if v, ok := os.LookupEnv(key); ok && v != "" {
		return v
	}
	return fallback
}

func getduration(key string, fallback time.Duration) time.Duration {
	if v, ok := os.LookupEnv(key); ok && v != "" {
		if secs, err := strconv.Atoi(v); err == nil && secs > 0 {
			return time.Duration(secs) * time.Second
		}
	}
	return fallback
}

func getint(key string, fallback int) int {
	if v, ok := os.LookupEnv(key); ok && v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return fallback
}

func splitAndTrim(s string) []string {
	parts := strings.Split(s, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}
