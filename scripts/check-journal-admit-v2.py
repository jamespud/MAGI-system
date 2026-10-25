#!/usr/bin/env python3
"""Offline static checker for the additive JOURNAL_ADMIT v2 design change.

This script never issues authority, creates a provider reservation, exposes
material, commits settlement or starts a transaction.
"""
import copy
from datetime import datetime
import hashlib
import json
from pathlib import Path
import subprocess
import sys

ROOT = Path(__file__).resolve().parents[1]
TRACE_PATH = ROOT / 'docs/contracts/authority-journal-v2/traces.json'
SCHEMA_PATH = ROOT / 'docs/contracts/authority-journal-v2/schema.json'
ROLES = {'melchior', 'balthasar', 'casper'}
MAX_PENDING = 512
EPOCH_KINDS = ('AUTHORITY', 'CONTEXT', 'CREDENTIAL')
SETTLEMENT_DIGEST_DOMAIN = b'magi:journal-settlement-v2\n'
ADMISSION_MUTATIONS = [
    'admission', 'dependency_edges', 'exposure_fence', 'journal_event',
    'journal_high_watermark', 'lineage', 'outbox', 'pending_transition',
    'provider_reservation', 'token_consumed', 'unique_binding',
]
SETTLEMENT_MUTATIONS = [
    'lineage', 'outbox', 'pending_transition', 'provider_settlement',
    'token_consumed', 'unique_binding',
]


def utc(value):
    return datetime.strptime(value, '%Y-%m-%dT%H:%M:%S.%fZ')


def settlement_digest(plan):
    body = copy.deepcopy(plan)
    body.pop('settlement_digest', None)
    raw = json.dumps(body, sort_keys=True, separators=(',', ':'), ensure_ascii=False).encode('utf-8')
    return hashlib.sha256(SETTLEMENT_DIGEST_DOMAIN + raw).hexdigest()


def materialize(data, case):
    result = copy.deepcopy(data['bases'][case['base']])
    for patch in case['mutations']:
        target = result
        parts = patch['path'].split('/')
        for part in parts[:-1]:
            target = target[int(part)] if isinstance(target, list) else target[part]
        key = int(parts[-1]) if isinstance(target, list) else parts[-1]
        if patch.get('delete'):
            del target[key]
        else:
            target[key] = copy.deepcopy(patch['value'])
    if result.get('operation') == 'JOURNAL_SETTLE_V2' and case.get('rebind_settlement', True):
        settlement = result.get('settlement')
        if settlement is not None and 'receipt' in settlement:
            digest = settlement_digest(settlement)
            settlement['settlement_digest'] = digest
            result['request_digest'] = digest
            if result.get('existing_binding') is not None and not case.get('preserve_existing_binding_digest', False):
                result['existing_binding']['request_digest'] = digest
    return result


def scope_map(items):
    return {item['kind']: (item['scope_id'], item['epoch']) for item in items}


def check_common(s):
    admission = s['admission']
    authority = s['authority']
    provider = s['provider']
    if admission['key']['persona_id'] not in ROLES:
        return 'DENIED'
    if admission['execution_generation'] != authority['current_generation']:
        return 'STALE'
    if admission['execution_generation'] != admission['fence']['execution_generation']:
        return 'STALE'
    if len(admission['scoped_epochs']) != 3 or len(admission['fence']['scoped_epochs']) != 3:
        return 'STALE'
    admission_scopes = scope_map(admission['scoped_epochs'])
    fence_scopes = scope_map(admission['fence']['scoped_epochs'])
    if set(admission_scopes) != set(EPOCH_KINDS) or set(fence_scopes) != set(EPOCH_KINDS):
        return 'STALE'
    if admission_scopes != fence_scopes:
        return 'STALE'
    current = {
        'AUTHORITY': authority['current_authority_epoch'],
        'CONTEXT': authority['current_context_epoch'],
        'CREDENTIAL': authority['current_credential_epoch'],
    }
    if any(admission_scope[1] != current[kind] for kind, admission_scope in admission_scopes.items()):
        return 'STALE'
    if not provider['available'] or not provider['enforces_fence']:
        return 'BLOCKED'
    if not provider['enforces_revocation_watermark']:
        return 'BLOCKED'
    if admission['fence']['mode'] == 'PROVIDER_ENFORCED_LEASE' and not provider['supports_lease']:
        return 'BLOCKED'
    return ''


def check_admission(s):
    if s['exposure_gate'] is not None or s['settlement'] is not None:
        return 'WRITE_SET'
    admission = s['admission']
    authority = s['authority']
    existing = s['existing_binding']
    if admission['mutation_set'] != ADMISSION_MUTATIONS:
        return 'WRITE_SET'
    if admission['high_watermark']['next'] != admission['high_watermark']['expected'] + 1:
        return 'WRITE_SET'
    if admission['high_watermark']['journal_id'] != admission['pending_transition']['journal_id']:
        return 'WRITE_SET'
    pending = admission['pending_transition']
    if pending['expected'] >= MAX_PENDING or pending['next'] != pending['expected'] + 1:
        return 'WRITE_SET'
    if admission['provider_reservation']['request_id'] != admission['key']['provider_request_id']:
        return 'TOKEN_BINDING'
    if admission['provider_reservation']['expected_status'] != 'ABSENT' or admission['provider_reservation']['next_status'] != 'RESERVED':
        return 'WRITE_SET'
    if admission['fence']['lease_id'] == '' or admission['fence']['lease_until'] == '':
        return 'WRITE_SET'

    if existing is not None:
        if existing['request_digest'] != s['request_digest']:
            return 'CONFLICT'
        if existing['generation'] != admission['execution_generation']:
            return 'STALE'
        if existing['status'] == 'REVOKED':
            return 'REVOKED'
        if existing['status'] in ('ADMITTED', 'EXPOSURE_GATED', 'SETTLED'):
            # A replay may expose the original lineage, never a new permit.
            return 'REPLAY'
        return 'UNKNOWN'
    if authority['admission_sequence'] != 0:
        return 'CONFLICT'

    common = check_common(s)
    if common:
        return common
    if authority['revocation_sequence'] > 0:
        return 'REVOKED'
    if utc(authority['trusted_now']) >= utc(admission['fence']['lease_until']):
        return 'EXPIRED'
    if utc(admission['fence']['lease_until']) > utc(s['provider']['max_lease_until']):
        return 'BLOCKED'
    return 'STATIC_VALID'


def check_gate(s):
    if s['exposure_gate'] is None or s['settlement'] is not None:
        return 'WRITE_SET'
    admission = s['admission']
    gate = s['exposure_gate']
    authority = s['authority']
    existing = s['existing_binding']
    if existing is None or existing['request_digest'] != s['request_digest']:
        return 'CONFLICT'
    if existing['admission_commit_id'] != authority['admission_commit_id']:
        return 'TOKEN_BINDING'
    if gate['admission_commit_id'] != authority['admission_commit_id']:
        return 'TOKEN_BINDING'
    if gate['fence_id'] != admission['fence']['lease_id']:
        return 'TOKEN_BINDING'
    if authority['admission_sequence'] == 0:
        return 'CONFLICT'
    if gate['linearization_sequence'] <= authority['admission_sequence']:
        return 'STALE'
    if gate['trusted_now'] != authority['trusted_now']:
        return 'STALE'

    common = check_common(s)
    if common:
        return common
    if authority['revocation_sequence'] > 0 and authority['revocation_sequence'] <= gate['linearization_sequence']:
        return 'REVOKED'
    if utc(authority['trusted_now']) >= utc(admission['fence']['lease_until']):
        return 'EXPIRED'
    if utc(authority['trusted_now']) > utc(s['provider']['max_lease_until']):
        return 'BLOCKED'
    if authority['gate_sequence'] > 0:
        if authority['gate_sequence'] == gate['linearization_sequence'] and existing['status'] == 'EXPOSURE_GATED':
            return 'REPLAY'
        return 'CONFLICT'
    if existing['status'] != 'ADMITTED':
        return 'CONFLICT'
    return 'STATIC_VALID'


def check_settlement(s):
    if s['exposure_gate'] is None or s['settlement'] is None:
        return 'WRITE_SET'
    admission = s['admission']
    settlement = s['settlement']
    gate = s['exposure_gate']
    authority = s['authority']
    existing = s['existing_binding']
    if settlement['mutation_set'] != SETTLEMENT_MUTATIONS:
        return 'WRITE_SET'
    expected_digest = settlement_digest(settlement)
    if settlement.get('settlement_digest') != expected_digest:
        return 'TOKEN_BINDING'
    if s['request_digest'] != expected_digest:
        return 'TOKEN_BINDING'
    if settlement['pending_transition']['journal_id'] != admission['high_watermark']['journal_id']:
        return 'WRITE_SET'
    if settlement['admission_commit_id'] != authority['admission_commit_id']:
        return 'TOKEN_BINDING'
    if gate['admission_commit_id'] != settlement['admission_commit_id']:
        return 'TOKEN_BINDING'
    if gate['fence_id'] != admission['fence']['lease_id']:
        return 'TOKEN_BINDING'
    if settlement['provider_request_id'] != admission['key']['provider_request_id']:
        return 'TOKEN_BINDING'
    if settlement['receipt']['provider_request_id'] != admission['key']['provider_request_id']:
        return 'TOKEN_BINDING'
    if settlement['receipt']['session_scope_id'] != admission['provider_reservation']['session_scope_id']:
        return 'TOKEN_BINDING'
    if existing is None or existing['request_digest'] != expected_digest:
        return 'CONFLICT'
    if existing['admission_commit_id'] != authority['admission_commit_id']:
        return 'TOKEN_BINDING'
    if authority['gate_sequence'] == 0:
        return 'CONFLICT'
    if gate['linearization_sequence'] != authority['gate_sequence']:
        return 'CONFLICT'

    if not settlement['receipt']['verified']:
        return 'DENIED'
    outcome = settlement['receipt']['outcome']
    if outcome == 'APPLIED_VERIFIED':
        if settlement['next_status'] != 'APPLIED_VERIFIED':
            return 'CONFLICT'
    elif outcome in ('UNKNOWN', 'UNVERIFIABLE'):
        if settlement['next_status'] != 'UNVERIFIABLE':
            return 'CONFLICT'
    else:
        return 'CONFLICT'

    pending = settlement['pending_transition']
    if outcome == 'APPLIED_VERIFIED':
        if pending['expected'] <= 0 or pending['expected'] > MAX_PENDING or pending['next'] != pending['expected'] - 1:
            return 'WRITE_SET'
    elif pending['next'] != pending['expected']:
        # UNKNOWN and UNVERIFIABLE preserve the possibly exposed delivery.
        return 'WRITE_SET'

    if authority['revocation_sequence'] > 0:
        if authority['revocation_sequence'] <= authority['gate_sequence']:
            return 'REVOKED'
        if authority['settlement_sequence'] == 0:
            return 'UNKNOWN'
    if authority['settlement_sequence'] > 0:
        if existing['status'] == 'SETTLED':
            return 'REPLAY'
        return 'CONFLICT'
    if existing['status'] not in ('EXPOSURE_GATED', 'SETTLED'):
        return 'CONFLICT'
    return 'STATIC_VALID'


def check_semantics(s):
    if s['contract_version'] != 'magi-authority-journal-v2/1':
        return 'WRITE_SET'
    operation = s['operation']
    if operation == 'JOURNAL_ADMIT_V2':
        return check_admission(s)
    if operation == 'JOURNAL_EXPOSURE_GATE_V2':
        return check_gate(s)
    if operation == 'JOURNAL_SETTLE_V2':
        return check_settlement(s)
    return 'WRITE_SET'


def schema_results(values, schema_path=None):
    result = subprocess.run(
        ['go', 'run', '../scripts/check-journal-admit-v2.go', '../scripts/journal-admit-v2-api.go',
         str(schema_path or SCHEMA_PATH)],
        cwd=ROOT / 'backend', input=json.dumps(values), capture_output=True, text=True,
    )
    if result.returncode:
        raise RuntimeError('schema oracle failed (not an accepted negative): ' + result.stderr)
    return json.loads(result.stdout)


def main():
    data = json.loads(TRACE_PATH.read_text())
    cases = data['cases']
    values = [materialize(data, case) for case in cases]
    shapes = schema_results(values)
    if len(shapes) != len(cases):
        raise RuntimeError('schema oracle result count mismatch')
    failed = []
    for case, value, shape in zip(cases, values, shapes):
        actual = shape or check_semantics(value)
        if actual != case['expected']:
            failed.append(f'{case["name"]}: expected {case["expected"]}, got {actual}')
    if failed:
        print('\n'.join(failed), file=sys.stderr)
        return 1
    print(f'PASS {len(cases)} JOURNAL_ADMIT v2 static declarations; live authority/provider/exposure/settlement NOT RUN')
    return 0


if __name__ == '__main__':
    sys.exit(main())
