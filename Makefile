# =============================================================================
# MAGI - Unified Development Environment
#
# Mirrors deer-flow's Makefile: a single entry point with categorized help,
# OS detection, per-component Makefile delegation, and thin shell/Python
# scripts as executors.
# =============================================================================

.DEFAULT_GOAL := help

# ---------------------------------------------------------------------------
# Environment
# ---------------------------------------------------------------------------
ifeq ($(OS),Windows_NT)
    SHELL := cmd.exe
else
    SHELL := /bin/bash
endif

BASH ?= bash
PYTHON ?= python3
SCRIPTS_DIR := scripts
BACKEND := backend
FRONTEND := frontend

# ---------------------------------------------------------------------------
# Targets
# ---------------------------------------------------------------------------
.PHONY: help setup doctor check install config config-upgrade \
        dev start stop clean \
        backend frontend \
        up down \
        docker-init docker-start docker-stop docker-logs \
        test fmt vet tidy lint

# ---------------------------------------------------------------------------
# Help
# ---------------------------------------------------------------------------
help:
	@echo ""
	@echo "============================ MAGI ============================"
	@echo ""
	@echo "Setup & Bootstrap"
	@echo "  make setup          - Interactive wizard: collect model/embedding/search/RAG -> .env + magi.yaml"
	@echo "  make doctor         - Read-only health report (deps, config, credentials)"
	@echo "  make check          - Check required tools are installed"
	@echo "  make install        - Install dependencies (go mod download + npm install)"
	@echo "  make config         - Copy config templates if missing (non-interactive)"
	@echo "  make config-upgrade - Merge missing fields from magi.yaml.example (backup first)"
	@echo ""
	@echo "Local Dev"
	@echo "  make dev            - Hot-reload stack: middleware + vite (:5173) + backend (:8080)"
	@echo "  make start          - Production-ish local: middleware + nginx (:80) + go binary"
	@echo "  make backend        - Backend only (go run :8080)"
	@echo "  make frontend       - Frontend only (vite dev :5173)"
	@echo "  make stop           - Stop local services"
	@echo "  make clean          - Remove build artifacts (bin/, frontend/dist/)"
	@echo ""
	@echo "Containerized Stack"
	@echo "  make up             - Build + start full web stack (mysql + server + nginx + RAG)"
	@echo "  make down           - Stop + remove full web stack"
	@echo ""
	@echo "Middleware (MySQL + Milvus + Elasticsearch)"
	@echo "  make docker-init    - Pull middleware images + create RAG volumes"
	@echo "  make docker-start   - Start dev middleware"
	@echo "  make docker-stop    - Stop dev middleware"
	@echo "  make docker-logs    - Follow dev middleware logs"
	@echo ""
	@echo "Quality"
	@echo "  make test           - Run all tests (Go + frontend)"
	@echo "  make fmt            - Format Go code"
	@echo "  make vet            - Static analysis (Go vet)"
	@echo "  make tidy           - Tidy Go modules"
	@echo "  make lint           - Lint backend + frontend"
	@echo ""
	@echo "=============================================================="

# ---------------------------------------------------------------------------
# Setup & Bootstrap
# ---------------------------------------------------------------------------
setup:
	$(PYTHON) $(SCRIPTS_DIR)/setup_wizard.py

doctor:
	$(PYTHON) $(SCRIPTS_DIR)/doctor.py

check:
	$(PYTHON) $(SCRIPTS_DIR)/check.py

install:
	@mkdir -p bin
	go -C $(BACKEND) mod download
	npm -C $(FRONTEND) install

config:
	$(PYTHON) $(SCRIPTS_DIR)/configure.py

config-upgrade:
	bash $(SCRIPTS_DIR)/config-upgrade.sh

# ---------------------------------------------------------------------------
# Local Dev
# ---------------------------------------------------------------------------
dev:
	bash $(SCRIPTS_DIR)/serve.sh --dev

start:
	bash $(SCRIPTS_DIR)/serve.sh --prod

backend:
	$(MAKE) -C $(BACKEND) run

frontend:
	$(MAKE) -C $(FRONTEND) dev

stop:
	bash $(SCRIPTS_DIR)/serve.sh --stop

clean:
	bash $(SCRIPTS_DIR)/build.sh clean

# ---------------------------------------------------------------------------
# Containerized Stack
# ---------------------------------------------------------------------------
up:
	bash $(SCRIPTS_DIR)/deploy.sh up

down:
	bash $(SCRIPTS_DIR)/deploy.sh down

# ---------------------------------------------------------------------------
# Middleware (MySQL + Milvus + Elasticsearch)
# ---------------------------------------------------------------------------
docker-init:
	bash $(SCRIPTS_DIR)/docker.sh init

docker-start:
	bash $(SCRIPTS_DIR)/docker.sh start

docker-stop:
	bash $(SCRIPTS_DIR)/docker.sh stop

docker-logs:
	bash $(SCRIPTS_DIR)/docker.sh logs

# ---------------------------------------------------------------------------
# Quality
# ---------------------------------------------------------------------------
test:
	$(MAKE) -C $(BACKEND) test
	$(MAKE) -C $(FRONTEND) test

fmt:
	$(MAKE) -C $(BACKEND) fmt

vet:
	$(MAKE) -C $(BACKEND) vet

tidy:
	$(MAKE) -C $(BACKEND) tidy

lint:
	$(MAKE) -C $(BACKEND) lint
	$(MAKE) -C $(FRONTEND) lint
