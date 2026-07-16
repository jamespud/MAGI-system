.PHONY: debug server middleware sync_db test build_server clean help

MAGI_BIN := ./bin/magi-server
COMPOSE_FILE := docker/docker-compose.yml
BACKEND := backend

debug: middleware server

server:
	@echo "Building and running MAGI server..."
	@cd $(BACKEND) && go build -o ../$(MAGI_BIN) ./cmd/magi-server && ../$(MAGI_BIN) $(ARGS)

build_server:
	@echo "Building MAGI server..."
	@cd $(BACKEND) && go build -o ../$(MAGI_BIN) ./cmd/magi-server

middleware:
	@echo "Starting middleware (MySQL)..."
	@docker compose -f $(COMPOSE_FILE) up -d

sync_db:
	@echo "Applying MAGI table migrations..."
	@docker compose -f $(COMPOSE_FILE) exec -T mysql mysql -u root -proot magi < docker/atlas/migrations/magi_s6_tables.sql
	@docker compose -f $(COMPOSE_FILE) exec -T mysql mysql -u root -proot magi < docker/atlas/migrations/magi_s7_tables.sql
	@docker compose -f $(COMPOSE_FILE) exec -T mysql mysql -u root -proot magi < docker/atlas/migrations/magi_s8_tables.sql
	@echo "Done."

test:
	@cd $(BACKEND) && go test ./domain/... ./adapter/... ./bootstrap/... ./application/... ./server/...

clean:
	@rm -f $(MAGI_BIN)
	@docker compose -f $(COMPOSE_FILE) down 2>/dev/null || true

help:
	@echo "MAGI Multi-Agent Decision Engine (v2 Server)"
	@echo ""
	@echo "Usage:"
	@echo "  make server             Build and run the HTTP server"
	@echo "  make build_server       Build binary only"
	@echo "  make middleware         Start MySQL via docker-compose"
	@echo "  make sync_db            Apply MAGI table migrations"
	@echo "  make test               Run all tests"
	@echo "  make clean              Stop middleware + remove binary"
	@echo ""
	@echo "The server listens on :8080 with GET /health endpoint."
