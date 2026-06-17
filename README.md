# Oddvice API

Go HTTP API boilerplate. **Standard library only** — no external dependencies.

## Requirements

- Go 1.24+ ([install](https://go.dev/dl/))

## Run

```bash
cp .env.example .env   # optional; defaults work out of the box
go run ./cmd/server    # listens on :8080
```

Or with the Makefile: `make run`, `make test`, `make build`.

## Endpoints

| Method | Path                                  | Description                       |
| ------ | ------------------------------------- | --------------------------------- |
| GET    | `/healthz`                            | Liveness probe                    |
| GET    | `/readyz`                             | Readiness probe                   |
| GET    | `/api/v1/version`                     | Service version + env             |
| GET    | `/api/v1/ping`                        | Returns `{"message":"pong"}`      |
| GET    | `/api/v1/football/matches/search?q=`  | Search football matches by name   |

```bash
curl localhost:8080/healthz
curl "localhost:8080/api/v1/football/matches/search?q=Arsenal%20vs%20Chelsea"
```

The football endpoint proxies **[TheSportsDB](https://www.thesportsdb.com)**'s
free API (public test key `3`, no signup). Swap providers by implementing
`football.Provider` in a new sub-package — the domain model, service, and HTTP
handler stay unchanged.

## Configuration

Environment variables (see `.env.example`):

| Var                    | Default                 | Description                       |
| ---------------------- | ----------------------- | --------------------------------- |
| `APP_ENV`              | `development`           | `development` or `production`     |
| `HOST`                 | `0.0.0.0`               | Bind host                         |
| `PORT`                 | `8080`                  | Bind port                         |
| `CORS_ALLOWED_ORIGINS` | `http://localhost:3000` | Comma-separated list, or `*`      |

## Layout

```
cmd/server/                    entrypoint (graceful shutdown)
internal/config/               env-based configuration
internal/httpx/                shared JSON response helpers
internal/server/               transport: router, middleware, system handlers
internal/football/             feature: domain model + Service + Provider iface
internal/football/thesportsdb/ Provider implementation (free, keyless)
```

Layering: `server` wires `config → provider → service → handler`. The
`football` package owns the domain and a `Provider` interface; concrete
providers (e.g. `thesportsdb`) depend on `football`, never the reverse — so
swapping the data source is a one-package change.

Middleware chain (outermost first): **recover → request-id → logging → CORS**.

## Docker

```bash
docker build -t oddvice-api .
docker run -p 8080:8080 oddvice-api
```
