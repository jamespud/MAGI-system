#!/usr/bin/env python3
"""Offline declarations ONLY. No issuer, authority success, database or send implementation."""
import copy
from datetime import datetime, timedelta
import hashlib
import json
from pathlib import Path
import subprocess
import sys

ROOT = Path(__file__).resolve().parents[1]
LOCK_ORDER = ['sequence', 'epochs', 'owner', 'round', 'journals', 'dependencies', 'token', 'unique_key', 'witness', 'gateway']
WRITE_SETS = {
    'JOURNAL_ADMIT': ['admission', 'dependency_edges', 'journal_event', 'journal_high_watermark', 'lineage', 'outbox', 'pending_transition', 'provider_settlement', 'token_consumed', 'unique_binding'],
    'QUIESCE': ['fence', 'journal_states', 'lineage', 'outbox', 'round_state', 'token_consumed', 'unique_binding'],
    'CANDIDATE_CLOSE': ['candidate', 'closure', 'evidence_closed', 'journal:balthasar', 'journal:casper', 'journal:melchior', 'lineage', 'outbox', 'token_consumed', 'unique_binding'],
    'SEND_COMMIT': ['action_transition', 'lineage', 'logical_operation_binding', 'outbox', 'quota', 'send_record', 'token_consumed', 'unique_binding'],
}
ROLES = {'melchior', 'balthasar', 'casper'}

def digest(value, domain):
    # Diagnostic transcript commitment, no new C0 artifact registration or signature.
    raw = json.dumps(value, sort_keys=True, separators=(',', ':'), ensure_ascii=False).encode()
    return hashlib.sha256(domain.encode() + b'\n' + raw).hexdigest()

def utc(value):
    return datetime.strptime(value, '%Y-%m-%dT%H:%M:%S.%fZ')

def scope_keys(items):
    return [(x['kind'], x['scope_id']) for x in items]

def materialize(data, case):
    result = copy.deepcopy(data['bases'][case['base']])
    for patch in case['mutations']:
        target = result
        parts = patch['path'].split('/')
        for part in parts[:-1]:
            target = target[int(part)] if isinstance(target, list) else target[part]
        key = int(parts[-1]) if isinstance(target, list) else parts[-1]
        target[key] = copy.deepcopy(patch['value'])
    return result

def check_audit_access(s):
    """Current authenticated read decision, independent of historical mutation rights."""
    access = s['audit_access']
    if access['status'] == 'UNAVAILABLE' or not access['identity_source_available']:
        return 'UNKNOWN'
    r = s['request']
    if (access['status'] != 'ALLOWED' or not access['authenticated']
            or access['requester'] != access['authenticated_principal']
            or access['permitted_case_round'] != r['case_round']
            or access['permitted_unique_key'] != r['unique_key']
            or access['permitted_target_id'] != r['exact_target_id']
            or access['trusted_historical_issuer'] != s['token']['issuer']):
        return 'DENIED'
    if not access['lineage_integrity_verified']:
        return 'UNKNOWN'
    try:
        if utc(access['trusted_now']) >= utc(access['valid_until']):
            return 'EXPIRED'
    except ValueError:
        return 'TIME'
    return 'STATIC_VALID'


def check_plan(s):
    p, r = s['commit']['write_plan'], s['request']
    if (p['plan_version'] != '1.0' or p['derivation_profile'] != 'magi-authority-derive/1'
            or p['operation'] != r['operation'] or p['outbox_kinds'] != ['COMMIT_RECORDED', r['operation']]):
        return 'WRITE_SET'
    variants = {'JOURNAL_ADMIT': 'admission', 'QUIESCE': 'quiescence', 'CANDIDATE_CLOSE': 'closure', 'SEND_COMMIT': 'send'}
    if (r['operation'] == 'SEND_COMMIT') != (r['send_intent'] is not None):
        return 'TOKEN_BINDING'
    selected = variants.get(r['operation'])
    if not selected or any((p[k] is not None) != (k == selected) for k in variants.values()):
        return 'WRITE_SET'
    if selected == 'admission':
        a = p['admission']
        hwm, pending = a['high_watermark'], a['pending_transition']
        if (hwm['journal_id'] != pending['journal_id'] or hwm['next'] != hwm['expected'] + 1
                or pending['expected'] == 0 or pending['next'] != pending['expected'] - 1
                or a['provider_settlement']['expected_status'] != 'PENDING'
                or a['provider_settlement']['next_status'] != 'APPLIED_VERIFIED'):
            return 'WRITE_SET'
    elif selected == 'quiescence':
        q = p['quiescence']
        transitions = [q['round'], *q['journals']]
        if (len({j['object_id'] for j in q['journals']}) != 3
                or any(t['expected_state'] != 'OPEN' or t['next_state'] != 'QUIESCING' for t in transitions)):
            return 'WRITE_SET'
    elif selected == 'closure':
        q = p['closure']
        if (q['candidate']['id'] != r['exact_target_id'] or len(q['journals']) != 3
                or [j['role'] for j in q['journals']] != sorted(ROLES)
                or any(j['expected_pending'] != 0 or j['expected_high_watermark'] != j['close_high_watermark'] for j in q['journals'])
                or q['round']['expected_state'] != 'QUIESCING' or q['round']['next_state'] != 'EVIDENCE_CLOSED'):
            return 'WRITE_SET'
    else:
        q, expected = p['send'], s['commit']['expected_state']
        action, quota = q['action_transition'], q['quota_transition']
        if (action['next_version'] != action['expected_version'] + 1
                or quota['next_version'] != quota['expected_version'] + 1
                or action['expected_state'] != 'AUDIT_READY' or action['next_state'] != 'SEND_COMMITTED'
                or action['gateway_id'] != expected['gateway_id'] or action['gateway_generation'] != expected['gateway_generation']
                or quota['amount'] <= 0 or quota['expected_remaining'] < quota['amount']
                or quota['next_remaining'] != quota['expected_remaining'] - quota['amount']
                or q['logical_binding']['mode'] != 'VERIFY_EXISTING'):
            return 'WRITE_SET'
    return 'STATIC_VALID'


def proposal_transcript(s):
    """Pre-token digest inputs contain no generated output reference or future value."""
    plan = s['commit']['write_plan']
    reserved = {'token_id', 'commit_id', 'authority_sequence', 'committed_at', 'result_refs', 'lineage_digest', 'plan_digest'}
    def visit(value):
        if isinstance(value, dict):
            if reserved.intersection(value):
                raise ValueError('transaction output in caller proposal')
            for child in value.values():
                visit(child)
        elif isinstance(value, list):
            for child in value:
                visit(child)
    visit(plan)
    return {'request_digest': digest(s['request'], 'magi:authority-request:v1'),
            'expected_state_digest': digest(s['commit']['expected_state'], 'magi:authority-expected-state:v1'),
            'plan_digest': digest(plan, 'magi:authority-plan:v1')}


def derive_example(s, allocation):
    """Pure offline construction witness. It issues no token and commits nothing."""
    transcript = proposal_transcript(s)
    p, r = s['commit']['write_plan'], s['request']
    if check_plan(s) != 'STATIC_VALID' or p['mutation_set'] != WRITE_SETS[r['operation']]:
        raise ValueError('incomplete plan')
    payloads = {}
    if p['admission']:
        a = p['admission']
        payloads.update(admission=a['admission_input'], journal_event=a['journal_event_input'],
                        dependency_edges=a['dependency_edge_inputs'], journal_high_watermark=a['high_watermark'],
                        provider_settlement=a['provider_settlement'], pending_transition=a['pending_transition'])
    if p['quiescence']:
        q = p['quiescence']
        payloads.update(fence={'fence_id': q['fence_id']}, round_state=q['round'], journal_states=q['journals'])
    if p['closure']:
        c = p['closure']
        payloads.update(candidate=c['candidate'], closure={'candidate': c['candidate'], 'fence_id': c['fence_id'],
                        'journals': c['journals'], 'receipts': c['receipts'], 'confirmations': c['confirmations']},
                        evidence_closed=c['round'])
        payloads.update({'journal:' + j['role']: j for j in c['journals']})
    if p['send']:
        send = p['send']
        payloads.update(action_transition=send['action_transition'], quota=send['quota_transition'],
                        logical_operation_binding=send['logical_binding'], send_record={'intent': send['intent'], 'binding': r['send_intent']})
    payloads.update(token_consumed={'token_id': allocation['token_id']},
                    unique_binding={'unique_key': r['unique_key'], 'target_id': r['exact_target_id']},
                    outbox={'event_kinds': p['outbox_kinds']})
    rows = []
    for kind in p['mutation_set']:
        if kind == 'lineage':
            continue
        body = {'kind': kind, 'allocation': copy.deepcopy(allocation), **transcript,
                'derivation_profile': p['derivation_profile'], 'payload': copy.deepcopy(payloads[kind])}
        rows.append({'id': allocation['commit_id'] + ':' + kind, 'body': body,
                     'digest': digest(body, 'magi:authority-result:v1')})
    lineage = {'allocation': copy.deepcopy(allocation), **transcript, 'derivation_profile': p['derivation_profile'],
               'unique_key': r['unique_key'], 'exact_target_id': r['exact_target_id'],
               'result_refs': [{'kind': row['body']['kind'], 'id': row['id'], 'digest': row['digest']} for row in rows]}
    return {'rows': rows, 'lineage': lineage, 'lineage_digest': digest(lineage, 'magi:authority-lineage:v1')}


def check_semantics(s):
    r, t, c, q, tx, replay, action = (s[k] for k in ('request', 'token', 'current', 'commit', 'transaction', 'replay', 'action'))
    if replay['mode'] in ('ORIGINAL', 'UNKNOWN'):
        audit_code = check_audit_access(s)
        if audit_code != 'STATIC_VALID':
            return audit_code
    try:
        for value in (r['trusted_now'], t['issued_at'], t['expires_at'], c['trusted_now'], c['lease_until'], c['witness']['valid_until']):
            utc(value)
    except ValueError:
        return 'TIME'
    if r['execution_generation'] == 0:
        return 'LEGACY'
    for epochs in (r['scoped_epochs'], t['scoped_epochs'], c['scoped_epochs'], q['expected_state']['scoped_epochs']):
        keys = scope_keys(epochs)
        if keys != sorted(set(keys)) or {x[0] for x in keys} != {'CONTEXT', 'AUTHORITY', 'CREDENTIAL'}:
            return 'SCOPE_SET'
    nodes = r['exact_dependencies']
    ids = [n['id'] for n in nodes]
    if ids != sorted(set(ids)):
        return 'DEPENDENCY_DAG'
    parents = {n['id']: n['parents'] for n in nodes}
    visiting, done = set(), set()
    def visit(node):
        if node in visiting or node not in parents:
            return False
        if node in done:
            return True
        visiting.add(node)
        p = parents[node]
        if p != sorted(set(p)) or not all(visit(x) for x in p):
            return False
        visiting.remove(node)
        done.add(node)
        return True
    if not all(visit(n) for n in ids):
        return 'DEPENDENCY_DAG'
    if tx['locks'] != LOCK_ORDER or tx['steps'] != ['lock', 'trusted_time', 'revalidate', 'linearize', 'write_all', 'commit']:
        return 'ORDER'
    required = WRITE_SETS.get(r['operation'])
    if required is None or q['write_plan']['mutation_set'] != required:
        return 'WRITE_SET'
    plan_code = check_plan(s)
    if plan_code != 'STATIC_VALID':
        return plan_code
    if any((t['principal_id'] != r['principal']['subject_id'], t['operation'] != r['operation'],
            t['exact_target_id'] != r['exact_target_id'], t['unique_key'] != r['unique_key'],
            q['unique_key'] != r['unique_key'], q['exact_target_id'] != r['exact_target_id'],
            q['token_id'] != t['token_id'], t['expected_state_digest'] != digest(q['expected_state'], 'magi:authority-expected-state:v1'), t['request_digest'] != digest(r, 'magi:authority-request:v1'),
            t['plan_digest'] != digest(q['write_plan'], 'magi:authority-plan:v1'),
            t['execution_generation'] != r['execution_generation'], t['scoped_epochs'] != r['scoped_epochs'])):
        return 'TOKEN_BINDING'
    if replay['mode'] in ('ORIGINAL', 'UNKNOWN'):
        if not all(replay[x] for x in ('same_request', 'same_target', 'same_token', 'same_plan')):
            return 'CONFLICT'
        if replay['resend']:
            return 'REPLAY'
        if not replay['lineage_present'] or replay['mode'] == 'UNKNOWN':
            return 'UNKNOWN'
        return 'STATIC_VALID'  # Read-only original lineage; not renewed authority.
    if not c['time_trusted']:
        return 'UNKNOWN'
    now = utc(c['trusted_now'])
    if c['revoked'] or 0 < tx['revoke_sequence'] <= tx['authority_sequence']:
        return 'REVOKED'
    if not c['proof_valid'] or not c['dependencies_valid']:
        return 'DENIED'
    g = s['governance']
    if not g['root_available']:
        return 'UNKNOWN'
    if (not g['root_installed_before_target'] or g['authenticated_subject'] != r['principal']['subject_id']
            or g['authenticated_identity_source_id'] != r['principal']['identity_source_id']
            or g['authenticated_identity_source_version'] != r['principal']['identity_source_version']
            or t['issuer'] != g['expected_token_issuer']):
        return 'DENIED'
    previous, seen = None, {g['root_id']}
    for hop in g['chain']:
        for field in ('resources', 'operations'):
            if hop[field] != sorted(set(hop[field])):
                return 'DENIED'
        if (hop['issuer'] != (previous['subject'] if previous else g['root_id'])
                or hop['subject'] in seen):
            return 'DENIED'
        if previous and (not previous['delegable']
                or not set(hop['resources']) <= set(previous['resources'])
                or not set(hop['operations']) <= set(previous['operations'])):
            return 'DENIED'
        try:
            if not utc(hop['not_before']) <= now < utc(hop['valid_until']):
                return 'EXPIRED'
            if previous and (utc(hop['not_before']) < utc(previous['not_before'])
                    or utc(hop['valid_until']) > utc(previous['valid_until'])):
                return 'DENIED'
        except ValueError:
            return 'TIME'
        seen.add(hop['subject'])
        previous = hop
    if (previous['subject'] != g['authenticated_subject'] or g['resource_id'] not in previous['resources']
            or r['operation'] not in previous['operations']):
        return 'DENIED'
    rule = g['owner_rule']
    if any(rule[k] != sorted(set(rule[k])) for k in ('owners', 'consents', 'delegates')):
        return 'DENIED'
    if not set(rule['consents']) <= set(rule['owners']):
        return 'DENIED'
    if rule['mode'] in ('OWNER_ALL', 'UNDECLARED') and rule['consents'] != rule['owners']:
        return 'DENIED'
    if rule['mode'] == 'OWNER_THRESHOLD' and (not rule['threshold_preapproved']
            or not 1 <= rule['threshold'] <= len(rule['owners']) or len(rule['consents']) < rule['threshold']):
        return 'DENIED'
    if rule['mode'] == 'SINGLE_DELEGATE' and (len(rule['delegates']) != 1
            or rule['delegates'][0] not in [hop['subject'] for hop in g['chain']]):
        return 'DENIED'

    if not utc(t['issued_at']) <= now < utc(t['expires_at']):
        return 'EXPIRED'
    if not utc(t['issued_at']) < utc(t['expires_at']) or utc(t['expires_at']) - utc(t['issued_at']) > timedelta(milliseconds=5000):
        return 'TOKEN_LIFETIME'
    expected = q['expected_state']
    if (r['execution_generation'] != c['execution_generation'] or t['authority_version'] != c['authority_version']
            or r['scoped_epochs'] != c['scoped_epochs'] or expected['owner_id'] != c['owner_id']
            or expected['execution_generation'] != c['execution_generation'] or expected['authority_version'] != c['authority_version']
            or expected['scoped_epochs'] != c['scoped_epochs'] or c['principal_id'] != r['principal']['subject_id']
            or now >= utc(c['lease_until'])):
        return 'STALE'
    if expected['state'] != c['expected_state']:
        return 'CONFLICT'
    if r['operation'] == 'JOURNAL_ADMIT':
        a = s['admission']
        planned = q['write_plan']['admission']
        if (planned['high_watermark']['journal_id'] != a['journal_id']
                or planned['high_watermark']['expected'] != a['journal_high_watermark']
                or planned['high_watermark']['next'] != a['local_next_sequence']
                or planned['provider_settlement']['request_id'] != a['provider_request_id']
                or planned['provider_settlement']['expected_status'] != a['provider_status']
                or planned['pending_transition']['expected'] != a['pending_count']):
            return 'CONFLICT'
        if (not a['reverse_edges_complete'] or not a['provider_registered']
                or a['local_next_sequence'] != a['journal_high_watermark'] + 1):
            return 'CONFLICT'
        state = c['expected_state']
        if state == 'QUIESCING' and (a['kind'] not in ('FINAL_SHARED', 'CONFIRMATION') or a['fence_id'] != a['registered_fence_id']):
            return 'CONFLICT'
        if state == 'INPUT_CLOSED' and (a['kind'] != 'REREAD' or not 0 < a['node_admitted_sequence'] <= a['cutoff_sequence']):
            return 'CONFLICT'
    if r['operation'] == 'QUIESCE' and c['expected_state'] != 'OPEN':
        return 'CONFLICT'
    if r['operation'] in ('CANDIDATE_CLOSE', 'SEND_COMMIT'):
        try:
            observed = utc(s['freshness']['observed_at'])
        except ValueError:
            return 'TIME'
        delta = now - observed
        age_ms = delta.days * 86400000 + delta.seconds * 1000 + delta.microseconds // 1000
        if age_ms < 0:
            return 'DENIED'
        if age_ms > s['freshness']['max_age_ms']:
            return 'EXPIRED'
    if r['operation'] == 'CANDIDATE_CLOSE':
        if (c['expected_state'] != 'QUIESCING' or c['pending_injections'] != 0
                or len(c['receipts']) != 3 or set(c['receipts']) != ROLES
                or len(c['confirmations']) != 3 or set(c['confirmations']) != ROLES
                or c['confirmation_target_ids'] != [r['exact_target_id']] * 3):
            return 'CONFLICT'
        if not tx['candidate_visible_after_commit'] or not tx['journals_closed_together']:
            return 'ATOMICITY'
    if r['operation'] == 'SEND_COMMIT':
        intent = r['send_intent']
        send = q['write_plan']['send']
        dependency = next((n for n in nodes if intent and n['id'] == intent['intent_id']), None)
        if (not intent or intent['action_id'] != r['exact_target_id']
                or action['action_id'] != r['exact_target_id']
                or action['intent_digest'] != intent['intent_digest']
                or action['encoded_request_digest'] != intent['encoded_request_digest']
                or not dependency or dependency['digest'] != intent['intent_digest']
                or send['intent']['id'] != intent['intent_id'] or send['intent']['digest'] != intent['intent_digest']
                or send['action_transition']['action_id'] != intent['action_id']
                or send['logical_binding']['logical_operation_id'] != intent['logical_operation_id']
                or send['logical_binding']['business_parameter_digest'] != intent['business_parameter_digest']):
            return 'TOKEN_BINDING'
        if (send['action_transition']['expected_version'] != action['row_version']
                                or send['quota_transition']['quota_id'] != c['quota_id']
                or send['quota_transition']['expected_version'] != c['quota_version']
                or send['quota_transition']['expected_remaining'] != c['quota_remaining']):
            return 'STALE'
        binding = c['logical_binding']
        if (not binding['exists'] or binding['logical_operation_id'] != intent['logical_operation_id']
                or binding['business_parameter_digest'] != intent['business_parameter_digest']):
            return 'CONFLICT'
        if action['state'] in ('SEND_COMMITTED', 'OUTCOME_UNKNOWN'):
            return 'REPLAY' if action['send_attempts'] > 0 else 'UNKNOWN'
        if not c['witness_available']:
            return 'UNKNOWN'
        w = c['witness']
        if (not w['durable'] or not w['signature_valid'] or w['trust_domain'] == c['egress_domain']
                or w['action_id'] != action['action_id'] or w['intent_digest'] != action['intent_digest']):
            return 'DENIED'
        if now >= utc(w['valid_until']):
            return 'EXPIRED'
        if (action['gateway_id'] != c['gateway_id'] or action['gateway_generation'] != c['gateway_generation']
                or expected['gateway_id'] != c['gateway_id'] or expected['gateway_generation'] != c['gateway_generation']):
            return 'STALE'
        if action['state'] != 'AUDIT_READY':
            return 'CONFLICT'
    return 'STATIC_VALID'

def schema_results(values, schema_path=None):
    result = subprocess.run(['go', 'run', '../scripts/check-authority-contract.go', '../scripts/authority-api.go',
                             str(schema_path or ROOT / 'docs/contracts/authority-v1/schema.json')],
                            cwd=ROOT / 'backend', input=json.dumps(values), capture_output=True, text=True)
    if result.returncode:
        raise RuntimeError('schema oracle failed (not an accepted negative): ' + result.stderr)
    return json.loads(result.stdout)

def main():
    data = json.loads((ROOT / 'docs/contracts/authority-v1/traces.json').read_text())
    cases = data['cases']
    values = [materialize(data, c) for c in cases]
    shapes = schema_results(values)
    if len(shapes) != len(cases):
        raise RuntimeError('schema oracle result count mismatch')
    failed = []
    for f, s, shape in zip(cases, values, shapes):
        actual = shape or check_semantics(s)
        if actual != f['expected']:
            failed.append(f'{f["name"]}: expected {f["expected"]}, got {actual}')
    if failed:
        print('\n'.join(failed), file=sys.stderr)
        return 1
    print(f'PASS {len(cases)} schema/static trace declarations; live authority/MySQL/provider/egress NOT RUN')
    return 0

if __name__ == '__main__':
    sys.exit(main())
