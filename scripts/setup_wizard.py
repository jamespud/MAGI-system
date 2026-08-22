#!/usr/bin/env python3
"""Interactive setup wizard (make setup).

Walks through the external-API credentials MAGI needs (LLM / Embedding / Web
search / RAG backends) and writes them into .env (the single secret source).
If backend/conf/magi.yaml is missing it is copied from the example so the
agents/roles config is present; existing magi.yaml is never overwritten.

Non-interactive runs print manual-editing guidance and exit 1.

Usage:
    python3 scripts/setup_wizard.py
"""

from __future__ import annotations

import shutil
import sys
from pathlib import Path

ROOT = Path(__file__).resolve().parent.parent
ENV_PATH = ROOT / ".env"
ENV_EXAMPLE = ROOT / ".env.example"
CONFIG_PATH = ROOT / "backend/conf/magi.yaml"
CONFIG_EXAMPLE = ROOT / "backend/conf/magi.yaml.example"


def _is_interactive() -> bool:
    return sys.stdin.isatty() and sys.stdout.isatty()


def _read_env(env_path: Path) -> dict[str, str]:
    result: dict[str, str] = {}
    if not env_path.exists():
        return result
    for line in env_path.read_text(encoding="utf-8").splitlines():
        line = line.strip()
        if not line or line.startswith("#") or "=" not in line:
            continue
        key, _, value = line.partition("=")
        result[key.strip()] = value.strip()
    return result


def _write_env(env_path: Path, pairs: dict[str, str]) -> None:
    """Merge pairs into an existing/new .env: update keys in place, append new,
    preserve comments and unrelated lines."""
    lines: list[str] = []
    if env_path.exists():
        lines = env_path.read_text(encoding="utf-8").splitlines()

    updated: set[str] = set()
    new_lines: list[str] = []
    for line in lines:
        stripped = line.strip()
        if stripped and not stripped.startswith("#") and "=" in stripped:
            key = stripped.split("=", 1)[0].strip()
            if key in pairs:
                new_lines.append(f"{key}={pairs[key]}")
                updated.add(key)
                continue
        new_lines.append(line)

    for key, value in pairs.items():
        if key not in updated:
            new_lines.append(f"{key}={value}")

    env_path.write_text("\n".join(new_lines) + "\n", encoding="utf-8")


def _ask(label: str, default: str = "") -> str:
    prompt = f"{label}" + (f" [{default}]" if default else "") + ": "
    value = input(prompt).strip()
    return value or default


def _print_header(text: str) -> None:
    print(f"\n=== {text} ===")


def main() -> int:
    if not _is_interactive():
        print(
            "Non-interactive environment detected.\n"
            "Please edit .env and backend/conf/magi.yaml directly, or run 'make setup' in a terminal."
        )
        return 1

    print("\nWelcome to MAGI Setup!")
    print("This wizard collects the external-API credentials MAGI needs.")
    print("Secrets are written to .env; functional config lives in backend/conf/magi.yaml.\n")

    _print_header("1/3 LLM (required)")
    model_base = _ask("Base URL", "https://api.deepseek.com")
    model_name = _ask("Model name", "deepseek-v4-flash")
    model_key = _ask("API key").strip()
    if not model_key:
        print("  WARN: no key provided — decisions will fail until MAGI_MODEL_API_KEY is set.")

    _print_header("2/3 Embedding (RAG; empty disables RAG)")
    emb_base = _ask("Base URL", "")
    emb_model = _ask("Model name", "BAAI/bge-m3")
    emb_key = _ask("API key (optional)").strip()

    _print_header("3/3 Web search & RAG backends (optional)")
    tavily_key = _ask("Tavily API key (optional)", "")
    milvus_addr = _ask("Milvus address (e.g. localhost:19530; empty = fake)", "")
    es_addrs = _ask("Elasticsearch address (e.g. http://localhost:9200; empty = fake)", "")

    pairs: dict[str, str] = {}
    if model_key:
        pairs["MAGI_MODEL_API_KEY"] = model_key
    pairs["MAGI_MODEL_BASE_URL"] = model_base
    pairs["MAGI_MODEL_NAME"] = model_name
    if emb_key:
        pairs["MAGI_EMBEDDING_API_KEY"] = emb_key
    if emb_base:
        pairs["MAGI_EMBEDDING_BASE_URL"] = emb_base
    pairs["MAGI_EMBEDDING_MODEL_NAME"] = emb_model
    if tavily_key:
        pairs["MAGI_TAVILY_API_KEY"] = tavily_key
    if milvus_addr:
        pairs["MAGI_MILVUS_ADDRESS"] = milvus_addr
    if es_addrs:
        pairs["MAGI_ES_ADDRESSES"] = es_addrs

    if not ENV_PATH.exists() and ENV_EXAMPLE.exists():
        shutil.copyfile(ENV_EXAMPLE, ENV_PATH)
        print("\n.env created from .env.example")
    _write_env(ENV_PATH, pairs)
    print(f"Secrets written to {ENV_PATH.relative_to(ROOT)}")

    if not CONFIG_PATH.exists():
        if CONFIG_EXAMPLE.exists():
            shutil.copyfile(CONFIG_EXAMPLE, CONFIG_PATH)
            print(f"{CONFIG_PATH.relative_to(ROOT)} created from example (edit model/roles as needed)")
        else:
            print(f"WARN: {CONFIG_EXAMPLE.name} not found; skipping config generation")
    else:
        print(f"{CONFIG_PATH.relative_to(ROOT)} already exists — left unchanged (secrets still come from .env)")

    print("\nSetup complete!")
    print(f"  LLM:        {model_base} / {model_name}")
    print(f"  Embedding:  {'enabled' if emb_base else 'disabled (fake RAG)'}")
    print(f"  Web search: {'enabled' if tavily_key else 'disabled'}")
    print()
    print("Next steps:")
    print("  make install    # Install dependencies (first time only)")
    print("  make docker-init# Pre-pull middleware images (optional)")
    print("  make dev        # Start hot-reload dev stack")
    print("  make start      # Start production-ish local stack")
    print()
    print("Run 'make doctor' to verify your setup at any time.")
    return 0


if __name__ == "__main__":
    sys.exit(main())
