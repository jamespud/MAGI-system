#!/usr/bin/env python3
"""Execute parity checker against semantic and formatting drift regressions."""
import copy
import importlib.util
import json
from pathlib import Path
ROOT = Path(__file__).resolve().parents[1]
spec = importlib.util.spec_from_file_location('parity', ROOT / 'scripts/check-readset-schema-parity.py')
p = importlib.util.module_from_spec(spec)
spec.loader.exec_module(p)
raw = (ROOT / 'docs/contracts/decision-artifacts-v2/schema.json').read_bytes()
p.check(raw, raw)
s = json.loads(raw)
cases = []
for name, field in [('ReadEvent', 'sequence'), ('SourceNode', 'source_version'), ('ReadSetSnapshot', 'schema_version'), ('ExternalRef', 'version')]:
    m = copy.deepcopy(s)
    m['$defs'][name]['properties'][field] = {'type': 'string'}
    cases.append(json.dumps(m).encode())
m = copy.deepcopy(s)
m['$defs']['ReadEvent']['additionalProperties'] = True
cases.extend([json.dumps(m).encode(), raw + b'\n'])
for changed in cases:
    try:
        p.check(raw, changed)
    except ValueError:
        continue
    raise AssertionError('parity checker accepted drift')
print(f'PASS: parity checker rejects {len(cases)} independent ReadSet/transitive/format drift cases')
