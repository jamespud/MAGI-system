# MAGI

Evidence-driven multi-agent decision engine (inspired by Evangelion's MAGI).

Three agents — **Melchior** (scientist), **Balthasar** (protector), **Casper** (innovator) — independently investigate a decision question, then vote, debate, reflect, and re-vote toward consensus.

## Quick Start

```bash
# Bootstrap environment (check deps, install dependencies, create config)
make prepare

# Start MySQL middleware (keep running in a separate terminal)
make docker-up

# Start development (backend + frontend in parallel)
make debug
```

## Development Commands

| Command | Description |
|---------|-------------|
| `make prepare` | Bootstrap: check deps, init `.env.local`, install Go + npm dependencies |
| `make backend` | Start Go server (`backend/cmd/magi-server`) |
| `make frontend` | Start React dev server (Vite) |
| `make debug` | Start backend + frontend in parallel |
| `make server` | Start production server |
| `make build` | Build backend (`bin/magi`) + frontend (`frontend/dist/`) |
| `make clean` | Remove `bin/` and `frontend/dist/` |

## Quality (Backend)

| Command | Description |
|---------|-------------|
| `make test` | Run Go tests with coverage |
| `make lint` | Run `golangci-lint` |
| `make fmt` | Run `go fmt ./...` |
| `make vet` | Run `go vet ./...` |
| `make tidy` | Run `go mod tidy` |

## Database

| Command | Description |
|---------|-------------|
| `make docker-up` | Start MySQL on `127.0.0.1:3307` |
| `make docker-down` | Stop MySQL |
| `make docker-logs` | Tail MySQL logs |
| `make migrate` | Apply Atlas migrations from `docker/atlas/migrations/` |
| `make seed` | Seed database (not yet implemented) |
| `make reset-db` | Reset database (not yet implemented) |

## Configuration

Copy `.env.example` to `.env.local` and customize for your environment. This is done
automatically by `make prepare` if `.env.local` does not exist.

```ini
APP_ENV=local
SERVER_PORT=8080

DB_HOST=127.0.0.1
DB_PORT=3307
DB_USER=magi
DB_PASS=magi123
DB_NAME=magi
```

All scripts read configuration from `.env.local`. The file is gitignored — only
`.env.example` is committed.

## Architecture

Four-layer DDD architecture: `Application → Orchestration → Agent Runtime → Port/Adapter`.

See `magi-design.md` (Chinese) for the frozen target design.
See `docs/superpowers/specs/2026-07-19-makefile-engineering.md` for the engineering infrastructure spec.
