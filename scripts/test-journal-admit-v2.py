#!/usr/bin/env python3
"""Independent regressions for the additive JOURNAL_ADMIT v2 Design Change."""
import copy
import importlib.util
import json
from pathlib import Path
import tempfile
import unittest

ROOT = Path(__file__).resolve().parents[1]
spec = importlib.util.spec_from_file_location('journal_admit_v2_checker', ROOT / 'scripts/check-journal-admit-v2.py')
checker = importlib.util.module_from_spec(spec)
spec.loader.exec_module(checker)




def committed_settlement_binding(value):
    return {
        'settlement_digest': value['settlement']['settlement_digest'],
        'admission_commit_id': value['authority']['admission_commit_id'],
        'provider_request_id': value['settlement']['provider_request_id'],
        'settlement_commit_id': 'commit-settlement-melchior-1',
        'generation': value['admission']['execution_generation'],
        'status': 'COMMITTED',
    }

class JournalAdmitV2Tests(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        cls.data = json.loads(checker.TRACE_PATH.read_text())
        cls.values = [checker.materialize(cls.data, case) for case in cls.data['cases']]
        cls.shapes = checker.schema_results(cls.values)

    def test_shapes_and_semantics_have_separate_expected_codes(self):
        self.assertEqual(len(self.shapes), len(self.values))
        for case, value, shape in zip(self.data['cases'], self.values, self.shapes):
            with self.subTest(case=case['name']):
                if case['expected'] in ('SCHEMA', 'C0'):
                    self.assertEqual(shape, case['expected'])
                else:
                    self.assertEqual(shape, '', 'semantic negative must reach the semantic checker')
                    self.assertEqual(checker.check_semantics(value), case['expected'])

    def test_broken_schema_is_infrastructure_failure(self):
        schema = json.loads(checker.SCHEMA_PATH.read_text())
        schema['properties']['operation'] = {'$ref': '#/$defs/DOES_NOT_EXIST'}
        with tempfile.TemporaryDirectory() as directory:
            path = Path(directory) / 'broken.json'
            path.write_text(json.dumps(schema))
            with self.assertRaisesRegex(RuntimeError, 'schema oracle failed'):
                checker.schema_results([self.values[0]], path)

    def test_typed_field_drift_is_infrastructure_failure(self):
        schema = json.loads(checker.SCHEMA_PATH.read_text())
        schema['$defs']['V2AdmissionPlan']['required'].remove('fence')
        with tempfile.TemporaryDirectory() as directory:
            path = Path(directory) / 'drift.json'
            path.write_text(json.dumps(schema))
            with self.assertRaisesRegex(RuntimeError, 'typed/schema field drift'):
                checker.schema_results([self.values[0]], path)

    def test_top_level_typed_field_drift_is_infrastructure_failure(self):
        schema = json.loads(checker.SCHEMA_PATH.read_text())
        schema['required'].remove('request_digest')
        with tempfile.TemporaryDirectory() as directory:
            path = Path(directory) / 'trace-drift.json'
            path.write_text(json.dumps(schema))
            with self.assertRaisesRegex(RuntimeError, 'typed/schema field drift V2Trace'):
                checker.schema_results([self.values[0]], path)

    def test_schema_type_enum_and_constraint_drift_is_infrastructure_failure(self):
        mutations = [
            ('field type', lambda schema: schema['$defs']['V2HighWatermarkPlan']['properties']['expected'].__setitem__('type', 'string')),
            ('enum member', lambda schema: schema['$defs']['V2AdmissionKey']['properties']['persona_id']['enum'].remove('melchior')),
            ('numeric bound', lambda schema: schema['$defs']['V2SafeUInt'].__setitem__('maximum', 500)),
            ('array bound', lambda schema: schema['$defs']['V2AdmissionPlan']['properties']['scoped_epochs'].__setitem__('maxItems', 4)),
            ('pattern', lambda schema: schema['$defs']['V2Digest'].__setitem__('pattern', '^[0-9a-f]+$')),
            ('additional properties', lambda schema: schema['$defs']['V2AdmissionKey'].__setitem__('additionalProperties', True)),
        ]
        for name, mutate in mutations:
            with self.subTest(drift=name):
                schema = copy.deepcopy(json.loads(checker.SCHEMA_PATH.read_text()))
                mutate(schema)
                with tempfile.TemporaryDirectory() as directory:
                    path = Path(directory) / 'constraint-drift.json'
                    path.write_text(json.dumps(schema))
                    with self.assertRaisesRegex(RuntimeError, 'typed/schema'):
                        checker.schema_results([self.values[0]], path)

    def test_query_only_fence_is_not_a_legal_mode(self):
        schema = json.loads(checker.SCHEMA_PATH.read_text())
        modes = schema['$defs']['V2ExposureFencePlan']['properties']['mode']['enum']
        self.assertNotIn('QUERY_ONLY', modes)
        self.assertEqual(set(modes), {'AUTHORITY_ORDERED_GATE', 'PROVIDER_ENFORCED_LEASE'})

    def test_admission_and_settlement_are_separate_write_sets(self):
        value = copy.deepcopy(self.data['bases']['admission'])
        value['settlement'] = copy.deepcopy(self.data['bases']['settlement']['settlement'])
        self.assertEqual(checker.check_semantics(value), 'WRITE_SET')
        value = copy.deepcopy(self.data['bases']['settlement'])
        value['settlement']['mutation_set'] = checker.ADMISSION_MUTATIONS
        self.assertEqual(checker.check_semantics(value), 'WRITE_SET')

    def test_revocation_after_gate_does_not_become_verified_success(self):
        value = copy.deepcopy(self.data['bases']['settlement'])
        value['authority']['revocation_sequence'] = value['authority']['gate_sequence'] + 1
        value['authority']['settlement_sequence'] = 0
        self.assertEqual(checker.check_semantics(value), 'UNKNOWN')

    def test_old_generation_cannot_reacquire_admission(self):
        value = copy.deepcopy(self.data['bases']['admission'])
        value['admission_binding'] = {
            'request_digest': value['request_digest'],
            'admission_commit_id': 'commit-old-generation',
            'generation': value['admission']['execution_generation'] - 1,
            'status': 'ADMITTED',
            'exposure_permitted': False,
        }
        self.assertEqual(checker.check_semantics(value), 'STALE')

    def test_settlement_replay_requires_receipt_digest_binding(self):
        value = copy.deepcopy(self.data['bases']['settlement'])
        value['authority']['settlement_sequence'] = 12
        value['settlement_binding'] = committed_settlement_binding(value)
        value['settlement']['receipt']['verification_ref']['digest'] = 'e' * 64
        self.assertEqual(checker.check_semantics(value), 'TOKEN_BINDING')

    def test_cross_stage_admission_gate_first_settlement_succeeds(self):
        admission = copy.deepcopy(self.data['bases']['admission'])
        gate = copy.deepcopy(self.data['bases']['exposure_gate'])
        settlement = copy.deepcopy(self.data['bases']['settlement'])
        self.assertEqual(checker.check_semantics(admission), 'STATIC_VALID')
        self.assertEqual(checker.check_semantics(gate), 'STATIC_VALID')
        self.assertEqual(checker.check_semantics(settlement), 'STATIC_VALID')
        self.assertEqual(settlement['admission_binding']['request_digest'], admission['request_digest'])
        self.assertIsNone(settlement['settlement_binding'])

    def test_settlement_replay_same_original_is_replay(self):
        value = copy.deepcopy(self.data['bases']['settlement'])
        value['authority']['settlement_sequence'] = 12
        value['settlement_binding'] = committed_settlement_binding(value)
        self.assertEqual(checker.check_semantics(value), 'REPLAY')

    def test_settlement_replay_with_recomputed_different_receipt_is_conflict(self):
        value = copy.deepcopy(self.data['bases']['settlement'])
        value['authority']['settlement_sequence'] = 12
        value['settlement_binding'] = committed_settlement_binding(value)
        value['settlement']['receipt']['verification_ref']['digest'] = 'e' * 64
        new_digest = checker.settlement_digest(value['settlement'])
        value['settlement']['settlement_digest'] = new_digest
        value['request_digest'] = new_digest
        # The authoritative settlement binding still points at the original receipt.
        self.assertEqual(checker.check_semantics(value), 'CONFLICT')

    def test_exposure_gate_core_does_not_create_a_permit(self):
        source = (ROOT / 'backend/adapter/authority_exposure_gate_blocked.go').read_text()
        self.assertIn("Outcome: \"BLOCKED\"", source)
        self.assertIn("authority_sequence, exposure_permitted, committed_at", source)
        self.assertIn("VALUES (?, ?, ?, ?, ?, 'BLOCKED', ?, ?, ?, ?, 0, 0, ?)", source)
        self.assertNotIn("exposure_permitted=1", source)
        migration = (ROOT / 'docker/atlas/migrations/magi_s34_authority_exposure_gate.sql').read_text()
        self.assertIn('magi_authority_exposure_gate_attempt_v2', migration)
        self.assertIn('magi_authority_exposure_gate_commit_v2', migration)
        self.assertIn('exposure_permitted TINYINT(1) NOT NULL DEFAULT 0', migration)

    def test_v1_contract_identity_is_unchanged(self):
        schema = json.loads((ROOT / 'docs/contracts/authority-v1/schema.json').read_text())
        self.assertEqual(schema['properties']['contract_version']['const'], 'magi-authority/1')
        self.assertIn('JOURNAL_ADMIT', schema['$defs']['Operation']['enum'])
        self.assertNotIn('JOURNAL_ADMIT_V2', schema['$defs']['Operation']['enum'])


if __name__ == '__main__':
    unittest.main()
