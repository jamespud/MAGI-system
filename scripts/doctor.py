#!/usr/bin/env python3
"""Read-only health report (make doctor).

Checks system requirements, config presence, and the external-API credentials
MAGI resolves at runtime. Because applyEnvOverrides lets a non-empty MAGI_*
environment value override the YAML field, "effective" here means: env value if
set, otherwise the YAML value. Reports a split-brain warning when an inline YAML
secret would be shadowed by a placeholder env value.

Never mutates config or installs anything.

Exit codes:
    0 — all required checks passed (warnings allowed)
    1 — one or more required checks failed

Usage:
    python3 scripts/doctor.py
"""

from __future__ import annotations

import shutil
import subprocess
import sys
from pathlib import Path

try:
    import yaml  # type: ignore[import-not-found]
except ImportError:
    yaml = None

ROOT = Path(__file__).resolve().parent.parent
CONFIG = ROOT / "backend/conf/magi.yaml"
ENV_FILE = ROOT / ".env"
PLACEHOLDER = "sk-your-api-key-here"


def _env(env_path: Path) -> dict[str, str]:
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


def _status(value: str | None) -> str:
    if not value:
        return "empty"
    if value == PLACEHOLDER:
        return "placeholder"
    return "set"


def _report_cred(label: str, env_key: str, yaml_value: str | None) -> None:
    env_value = envs.get(env_key)
    env_st = _status(env_value)
    yaml_st = _status(yaml_value)
    # Effective value: env wins if non-empty (applyEnvOverrides semantics).
    effective = env_value if env_value else yaml_value
    eff_st = _status(effective)

    if eff_st == "set":
        print(f"  [OK]   {label}: set")
    elif eff_st == "placeholder":
        if yaml_st == "set" and env_st == "placeholder":
            print(f"  [WARN] {label}: {env_key} is a placeholder and will SHADOW the real key in magi.yaml")
            print(f"         set {env_key} in .env (migrate via 'make setup' or copy the value)")
        else:
            print(f"  [WARN] {label}: placeholder in {env_key} — no usable key")
    else:
        print(f"  [SKIP] {label}: empty (disabled)" if env_key != "MAGI_MODEL_API_KEY" else f"  [FAIL] {label}: no key set")


def _run(cmd: list[str]) -> str | None:
    try:
        result = subprocess.run(cmd, capture_output=True, text=True, check=True)
        return (result.stdout or result.stderr).strip()
    except Exception:
        return None


envs: dict[str, str] = {}


def main() -> int:
    global envs
    failed = False

    print("== System requirements ==")
    required = [("go", ["go", "version"]), ("node", ["node", "-v"]),
                ("npm", ["npm", "-v"]), ("docker", ["docker", "--version"])]
    for name, command in required:
        if shutil.which(name) is None:
            print(f"  [FAIL] {name} is missing")
            failed = True
        else:
            print(f"  [OK]   {name}: {_run(command) or 'found'}")
    if shutil.which("python3") is None:
        print("  [FAIL] python3 is required for setup/check/doctor")
        failed = True
    else:
        print(f"  [OK]   python3: {_run(['python3', '--version']) or 'found'}")

    print("\n== Configuration ==")
    if not CONFIG.exists():
        print(f"  [FAIL] {CONFIG} not found")
        print("         run 'make config' or 'make setup' to generate it.")
        failed = True
    else:
        print(f"  [OK]   {CONFIG.name} present")

    envs = _env(ENV_FILE) if ENV_FILE.exists() else {}

    cfg: dict = {}
    if yaml is None:
        print("  [WARN] PyYAML not available; skipping config field report")
    elif CONFIG.exists():
        try:
            cfg = yaml.safe_load(CONFIG.read_text(encoding="utf-8")) or {}
        except Exception as exc:
            print(f"  [FAIL] {CONFIG.name} is invalid YAML: {exc}")
            failed = True

    print("\n== External API credentials (effective = env override > yaml) ==")
    _report_cred("model", "MAGI_MODEL_API_KEY", cfg.get("model", {}).get("api_key"))
    _report_cred("embedding", "MAGI_EMBEDDING_API_KEY", cfg.get("embedding", {}).get("api_key"))
    _report_cred("web search", "MAGI_TAVILY_API_KEY", cfg.get("tavily", {}).get("api_key"))
    # Model is the one hard requirement.
    if _status(envs.get("MAGI_MODEL_API_KEY") if envs.get("MAGI_MODEL_API_KEY") else cfg.get("model", {}).get("api_key")) != "set":
        if not (cfg.get("model", {}).get("api_key") and _status(cfg.get("model", {}).get("api_key")) == "set"):
            print("  [FAIL] model: no usable api_key (set MAGI_MODEL_API_KEY in .env or model.api_key in magi.yaml)")
            failed = True

    print("\n== RAG backends ==")
    if cfg.get("milvus", {}).get("address") or envs.get("MAGI_MILVUS_ADDRESS"):
        print("  [OK]   milvus.address configured")
    else:
        print("  [SKIP] milvus.address empty — fake vector index")
    if cfg.get("elasticsearch", {}).get("addresses") or envs.get("MAGI_ES_ADDRESSES"):
        print("  [OK]   elasticsearch.addresses configured")
    else:
        print("  [SKIP] elasticsearch.addresses empty — fake lexical index")

    print()
    if failed:
        print("doctor: FAIL (fix the reported issues, then run 'make doctor' again)")
        return 1
    print("doctor: OK")
    return 0


if __name__ == "__main__":
    sys.exit(main())
