# =============================================================================
# MAGI Makefile — Engineering Entry Point
# =============================================================================

.DEFAULT_GOAL := help

SCRIPTS_DIR := scripts

.PHONY: help prepare backend frontend debug server build \
        test lint fmt vet tidy \
        migrate seed reset-db \
        docker-up docker-down docker-logs \
        up down logs ps \
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
	@echo "Full stack (containers)"
	@echo "  make up               Build + start mysql + magi-server + web"
	@echo "  make down             Stop the full stack"
	@echo "  make logs             Tail full-stack logs"
	@echo "  make ps               Show stack container status"
	@echo ""
	@echo "Docker (dev middleware)"
	@echo "  make docker-up        Start MySQL middleware (for make debug)"
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

# =============================================================================
# Full stack (containerized: mysql + migrate + magi-server + web)
# =============================================================================

COMPOSE_STACK := docker/docker-compose.stack.yml

up:
	docker compose --project-directory . -f $(COMPOSE_STACK) up -d --build

down:
	docker compose --project-directory . -f $(COMPOSE_STACK) down

logs:
	docker compose --project-directory . -f $(COMPOSE_STACK) logs -f

ps:
	docker compose --project-directory . -f $(COMPOSE_STACK) ps
