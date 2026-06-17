// Package config loads runtime configuration from environment variables.
package config

import (
	"os"
	"strings"
)

// Config holds the server's runtime configuration.
type Config struct {
	Env            string   // "development" | "production"
	Host           string   // bind host, e.g. "0.0.0.0"
	Port           string   // bind port, e.g. "8080"
	AllowedOrigins []string // CORS allow-list; ["*"] allows any origin
}

// Load reads configuration from the environment, applying sensible defaults.
func Load() Config {
	cfg := Config{
		Env:            getenv("APP_ENV", "development"),
		Host:           getenv("HOST", "0.0.0.0"),
		Port:           getenv("PORT", "8080"),
		AllowedOrigins: splitAndTrim(getenv("CORS_ALLOWED_ORIGINS", "http://localhost:3000")),
	}
	return cfg
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
