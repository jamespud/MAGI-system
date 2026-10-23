# C1-B1 DomainProfile offline validator

## A. Baseline and scope

Fetched `origin/main=645a5f3826ec67cc8f7c40cb777bc4941a4738e5` on 2026-10-23 (Asia/Shanghai). GitHub #33, #34 and #35 are merged; open PR count was zero at start. Epics #3/#4 remain OPEN. Existing Issue #13 is outside this change. CAS-9 is referenced by those Epics and the contracts; authenticated Linear access is unavailable in this runtime, so its current status is **NOT VERIFIED** and is not changed.

Authority: Rev4 sections 2, 4, 8, 13, 17–18; C0; input-artifact contract sections 2–3 and 9; decision-protocol contract. This package does not change those documents or the input Schema. It implements the first offline C1-B slice only.

## B. Implementation and usage

`backend/domain/inputartifact` owns public DomainProfile body types matching the existing `$defs/DomainProfile` and referenced definitions. It uses the existing `domain/validation` engine with an embedded **byte-identical mirror** of the contract Schema; `TestDomainSchemaMirror` prevents drift. Run `go generate ./domain/inputartifact` from backend to refresh the mirror after a separately approved contract change. No new engine or schema dialect is introduced.

```go
validated, err := inputartifact.ValidateDomainProfile(raw, expectedID,
    inputartifact.ValidationOptions{})
// err == nil means local Schema and semantic checks passed.
// validated.ArtifactID() binds precisely validated.CanonicalBytes().
// validated.PendingDependencies() remains nonempty: no publication authority.
```

The entrypoint applies C0 strict parsing with resource limits, the existing JSON Schema engine, local semantics, and finally domain-separated `dprf_` identity. It does not strip Markdown fences, repair JSON, sort sets, normalize times or fill missing fields. `*inputartifact.Error` exposes `Kind`, `Path`, `Detail` and an unwrapped C0 cause when applicable. Rejections return an empty validated result. Copies returned by `Body`, `CanonicalBytes` and `PendingDependencies` cannot mutate the validated identity.

`ReadDomainProfile` requires an expected ID, prechecks explicit limits, calls C0 `Adopt` on original bytes, and rechecks type/Schema/semantics/identity. `ReadDomainProfileEnvelope` additionally checks the exact five wrapper fields, profile, algorithm, digest and request ID; it examines the original nested body bytes, not a repaired re-encoding. The wrapper is not a new hashed artifact.

`RequireCompleteQualification=true` **fails closed** with `UNVERIFIABLE_REFERENCE`. It is a stricter request, never an authorization flag. There is deliberately no success-capable qualification resolver, approval boolean, signature verifier or publication API in B1. Each ExternalRef path remains an unresolved dependency even with a nonempty digest. Subject identity, rule/method registration and billing mappings have additional explicit pending records. Contradictory content digests for the same external namespace/object/version are rejected; identical references at different paths remain individually reported.

Exact quantities enforce ratio/percentage-point ranges and discrete scale zero. Comparisons and rational splits use `math/big`; serialization rejects results beyond C0 safe integers. Money checks shape and dimensions, with actual currency/minor-unit/billing mapping left unverified. `ComposeBudgets` intersects supplied caps, including simultaneous task/role caps and compatible currency dimensions; consumers must provide all applicable Charter/engineering/Persona caps. `ComposePersonaV1Budget` additionally caps roles at 6 calls / 6000 output tokens without claiming to validate a PersonaSpec. Larger generic domain budgets remain legal offline. ROLLOUT-LAB separately enforces its reference ceilings and locally representable fixed instance policy.

## C. RED → GREEN and vectors

All twenty requested named tests are present in `domain_test.go`; every rejection helper first validates a complete positive body. The initial test-first run failed because the public types/entrypoint did not exist. With C0/Schema/identity plumbing in place and local semantics absent, the semantic tests independently failed on accepted unknown attributes, missing evidence, duplicate/unsorted IDs, weakened risk rules, predicate shape, ratio/dimension errors, unreduced fractions, calendar/interval errors, mutable versions, unsupported tool policy, omitted qualification dependencies and reference-instance budgets. Cases already enforced by reused C0/Schema passed immediately and are not described as new semantic REDs.

Independent review found incomplete ROLLOUT checks and an overly restrictive R2 quota gate. Dedicated RED tests reproduced acceptance of missing thresholds, excessive cache retention, weakened P95 and excessive telemetry TTL, and rejection of conservative R2 escalation. The fixes require local instance fields and allow domains to raise otherwise safe operations to R2. A later RED pair caught unsupported registry types hidden behind KNOWN and conflicting ExternalRef digests. All these tests now pass.

Developer evidence logs are in `/tmp/magi-c1b1-{red,review-red,reference-red,green}.log` in the implementation session; these transient logs are not production dependencies. Reproduce the final tests with:

```sh
GOCACHE=/tmp/magi-c1b1-go-cache go test ./domain/inputartifact -count=1 -v
python3 domain/inputartifact/testdata/verify_domain_golden.py
```

`testdata/domain_profile.json` is a complete offline positive body. `rollout_profile.json` additionally exercises the local reference-instance checks. Both use synthetic publishers/references and prove **no real source existence or eligibility**. The frozen canonical bytes and identities were computed in Python using the existing independent C0 reference, before the Go semantic implementation. The verifier recomputes the base and six authority-field mutation identities without executing Go; Go independently checks the same bytes/identities and rejects invalid mutations before deriving a validated ID.

Frozen base SHA-256: `59ef52c81a8dcebaa201ac5cab557dc3628ad3c47b48eb1b00ec70e8e9b8afea`.

Frozen ArtifactID: `dprf_59ef52c81a8dcebaa201ac5cab557dc3628ad3c47b48eb1b00ec70e8e9b8afea`.

## D. Validation

Local baseline `go test ./...` passed using a writable `/tmp` Go cache; the first default-cache attempt was blocked by the filesystem and is not a test failure. Local final results and CI are recorded below before PR handoff. Existing MySQL-gated tests without a DSN do not count as MySQL integration evidence; the unchanged MySQL GitHub jobs supply that evidence separately.

- Focused tests: 33 top-level PASS, 3 explicitly SKIP (authority, classifier, distributed revocation).
- `go test ./...`: PASS (54 packages with tests; baseline had 53).
- `go test -race ./...`: PASS, including a final rerun after the last semantic changes.
- `go vet ./...`: PASS.
- `gofmt -l .`: empty output.
- C0 Python Golden verifier: `OK (0 failures)`.
- C1-A checker: 5 structural positives, 46 negatives, identity/adoption checks, 14 D-04 checks and ReadSet boundary checks passed. Its final NOT RUN statement describes that structural script's scope, not the separately executed B1 suite.
- New Python verifier: base bytes/digest/ID and six authority-field mutations PASS.
- Independent code review: original two P2 findings fixed and rechecked; final targeted review has no remaining findings. This is code review, not publication approval.
- GitHub CI: the existing six jobs (backend, scripts, frontend, runtime, MySQL migration and MySQL writer cutover) are required for handoff. See the Draft PR check results for evidence at its exact head; no MySQL claim is made from the default local test run.
- Existing C0/C0-R vectors and all legacy producers/expected digests remain unchanged. Full backend regressions exercise those pins; the new named Legacy test also checks the frozen tool-intent key.

## E. Findings and implementation choices

1. **Typed risk vocabulary is not frozen by the Schema.** B1 uses explicit local boolean codes `protected_effect`, `bounded`, `in_domain`, `recovery_verified`, `reversible`, `within_quota`. They must remain registered and required; callers cannot redefine their types. R0 rules require explicit no-effect, R1 rules require all safety gates, and R2 rules retain bounded/reversible/recovery/domain gates while allowing stricter domain escalation. R3 UNKNOWN rules are legal; UNKNOWN cannot justify a lower class. These conservative sufficient conditions are not a complete runtime classifier or a claim that all possible policies can be proved locally. Runtime universal R3 fallback/MAX_APPLICABLE and actual evidence validation are future work.
2. **Code-to-method qualification is external.** Optional `Vocabulary` supplies local attribute types, metric units and parameter units, not trusted policies. The local reference vocabulary names telemetry `fact`, metrics `active-decision-p95`, `automatic-coverage`, `indeterminate`, `protocol-completion`, and parameters `active-task-limit`, `canary-c2/c3`, `error-increase`, `execution-limit`, `recovery-bound`, `terminal-review-freshness`, `trial-duration`. Their values trace to Rev4 §18; these spellings are implementation choices, not silent Schema amendments. Unknown codes reject. Qualification and binding to external methods remain pending. Generic release syntax is checked by the existing Schema; real release registration is unverified.
3. **Engineering limits remain provisional.** Zero option limits use existing C0 defaults, with existing Schema collection capacities. Callers may supply deployment ceilings. B1 does not silently promote C1-A §2.5 proposed limits to Rev4 requirements. Aggregate ceilings can reject before per-field capacity is exhausted.
4. **Adapter vocabulary differs.** Schema `UNSAFE` does not freeze a qualification mapping to Rev4 `UNFENCED_NONIDEMPOTENT`. B1 rejects unsupported UNSAFE/READ_ONLY writes rather than inventing one. `IDEMPOTENT_ONLY` is only a generic declaration and leaves explicit policy acceptance unresolved; ROLLOUT writes require `FENCED_IDEMPOTENT`. READ declarations are supported with `READ_ONLY`. No declaration proves capability or grants write rights.
5. **Schema examples are structural only.** Their unsorted sets, unreduced alpha and synthetic references are preserved. B1 has separate semantically valid fixtures and does not change old vectors. No Schema/Rev4 discrepancy was silently corrected.

## F. Release boundary

This deliverable is eligible for an independent **Draft PR review only**. It cannot approve or publish DomainProfile, authenticate its publisher, certify a real risk classification, verify distributed revocation, establish actual fees, or enable a runtime/egress path. C1-B2, C1-C, C2/C3, AuthorityProof, governance roots, signatures, MySQL publishing, Agent loops and behavioral experiments are outside scope. Epics/Linear remain open and unchanged; no Ready/merge action is authorized by this report.

Explicit integration evidence boundaries are executable skipped tests:

- `TestDomainAuthorityIntegration`: **BLOCKED**, C2 identity/governance/signature/delegation services absent.
- `TestDomainCandidateClassificationIntegration`: **NOT RUN**, no runtime classifier or actual qualified evidence in this PR.
- `TestDomainDistributedRevocationIntegration`: **BLOCKED**, ordered C2/C3 revocation/final-send protocol absent.
