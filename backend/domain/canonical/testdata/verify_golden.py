#!/usr/bin/env python3
"""Independent reference implementation of MAGI canonical profile magi-cj/1.

This is deliberately NOT the Go implementation: it exists so the Go encoder is
verified against a second language on the same golden vectors (C0 requirement).
Rules implemented (see docs/canonical-encoding-contract.md sections 3-5):
  - object keys sorted by UTF-16 code unit order, arrays keep order
  - RFC 8785 minimal string escaping (no HTML escaping)
  - MAGI: integers only, range +/-(2**53-1), "-0" rejected
  - MAGI: NFC required, never applied
  - duplicate keys rejected (also `"a"` vs `"\\u0061"`)
  - lone surrogates rejected
Usage: verify_golden.py [golden_vectors.json]
Exit 0 = every vector reproduced; 1 = mismatch.
"""
import hashlib
import json
import sys
import unicodedata

MAX_SAFE = 2**53 - 1
PROFILE = "magi-cj/1"
TYPE_PREFIXES = {
    "decision_charter": "chtr", "domain_profile": "dprf", "persona_spec": "pspc",
    "context_manifest": "ctxm", "candidate_snapshot": "csnp", "sealed_ballot": "sbll",
    "ballot_set": "bset", "decision_record": "drec", "authority_proof": "aprf",
    "action_intent": "aint", "audit_receipt": "arcp", "send_record": "srec",
    "acceptance_manifest": "aman", "read_set_snapshot": "rset",
}


class CanonicalError(Exception):
    def __init__(self, kind, detail=""):
        super().__init__(f"{kind}: {detail}" if detail else kind)
        self.kind = kind


def _no_dup_pairs(pairs):
    seen = set()
    for k, _ in pairs:
        if k in seen:
            raise CanonicalError("duplicate_key", k)
        seen.add(k)
    return dict(pairs)


def _reject_constant(name):
    raise CanonicalError("float_number", name)


def _parse_int(literal):
    # json passes the raw integer literal, so "-0" is still visible here.
    if literal == "-0":
        raise CanonicalError("negative_zero")
    value = int(literal)
    if abs(value) > MAX_SAFE:
        raise CanonicalError("integer_out_of_range", literal)
    return value


def _parse_float(literal):
    # Any fraction or exponent is rejected by MAGI, including "1.0" and "1e0".
    raise CanonicalError("float_number", literal)


def _check_string(s):
    if any(0xD800 <= ord(ch) <= 0xDFFF for ch in s):
        raise CanonicalError("lone_surrogate")
    if not unicodedata.is_normalized("NFC", s):
        raise CanonicalError("not_nfc")


def _check_value(v):
    if isinstance(v, bool) or v is None:
        return
    if isinstance(v, int):
        if abs(v) > MAX_SAFE:
            raise CanonicalError("integer_out_of_range", str(v))
        return
    if isinstance(v, float):
        raise CanonicalError("float_number", repr(v))
    if isinstance(v, str):
        _check_string(v)
        return
    if isinstance(v, list):
        for item in v:
            _check_value(item)
        return
    if isinstance(v, dict):
        for k, val in v.items():
            _check_string(k)
            _check_value(val)
        return
    raise CanonicalError("unsupported_type", type(v).__name__)


def _escape(s):
    out = ['"']
    for ch in s:
        o = ord(ch)
        if ch == '"':
            out.append('\\"')
        elif ch == "\\":
            out.append("\\\\")
        elif ch == "\b":
            out.append("\\b")
        elif ch == "\t":
            out.append("\\t")
        elif ch == "\n":
            out.append("\\n")
        elif ch == "\f":
            out.append("\\f")
        elif ch == "\r":
            out.append("\\r")
        elif o < 0x20:
            out.append("\\u%04x" % o)
        else:
            out.append(ch)
    out.append('"')
    return "".join(out)


def canonical_bytes(value):
    """Return canonical UTF-8 bytes; raises CanonicalError."""
    _check_value(value)

    def emit(v):
        if v is None:
            return "null"
        if v is True:
            return "true"
        if v is False:
            return "false"
        if isinstance(v, int):
            return str(v)
        if isinstance(v, str):
            return _escape(v)
        if isinstance(v, list):
            return "[" + ",".join(emit(x) for x in v) + "]"
        if isinstance(v, dict):
            # UTF-16 code unit order == byte order of UTF-16BE encoding
            keys = sorted(v.keys(), key=lambda k: k.encode("utf-16-be"))
            return "{" + ",".join(_escape(k) + ":" + emit(v[k]) for k in keys) + "}"
        raise CanonicalError("unsupported_type", type(v).__name__)

    return emit(value).encode("utf-8")


def parse(raw):
    """Strict parse: duplicates, NaN/Infinity and floats are rejected."""
    try:
        v = json.loads(
            raw,
            object_pairs_hook=_no_dup_pairs,
            parse_constant=_reject_constant,
            parse_int=_parse_int,
            parse_float=_parse_float,
        )
    except CanonicalError:
        raise
    except json.JSONDecodeError as e:
        raise CanonicalError("invalid_json", str(e))
    return v


def digest(artifact_type, canon):
    prefix = "magi:canonical:v1:" + artifact_type + "\n"
    h = hashlib.sha256()
    h.update(prefix.encode("utf-8"))
    h.update(canon)
    return h.hexdigest()


def verify(path):
    vectors = json.load(open(path, encoding="utf-8"))
    failures = []
    for v in vectors["positive"]:
        name = v["name"]
        try:
            value = parse(v["input_json"])
            canon = canonical_bytes(value)
        except CanonicalError as e:
            failures.append(f"{name}: unexpected {e}")
            continue
        if canon.hex() != v["canonical_bytes_hex"]:
            failures.append(f"{name}: canonical {canon.hex()} != {v['canonical_bytes_hex']}")
            continue
        if v.get("digest"):
            got = digest(v["artifact_type"], canon)
            if got != v["digest"]:
                failures.append(f"{name}: digest {got} != {v['digest']}")
        if "artifact_id" in v:
            got_id = TYPE_PREFIXES[v["artifact_type"]] + "_" + digest(v["artifact_type"], canon)
            if got_id != v["artifact_id"]:
                failures.append(f"{name}: artifact ID {got_id} != {v['artifact_id']}")
    for v in vectors["negative"]:
        name = v["name"]
        # Rejection may happen at parse time (duplicates, bad JSON, numbers) or at
        # canonicalization time (NFC, lone surrogates), so run the whole pipeline.
        try:
            canon = canonical_bytes(parse(v["input_json"]))
        except CanonicalError as e:
            if e.kind != v["expected_error_kind"]:
                failures.append(f"{name}: error {e.kind} != {v['expected_error_kind']}")
            continue
        failures.append(f"{name}: expected {v['expected_error_kind']}, canonicalized {canon.hex()}")
    return failures


if __name__ == "__main__":
    target = sys.argv[1] if len(sys.argv) > 1 else "golden_vectors.json"
    bad = verify(target)
    for b in bad:
        print("FAIL", b)
    print(f"{'FAIL' if bad else 'OK'} ({len(bad)} failures)")
    sys.exit(1 if bad else 0)
