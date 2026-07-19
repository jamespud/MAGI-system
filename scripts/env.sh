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
