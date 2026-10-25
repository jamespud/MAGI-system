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
        value['existing_binding'] = {
            'request_digest': value['request_digest'],
            'admission_commit_id': 'commit-old-generation',
            'generation': value['admission']['execution_generation'] - 1,
            'status': 'ADMITTED',
            'exposure_permitted': False,
        }
        self.assertEqual(checker.check_semantics(value), 'STALE')

    def test_v1_contract_identity_is_unchanged(self):
        schema = json.loads((ROOT / 'docs/contracts/authority-v1/schema.json').read_text())
        self.assertEqual(schema['properties']['contract_version']['const'], 'magi-authority/1')
        self.assertIn('JOURNAL_ADMIT', schema['$defs']['Operation']['enum'])
        self.assertNotIn('JOURNAL_ADMIT_V2', schema['$defs']['Operation']['enum'])


if __name__ == '__main__':
    unittest.main()
