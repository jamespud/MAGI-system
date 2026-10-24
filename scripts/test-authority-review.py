#!/usr/bin/env python3
"""Issue #45 independent-review regression declarations; no live issuer/commit."""
import copy
import importlib.util
import json
from pathlib import Path
import re
import unittest
ROOT = Path(__file__).resolve().parents[1]
spec = importlib.util.spec_from_file_location('authority_review_checker', ROOT / 'scripts/check-authority-contract.py')
checker = importlib.util.module_from_spec(spec)
spec.loader.exec_module(checker)
DATA = json.loads((ROOT / 'docs/contracts/authority-v1/traces.json').read_text())

class ReviewRegression(unittest.TestCase):
    def test_B01_proposal_has_no_prepared_transaction_outputs(self):
        api = (ROOT / 'scripts/authority-api.go').read_text()
        body = re.search(r'type CompleteWrite(?:Plan|Set) struct \{(.*?)\n\}', api, re.S).group(1)
        self.assertNotRegex(body, r'(Lineage|ConsumedToken|UniqueBinding)\s+ImmutableRow')

    def test_B01_first_proposal_constructs_and_roundtrips_without_future_values(self):
        for base in ('candidate', 'journal_admit', 'quiesce', 'send'):
            value = copy.deepcopy(DATA['bases'][base])
            # Construct before a token or transaction allocation even exists.
            token_declaration = value.pop('token')
            del value['commit']['token_id']
            before = checker.proposal_transcript(value)
            allocation = {'token_id': 'generated-token-99', 'commit_id': 'generated-commit-23',
                          'authority_sequence': 23, 'committed_at': '2026-10-24T01:00:01.000Z'}
            token_declaration.update(before, token_id=allocation['token_id'])
            value['token'] = token_declaration
            value['commit']['token_id'] = token_declaration['token_id']
            self.assertEqual(checker.schema_results([value]), [''])
            self.assertEqual(checker.check_semantics(value), 'STATIC_VALID')
            result = checker.derive_example(value, allocation)
            self.assertEqual(before, checker.proposal_transcript(value))
            self.assertEqual(result['lineage']['plan_digest'], before['plan_digest'])
            self.assertNotIn('lineage_digest', result['lineage'])
            self.assertEqual(result['lineage_digest'], checker.digest(result['lineage'], 'magi:authority-lineage:v1'))
            for row, ref in zip(result['rows'], result['lineage']['result_refs']):
                self.assertEqual(row['digest'], checker.digest(row['body'], 'magi:authority-result:v1'))
                self.assertEqual(ref, {'kind': row['body']['kind'], 'id': row['id'], 'digest': row['digest']})
                self.assertEqual(row['body']['plan_digest'], before['plan_digest'])
                self.assertNotIn('result_refs', row['body'])
            allocation['authority_sequence'] += 1
            second = checker.derive_example(value, allocation)
            self.assertEqual(before, checker.proposal_transcript(value))
            self.assertNotEqual(result['lineage_digest'], second['lineage_digest'])
            value['commit']['write_plan']['commit_id'] = 'future-id'
            with self.assertRaisesRegex(ValueError, 'transaction output'):
                checker.proposal_transcript(value)

    def test_B02_journal_and_send_omissions_are_rejected(self):
        for base, omitted in [('journal_admit', 'journal_high_watermark'),
                              ('journal_admit', 'provider_settlement'),
                              ('journal_admit', 'pending_transition'),
                              ('send', 'action_transition'), ('send', 'logical_operation_binding')]:
            value = copy.deepcopy(DATA['bases'][base])
            q = value['commit']
            if 'write_plan' in q:
                q['write_plan']['mutation_set'] = [x for x in q['write_plan']['mutation_set'] if x != omitted]
            else:
                q['complete_write_set'] = [x for x in q['complete_write_set'] if x != omitted]
            with self.subTest(omitted=omitted):
                self.assertEqual(checker.check_semantics(value), 'WRITE_SET')

    def test_B02_review_quota_identity_is_bound(self):
        value = copy.deepcopy(DATA['bases']['send'])
        value['commit']['write_plan']['send']['quota_transition']['quota_id'] = 'quota-2'
        transcript = checker.proposal_transcript(value)
        value['token'].update(transcript)
        self.assertEqual(checker.schema_results([value]), [''])
        self.assertEqual(checker.check_semantics(value), 'STALE')

    def test_B02_concrete_transition_fields_are_required_by_schema(self):
        values = []
        for base, variant, omitted in [('journal_admit', 'admission', 'high_watermark'),
                                       ('journal_admit', 'admission', 'provider_settlement'),
                                       ('journal_admit', 'admission', 'pending_transition'),
                                       ('send', 'send', 'action_transition'),
                                       ('send', 'send', 'logical_binding')]:
            value = copy.deepcopy(DATA['bases'][base])
            del value['commit']['write_plan'][variant][omitted]
            values.append(value)
        self.assertEqual(checker.schema_results(values), ['SCHEMA'] * len(values))

    def test_B03_detached_action_and_digest_are_rejected(self):
        values = []
        detached = copy.deepcopy(DATA['bases']['send'])
        detached['action']['action_id'] = detached['current']['witness']['action_id'] = 'action-2'
        values.append(detached)
        swapped = copy.deepcopy(DATA['bases']['send'])
        swapped['action']['intent_digest'] = swapped['current']['witness']['intent_digest'] = 'e' * 64
        values.append(swapped)
        self.assertEqual(checker.schema_results(values), ['', ''])
        for value in values:
            self.assertEqual(checker.check_semantics(value), 'TOKEN_BINDING')

    def test_B04_original_requires_current_audit_gate(self):
        for status, expected in [('DENIED', 'DENIED'), ('UNAVAILABLE', 'UNKNOWN')]:
            value = copy.deepcopy(DATA['bases']['send'])
            value['replay']['mode'] = 'ORIGINAL'
            value.setdefault('audit_access', {})['status'] = status
            with self.subTest(status=status):
                self.assertEqual(checker.check_semantics(value), expected)

    def test_B04_historical_mutation_is_not_current_audit_permission(self):
        value = copy.deepcopy(DATA['bases']['send'])
        value['replay']['mode'] = 'ORIGINAL'
        value['current']['revoked'] = True
        value['current']['trusted_now'] = '2026-10-25T01:00:00.000Z'
        value['governance']['root_available'] = False
        value['governance']['authenticated_subject'] = 'retired-worker'
        value['audit_access']['trusted_now'] = '2026-10-25T01:00:00.000Z'
        self.assertEqual(checker.schema_results([value]), [''])
        self.assertEqual(checker.check_semantics(value), 'STATIC_VALID')
        value['audit_access']['requester']['identity_source_id'] = 'untrusted'
        self.assertEqual(checker.check_semantics(value), 'DENIED')
        value['audit_access']['identity_source_available'] = False
        self.assertEqual(checker.check_semantics(value), 'UNKNOWN')

if __name__ == '__main__':
    unittest.main()
