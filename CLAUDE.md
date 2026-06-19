# Oddvice API — context for Claude

This is the **Go backend** for **Oddvice**, a sports-betting **advice** app (not a
bookmaker) focused on the **2026 FIFA World Cup**. It is consumed by two clients
that live in separate projects: a **Next.js 16 PWA** (`web/`) and a **Flutter**
app (`mobile/`). This repo is the API only.

Positioning matters: advice/analysis only, **18+ / responsible gambling**, no
pirated streams, no copyrighted/likeness imagery. Keep that framing.

## Run / build / test
```bash
go build -o bin/server ./cmd/server
./bin/server                 # reads .env from cwd (and next to the binary)
go test ./... && go vet ./...
```
Requires Go 1.22+ (uses `net/http.ServeMux` method+wildcard patterns). Deploy:
see `deploy/README.md` (Ubuntu + systemd + Caddy, binds 127.0.0.1:8080 behind HTTPS).

## Architecture
Stdlib HTTP, layered, provider interfaces so data sources swap without touching
callers. `internal/server` wires everything in `registerFeatures`.

- `cmd/server/main.go` — entry; tiny `.env` loader (cwd + exe dir, no override), graceful shutdown.
- `internal/config` — env-driven `Config` (Football, Teams/APIFootball, News).
- `internal/httpx` — JSON/error response helpers.
- `internal/server` — routes, middleware (request-id, logging, recover, **CORS allow-list**).
- `internal/football` — `Match`/`Group` model, `Service`, `Handler`, `Provider`.
  - `footballdata/` — football-data.org provider (matches, standings). Free tier
    ~10 req/min: caches the full match list, serves stale on 429, honors throttle headers.
- `internal/teams` — `Team`/`Stats`/`Detail`/`Lineup` model, `Service`, `Handler`, `Provider`, **`Normalize`/`NameMatches`** (reconciles provider spelling: USA↔United States, Türkiye↔Turkey, Czechia↔Czech Republic, Korea Republic↔South Korea, etc.).
  - `apifootball/` — api-football.com provider: team statistics (form, formation,
    goals, clean sheets, cards), and **lineups** (resolves the fixture by team
    names + date, then `/fixtures/lineups`). Cached; backs off on 429.
- `internal/tips` — betting tips. **`MockProvider` (deterministic placeholder) — NOT real yet.** `Provider` interface is ready for a Claude/DB-backed impl.
- `internal/news` — `Article` model, `Service`, `Handler`, `Provider`.
  - `googlenews/` — Google News RSS, **localized per request language** (en/ro/de/fr/es/it/nl/pl/cs), cached per lang.

## Endpoints
- `GET /healthz`, `/readyz`, `/api/v1/version`, `/api/v1/ping`
- `GET /api/v1/football/matches` · `/matches/search?q=` · `/matches/upcoming` · `/matches/results` · `/matches/{id}` · `/standings`
- `GET /api/v1/tips` (today's matchday only, mock) · `/api/v1/tips/{matchId}`
- `GET /api/v1/teams` · `/api/v1/teams/by-name?name=` · `/api/v1/teams/{id}` · `/api/v1/lineups?home=&away=&date=YYYY-MM-DD`
- `GET /api/v1/news?lang=xx` · `/api/v1/news/{id}?lang=xx`

## Config (.env — gitignored; see .env.example)
`FOOTBALL_DATA_API_KEY`, `APIFOOTBALL_API_KEY`, `APIFOOTBALL_SEASON` (2026 — needs
api-football **Pro**; free covers 2022–2024), `CORS_ALLOWED_ORIGINS` (exact web
origins), `HOST`/`PORT`. **Never commit real keys.**

## Current state & likely next steps
- **Tips are mock** (`tips.MockProvider`): 3 picks/match — 1 free "safe" + 2
  premium ("value", "bold"). The real plan: generate analysis **once per match
  via `claude -p` on the VPS** (Claude Code CLI, not the paid API), parse the JSON
  defensively, persist it, and serve from storage. Implement a new `tips.Provider`
  (e.g. DB-backed) — don't change handlers/clients.
- **Supabase**: auth (Google/Apple) + a Postgres DB for tips/premium entitlement
  is planned but not built here. Premium gating is currently client-side demo only.
- **Lineups/stats** are live on api-football Pro (season 2026). The pitch/web shows
  real players when a lineup exists (~1h pre-kickoff), else a formation schematic.

## Conventions
- Match existing style; keep providers behind interfaces.
- Don't log or commit secrets. Don't break the advice-only / 18+ framing.
- Git author for this repo: **Scroti**.
