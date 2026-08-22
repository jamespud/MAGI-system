#!/usr/bin/env python3
"""Copy-if-missing config bootstrap (make config).

Creates backend/conf/magi.yaml and .env from their .example templates if absent.
Aborts when either already exists, so existing local config is never overwritten.

Usage:
    python3 scripts/configure.py
"""

from __future__ import annotations

import shutil
import sys
from pathlib import Path


def copy_if_missing(src: Path, dst: Path) -> None:
    if dst.exists():
        return
    if not src.exists():
        raise FileNotFoundError(f"missing template: {src}")
    dst.parent.mkdir(parents=True, exist_ok=True)
    shutil.copyfile(src, dst)


def main() -> int:
    root = Path(__file__).resolve().parent.parent
    targets = [
        (root / "backend/conf/magi.yaml.example", root / "backend/conf/magi.yaml"),
        (root / ".env.example", root / ".env"),
    ]

    if any(dst.exists() for _, dst in targets):
        print("config already exists (backend/conf/magi.yaml or .env). Aborting.")
        return 1

    for src, dst in targets:
        try:
            copy_if_missing(src, dst)
        except (FileNotFoundError, OSError) as exc:
            print(f"error generating configuration files: {exc}")
            return 1

    print("OK configuration files generated.")
    return 0


if __name__ == "__main__":
    sys.exit(main())
