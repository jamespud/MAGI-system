#!/usr/bin/env python3
"""Reproduce frozen C1-B1 bytes/identities using the independent C0 Python oracle.
No Go implementation is executed. This checks identities, not qualification.
"""
import copy
import hashlib
import importlib.util
import json
from pathlib import Path

HERE = Path(__file__).resolve().parent
ROOT = HERE.parents[3]
spec = importlib.util.spec_from_file_location(
    "canonical_reference", ROOT / "backend/domain/canonical/testdata/verify_golden.py")
reference = importlib.util.module_from_spec(spec)
spec.loader.exec_module(reference)


def identity(body):
    canonical = reference.canonical_bytes(body)
    digest = hashlib.sha256(b"magi:canonical:v1:domain_profile\n" + canonical).hexdigest()
    return canonical, digest, "dprf_" + digest


def main():
    body = reference.parse((HERE / "domain_profile.json").read_bytes())
    golden = json.loads((HERE / "golden.json").read_text())
    canonical, digest, artifact_id = identity(body)
    assert canonical == (HERE / "domain_profile.canonical.json").read_bytes(), "canonical mismatch"
    assert digest == golden["digest"], "digest mismatch"
    assert artifact_id == golden["artifact_id"], "ID mismatch"
    for mutation in golden["mutations"]:
        changed = copy.deepcopy(body)
        parts = mutation["path"].split(".")
        target = changed
        for part in parts[:-1]:
            target = target[part]
        target[parts[-1]] = mutation["value"]
        _, _, changed_id = identity(changed)
        assert changed_id == mutation["artifact_id"] and changed_id != artifact_id, mutation["path"]
    assert golden["qualification"] == "UNVERIFIABLE_REFERENCE"
    print(f"PASS: C1-B1 bytes/digest/ID and {len(golden['mutations'])} authority-field mutations; qualification UNVERIFIABLE_REFERENCE")


if __name__ == "__main__":
    main()
