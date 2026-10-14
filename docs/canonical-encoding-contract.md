# Canonical Encoding & Artifact Identity Contract (C0) — v1

> Scope: Linear **CAS-9 / stage C0**. Authority: `docs/magi-final-personality-acceptance.md`
> (Rev4) is the top design authority; `docs/decision-protocol-contract.md` freezes the
> decision-protocol semantics; this document freezes the **encoding and identity** layer.
>
> **Status: frozen for implementation (design-approved 2026-10-15).** Rev4 §2 fixes the
> *requirements* (fixed field name/order/type/default rules; reject duplicate keys;
> reject float ambiguity). The concrete algorithm choices below are a **MAGI design
> decision**, not a restatement of Rev4 or of RFC 8785. Where this contract is stricter
> than RFC 8785, it says so explicitly — none of the extra restrictions may be
> attributed to JCS.

## 1. Rev4 requirements this contract satisfies

Rev4 §2: 版本号用于人类识别、内容摘要用于机器绑定且都须校验；工件修改产生新标识；
安全敏感摘要使用明确的编码契约（字段名/顺序/类型/缺省规则固定；**拒绝重复字段和浮点歧义**）；
授权密钥不进入工件正文。

## 2. Profiles: one new profile, three frozen legacy algorithms

The repository already produces **three different, non-interchangeable** digests, and all
three are persisted and re-compared at runtime. They MUST NOT be described as one
"go-json/v0" algorithm, and none of them may be recomputed, backfilled or silently
tolerated on mismatch.

| Compatibility algorithm id | Producer | Exact preimage construction | Persisted as |
| --- | --- | --- | --- |
| `legacy-manifest/v1` | `domain/execution/manifest.go` `canonicalDigest(prefix, value)` | `sha256(prefix + "\|" + json.Marshal(value))`, Go struct field order, HTML-escaped | `magi_agent_checkpoint.manifest_digest` |
| `legacy-approval-intent/v1` | `domain/execution/identity.go` `ApprovalIntentDigest` / `ToolIdempotencyKey` | `sha256("approval-intent:v1\|" + toolName + "\|" + sha256hex(canonicalArguments))` | `approval.intent_digest` |
| `legacy-a2a-request/v1` | `application/a2a/input.go` | `sha256(json.Marshal(canonicalInput))`, **no domain separator** | `a2a_submission.request_hash` |

Rules:

- **L2-01** Legacy producers keep producing these exact bytes for as long as the legacy
  protocol runs. C0 does **not** stop legacy writers and does **not** migrate their columns.
- **L2-02** A legacy digest is only compared with the same algorithm id. Re-encoding an
  existing row into a new algorithm is forbidden; so is treating a mismatch as success.
- **L2-03** New authoritative protocol artifacts (Charter, DomainProfile, PersonaSpec,
  ContextManifest, CandidateSnapshot, SealedBallot, BallotSet, DecisionRecord,
  AuthorityProof, ActionIntent, AuditReceipt, SendRecord, AcceptanceManifest) MUST use the
  new profile. The legacy algorithms are never used for new artifacts.
- **L2-04** Profile is part of the artifact's declared metadata (`canonical_profile`,
  `digest_alg`) so a future profile change is distinguishable from tampering.

## 3. `magi-cj/1` canonical byte form

Input MUST be a JSON value in the I-JSON subset. Canonical bytes follow RFC 8785 (JCS) plus
the MAGI restrictions marked **[MAGI]**. Anything marked **[MAGI]** is our choice, not JCS.

1. **Object keys sorted** by UTF-16 code unit order (RFC 8785 §3.2.3, including non-ASCII
   and surrogate-pair boundaries). Arrays keep producer order; set-semantics fields must be
   sorted by the producer before encoding — the encoder never reorders arrays.
2. **No insignificant whitespace.**
3. **Strings**: RFC 8785 minimal escaping — only `"`, `\` and U+0000–U+001F are escaped,
   using `\b \t \n \f \r` where defined and `\u00xx` otherwise. HTML escaping (`\u003c`) is
   **forbidden**. Output is UTF-8. The original Unicode content is preserved: **JCS does not
   normalize**, so the encoder never rewrites the string it signs.
4. **[MAGI] NFC is verified, not applied.** For *new authoritative artifacts' canonical text
   fields*, non-NFC input is a validation error. The encoder MUST NOT silently normalize and
   then sign the normalized form.
   - **Exception (byte fidelity):** external evidence raw text, HTTP raw bodies and
     third-party signed payloads must be preserved byte-for-byte. They bind via a raw-byte
     digest, an immutable source reference, or an explicit wrapper structure — never by
     NFC-transforming the content and presenting it as the original.
5. **[MAGI] Numbers: integer literals only.** JCS itself accepts IEEE-754 doubles and
   defines their serialization; MAGI restricts the profile further because the protocol has
   no legitimate use for them:
   - accepted: decimal integer literals with no fraction or exponent; `-0` is rejected;
   - rejected: `1.0`, `1e0`, `NaN`, `Infinity`, and any non-standard JSON number;
   - accepted integer range: **[-(2^53-1), 2^53-1]** (cross-language exact);
   - larger integers and exact decimals MUST use a schema-constrained decimal **string**, or
     an **integer plus a declared unit/scale**.
   - **Unit discipline:** a fractional value may only be converted to an integer form with a
     rule that matches its dimension. Error *ratios*, *percentage points*, latency, money and
     confidence bounds each need their own documented conversion; `0.005` must not be
     blanket-converted to `5000` without recording what it measures.
6. **`null` vs absent vs empty** are three distinct, schema-declared cases: required field
   must be present (`null` only where the schema is nullable); optional absent means "not
   supplied"; `[]`/`""`/`{}` are ordinary values and never mean absence. Canonical form keeps
   explicit `null` and never materializes absent fields.
7. **[MAGI] Time: UTC, exactly three fractional digits** — `2026-10-15T00:00:00.000Z`. Other
   offsets and other precisions are rejected for new protocol time fields.
   - The digest step MUST NOT round, truncate or extend precision. A new artifact must
     produce its millisecond-precision value before it is frozen/hashed.
   - Precision handling MUST NOT enlarge an authorization/expiry window; the original
     expiry semantics are preserved.
   - Existing nanosecond timestamps and historical records keep legacy semantics untouched.
8. **Booleans** are `true`/`false`; `0`/`1` are rejected for boolean fields.
9. **Control characters** are not banned by the common encoder: JCS permits escaped `\n`,
   `\t` etc. Whether a field may contain them is decided by that field's schema, so a
   string-level ban MUST NOT be attributed to JCS.

## 4. Digest and identity

```text
domain_separator = "magi:canonical:v1:" + artifact_type + "\n"
digest_bytes     = SHA-256( UTF8(domain_separator) || canonical_bytes )
digest           = lowercase hex(digest_bytes)            # 64 chars
artifact_id      = <type_prefix> + "_" + digest            # derived, never supplied
```

- **`artifact_type` is a closed whitelist** (`decision_charter`, `domain_profile`,
  `persona_spec`, `context_manifest`, `candidate_snapshot`, `sealed_ballot`, `ballot_set`,
  `decision_record`, `authority_proof`, `action_intent`, `audit_receipt`, `send_record`,
  `acceptance_manifest`). Unknown types are an error. Type strings are `[a-z][a-z0-9_]*`;
  they can never contain `\n` or other separators, so the preimage is unambiguous.
- The digest is determined by **profile version + artifact type + canonical bytes** together.
- **Every authoritative field participates**, including `schema_version`. Only the
  artifact's **own** circular derived fields (`artifact_id`, `digest`,
  `canonical_profile`, `digest_alg`) are excluded. No other identity field may be excluded.
- **`artifact_id` is derived from the full digest**; a caller-supplied id is not accepted.
- A digest proves **content equality only**. Signature validity, authorization validity,
  freshness/revocation and execution success are separate checks and MUST NOT be inferred.

## 5. Parsing rules (strict, fail-closed, explicit errors)

Parsing MUST return a specific error — never `panic`, never a silent overwrite — for:

- **duplicate object keys**, including keys that only collide *after* unescaping
  (`{"a":1,"\u0061":2}` is a duplicate). Go's `encoding/json` keeps the last value, so it
  must not be used directly on untrusted artifact JSON;
- unknown fields (schemas are `additionalProperties: false`) and unknown enum values;
- unknown `schema_version` or unknown artifact type;
- non-NFC canonical text fields (§3.4), lone surrogates, and invalid UTF-8;
- floats, exponents, `-0`, `NaN`/`Inf`, and integers outside ±(2^53-1);
- timestamps that are not UTC RFC 3339 with exactly three fractional digits;
- trailing data after the JSON value, and payloads over the per-type byte ceiling.

## 6. Golden vectors and RED evidence

`backend/domain/canonical/testdata/golden_vectors.json` holds
`{algorithm, artifact_type, input_json, canonical_bytes_hex, digest|null}` triples plus
negative cases `{input_json, expected_error_kind}`. C0 is not complete until:

- every §3–§5 rule has a positive and a negative vector;
- an **independent implementation** reproduces every canonical byte string and digest from
  the same vector file, covering non-ASCII UTF-16 ordering, surrogate pairs, lone surrogates,
  and nested objects/arrays — not just ASCII fields;
- mutations of authoritative fields change the digest; mutations confined to non-authoritative
  display fields do not (or are documented as authoritative);
- the same semantic input with reordered object keys or equivalent but non-canonical number
  spellings yields the same canonical bytes.

**RED evidence must show a real violation, not a profile difference.** The following are
*positive invariants of the new encoder*, not historical bugs — Go decodes into a struct and
re-marshals, so key reordering and `\u003c`-vs-`<` may well produce identical legacy output:

- reordered object keys producing the same digest;
- `\u003c` and `<` producing the same digest.

The RED cases that must fail before the fix are: duplicate-key acceptance; wrong JSON types;
float/exponent/`-0`/`NaN` acceptance; out-of-range integers; non-NFC acceptance; cross-type
identity reuse (the legacy A2A hash has no domain separator); and the pre-agreed
cross-language golden vector disagreeing with the legacy digest.

## 7. Downstream obligations

- Decision-protocol artifacts adopt `magi-cj/1`; their `*_digest` fields are defined here.
- C1/C2/C3 artifacts inherit §3–§5 unchanged; they may add field-level rules but MUST NOT
  redefine canonicalization.
- `AuditReceipt`, `SendRecord`, `AcceptanceManifest` declare `canonical_profile` +
  `digest_alg`; their business semantics stay with epics #6/#8.
- Slice 1 validation reuses the existing `domain/validation` JSON-Schema facilities; C0 does
  not build a second schema engine.
- Migrating the legacy algorithms is explicitly **out of scope** for C0.
