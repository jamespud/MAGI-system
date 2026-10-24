#!/usr/bin/env python3
"""Require the offline ReadSet consumer to embed the exact frozen v2 Schema."""
import json
from pathlib import Path
import sys
ROOT = Path(__file__).resolve().parents[1]
def check(source, mirror):
    if source != mirror:
        raise ValueError('SCHEMA_DRIFT: frozen Decision v2 and ReadSet consumer mirror differ')
    s = json.loads(source)
    if s['$defs']['ReadSetSnapshot']['properties']['schema_version'] != {'const': '2.0'}:
        raise ValueError('SCHEMA_DRIFT: ReadSet v2 required')
    return len(s['$defs'])
def main():
    args = sys.argv[1:]
    if len(args) not in (0, 2):
        raise ValueError('usage: check-readset-schema-parity [source mirror]')
    paths = list(map(Path, args)) if args else [ROOT / 'docs/contracts/decision-artifacts-v2/schema.json', ROOT / 'backend/domain/inputartifact/readset_schema.json']
    count = check(*(p.read_bytes() for p in paths))
    print(f'PASS: exact frozen ReadSet v2 Schema mirror including all {count} definitions')
if __name__ == '__main__':
    try:
        main()
    except (ValueError, KeyError) as error:
        print(str(error), file=sys.stderr)
        sys.exit(1)
