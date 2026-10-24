#!/usr/bin/env python3
"""Exact negative codes; schema/compiler failures never count as semantic denials."""
import copy
import importlib.util
import json
from pathlib import Path
import tempfile
import unittest
ROOT = Path(__file__).resolve().parents[1]
spec = importlib.util.spec_from_file_location('authority_check', ROOT / 'scripts/check-authority-contract.py')
checker = importlib.util.module_from_spec(spec)
spec.loader.exec_module(checker)

class ContractTests(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        cls.data = json.loads((ROOT / 'docs/contracts/authority-v1/traces.json').read_text())
        cls.values = [checker.materialize(cls.data, c) for c in cls.data['cases']]
        cls.shapes = checker.schema_results(cls.values)

    def test_shapes_and_semantics_have_separate_expected_codes(self):
        self.assertEqual(len(self.shapes), len(self.values))
        for f, value, shape in zip(self.data['cases'], self.values, self.shapes):
            with self.subTest(fixture=f['name']):
                if f['expected'] in ('SCHEMA', 'C0'):
                    self.assertEqual(shape, f['expected'])
                else:
                    self.assertEqual(shape, '', 'semantic negative must reach semantic checker')
                    self.assertEqual(checker.check_semantics(value), f['expected'])

    def test_broken_schema_is_infrastructure_failure(self):
        schema = json.loads((ROOT / 'docs/contracts/authority-v1/schema.json').read_text())
        schema['properties']['action'] = {'$ref': '#/$defs/DOES_NOT_EXIST'}
        with tempfile.TemporaryDirectory() as directory:
            path = Path(directory) / 'broken.json'
            path.write_text(json.dumps(schema))
            with self.assertRaisesRegex(RuntimeError, 'schema oracle failed'):
                checker.schema_results([self.values[0]], path)

    def test_typed_field_drift_is_infrastructure_failure(self):
        schema = json.loads((ROOT / 'docs/contracts/authority-v1/schema.json').read_text())
        schema['$defs']['VerifiedToken']['required'].remove('operation')
        with tempfile.TemporaryDirectory() as directory:
            path = Path(directory) / 'drift.json'
            path.write_text(json.dumps(schema))
            with self.assertRaisesRegex(RuntimeError, 'typed/schema field drift'):
                checker.schema_results([self.values[0]], path)

    def test_original_query_cannot_mint_new_authority(self):
        value = copy.deepcopy(self.data['bases']['send'])
        value['replay']['mode'] = 'ORIGINAL'
        value['current']['revoked'] = True
        self.assertEqual(checker.check_semantics(value), 'STATIC_VALID')
        value['replay']['resend'] = True
        self.assertEqual(checker.check_semantics(value), 'REPLAY')

if __name__ == '__main__':
    unittest.main()
