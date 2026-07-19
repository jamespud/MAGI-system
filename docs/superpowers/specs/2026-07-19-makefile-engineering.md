# MAGI Makefile & Scripts Engineering Spec

**Date**: 2026-07-19
**Status**: Approved
**Scope**: Replace ad-hoc Makefile with a script-delegated engineering entry point

## Design Goals

- Makefile = task dispatcher (no business logic, no inline shell)
- Shell scripts = task executors (one responsibility per script)
- Every script independently executable (reusable in CI/CD)
- `make` becomes the sole development entry point
- Lightweight — no over-engineering for a solo developer project

## Directory Structure (Final)

```
magi/
├── Makefile                    # rewritten, ~80 lines, pure delegation
├── scripts/
│   ├── env.sh                  # environment bootstrap (prepare)
│   ├── dev.sh                  # development runtime (backend/frontend/debug/server)
│   ├── build.sh                # builds (backend/frontend/all) + clean
│   ├── docker.sh               # Docker middleware (up/down/logs/restart)
│   ├── db.sh                   # database (migrate/seed/reset)
│   └── tools.sh                # toolchain (fmt/lint/vet/tidy/test)
├── docker/
│   ├── docker-compose.yml      # base — MySQL only
│   ├── docker-compose.dev.yml  # dev overrides
│   ├── atlas/migrations/       # kept as-is
│   ├── nginx/                  # kept as-is
│   └── data/                   # kept as-is
├── .env.example                # committed template
├── .env.local                  # gitignored, local overrides
├── backend/
├── frontend/
└── bin/                        # build output
```

### Removed

- `docker/docker-compose.yml` (replaced by the two files above)
- Old Makefile targets: `debug_backend`, `debug_frontend`, `build_backend`, `build_frontend`, `test_backend`, `test_frontend`, `lint_backend`, `lint_frontend`, `migrate_down`, `sync_db`, `middleware`

## Makefile Targets

| Category     | Target        | Delegates to              |
|-------------|---------------|---------------------------|
| Env         | `help`        | inline echo               |
| Env         | `prepare`     | `scripts/env.sh`          |
| Dev         | `backend`     | `scripts/dev.sh backend`  |
| Dev         | `frontend`    | `scripts/dev.sh frontend` |
| Dev         | `debug`       | `scripts/dev.sh debug`    |
| Dev         | `server`      | `scripts/dev.sh server`   |
| Build       | `build`       | `scripts/build.sh all`    |
| Quality     | `test`        | `scripts/tools.sh test`   |
| Quality     | `lint`        | `scripts/tools.sh lint`   |
| Quality     | `fmt`         | `scripts/tools.sh fmt`    |
| Quality     | `vet`         | `scripts/tools.sh vet`    |
| Quality     | `tidy`        | `scripts/tools.sh tidy`   |
| Database    | `migrate`     | `scripts/db.sh migrate`   |
| Database    | `seed`        | `scripts/db.sh seed`      |
| Database    | `reset-db`    | `scripts/db.sh reset`     |
| Docker      | `docker-up`   | `scripts/docker.sh up`    |
| Docker      | `docker-down` | `scripts/docker.sh down`  |
| Docker      | `docker-logs` | `scripts/docker.sh logs`  |
| Maintenance | `clean`       | `scripts/build.sh clean`  |

### Key behaviors

- `prepare` checks dependencies (go, node, docker), creates `.env.local` from template if absent, creates `bin/`, and installs dependencies (`go mod download`, `npm install`).
- `debug` starts backend + frontend in parallel. It does **NOT** auto-start Docker — the developer runs `make docker-up` once manually.
- `build` only compiles, does not install dependencies.
- `docker-up/down/logs` always use both compose files: `-f docker-compose.yml -f docker-compose.dev.yml`.

## Script Specifications

### env.sh

```
Subcommands: (none — single purpose)
```

- Check that `go`, `node`, `docker` are on PATH
- If `.env.local` does not exist, copy `.env.example` → `.env.local`
- `mkdir -p bin`
- `go -C backend mod download`
- `npm -C frontend install`

### dev.sh

```
Subcommands: backend | frontend | debug | server
```

- `backend`: `go -C backend run ./cmd/magi-server`
- `frontend`: `npm -C frontend run dev`
- `debug`: start backend (background `&`) and frontend in parallel, `wait` on both
- `server`: `ENV=prod go -C backend run ./cmd/magi-server`

### build.sh

```
Subcommands: backend | frontend | all | clean
```

- `backend`: `go -C backend build -o ../bin/magi ./cmd/magi-server`
- `frontend`: `npm -C frontend run build`
- `all`: backend then frontend (sequential)
- `clean`: `rm -rf bin frontend/dist && go -C backend clean -cache -testcache`

Note: does NOT run `npm install`. Dependency installation belongs to `prepare`.

### docker.sh

```
Subcommands: up | down | logs | restart
```

Uses `COMPOSE_FILES="docker/docker-compose.yml docker/docker-compose.dev.yml"`.

- `up`: `docker compose -f ... -f ... up -d`
- `down`: `docker compose -f ... -f ... down`
- `logs`: `docker compose -f ... -f ... logs -f`
- `restart`: down → up

### db.sh

```
Subcommands: migrate | seed | reset
```

Reads DB credentials from `.env.local`:
```
DB_HOST=127.0.0.1
DB_PORT=3307
DB_USER=magi
DB_PASS=magi123
DB_NAME=magi
```

- `migrate`: `atlas migrate apply --dir "file://docker/atlas/migrations" --url "mysql://${DB_USER}:${DB_PASS}@${DB_HOST}:${DB_PORT}/${DB_NAME}"`
- `seed`: placeholder (echo "TODO: seed not implemented")
- `reset`: placeholder (echo "TODO: reset-db not implemented")

### tools.sh

```
Subcommands: fmt | lint | vet | tidy | test
```

- `fmt`: `go -C backend fmt ./...`
- `lint`: `golangci-lint -C backend run`
- `vet`: `go -C backend vet ./...`
- `tidy`: `go -C backend mod tidy`
- `test`: `go -C backend test ./... -cover`

Note: frontend lint/test are NOT included in tools.sh for now. The plan says `make lint` = golangci-lint + eslint, but currently no eslint setup is confirmed. When frontend lint/test tooling matures, add corresponding steps.

## Docker Compose Files

### docker-compose.yml (base)

Only the MySQL service — the common middleware layer:

```yaml
name: magi-dev
services:
  mysql:
    image: mysql:8.4.5
    container_name: magi-mysql
    environment:
      MYSQL_ROOT_PASSWORD: root
      MYSQL_DATABASE: magi
      MYSQL_USER: magi
      MYSQL_PASSWORD: magi123
    ports:
      - '127.0.0.1:3307:3306'
    volumes:
      - ./data/mysql:/var/lib/mysql
    healthcheck:
      test: ['CMD', 'mysqladmin', 'ping', '-h', 'localhost', '-u', 'magi', '-pmagi123']
      interval: 5s
      timeout: 5s
      retries: 10
      start_period: 20s
```

### docker-compose.dev.yml (dev overrides)

Minimal — exists for future dev-specific overrides (e.g., different env vars, exposed ports). Initially may be near-empty:

```yaml
name: magi-dev
services:
  mysql:
    # dev overrides — currently same as base, placeholder for future
```

## Environment Variables

### .env.example (committed)

```ini
# MAGI Environment Configuration
# Copy to .env.local and customize

DB_HOST=127.0.0.1
DB_PORT=3307
DB_USER=magi
DB_PASS=magi123
DB_NAME=magi
```

### .env.local (gitignored)

Generated by `prepare` from `.env.example` if absent. Developer edits for local overrides. Read by `db.sh`, `docker.sh`, etc.

## What Is NOT Included (YAGNI)

- No `dump`, `rollback`, `status` for database — add when needed
- No `test_backend`/`test_frontend`/`lint_backend`/`lint_frontend` separate targets — solo dev doesn't need granularity
- No `ci/`, `release/`, `utils/` subdirectories — premature
- No magi-server/magi-web Docker service definitions in compose files — currently run outside Docker
- No `sync_db` or `middleware` targets — the Go tools they referenced no longer exist
- Frontend lint/test in tools.sh — add when tooling is confirmed

## Extension Strategy

- When Wire/Mock/OpenAPI/JSON Schema generators are added → new subcommand in `tools.sh`
- When Docker Compose files exceed 2 → split into `docker/compose/` directory
- When any script exceeds ~200 lines → split into subdirectory
- When automatic releases are needed → new `scripts/release.sh`
- When CI/CD grows complex → new `scripts/ci/` directory

Principle: **evolve with needs, not with speculation**.
