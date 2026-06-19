// Command server starts the Oddvice HTTP API.
package main

import (
	"bufio"
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/oddvice/api/internal/config"
	"github.com/oddvice/api/internal/server"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	}))
	slog.SetDefault(logger)

	// Load .env from the working directory, and also from next to the binary
	// and its parent (so `bin\server.exe` finds `api\.env` regardless of cwd).
	// loadDotenv never overrides already-set vars, so the first hit per key wins.
	loadDotenv(".env")
	if exe, err := os.Executable(); err == nil {
		exeDir := filepath.Dir(exe)
		loadDotenv(filepath.Join(exeDir, ".env"))
		loadDotenv(filepath.Join(filepath.Dir(exeDir), ".env"))
	}
	cfg := config.Load()

	srv := &http.Server{
		Addr:              cfg.Addr(),
		Handler:           server.New(cfg, logger),
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      15 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	// Run the server in the background so we can wait for a shutdown signal.
	serverErr := make(chan error, 1)
	go func() {
		logger.Info("server starting", "addr", srv.Addr, "env", cfg.Env)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			serverErr <- err
		}
	}()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	select {
	case err := <-serverErr:
		logger.Error("server failed", "error", err)
		os.Exit(1)
	case <-ctx.Done():
		logger.Info("shutdown signal received")
	}

	// Graceful shutdown with a bounded timeout.
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		logger.Error("graceful shutdown failed", "error", err)
		os.Exit(1)
	}
	logger.Info("server stopped")
}

// loadDotenv loads simple KEY=VALUE lines from a .env file into the environment
// (without overriding already-set vars). Missing file is ignored.
func loadDotenv(path string) {
	f, err := os.Open(path)
	if err != nil {
		return
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, val, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		key, val = strings.TrimSpace(key), strings.Trim(strings.TrimSpace(val), `"'`)
		if _, exists := os.LookupEnv(key); !exists {
			_ = os.Setenv(key, val)
		}
	}
}
