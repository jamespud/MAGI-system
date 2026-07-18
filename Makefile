.PHONY: debug server down_server run_server build_server middleware sync_db test clean help fe fe_dev fe_test fe_lint

MAGI_BIN := ./bin/magi-server
COMPOSE_FILE := docker/docker-compose.yml
BACKEND := backend
FRONTEND_DIR := ./frontend
FRONTEND_STATIC := ./bin/resources/static

# ── Local Development ──────────────────────────────────────────────

debug: middleware run_server
	@echo ""
	@echo "MAGI running at http://localhost:8080"
	@echo "Start frontend dev server: make fe_dev"

run_server:
	@if [ ! -d "$(FRONTEND_STATIC)" ] || [ -z "$$(ls -A $(FRONTEND_STATIC) 2>/dev/null)" ]; then \
		echo "Static files missing, building frontend..."; \
		$(MAKE) fe; \
	fi
	@echo "Building and running MAGI server..."
	@cd $(BACKEND) && go build -o ../$(MAGI_BIN) ./cmd/magi-server && ../$(MAGI_BIN) $(ARGS)

# ── Containerized ──────────────────────────────────────────────────

server: fe
	@echo "Starting MAGI (containerized)..."
	@docker compose -f $(COMPOSE_FILE) --profile server down 2>/dev/null || true
	@docker compose -f $(COMPOSE_FILE) --profile server up -d --build
	@echo ""
	@echo "MAGI running at http://localhost:$${WEB_PORT:-80}"

down_server:
	@echo "Stopping MAGI containers..."
	@docker compose -f $(COMPOSE_FILE) --profile server down

# ── Build ──────────────────────────────────────────────────────────

build_server:
	@echo "Building MAGI server..."
	@cd $(BACKEND) && go build -o ../$(MAGI_BIN) ./cmd/magi-server

fe:
	@echo "Building frontend..."
	@cd $(FRONTEND_DIR) && npm ci && npm run build
	@rm -rf $(FRONTEND_STATIC)
	@mkdir -p $(FRONTEND_STATIC)
	@cp -r $(FRONTEND_DIR)/dist/* $(FRONTEND_STATIC)/
	@echo "Frontend built and copied to $(FRONTEND_STATIC)"

fe_dev:
	@echo "Starting frontend dev server (HMR)..."
	@cd $(FRONTEND_DIR) && npm run dev

fe_test:
	@echo "Running frontend tests..."
	@cd $(FRONTEND_DIR) && npm run test

fe_lint:
	@echo "Linting frontend..."
	@cd $(FRONTEND_DIR) && npm run lint

# ── Database ───────────────────────────────────────────────────────

middleware:
	@echo "Starting middleware (MySQL)..."
	@docker compose -f $(COMPOSE_FILE) --profile debug up -d

sync_db:
	@echo "Applying MAGI table migrations..."
	@docker compose -f $(COMPOSE_FILE) exec -T mysql mysql -u root -proot magi < docker/atlas/migrations/magi_s6_tables.sql
	@docker compose -f $(COMPOSE_FILE) exec -T mysql mysql -u root -proot magi < docker/atlas/migrations/magi_s7_tables.sql
	@docker compose -f $(COMPOSE_FILE) exec -T mysql mysql -u root -proot magi < docker/atlas/migrations/magi_s8_tables.sql
	@echo "Done."

# ── Test ───────────────────────────────────────────────────────────

test:
	@cd $(BACKEND) && go test ./domain/... ./adapter/... ./bootstrap/... ./application/... ./server/...

# ── Cleanup ────────────────────────────────────────────────────────

clean:
	@rm -f $(MAGI_BIN)
	@docker compose -f $(COMPOSE_FILE) --profile '*' down 2>/dev/null || true

# ── Help ───────────────────────────────────────────────────────────

help:
	@echo "MAGI Multi-Agent Decision Engine"
	@echo ""
	@echo "Local Development:"
	@echo "  make debug              Start MySQL + local Go server (then: make fe_dev)"
	@echo "  make fe_dev             Start frontend Vite HMR dev server"
	@echo ""
	@echo "Containerized:"
	@echo "  make server             Build frontend + start all containers"
	@echo "  make down_server        Stop MAGI containers"
	@echo ""
	@echo "Build:"
	@echo "  make fe                 Build frontend to bin/resources/static/"
	@echo "  make build_server       Build Go binary only"
	@echo ""
	@echo "Database:"
	@echo "  make middleware         Start MySQL (debug profile)"
	@echo "  make sync_db            Apply MAGI table migrations"
	@echo ""
	@echo "Test:"
	@echo "  make test               Run backend Go tests"
	@echo "  make fe_test            Run frontend tests"
	@echo ""
	@echo "Cleanup:"
	@echo "  make clean              Stop all containers + remove binary"
	@echo ""
	@echo "The server listens on :8080."
