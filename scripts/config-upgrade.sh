#!/usr/bin/env bash
# config-upgrade.sh — merge missing fields from magi.yaml.example into
# backend/conf/magi.yaml, backing up the existing file first. Read-only on the
# config beyond adding missing keys; never overwrites existing values.
#
# Usage: scripts/config-upgrade.sh
set -e

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
CFG="$ROOT/backend/conf/magi.yaml"
EX="$ROOT/backend/conf/magi.yaml.example"

if [ ! -f "$EX" ]; then
  echo "missing template: $EX"
  exit 1
fi

if [ ! -f "$CFG" ]; then
  cp "$EX" "$CFG"
  echo "OK created magi.yaml from example."
  exit 0
fi

python3 - "$CFG" "$EX" <<'PY'
import sys, shutil, yaml, copy
cfg_path, ex_path = sys.argv[1], sys.argv[2]
user = yaml.safe_load(open(cfg_path, encoding="utf-8")) or {}
ex   = yaml.safe_load(open(ex_path, encoding="utf-8")) or {}
added = []
def merge(target, source, path=""):
    for key, value in source.items():
        key_path = f"{path}.{key}" if path else key
        if key not in target:
            target[key] = copy.deepcopy(value)
            added.append(key_path)
        elif isinstance(value, dict) and isinstance(target[key], dict):
            merge(target[key], value, key_path)
merge(user, ex)
shutil.copy2(cfg_path, cfg_path + ".bak")
yaml.dump(user, open(cfg_path, "w", encoding="utf-8"), sort_keys=False, allow_unicode=True)
print(f"OK merged {len(added)} missing field(s); backup saved to magi.yaml.bak.")
PY
