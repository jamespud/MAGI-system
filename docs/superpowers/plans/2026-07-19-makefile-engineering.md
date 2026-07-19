# MAGI Makefile & Scripts Engineering Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace the ad-hoc Makefile with a script-delegated engineering entry point — 6 scripts, 2 compose files, unified env config, and a clean Makefile that is pure task dispatch.

**Architecture:** Makefile is a thin dispatcher (~80 lines, no shell logic). Six focused scripts under `scripts/` each own one responsibility (env, dev, build, docker, db, tools). Scripts are independently executable and CI/CD-ready. All paths anchored at repo root via `go -C backend` / `npm -C frontend`.

**Tech Stack:** GNU Make, Bash, Go 1.24+, Docker Compose v2, Atlas (DB migrations), Node/npm

## Global Constraints

- Every script: `#!/usr/bin/env bash` + `set -Eeuo pipefail`
- Makefile contains zero shell logic — only variable definitions, PHONY targets, and `bash scripts/<name>.sh <subcommand>` calls
- All paths repo-root-relative; no `cd` side effects
- `prepare` installs dependencies (`go mod download`, `npm install`); `build` only compiles
- `debug` does NOT auto-start Docker — `docker-up` is manual
- `tools.sh` is backend-only (Go fmt/lint/vet/tidy/test)
- `clean` removes `bin/` + `frontend/dist/` only, never Go cache
- `seed` and `reset-db` output TODO placeholders
- `.env.local` must be gitignored

---
```

### Task 1: New Makefile + dev.sh + build.sh (core dispatch working)

**Files:**
- Create: `scripts/dev.sh`
- Create: `scripts/build.sh`
- Modify: `Makefile` (full rewrite)

**Interfaces:**
- Produces: `scripts/dev.sh {backend|frontend|debug|server}`, `scripts/build.sh {backend|frontend|all|clean}`
- Produces: Makefile targets `help`, `backend`, `frontend`, `debug`, `server`, `build`, `clean`, `prepare`, `test`, `lint`, `fmt`, `vet`, `tidy`, `migrate`, `seed`, `reset-db`, `docker-up`, `docker-down`, `docker-logs`

- [ ] **Step 1: Create scripts directory**

```bash
mkdir -p scripts
```

- [ ] **Step 2: Write scripts/dev.sh**

```bash
#!/usr/bin/env bash
set -Eeuo pipefail

case "${1:-}" in
  backend)
    go -C backend run ./cmd/magi-server
    ;;
  frontend)
    npm -C frontend run dev
    ;;
  debug)
    trap 'kill 0' EXIT
    go -C backend run ./cmd/magi-server &
    npm -C frontend run dev &
    wait
    ;;
  server)
    go -C backend run ./cmd/magi-server
    ;;
  *)
    echo "Usage: dev.sh {backend|frontend|debug|server}"
    exit 1
    ;;
esac
```

- [ ] **Step 3: Write scripts/build.sh**

```bash
#!/usr/bin/env bash
set -Eeuo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
PROJECT_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"

case "${1:-}" in
  backend)
    go -C "$PROJECT_ROOT/backend" build -o "$PROJECT_ROOT/bin/magi" ./cmd/magi-server
    ;;
  frontend)
    npm -C "$PROJECT_ROOT/frontend" run build
    ;;
  all)
    echo "=== Building backend ==="
    go -C "$PROJECT_ROOT/backend" build -o "$PROJECT_ROOT/bin/magi" ./cmd/magi-server
    echo "=== Building frontend ==="
    npm -C "$PROJECT_ROOT/frontend" run build
    echo "=== Build complete: bin/magi + frontend/dist/ ==="
    ;;
  clean)
    rm -rf "$PROJECT_ROOT/bin" "$PROJECT_ROOT/frontend/dist"
    echo "Cleaned: bin/ frontend/dist/"
    ;;
  *)
    echo "Usage: build.sh {backend|frontend|all|clean}"
    exit 1
    ;;
esac
```

- [ ] **Step 4: Make scripts executable**

```bash
chmod +x scripts/dev.sh scripts/build.sh
```

- [ ] **Step 5: Rewrite Makefile (full replacement)**

```makefile
# =============================================================================
# MAGI Makefile — Engineering Entry Point
# =============================================================================

.DEFAULT_GOAL := help

SCRIPTS_DIR := scripts

.PHONY: help prepare backend frontend debug server build \
        test lint fmt vet tidy \
        migrate seed reset-db \
        docker-up docker-down docker-logs \
        clean

# =============================================================================
# Environment
# =============================================================================

help:
	@echo ""
	@echo "========================== MAGI =========================="
	@echo ""
	@echo "Environment"
	@echo "  make prepare          Bootstrap environment (deps, env, dirs)"
	@echo ""
	@echo "Development"
	@echo "  make backend          Start Go server"
	@echo "  make frontend         Start React dev server"
	@echo "  make debug            Start backend + frontend in parallel"
	@echo "  make server           Start production server"
	@echo ""
	@echo "Build"
	@echo "  make build            Build backend + frontend"
	@echo "  make clean            Remove build artifacts"
	@echo ""
	@echo "Quality (backend)"
	@echo "  make test             Run Go tests"
	@echo "  make lint             Run golangci-lint"
	@echo "  make fmt              Run gofmt"
	@echo "  make vet              Run go vet"
	@echo "  make tidy             Run go mod tidy"
	@echo ""
	@echo "Database"
	@echo "  make migrate          Apply Atlas migrations"
	@echo "  make seed             Seed database (TODO)"
	@echo "  make reset-db         Reset database (TODO)"
	@echo ""
	@echo "Docker"
	@echo "  make docker-up        Start MySQL middleware"
	@echo "  make docker-down      Stop MySQL middleware"
	@echo "  make docker-logs      Tail middleware logs"
	@echo ""
	@echo "=========================================================="

prepare:
	bash $(SCRIPTS_DIR)/env.sh

# =============================================================================
# Development
# =============================================================================

backend:
	bash $(SCRIPTS_DIR)/dev.sh backend

frontend:
	bash $(SCRIPTS_DIR)/dev.sh frontend

debug:
	bash $(SCRIPTS_DIR)/dev.sh debug

server:
	bash $(SCRIPTS_DIR)/dev.sh server

# =============================================================================
# Build
# =============================================================================

build:
	bash $(SCRIPTS_DIR)/build.sh all

clean:
	bash $(SCRIPTS_DIR)/build.sh clean

# =============================================================================
# Quality
# =============================================================================

test:
	bash $(SCRIPTS_DIR)/tools.sh test

lint:
	bash $(SCRIPTS_DIR)/tools.sh lint

fmt:
	bash $(SCRIPTS_DIR)/tools.sh fmt

vet:
	bash $(SCRIPTS_DIR)/tools.sh vet

tidy:
	bash $(SCRIPTS_DIR)/tools.sh tidy

# =============================================================================
# Database
# =============================================================================

migrate:
	bash $(SCRIPTS_DIR)/db.sh migrate

seed:
	bash $(SCRIPTS_DIR)/db.sh seed

reset-db:
	bash $(SCRIPTS_DIR)/db.sh reset

# =============================================================================
# Docker
# =============================================================================

docker-up:
	bash $(SCRIPTS_DIR)/docker.sh up

docker-down:
	bash $(SCRIPTS_DIR)/docker.sh down

docker-logs:
	bash $(SCRIPTS_DIR)/docker.sh logs
```

- [ ] **Step 6: Verify make help works**

```bash
make help
```

Expected: formatted help output listing all targets.

- [ ] **Step 7: Commit**

```bash
git add Makefile scripts/dev.sh scripts/build.sh
git commit -m "feat: new Makefile + dev.sh + build.sh — script-delegated engineering entry"
```

---

### Task 2: Create remaining scripts (docker.sh, db.sh stub, tools.sh, env.sh stub)

**Files:**
- Create: `scripts/docker.sh`
- Create: `scripts/tools.sh`
- Create: `scripts/db.sh`
- Create: `scripts/env.sh`

**Interfaces:**
- Consumes: `Makefile` targets from Task 1
- Produces: `scripts/docker.sh {up|down|logs|restart}`, `scripts/tools.sh {fmt|lint|vet|tidy|test}`, `scripts/db.sh {migrate|seed|reset}`, `scripts/env.sh`

- [ ] **Step 1: Write scripts/docker.sh**

```bash
#!/usr/bin/env bash
set -Eeuo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
PROJECT_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"

COMPOSE_BASE="$PROJECT_ROOT/docker/docker-compose.yml"
COMPOSE_DEV="$PROJECT_ROOT/docker/docker-compose.dev.yml"
COMPOSE_FILES=(-f "$COMPOSE_BASE" -f "$COMPOSE_DEV")

case "${1:-}" in
  up)
    docker compose "${COMPOSE_FILES[@]}" up -d
    ;;
  down)
    docker compose "${COMPOSE_FILES[@]}" down
    ;;
  logs)
    docker compose "${COMPOSE_FILES[@]}" logs -f
    ;;
  restart)
    docker compose "${COMPOSE_FILES[@]}" down
    docker compose "${COMPOSE_FILES[@]}" up -d
    ;;
  *)
    echo "Usage: docker.sh {up|down|logs|restart}"
    exit 1
    ;;
esac
```

- [ ] **Step 2: Write scripts/tools.sh**

```bash
#!/usr/bin/env bash
set -Eeuo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
PROJECT_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"

case "${1:-}" in
  fmt)
    go -C "$PROJECT_ROOT/backend" fmt ./...
    ;;
  lint)
    golangci-lint -C "$PROJECT_ROOT/backend" run
    ;;
  vet)
    go -C "$PROJECT_ROOT/backend" vet ./...
    ;;
  tidy)
    go -C "$PROJECT_ROOT/backend" mod tidy
    ;;
  test)
    go -C "$PROJECT_ROOT/backend" test ./... -cover
    ;;
  *)
    echo "Usage: tools.sh {fmt|lint|vet|tidy|test}"
    exit 1
    ;;
esac
```

- [ ] **Step 3: Write scripts/db.sh (stub — migrate works, seed/reset are TODO)**

```bash
#!/usr/bin/env bash
set -Eeuo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
PROJECT_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"

if [ -f "$PROJECT_ROOT/.env.local" ]; then
  set -a
  source "$PROJECT_ROOT/.env.local"
  set +a
fi

MIGRATIONS_DIR="file://$PROJECT_ROOT/docker/atlas/migrations"
DB_URL="mysql://${DB_USER:-}:${DB_PASS:-}@${DB_HOST:-127.0.0.1}:${DB_PORT:-3307}/${DB_NAME:-magi}"

case "${1:-}" in
  migrate)
    atlas migrate apply --dir "$MIGRATIONS_DIR" --url "$DB_URL"
    ;;
  seed)
    echo "TODO: seed not implemented"
    ;;
  reset)
    echo "TODO: reset-db not implemented"
    ;;
  *)
    echo "Usage: db.sh {migrate|seed|reset}"
    exit 1
    ;;
esac
```

- [ ] **Step 4: Write scripts/env.sh (stub — dependency check only)**

```bash
#!/usr/bin/env bash
set -Eeuo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
PROJECT_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"

echo "=== Checking dependencies ==="

missing=0

check_cmd() {
  local cmd="$1" label="$2"
  if command -v "$cmd" >/dev/null 2>&1; then
    echo "  [OK] $label: $($cmd version 2>/dev/null || $cmd --version 2>/dev/null || echo 'found')"
  else
    echo "  [MISSING] $label ($cmd)"
    missing=1
  fi
}

check_cmd go     "Go"
check_cmd node   "Node.js"
check_cmd docker "Docker"

if [ "$missing" -eq 1 ]; then
  echo "ERROR: Install missing dependencies before continuing."
  exit 1
fi

echo "=== All dependencies found ==="
```

- [ ] **Step 5: Make all scripts executable**

```bash
chmod +x scripts/docker.sh scripts/tools.sh scripts/db.sh scripts/env.sh
```

- [ ] **Step 6: Verify scripts run independently**

```bash
bash scripts/tools.sh          # expect usage message (no args)
bash scripts/docker.sh          # expect usage message
bash scripts/db.sh              # expect usage message
bash scripts/env.sh             # expect dependency check output
bash scripts/dev.sh             # expect usage message
bash scripts/build.sh           # expect usage message
```

- [ ] **Step 7: Commit**

```bash
git add scripts/docker.sh scripts/tools.sh scripts/db.sh scripts/env.sh
git commit -m "feat: add docker.sh, tools.sh, db.sh, env.sh scripts"
```

---

### Task 3: Docker Compose split (new compose files)

**Files:**
- Modify: `docker/docker-compose.yml` (replace with MySQL-only base)
- Create: `docker/docker-compose.dev.yml`

**Interfaces:**
- Consumes: `scripts/docker.sh` from Task 2 (references these exact file paths)
- Produces: valid compose files at `docker/docker-compose.yml` + `docker/docker-compose.dev.yml`

- [ ] **Step 1: Replace docker/docker-compose.yml with MySQL-only base**

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

- [ ] **Step 2: Create docker/docker-compose.dev.yml**

```yaml
name: magi-dev
services: {}
```

- [ ] **Step 3: Test docker-up starts MySQL**

```bash
make docker-up
```

Expected: MySQL container starts on 127.0.0.1:3307.

- [ ] **Step 4: Test docker-logs shows MySQL output**

```bash
make docker-logs
```

Expected: MySQL logs stream. Ctrl+C to exit.

- [ ] **Step 5: Test docker-down stops MySQL**

```bash
make docker-down
```

Expected: MySQL container stops and is removed.

- [ ] **Step 6: Commit**

```bash
git add docker/docker-compose.yml docker/docker-compose.dev.yml
git commit -m "feat: split docker-compose — MySQL base + empty dev override"
```

---

### Task 4: Environment config (.env.example, .env.local, env.sh full)

**Files:**
- Create: `.env.example`
- Create: `.env.local`
- Modify: `scripts/env.sh` (add .env.local bootstrap + dep install)
- Modify: `.gitignore` (add .env.local)

**Interfaces:**
- Consumes: `scripts/env.sh` stub from Task 2
- Produces: `.env.example` template, `.env.local` (gitignored), full `env.sh`

- [ ] **Step 1: Create .env.example**

```ini
# MAGI Environment Configuration
# Copy to .env.local and customize for your local environment

APP_ENV=local
SERVER_PORT=8080

DB_HOST=127.0.0.1
DB_PORT=3307
DB_USER=magi
DB_PASS=magi123
DB_NAME=magi
```

- [ ] **Step 2: Create .env.local (from .env.example)**

```bash
cp .env.example .env.local
```

- [ ] **Step 3: Add .env.local to .gitignore**

```gitignore
# Environment (local overrides)
.env.local
```

Append this to `.gitignore`.

- [ ] **Step 4: Replace scripts/env.sh with full implementation**

```bash
#!/usr/bin/env bash
set -Eeuo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
PROJECT_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"

echo "=== Checking dependencies ==="

missing=0

check_cmd() {
  local cmd="$1" label="$2"
  if command -v "$cmd" >/dev/null 2>&1; then
    local ver
    ver=$("$cmd" version 2>/dev/null || "$cmd" --version 2>/dev/null || echo "found")
    echo "  [OK] $label: $(echo "$ver" | head -1)"
  else
    echo "  [MISSING] $label ($cmd)"
    missing=1
  fi
}

check_cmd go     "Go"
check_cmd node   "Node.js"
check_cmd docker "Docker"

if [ "$missing" -eq 1 ]; then
  echo ""
  echo "ERROR: Install missing dependencies before continuing."
  exit 1
fi

echo ""
echo "=== Environment setup ==="

if [ ! -f "$PROJECT_ROOT/.env.local" ]; then
  if [ -f "$PROJECT_ROOT/.env.example" ]; then
    cp "$PROJECT_ROOT/.env.example" "$PROJECT_ROOT/.env.local"
    echo "Created .env.local from .env.example — edit for local overrides"
  else
    echo "WARNING: .env.example not found, skipping .env.local creation"
  fi
else
  echo ".env.local already exists"
fi

mkdir -p "$PROJECT_ROOT/bin"

echo ""
echo "=== Installing Go dependencies ==="
go -C "$PROJECT_ROOT/backend" mod download

echo ""
echo "=== Installing frontend dependencies ==="
npm -C "$PROJECT_ROOT/frontend" install

echo ""
echo "=== Environment ready ==="
```

- [ ] **Step 5: Test make prepare**

```bash
make prepare
```

Expected: dependency check passes, "Environment ready" message.

- [ ] **Step 6: Verify .env.local is gitignored**

```bash
git status --short .env.local
```

Expected: no output (file is ignored by git).

- [ ] **Step 7: Commit**

```bash
git add .env.example scripts/env.sh .gitignore
git commit -m "feat: add env config — .env.example template + env.sh bootstrap"
```

Note: `.env.local` is deliberately NOT committed (gitignored).

---

### Task 5: db.sh full implementation (Atlas migration)

**Files:**
- Modify: `scripts/db.sh` (finalize migrate, seed, reset)

**Interfaces:**
- Consumes: `.env.local` from Task 4 (DB_HOST, DB_PORT, DB_USER, DB_PASS, DB_NAME)
- Produces: working `make migrate` via Atlas

- [ ] **Step 1: Replace scripts/db.sh with final implementation**

```bash
#!/usr/bin/env bash
set -Eeuo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
PROJECT_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"

if [ -f "$PROJECT_ROOT/.env.local" ]; then
  set -a
  source "$PROJECT_ROOT/.env.local"
  set +a
fi

MIGRATIONS_DIR="file://$PROJECT_ROOT/docker/atlas/migrations"
DB_URL="mysql://${DB_USER:-magi}:${DB_PASS:-magi123}@${DB_HOST:-127.0.0.1}:${DB_PORT:-3307}/${DB_NAME:-magi}"

case "${1:-}" in
  migrate)
    echo "=== Applying Atlas migrations ==="
    atlas migrate apply --dir "$MIGRATIONS_DIR" --url "$DB_URL"
    ;;
  seed)
    echo "TODO: seed not implemented"
    ;;
  reset)
    echo "TODO: reset-db not implemented"
    ;;
  *)
    echo "Usage: db.sh {migrate|seed|reset}"
    exit 1
    ;;
esac
```

- [ ] **Step 2: Test make migrate (requires docker-up first)**

```bash
make docker-up
make migrate
```

Expected: Atlas applies migrations successfully, or reports "no migration files" / "already applied" (both are non-error outcomes).

- [ ] **Step 3: Test make seed and make reset-db (TODO placeholders)**

```bash
make seed
make reset-db
```

Expected: `TODO: seed not implemented` and `TODO: reset-db not implemented`.

- [ ] **Step 4: Commit**

```bash
git add scripts/db.sh
git commit -m "feat: finalize db.sh — Atlas migrate, TODO seed/reset"
```

---

### Task 6: Cleanup — remove deprecated targets and old files

**Files:**
- No files to delete (old docker-compose.yml was already replaced in Task 3)
- The old Makefile has already been fully replaced in Task 1

**Actions:** Verify nothing stale remains. The old Makefile targets (`debug_backend`, `debug_frontend`, `build_backend`, `build_frontend`, `test_backend`, `test_frontend`, `lint_backend`, `lint_frontend`, `migrate_down`, `sync_db`, `middleware`) were already removed when the Makefile was rewritten in Task 1.

- [ ] **Step 1: Verify no stale references remain**

```bash
grep -r "debug_backend\|debug_frontend\|build_backend\|build_frontend\|test_backend\|test_frontend\|lint_backend\|lint_frontend\|migrate_down\|sync_db\|middleware" Makefile || echo "No stale references found"
```

Expected: "No stale references found".

- [ ] **Step 2: Verify docker-compose.yml is the new MySQL-only version**

```bash
grep "profiles:" docker/docker-compose.yml || echo "No profiles block — correct"
grep "magi-server:" docker/docker-compose.yml || echo "No server service — correct"
grep "magi-web:" docker/docker-compose.yml || echo "No web service — correct"
```

Expected: all three report no match (no profiles, no server, no web — MySQL only).

- [ ] **Step 3: Verify scripts/ directory structure matches spec**

```bash
ls scripts/
```

Expected: `build.sh  db.sh  dev.sh  docker.sh  env.sh  tools.sh`.

- [ ] **Step 4: Commit if any cleanup changes made**

```bash
# Only if changes were needed
git add -A && git commit -m "chore: remove deprecated Makefile targets and old compose services"
```

---

### Task 7: Full regression verification + documentation

**Files:**
- Create: `README.md` (new — development command reference)

**Verification checklist:** Run every `make` target and confirm expected behavior.

- [ ] **Step 1: Verify make help**

```bash
make help
```

Expected: formatted help with all targets.

- [ ] **Step 2: Verify make prepare**

```bash
make prepare
```

Expected: dependency check passes, "Environment ready".

- [ ] **Step 3: Verify make fmt**

```bash
make fmt
```

Expected: `go fmt ./...` runs, no output (already formatted).

- [ ] **Step 4: Verify make vet**

```bash
make vet
```

Expected: `go vet ./...` runs, no errors.

- [ ] **Step 5: Verify make tidy**

```bash
make tidy
```

Expected: `go mod tidy` runs, no changes.

- [ ] **Step 6: Verify make test**

```bash
make test
```

Expected: Go tests run, pass (or any pre-existing failures, not caused by this change).

- [ ] **Step 7: Verify make lint**

```bash
make lint
```

Expected: `golangci-lint` runs. May show pre-existing issues — acceptable as long as the command itself executes.

- [ ] **Step 8: Verify make build**

```bash
make build
```

Expected: backend binary at `bin/magi`, frontend output at `frontend/dist/`.

- [ ] **Step 9: Verify make clean**

```bash
make clean
```

Expected: `bin/` and `frontend/dist/` removed.

- [ ] **Step 10: Verify make docker-up / docker-logs / docker-down**

```bash
make docker-up
make docker-logs   # Ctrl+C to exit
make docker-down
```

Expected: MySQL starts, logs stream, container stops.

- [ ] **Step 11: Verify make migrate**

```bash
make docker-up
make migrate
make docker-down
```

Expected: Atlas runs migrations against MySQL.

- [ ] **Step 12: Verify make seed / make reset-db (TODO placeholders)**

```bash
make seed
make reset-db
```

Expected: TODO messages.

- [ ] **Step 13: Verify make backend (smoke test)**

```bash
# Start in background, kill after a few seconds
make backend &
sleep 5
kill %1 2>/dev/null || true
```

Expected: Go server starts, no immediate crash.

- [ ] **Step 14: Create README.md**

```markdown
# MAGI

Evidence-driven multi-agent decision engine.

## Quick Start

```bash
# Bootstrap environment
make prepare

# Start MySQL middleware (keep running in a separate terminal)
make docker-up

# Start development (backend + frontend in parallel)
make debug
```

## Development Commands

| Command | Description |
|---------|-------------|
| `make prepare` | Bootstrap: check deps, init env, install dependencies |
| `make backend` | Start Go server |
| `make frontend` | Start React dev server |
| `make debug` | Start backend + frontend in parallel |
| `make server` | Start production server |
| `make build` | Build backend + frontend |
| `make clean` | Remove build artifacts |
| `make test` | Run backend tests |
| `make lint` | Run golangci-lint |
| `make fmt` | Format Go code |
| `make vet` | Run go vet |
| `make tidy` | Run go mod tidy |

## Database

| Command | Description |
|---------|-------------|
| `make docker-up` | Start MySQL (127.0.0.1:3307) |
| `make docker-down` | Stop MySQL |
| `make docker-logs` | Tail MySQL logs |
| `make migrate` | Apply Atlas migrations |
| `make seed` | Seed database (TODO) |
| `make reset-db` | Reset database (TODO) |

## Configuration

Copy `.env.example` to `.env.local` and customize (done automatically by `make prepare`).
All scripts read configuration from `.env.local`.
```

- [ ] **Step 15: Final commit**

```bash
git add README.md
git commit -m "docs: add README with development command reference"
```

---

### Acceptance Checklist (final)

Run all of these before declaring done:

```
[ ] make help           — formatted help output
[ ] make prepare        — deps check + env bootstrap + deps install
[ ] make backend        — Go server starts
[ ] make frontend       — React dev server starts
[ ] make debug          — backend + frontend parallel
[ ] make server         — production server starts
[ ] make build          — bin/magi + frontend/dist/ produced
[ ] make clean          — bin/ + frontend/dist/ removed
[ ] make docker-up      — MySQL starts on 3307
[ ] make docker-down    — MySQL stops
[ ] make docker-logs    — MySQL logs stream
[ ] make migrate        — Atlas migration runs
[ ] make seed           — TODO placeholder
[ ] make reset-db       — TODO placeholder
[ ] make fmt            — go fmt runs
[ ] make lint           — golangci-lint runs
[ ] make vet            — go vet runs
[ ] make tidy           — go mod tidy runs
[ ] make test           — Go tests run
```

### Engineering Standards Checklist

```
[ ] All scripts: #!/usr/bin/env bash
[ ] All scripts: set -Eeuo pipefail
[ ] All scripts: usage message on missing/wrong args
[ ] All scripts: independently executable from anywhere
[ ] Makefile: zero inline shell logic beyond bash scripts/<name>.sh <args>
[ ] All paths: repo-root-relative (go -C backend, npm -C frontend)
[ ] .env.local: gitignored
[ ] No deprecated targets in Makefile
[ ] No old docker-compose profiles/services
```
