#!/usr/bin/env python3
"""Independent Python C0 identities for synthetic/offline ContextManifest v1.

Never executes Go. --write freezes only this slice's new vectors; default checks.
Bytes/digests prove neither snapshot authenticity nor role/data permission.
"""
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
ROLES = ("melchior", "balthasar", "casper")


def main():
    write = sys.argv[1:] == ["--write"]
    assert write or not sys.argv[1:]
    materials = json.loads((HERE / "context_snapshot_bytes.json").read_text())
    for name in ("context_manifest", "context_manifest_empty_views"):
        body = reference.parse((HERE / (name + ".json")).read_bytes())
        assert body["artifact_type"] == "context_manifest" and body["schema_version"] == "1.0"
        assert set(body["roles"]) == set(ROLES)
        required = {body["shared_fact_snapshot_id"]: "SHARED_FACT",
                    body["user_preference_snapshot_id"]: "USER_PREFERENCE",
                    body["source_dependency_index_version"]: "DEPENDENCY_INDEX"}
        for role in ROLES:
            view = body["roles"][role]
            assert view["persona_spec_id"] == json.loads(
                (HERE / ("persona_" + role + ".golden.json")).read_text())["artifact_id"]
            required[view["experience_snapshot_id"]] = "EXPERIENCE"
            assert view["allowed_context_ids"] == sorted(set(view["allowed_context_ids"]))
            required.update({id: "CONTEXT" for id in view["allowed_context_ids"]})
        refs = body["snapshot_refs"]
        assert [s["snapshot_id"] for s in refs] == sorted(required)
        assert body["domain_profile_id"] == json.loads((HERE / "golden.json").read_text())["artifact_id"]
        for snapshot in refs:
            id = snapshot["snapshot_id"]
            assert snapshot["kind"] == required[id]
            assert hashlib.sha256(bytes.fromhex(materials[id])).hexdigest() == snapshot["content_digest"]
        canonical = reference.canonical_bytes(body)
        digest = hashlib.sha256(b"magi:canonical:v1:context_manifest\n" + canonical).hexdigest()
        golden = {"canonical_profile": "magi-cj/1", "digest_alg": "sha256",
                  "digest": digest, "artifact_id": "ctxm_" + digest,
                  "canonical_bytes": len(canonical), "snapshot_count": len(refs),
                  "qualification": "UNVERIFIABLE_REFERENCE", "actual_reads": "NOT_RUN"}
        cb, gp = HERE / (name + ".canonical.json"), HERE / (name + ".golden.json")
        if write:
            cb.write_bytes(canonical)
            gp.write_text(json.dumps(golden, indent=2) + "\n")
        else:
            assert cb.read_bytes() == canonical, name + " canonical bytes"
            assert json.loads(gp.read_text()) == golden, name + " frozen identity"
        print(f"PASS: {name}: Python full-body bytes/digest/ctxm, {len(refs)} raw snapshot digests; offline only")


if __name__ == "__main__":
    main()
