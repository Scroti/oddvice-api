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

| Method | Path               | Description              |
| ------ | ------------------ | ------------------------ |
| GET    | `/healthz`         | Liveness probe           |
| GET    | `/readyz`          | Readiness probe          |
| GET    | `/api/v1/version`  | Service version + env    |
| GET    | `/api/v1/ping`     | Returns `{"message":"pong"}` |

```bash
curl localhost:8080/healthz
```

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
cmd/server/         entrypoint (graceful shutdown)
internal/config/    env-based configuration
internal/server/    routing, handlers, middleware, JSON helpers
```

Middleware chain (outermost first): **recover → request-id → logging → CORS**.

## Docker

```bash
docker build -t oddvice-api .
docker run -p 8080:8080 oddvice-api
```
