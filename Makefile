# =============================================================================
# MAGI Makefile
# =============================================================================

.DEFAULT_GOAL := help
SCRIPTS_DIR := scripts

.PHONY: help prepare debug backend frontend \
        db-up db-down db-logs db-reset \
        rag_up rag_down \
        stop \
        fmt vet tidy lint \
        build clean \
        test \
        web-up web-down web-logs web-ps

# =============================================================================
# Environment
# =============================================================================

help:
	@echo ""
	@echo "============================ MAGI ============================"
	@echo ""
	@echo "Environment"
	@echo "  make prepare          Bootstrap: deps, .env, dirs"
	@echo ""
	@echo "Dev (local development)"
	@echo "  make debug            MySQL + backend (go run) + nginx (:80)"
	@echo "  make backend          Backend only (go run :8080)"
	@echo "  make frontend         Frontend only (vite dev :5173)"
	@echo ""
	@echo "Database"
	@echo "  make db-up            Start MySQL middleware"
	@echo "  make db-down          Stop MySQL middleware"
	@echo "  make db-logs          MySQL middleware logs"
	@echo "  make db-reset         Drop all tables (AutoMigrate recreates)"
	@echo ""
	@echo "Build"
	@echo "  make build            Build binary + frontend + Docker images"
	@echo "  make clean            Remove build artifacts (not images)"
	@echo ""
	@echo "Quality"
	@echo "  make test             Run all tests (Go + frontend)"
	@echo ""
	@echo "Web (containerized stack)"
	@echo "  make web-up           Build + start full stack (mysql + server + nginx)"
	@echo "  make web-down         Stop full stack"
	@echo "  make web-logs         Full stack logs"
	@echo "  make web-ps           Full stack status"
	@echo ""
	@echo "=============================================================="

prepare:
	bash $(SCRIPTS_DIR)/env.sh

# =============================================================================
# Dev (local development)
# =============================================================================

debug:
	bash $(SCRIPTS_DIR)/dev.sh debug

backend:
	bash $(SCRIPTS_DIR)/dev.sh backend

frontend:
	bash $(SCRIPTS_DIR)/dev.sh frontend

# =============================================================================
# Database
# =============================================================================

db-up:
	bash $(SCRIPTS_DIR)/docker.sh up

db-down:
	bash $(SCRIPTS_DIR)/docker.sh down

db-logs:
	bash $(SCRIPTS_DIR)/docker.sh logs

db-reset:
	bash $(SCRIPTS_DIR)/db.sh reset

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
	go -C backend test ./...
	npm -C frontend test

fmt:
	go -C backend fmt ./...

vet:
	go -C backend vet ./...

tidy:
	go -C backend mod tidy

lint: fmt vet

# =============================================================================
# Web (containerized stack)
# =============================================================================

COMPOSE_DEBUG := docker/docker-compose-debug.yml
COMPOSE_WEB := docker/docker-compose-web.yml

web-down:
	docker compose --project-directory . -f $(COMPOSE_WEB) down

rag_up:
	docker compose --project-directory . -f $(COMPOSE_DEBUG) up -d milvus-standalone elasticsearch

rag_down:
	docker compose --project-directory . -f $(COMPOSE_DEBUG) stop milvus-standalone elasticsearch

# rag-prepare: create the external named volumes shared by debug + web RAG
# stacks (Milvus/ES/etcd/minio data). Idempotent; run once before first web-up/debug.
rag-prepare:
	docker volume create magi-milvus-data 2>/dev/null || true
	docker volume create magi-es-data 2>/dev/null || true
	docker volume create magi-etcd-data 2>/dev/null || true
	docker volume create magi-minio-data 2>/dev/null || true

web-up:
	$(MAKE) rag-prepare
	docker compose --project-directory . -f $(COMPOSE_WEB) up -d --build

web-logs:
	docker compose --project-directory . -f $(COMPOSE_WEB) logs -f

web-ps:
	docker compose --project-directory . -f $(COMPOSE_WEB) ps

# stop: one-key stop all dev + web containers (both compose projects + debug nginx).
stop:
	docker compose --project-directory . -f $(COMPOSE_DEBUG) down 2>/dev/null || true
	docker compose --project-directory . -f $(COMPOSE_WEB) down 2>/dev/null || true
	docker rm -f magi-dev-nginx 2>/dev/null || true
	@echo "All dev/web containers stopped."
