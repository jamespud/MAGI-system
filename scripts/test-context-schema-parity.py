#!/usr/bin/env python3
"""Exercise the parity command against actual schemas and deliberate drift."""
import copy
import json
from pathlib import Path
import subprocess
import sys
import tempfile
import unittest

ROOT = Path(__file__).resolve().parents[1]
CHECKER = ROOT / 'scripts/check-context-schema-parity.py'
INPUT = json.loads((ROOT / 'docs/contracts/input-artifacts/schema.json').read_text())
MIRROR = json.loads((ROOT / 'docs/contracts/decision-artifacts-v2/schema.json').read_text())


class ParityTest(unittest.TestCase):
    def run_check(self, left, right):
        with tempfile.TemporaryDirectory() as directory:
            paths = [Path(directory) / n for n in ('input.json', 'mirror.json')]
            for path, body in zip(paths, (left, right)):
                path.write_text(json.dumps(body))
            return subprocess.run([sys.executable, str(CHECKER), *map(str, paths)],
                                  capture_output=True, text=True)

    def test_existing_four_refinements_pass(self):
        result = self.run_check(INPUT, MIRROR)
        self.assertEqual(result.returncode, 0, result.stderr)

    def test_deliberate_drift_is_rejected(self):
        def missing_required(d):
            d['ContextManifest']['required'].remove('roles')

        def role_expansion(d):
            d['ContextManifest']['properties']['roles']['properties']['other'] = {'$ref': '#/$defs/RoleView'}

        def loose_descriptor(d):
            d['SnapshotRef']['additionalProperties'] = True

        def loose_token(d):
            d['Token']['pattern'] = '^.*$'

        def index_ref_drift(d):
            d['ContextManifest']['properties']['source_dependency_index_version'] = {'$ref': '#/$defs/Token'}

        def raw_ref_drift(d):
            d['ExternalRef']['properties']['version'] = {'$ref': '#/$defs/Token'}

        def epoch_overflow(d):
            d['ContextManifest']['properties']['context_revocation_epoch']['maximum'] += 1

        def alias_escape(d):
            d['FixedVersion']['allOf'][1]['not']['pattern'] = 'latest'

        def snapshot_kind(d):
            d['SnapshotRef']['properties']['kind']['enum'].append('ARBITRARY')

        for edit in (missing_required, role_expansion, loose_descriptor, loose_token,
                     index_ref_drift, raw_ref_drift, epoch_overflow, alias_escape, snapshot_kind):
            with self.subTest(edit=edit.__name__):
                right = copy.deepcopy(MIRROR)
                edit(right['$defs'])
                result = self.run_check(INPUT, right)
                self.assertNotEqual(result.returncode, 0, 'checker accepted drift: ' + edit.__name__)
                self.assertIn('SCHEMA_DRIFT', result.stderr)

    def test_drift_in_input_is_also_rejected(self):
        left = copy.deepcopy(INPUT)
        left['$defs']['RoleView']['required'].remove('persona_spec_id')
        result = self.run_check(left, MIRROR)
        self.assertNotEqual(result.returncode, 0, 'checker ignored input-side drift')
        self.assertIn('SCHEMA_DRIFT', result.stderr)


if __name__ == '__main__':
    unittest.main()
