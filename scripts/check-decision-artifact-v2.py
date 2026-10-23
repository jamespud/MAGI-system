#!/usr/bin/env python3
"""Fixture-only Decision Artifact v2 gate, not a production protocol validator.

Go uses the existing C0 and JSON Schema validators; Python independently verifies
identities and checks synthetic bindings. No network, DB, signature or session I/O.
"""
import copy
import hashlib
from datetime import datetime
import importlib.util
import json
from pathlib import Path
import subprocess
import sys

ROOT = Path(__file__).resolve().parents[1]
DATA = ROOT / 'docs/contracts/decision-artifacts-v2'
spec = importlib.util.spec_from_file_location(
    'canonical_reference', ROOT / 'backend/domain/canonical/testdata/verify_golden.py')
reference = importlib.util.module_from_spec(spec)
spec.loader.exec_module(reference)
ROLES = ('melchior', 'balthasar', 'casper')
SCOPES = ('execution_generation', 'context_revocation_scope_id',
          'context_revocation_epoch', 'authority_scope_id', 'authority_epoch',
          'credential_scope_id', 'credential_epoch')


def envelope(body):
    raw = reference.canonical_bytes(body)
    digest = hashlib.sha256(b'magi:canonical:v1:' + body['artifact_type'].encode() + b'\n' + raw).hexdigest()
    return dict(artifact_id=reference.TYPE_PREFIXES[body['artifact_type']] + '_' + digest,
                digest=digest, canonical_profile='magi-cj/1', digest_alg='sha256',
                body=body, canonical_bytes_hex=raw.hex())


def replace(value, before, after):
    """Update exact references to a changed ancestor, never aliases or latest."""
    if isinstance(value, dict):
        return {k: replace(v, before, after) for k, v in value.items()}
    if isinstance(value, list):
        return [replace(v, before, after) for v in value]
    return after if value == before else value


def reseal(bundle):
    # A topological order: every raw record or artifact only names ancestors.
    order = [('artifacts', 'charter'), ('records', 'approval'), ('artifacts', 'manifest')]
    order += [('records', 'receipt.' + r) for r in ROLES]
    order += [('records', n) for n in bundle['records'] if n.startswith('classification.')]
    order += [('artifacts', 'candidate')]
    order += [('records', 'confirmation.' + r) for r in ROLES]
    order += [('records', 'closure')]
    order += [('records', 'completeness.' + r) for r in ROLES]
    order += [('artifacts', 'readset.' + r) for r in ROLES]
    order += [('artifacts', 'ballot.' + r) for r in ROLES]
    for group, name in order:
        entry = bundle[group][name]
        if group == 'artifacts':
            changed = envelope(entry['body'])
            bundle = replace(bundle, entry['artifact_id'], changed['artifact_id'])
            bundle = replace(bundle, entry['digest'], changed['digest'])
            # SourceNode.raw_content_digest hashes raw bytes, without C0's domain.
            old_raw_digest = hashlib.sha256(bytes.fromhex(entry['canonical_bytes_hex'])).hexdigest()
            new_raw_digest = hashlib.sha256(bytes.fromhex(changed['canonical_bytes_hex'])).hexdigest()
            bundle = replace(bundle, old_raw_digest, new_raw_digest)
            bundle[group][name] = changed
        else:
            old = entry['ref']['content_digest']
            new = hashlib.sha256(reference.canonical_bytes(entry['body'])).hexdigest()
            bundle = replace(bundle, old, new)
    return bundle


def patch(bundle, patches):
    for change in patches:
        target = bundle
        for key in change['path'][:-1]:
            target = target[int(key)] if isinstance(target, list) else target[key]
        key = change['path'][-1]
        if isinstance(target, list):
            key = int(key)
        if change['op'] == 'delete':
            del target[key]
        elif change['op'] == 'append':
            target[key].append(copy.deepcopy(change['value']))
        else:
            target[key] = copy.deepcopy(change['value'])
    return bundle


class Rejected(Exception):
    pass


def require(condition, code):
    if not condition:
        raise Rejected(code)


def semantic(bundle):
    a = bundle['artifacts']
    b = {name: entry['body'] for name, entry in a.items()}
    charter, manifest, candidate = (b[n] for n in ('charter', 'manifest', 'candidate'))
    records = bundle['records']
    state = bundle['authority_state']

    def resolve(ref, typ):
        matches = [e for e in records.values() if e['ref'] == ref]
        require(len(matches) == 1, 'REFERENCE')
        entry = matches[0]
        require(entry['body']['record_type'] == typ, 'REFERENCE')
        digest = hashlib.sha256(reference.canonical_bytes(entry['body'])).hexdigest()
        require(ref['content_digest'] == digest, 'REFERENCE')
        return entry['body']

    def same_scopes(value):
        require(all(value[k] == state[k] for k in SCOPES), 'STALE')

    def instant(value):
        return datetime.fromisoformat(value.replace('Z', '+00:00'))

    def ordered(*times):
        return all(instant(x) <= instant(y) for x, y in zip(times, times[1:]))

    def key(role):
        return dict(case_id=manifest['case_id'], round=manifest['round'],
                    context_manifest_id=a['manifest']['artifact_id'], persona_id=role)

    def view(value, role, journal=True):
        v = manifest['roles'][role]
        require(value['persona_spec_id'] == v['persona_spec_id'] and
                value.get('experience_snapshot_id', v['experience_snapshot_id']) == v['experience_snapshot_id'] and
                value.get('user_preference_snapshot_id', manifest['user_preference_snapshot_id']) == manifest['user_preference_snapshot_id'], 'VIEW')
        if journal:
            require(value['journal_key'] == key(role), 'VIEW')

    require(charter['domain_profile_id'] == manifest['domain_profile_id'], 'DOMAIN')
    require(manifest['charter_id'] == a['charter']['artifact_id'], 'BINDING')
    for field in ('case_id', 'round', 'charter_id', 'domain_profile_id'):
        require(candidate[field] == manifest[field], 'BINDING')
    require(candidate['context_manifest_id'] == a['manifest']['artifact_id'], 'BINDING')
    require(candidate['persona_views'] == manifest['roles'], 'VIEW')
    require(manifest['context_revocation_scope_id'] == state['context_revocation_scope_id'] and
            manifest['context_revocation_epoch'] == state['context_revocation_epoch'], 'STALE')
    required_capabilities = {(t, '2.0', 'magi-cj/1') for t in
                             ('decision_charter', 'candidate_snapshot', 'read_set_snapshot', 'sealed_ballot')}
    require({tuple(c[k] for k in ('artifact_type', 'schema_version', 'canonical_profile'))
             for c in bundle['capabilities']} == required_capabilities, 'CAPABILITY')

    # This checks a declared authority-state snapshot, not live ownership or CAS.
    checks = bundle['authority_checks']
    require(len(checks) == 6 and {c['operation'] for c in checks} ==
            {'commit', 'submit.melchior', 'submit.balthasar', 'submit.casper', 'aggregate', 'adjudicate'}, 'AUTHORITY')
    for check in checks:
        same_scopes(check)
        require(check['case_id'] == manifest['case_id'] and check['round'] == manifest['round'] and
                check['charter_id'] == a['charter']['artifact_id'] and
                check['context_manifest_id'] == a['manifest']['artifact_id'] and
                check['candidate_snapshot_id'] == a['candidate']['artifact_id'], 'AUTHORITY')
        require(check['status'] == 'VERIFIED' and check['subject_id'] == state['owner_subject_id'] ==
                check['owner_subject_id'], 'AUTHORITY')
    proof = checks[0]['authority_proof_id']
    require(all(c['authority_proof_id'] == proof for c in checks), 'AUTHORITY')
    approval = records['approval']['body']
    resolve(records['approval']['ref'], 'CharterApproval')
    approval_authority = bundle.get('approval_authority')
    require(approval_authority is not None and approval_authority['status'] == 'VERIFIED', 'AUTHORITY')
    require(approval['charter_id'] == a['charter']['artifact_id'] and
            approval['domain_profile_id'] == charter['domain_profile_id'] and
            approval['authority_proof_id'] == approval_authority['authority_proof_id'] and
            approval['issuer']['subject_id'] == approval_authority['issuer_subject_id'] and
            approval['charter_id'] == approval_authority['charter_id'] and
            approval['domain_profile_id'] == approval_authority['domain_profile_id'] and
            approval['authority_scope_id'] == approval_authority['authority_scope_id'] and
            approval['authority_epoch'] == approval_authority['authority_epoch'], 'AUTHORITY')
    deadlines = [charter[k] for k in ('submission_deadline', 'objection_deadline',
                                     'revision_deadline', 'freeze_deadline', 'validity_horizon')]
    require(all(instant(x) < instant(y) for x, y in zip(deadlines, deadlines[1:])), 'ORDER')
    require(ordered(charter['frozen_at'], approval['approved_at'], manifest['frozen_at'], candidate['frozen_at']), 'ORDER')
    included = [c for c in candidate['candidates'] if c['state'] == 'INCLUDED']
    require(candidate['candidate_count'] == len(included) and
            candidate['candidate_entries'] == len(candidate['candidates']) and
            1 <= len(included) <= charter['candidate_limit'] and
            len({c['candidate_id'] for c in candidate['candidates']}) == len(candidate['candidates']), 'AGENDA')
    require(any(c['is_no_action'] for c in candidate['candidates']) or charter['no_action_exception_reasons'], 'AGENDA')
    require(not candidate['material_pending'], 'AGENDA')
    for item in candidate['candidates']:
        classification = resolve(item['risk_classification_ref'], 'RiskClassificationRecord')
        require(classification['candidate_id'] == item['candidate_id'] and
                classification['domain_profile_id'] == charter['domain_profile_id'] and
                classification['risk_level'] == item['risk_level'], 'RISK')
        require(ordered(classification['classified_at'], candidate['frozen_at']), 'ORDER')

    expected_round = dict(case_id=manifest['case_id'], round=manifest['round'],
                          context_manifest_id=a['manifest']['artifact_id'],
                          candidate_snapshot_id=a['candidate']['artifact_id'])
    require(bundle['round_bindings'] and all(x == expected_round for x in bundle['round_bindings']), 'ROUND_BINDING')
    shared = candidate['final_shared_inputs']
    require(shared['kind'] == 'SHARED_FACT', 'SHARED')
    closure_entry = records.get('closure')
    require(closure_entry is not None, 'REFERENCE')
    closure_ref = closure_entry['ref']
    closure = resolve(closure_ref, 'EvidenceClosureRecord')
    require(all(closure[k] == v for k, v in expected_round.items()), 'BINDING')
    same_scopes(closure)
    require(closure['persona_views'] == manifest['roles'], 'VIEW')
    require(closure['final_shared_inputs'] == shared and
            closure['receipt_refs'] == candidate['final_shared_delivery_receipt_refs'], 'SHARED')
    for item in candidate['candidates']:
        classification = resolve(item['risk_classification_ref'], 'RiskClassificationRecord')
        require(instant(classification['classified_at']) <= instant(closure['committed_at']) <
                instant(classification['valid_until']), 'FRESHNESS')
        attributes = [v['attribute_id'] for v in classification['input_attribute_values']]
        require(attributes == sorted(set(attributes)), 'RISK')
    require(ordered(candidate['frozen_at'], closure['committed_at'], state['trusted_now']) and
            instant(closure['committed_at']) < instant(charter['freeze_deadline']), 'ORDER')
    age = (instant(closure['committed_at']) - instant(state['source_observed_at'])).total_seconds() * 1000
    require(0 <= age <= state['freshness_limit_ms'] == 30000, 'FRESHNESS')
    for value in (charter, manifest, candidate, shared, *manifest['snapshot_refs']):
        validity = value['validity']
        require(instant(validity['not_before']) <= instant(closure['committed_at']) and
                (validity['valid_until'] is None or instant(closure['committed_at']) < instant(validity['valid_until'])), 'FRESHNESS')

    seal_keys = set()
    for binding in bundle['seal_bindings']:
        role = binding['persona_id']
        require(role in ROLES, 'SEAL_BINDING')
        expected = dict(**key(role), read_set_snapshot_id=a['readset.' + role]['artifact_id'],
                        ballot_id=a['ballot.' + role]['artifact_id'])
        require(binding == expected, 'SEAL_BINDING')
        seal_keys.add(role)
    require(seal_keys == set(ROLES), 'SEAL_BINDING')
    seen_sessions, seen_requests = set(), set()
    for role in ROLES:
        receipt_ref = candidate['final_shared_delivery_receipt_refs'][role]
        receipt = resolve(receipt_ref, 'TrustedDeliveryReceipt')
        view(receipt, role)
        require(receipt['controlled_session_id'] not in seen_sessions and
                receipt['provider_request_id'] not in seen_requests, 'DELIVERY')
        seen_sessions.add(receipt['controlled_session_id'])
        seen_requests.add(receipt['provider_request_id'])
        same_scopes(receipt)
        require(receipt['final_shared_inputs'] == shared, 'SHARED')
        require(receipt['quiescence_fence_id'] == closure['quiescence_fence_id'], 'DELIVERY')
        require(ordered(receipt['verified_at'], candidate['frozen_at']), 'ORDER')
        confirmation = resolve(closure['confirmation_refs'][role], 'CandidateConfirmation')
        view(confirmation, role)
        same_scopes(confirmation)
        require(confirmation['candidate_snapshot_id'] == a['candidate']['artifact_id'] and
                confirmation['final_shared_inputs'] == shared and confirmation['receipt_ref'] == receipt_ref, 'CONFIRMATION')
        require(ordered(candidate['frozen_at'], confirmation['confirmed_at'], closure['committed_at']), 'ORDER')
        readset = b['readset.' + role]
        ballot = b['ballot.' + role]
        view(readset, role)
        same_scopes(readset)
        require(readset['candidate_snapshot_id'] == a['candidate']['artifact_id'] and
                readset['evidence_cutoff_sequence'] == closure['evidence_cutoff_sequence'] and
                readset['final_shared_inputs'] == shared and readset['evidence_closure_record_ref'] == closure_ref and
                readset['final_shared_delivery_receipt_ref'] == receipt_ref and
                readset['source_dependency_index_version'] == manifest['source_dependency_index_version'], 'READSET')
        events = readset['events']
        high = closure['close_high_watermarks'][role]
        require(readset['journal_high_watermark'] == len(events) >= high > 0, 'READSET')
        require([e['sequence'] for e in events] == list(range(1, len(events) + 1)), 'READSET')
        require(len({e['delivery_id'] for e in events}) == len(events), 'READSET')
        seq = receipt['registered_injection_sequence']
        require(seq <= high, 'DELIVERY')
        confirmation_seq = confirmation['confirmation_injection_sequence']
        require(seq < confirmation_seq <= high and
                confirmation['controlled_session_id'] == receipt['controlled_session_id'] and
                confirmation['provider_request_id'] != receipt['provider_request_id'], 'CONFIRMATION')
        confirmation_event = events[confirmation_seq - 1]
        require(confirmation_event['source']['source_entry_id'] == a['candidate']['artifact_id'] and
                confirmation_event['source']['source_version'] == candidate['version'] and
                confirmation_event['source']['raw_content_digest'] ==
                hashlib.sha256(reference.canonical_bytes(candidate)).hexdigest(), 'CONFIRMATION')
        require(ordered(candidate['frozen_at'], confirmation_event['injected_at'], confirmation['confirmed_at']), 'ORDER')
        injection = events[seq - 1]
        require(injection['kind'] == 'SHARED_FACT' and injection['source']['source_entry_id'] == shared['snapshot_id'] and
                injection['source']['source_version'] == shared['version'] and
                injection['source']['raw_content_digest'] == shared['content_digest'], 'DELIVERY')
        require(ordered(injection['injected_at'], receipt['verified_at']), 'ORDER')
        nodes = {n['node_id']: n for n in readset['dependency_nodes']}
        require(len(nodes) == len(readset['dependency_nodes']), 'DAG')
        require(list(nodes) == sorted(nodes, key=lambda s: s.encode('utf-16-be')), 'DAG')
        visited, active = set(), set()

        def visit(node_id):
            require(node_id in nodes and node_id not in active, 'DAG')
            if node_id in visited:
                return
            active.add(node_id)
            node = nodes[node_id]
            parents = node['parent_node_ids']
            require(parents == sorted(set(parents)) and
                    ((node['kind'] == 'DERIVED' and bool(parents)) or
                     (node['kind'] != 'DERIVED' and not parents)), 'DAG')
            for parent in parents:
                visit(parent)
            active.remove(node_id)
            visited.add(node_id)

        for node_id in nodes:
            visit(node_id)
            validity = nodes[node_id]['validity']
            require(instant(validity['not_before']) <= instant(closure['committed_at']) and
                    (validity['valid_until'] is None or instant(closure['committed_at']) < instant(validity['valid_until'])), 'FRESHNESS')

        def ancestors(node_id):
            result = {node_id}
            for parent_id in nodes[node_id]['parent_node_ids']:
                result.update(ancestors(parent_id))
            return result
        admitted = set()
        last_authority_sequence = 0
        for event in events:
            require(event['journal_key'] == key(role) and event['target_persona'] == role, 'VIEW')
            require(event['execution_generation'] == state['execution_generation'], 'STALE')
            node = event['source']
            require(nodes.get(node['node_id']) == node and event['parent_node_ids'] == node['parent_node_ids'], 'DAG')
            require(event['authority_sequence'] > last_authority_sequence, 'CUTOFF')
            last_authority_sequence = event['authority_sequence']
            if event['sequence'] <= high:
                require(event['authority_sequence'] < closure['evidence_cutoff_sequence'], 'CUTOFF')
                require(ordered(event['injected_at'], closure['committed_at']), 'ORDER')
                admitted.update(ancestors(node['node_id']))
            else:
                require(event['authority_sequence'] > closure['evidence_cutoff_sequence'] and
                        node['node_id'] in admitted and set(node['parent_node_ids']) <= admitted, 'CUTOFF')
                require(ordered(closure['committed_at'], event['injected_at'], readset['frozen_at']), 'ORDER')
        complete = resolve(readset['completeness_record_ref'], 'CompletenessRecord')
        require(complete['journal_key'] == key(role) and complete['events'] == events and
                complete['journal_high_watermark'] == len(events), 'COMPLETENESS')
        require(ordered(events[-1]['injected_at'], complete['recorded_at'], readset['frozen_at'], ballot['submitted_at']), 'ORDER')
        require(ordered(closure['committed_at'], readset['frozen_at']), 'ORDER')
        require(all(ballot[k] == v for k, v in key(role).items()) and
                ballot['charter_id'] == a['charter']['artifact_id'] and
                ballot['domain_profile_id'] == charter['domain_profile_id'] and
                ballot['snapshot_id'] == a['candidate']['artifact_id'], 'BINDING')
        view(ballot, role, journal=False)
        same_scopes(ballot)
        require(ballot['read_set_snapshot_id'] == a['readset.' + role]['artifact_id'] and
                ballot['read_set_digest'] == a['readset.' + role]['digest'] and
                ballot['read_set_snapshot_id'] == 'rset_' + ballot['read_set_digest'] and
                ballot['evidence_closure_record_ref'] == closure_ref, 'READSET')
        require(ballot['authority_proof_id'] == proof, 'AUTHORITY')
        require(ballot['frozen_at'] == ballot['submitted_at'], 'ORDER')
        require(set(ballot['attitudes']) == {c['candidate_id'] for c in included}, 'AGENDA')


def structure_probes(bundle):
    return list(bundle['artifacts'].values()) + [dict(body=e['body']) for e in bundle['records'].values()]


def main():
    baseline = reference.parse((DATA / 'positive.json').read_bytes())
    golden = reference.parse((DATA / 'golden.json').read_bytes())
    for name, entry in baseline['artifacts'].items():
        actual = envelope(entry['body'])
        for field, expected in golden[name].items():
            require(actual[field] == expected == entry[field], 'GOLDEN:' + name + ':' + field)
    cases = [('positive', baseline, '')]
    retries = copy.deepcopy(baseline)
    retries['round_bindings'].append(copy.deepcopy(retries['round_bindings'][0]))
    retries['seal_bindings'].append(copy.deepcopy(retries['seal_bindings'][0]))
    cases.append(('same-key-same-id-retry', retries, ''))
    reread = copy.deepcopy(baseline)
    event = copy.deepcopy(reread['artifacts']['readset.casper']['body']['events'][0])
    event.update(sequence=3, authority_sequence=101, delivery_id='casper-reread',
                 kind='CACHE_REPLAY', injected_at='2026-10-23T00:00:10.500Z')
    for body in (reread['artifacts']['readset.casper']['body'],
                 reread['records']['completeness.casper']['body']):
        body['events'].append(copy.deepcopy(event))
        body['journal_high_watermark'] = 3
    cases.append(('post-cutoff-admitted-reread', reseal(reread), ''))
    derived = copy.deepcopy(reread)
    readset = derived['artifacts']['readset.casper']['body']
    complete = derived['records']['completeness.casper']['body']
    parent = copy.deepcopy(readset['dependency_nodes'][1])
    parent.update(node_id='node-ancestor', source_entry_id='source-ancestor')
    readset['dependency_nodes'].insert(0, parent)
    for node in [readset['dependency_nodes'][2], readset['events'][0]['source'], complete['events'][0]['source']]:
        node.update(kind='DERIVED', parent_node_ids=['node-ancestor'])
    for body in (readset, complete):
        body['events'][0]['parent_node_ids'] = ['node-ancestor']
        body['events'][2]['source'] = copy.deepcopy(parent)
        body['events'][2]['parent_node_ids'] = []
    cases.append(('post-cutoff-admitted-parent-without-own-read-event', reseal(derived), ''))
    boundary = copy.deepcopy(baseline)
    boundary['authority_state'].update(trusted_now='2026-10-23T00:00:33.000Z')
    boundary['records']['closure']['body']['committed_at'] = '2026-10-23T00:00:31.000Z'
    boundary['artifacts']['charter']['body']['freeze_deadline'] = '2026-10-23T00:00:40.000Z'
    for role in ROLES:
        boundary['records']['completeness.' + role]['body']['recorded_at'] = '2026-10-23T00:00:32.000Z'
        boundary['artifacts']['readset.' + role]['body']['frozen_at'] = '2026-10-23T00:00:32.000Z'
        ballot = boundary['artifacts']['ballot.' + role]['body']
        ballot['submitted_at'] = ballot['frozen_at'] = '2026-10-23T00:00:33.000Z'
    cases.append(('freshness-exactly-30000ms', reseal(boundary), ''))
    negatives = reference.parse((DATA / 'negative-cases.json').read_bytes())
    for case in negatives:
        changes = copy.deepcopy(case['patches'])
        for name, entry in baseline['artifacts'].items():
            changes = replace(changes, '__' + name + '__', entry['artifact_id'])
        changed = patch(copy.deepcopy(baseline), changes)
        if case['reseal']:
            changed = reseal(changed)
        cases.append((case['name'], changed, case['expected']))
    probes = [p for _, b, _ in cases for p in structure_probes(b)]
    result = subprocess.run(['go', 'run', '../scripts/check-decision-artifact-v2.go'],
                            cwd=ROOT / 'backend', input=json.dumps(probes), text=True,
                            capture_output=True)
    if result.returncode:
        sys.stderr.write(result.stderr)
        raise RuntimeError('Go structure oracle failed; this is not a negative-case pass')
    statuses = json.loads(result.stdout)
    require(len(statuses) == len(probes), 'ORACLE_COUNT')
    offset = 0
    for name, bundle, expected in cases:
        count = len(structure_probes(bundle))
        errors = [s for s in statuses[offset:offset + count] if s]
        offset += count
        actual = errors[0] if errors else ''
        if not actual:
            try:
                semantic(bundle)
            except Rejected as error:
                actual = str(error)
        if actual != expected:
            raise AssertionError(f'{name}: expected {expected or "ACCEPT"}, got {actual or "ACCEPT"}')
        print(f'PASS {name}: {actual or "ACCEPT"}')
    print(f'{len(baseline["artifacts"])} Python/Go Golden identities; {len(cases)-len(negatives)} positive boundaries; {len(negatives)} named negative fixture gates')
    print('Static declarations only; MySQL/session/C2-C3 fault injection: NOT RUN / BLOCKED')


if __name__ == '__main__':
    main()
