#!/usr/bin/env python3
"""Guard the frozen v1 ContextManifest mirror, including transitive definitions.

PR #39 intentionally strengthens exactly four Token references to FixedVersion.
They are named below; blanket Token/FixedVersion normalization would hide drift.
This checks schema declarations, not runtime or external source qualification.
"""
import copy
import json
from pathlib import Path
import sys

ROOT = Path(__file__).resolve().parents[1]
REFINEMENTS = (
    ('ContextManifest', 'source_dependency_index_version'),
    ('SnapshotRef', 'version'),
    ('ExternalRef', 'version'),
    ('SubjectRef', 'identity_source_version'),
)
ALIASES = ('latest', 'current', 'stable', 'main', 'master', 'head',
           'default', 'active', 'newest', 'next', 'x')


def require(value, detail):
    if not value:
        raise ValueError('SCHEMA_DRIFT: ' + detail)


def check(left, right):
    source, mirror = left['$defs'], right['$defs']
    expected = copy.deepcopy(source)
    for definition, field in REFINEMENTS:
        prop = expected[definition]['properties'][field]
        require(prop == {'$ref': '#/$defs/Token'}, f'{definition}.{field} source refinement changed')
        expected[definition]['properties'][field] = {'$ref': '#/$defs/FixedVersion'}
    pattern = '(^|[^A-Za-z0-9])(' + '|'.join(
        ''.join('[' + c.upper() + c + ']' for c in word) for word in ALIASES) + ')([^A-Za-z0-9]|$)'
    fixed = {k: v for k, v in mirror['FixedVersion'].items() if k != 'description'}
    require(fixed == {'allOf': [{'$ref': '#/$defs/Token'}, {'not': {'pattern': pattern}}]},
            'FixedVersion alias semantics changed')
    seen = set()

    def walk(value):
        if isinstance(value, dict):
            if '$ref' in value:
                ref = value['$ref']
                require(ref.startswith('#/$defs/'), 'nonlocal reference ' + ref)
                name = ref.removeprefix('#/$defs/')
                if name not in seen:
                    seen.add(name)
                    require(name in expected and name in mirror, 'missing definition ' + name)
                    require(expected[name] == mirror[name], 'transitive definition ' + name)
                    walk(expected[name])
            for child in value.values():
                walk(child)
        elif isinstance(value, list):
            for child in value:
                walk(child)

    # FixedVersion is the only newly reachable definition, with the fully
    # checked semantics above. Its presence must not suppress Token comparison.
    expected['FixedVersion'] = copy.deepcopy(mirror['FixedVersion'])
    require(expected['ContextManifest'] == mirror['ContextManifest'], 'ContextManifest definition')
    walk(expected['ContextManifest'])
    return len(seen) + 1


def main():
    args = sys.argv[1:]
    require(len(args) in (0, 2), 'usage: check-context-schema-parity [input-schema mirror-schema]')
    paths = list(map(Path, args)) if args else [
        ROOT / 'docs/contracts/input-artifacts/schema.json',
        ROOT / 'docs/contracts/decision-artifacts-v2/schema.json']
    count = check(*(json.loads(p.read_text()) for p in paths))
    print(f'PASS: ContextManifest v1: {count} transitive definitions; 4 named FixedVersion refinements')


if __name__ == '__main__':
    try:
        main()
    except (ValueError, KeyError, TypeError) as error:
        print('SCHEMA_DRIFT: ' + str(error), file=sys.stderr)
        sys.exit(1)
