#!/usr/bin/env python3
"""Independent C1-B2 oracle: C0 Python bytes/identity, never Go output.

--write deliberately refreshes frozen vectors from the checked-in raw fixtures.
The default verifies them and traces all fixed baseline codes to C1-A section 4.2.
Synthetic policy references do not establish qualification or behavior.
"""
import copy
import hashlib
import importlib.util
import json
import sys
from pathlib import Path

HERE = Path(__file__).resolve().parent
ROOT = HERE.parents[3]
spec = importlib.util.spec_from_file_location(
    "canonical_reference", ROOT / "backend/domain/canonical/testdata/verify_golden.py")
reference = importlib.util.module_from_spec(spec)
spec.loader.exec_module(reference)


def identity(body):
    canonical = reference.canonical_bytes(body)
    digest = hashlib.sha256(b"magi:canonical:v1:persona_spec\n" + canonical).hexdigest()
    return canonical, digest, "pspc_" + digest


def references(value, path="$"):
    if isinstance(value, dict):
        if "content_digest" in value:
            return [{"path": path, "reference": value}]
        return [r for k in sorted(value) for r in references(value[k], path + "." + k)]
    if isinstance(value, list):
        return [r for i, child in enumerate(value) for r in references(child, f"{path}[{i}]")]
    return []


def baseline_from_contract(role):
    text = (ROOT / "docs/input-artifact-contract.md").read_text()
    section = text.split("### 4.2 ")[1].split("### 4.3 ")[0]
    rows = [line.split("|")[1:-1] for line in section.splitlines()
            if line.startswith("| " + role + " |")]
    assert len(rows) == 3
    core, review = [x.strip() for x in rows[0][1:]]
    investigation, evidence, buckets = [x.strip() for x in rows[1][1:]]
    # Requirement names, unlike their explanatory prose, are ASCII tokens.
    import re
    requirements = re.findall(r"[A-Z][A-Z_]+(?=：)", investigation)
    conditions = [[x.strip() for x in cell.strip().split("、")] for cell in rows[2][1:]]
    return {
        "core_priority": core.split(" → "), "mandatory_reviews": sorted(review.split("、")),
        "minimum_investigation": sorted(requirements), "evidence_priority": evidence.split(" → "),
        "call_allocation": buckets.split(" / "),
        **dict(zip(["support_conditions", "reject_conditions", "abstain_conditions", "correction_triggers"],
                   [sorted(c) for c in conditions])),
    }


def main():
    write = sys.argv[1:] == ["--write"]
    assert write or not sys.argv[1:]
    domain_id = json.loads((HERE / "golden.json").read_text())["artifact_id"]
    for role in ("melchior", "balthasar", "casper"):
        body = reference.parse((HERE / f"persona_{role}.json").read_bytes())
        baseline = baseline_from_contract(role)
        for key, expected in baseline.items():
            actual = body[key]
            if key == "minimum_investigation":
                actual = [x["requirement_id"] for x in actual]
            elif key == "call_allocation":
                assert [x["calls"] for x in actual] == [2, 2, 1, 1]
                actual = [x["bucket_id"] for x in actual]
            assert actual == expected, (role, key, actual, expected)
        canonical, digest, artifact_id = identity(body)
        mutations = []
        for field in ("core_priority", "mandatory_reviews", "minimum_investigation", "evidence_priority",
                      "call_allocation", "support_conditions", "reject_conditions", "abstain_conditions",
                      "correction_triggers"):
            changed = copy.deepcopy(body)
            if field == "minimum_investigation":
                changed[field][0]["requirement_id"] = "UNRELATED"
            elif field == "call_allocation":
                changed[field][0]["bucket_id"] = "UNRELATED"
            else:
                changed[field][0] = "UNRELATED"
            _, changed_digest, changed_id = identity(changed)
            mutations.append({"field": field, "digest": changed_digest, "artifact_id": changed_id,
                              "validation": "REJECTED"})
        golden = {
            "canonical_profile": "magi-cj/1", "digest_alg": "sha256",
            "digest": digest, "artifact_id": artifact_id, "selected_domain_profile_id": domain_id,
            "status": {"baseline": "VALID_OFFLINE", "domain_relations": "PARTIAL_OFFLINE",
                       "qualification": "UNVERIFIABLE_REFERENCE", "behavior": "NOT_RUN"},
            "pending_references": references(body), "mutations": mutations,
        }
        cb_path = HERE / f"persona_{role}.canonical.json"
        g_path = HERE / f"persona_{role}.golden.json"
        if write:
            cb_path.write_bytes(canonical)
            g_path.write_text(json.dumps(golden, indent=2) + "\n")
        else:
            assert cb_path.read_bytes() == canonical, role + " canonical bytes"
            assert json.loads(g_path.read_text()) == golden, role + " golden mismatch"
        assert all(m["artifact_id"] != artifact_id for m in mutations)
        print(f"PASS: {role}: C1-A baseline, Python bytes/digest/ID, 9 rejected core mutations; qualification unverified; behavior NOT RUN")


if __name__ == "__main__":
    main()
