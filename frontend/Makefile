# =============================================================================
# MAGI frontend (React + Vite) - per-component Makefile
# Invoked by the root Makefile: make -C frontend <target>
# =============================================================================

NPM ?= npm

.PHONY: install dev build preview test test-watch lint clean

install:
	$(NPM) install

dev:
	$(NPM) run dev

build:
	$(NPM) run build

preview:
	$(NPM) run preview

test:
	$(NPM) run test

test-watch:
	$(NPM) run test:watch

lint:
	$(NPM) run lint

clean:
	rm -rf dist
