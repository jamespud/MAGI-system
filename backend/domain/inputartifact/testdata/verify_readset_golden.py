#!/usr/bin/env python3
"""Independent full ReadSet v2 C0 bytes/digest/id; synthetic, offline only."""
import hashlib
import importlib.util
import json
from pathlib import Path
import sys
HERE = Path(__file__).resolve().parent
ROOT = HERE.parents[3]
spec = importlib.util.spec_from_file_location('canonical_reference', ROOT / 'backend/domain/canonical/testdata/verify_golden.py')
reference = importlib.util.module_from_spec(spec)
spec.loader.exec_module(reference)
def main():
    write = sys.argv[1:] == ['--write']
    assert write or not sys.argv[1:]
    fixture = reference.parse((HERE / 'readset_v2_fixture.json').read_bytes())
    body = fixture['body']
    assert body['artifact_type'] == 'read_set_snapshot' and body['schema_version'] == '2.0'
    assert body['journal_high_watermark'] == len(body['events']) == 2
    for event in body['events']:
        assert event['source'] in body['dependency_nodes']
        assert event['journal_key'] == body['journal_key']
    for record in fixture['records'].values():
        assert hashlib.sha256(reference.canonical_bytes(record['body'])).hexdigest() == record['ref']['content_digest']
    candidate = fixture['candidate']
    cb = reference.canonical_bytes(candidate['body'])
    assert candidate['artifact_id'] == 'csnp_' + hashlib.sha256(b'magi:canonical:v1:candidate_snapshot\n' + cb).hexdigest()
    raw = reference.canonical_bytes(body)
    digest = hashlib.sha256(b'magi:canonical:v1:read_set_snapshot\n' + raw).hexdigest()
    golden = dict(canonical_profile='magi-cj/1', digest_alg='sha256', digest=digest, artifact_id='rset_' + digest, canonical_bytes=len(raw), actual_reads='NOT_RUN', qualification='UNVERIFIABLE_REFERENCE')
    cp, gp = HERE / 'readset_v2.canonical.json', HERE / 'readset_v2.golden.json'
    if write:
        cp.write_bytes(raw)
        gp.write_text(json.dumps(golden, indent=2) + '\n')
    else:
        assert cp.read_bytes() == raw
        assert json.loads(gp.read_text()) == golden
    print('PASS: full-body ReadSet v2 Python canonical bytes/digest/rset and supplied immutable record/candidate identities; offline only')
if __name__ == '__main__':
    main()
