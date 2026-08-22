#!/usr/bin/env python3
"""Dependency check (make check).

Reports presence (and, where useful, version) of the tools MAGI's dev
environment needs. Read-only; never installs anything.

Usage:
    python3 scripts/check.py
"""

from __future__ import annotations

import shutil
import subprocess
import sys


def version_of(command: list[str]) -> str | None:
    try:
        result = subprocess.run(command, capture_output=True, text=True, check=True)
        return (result.stdout or result.stderr).strip()
    except Exception:
        return None


def main() -> int:
    checks = [
        ("go", ["go", "version"]),
        ("node", ["node", "-v"]),
        ("npm", ["npm", "-v"]),
        ("docker", ["docker", "--version"]),
        ("python3", ["python3", "--version"]),
    ]

    failed = False
    for name, command in checks:
        if shutil.which(name) is None:
            print(f"  [MISSING] {name}")
            failed = True
            continue
        version = version_of(command)
        print(f"  [OK] {name}: {version or 'found'}")

    if failed:
        print("FAIL some required tools are missing")
        return 1

    print("All required tools present.")
    return 0


if __name__ == "__main__":
    sys.exit(main())
