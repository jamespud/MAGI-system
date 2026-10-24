# #45 C2/C3 shared authority contract — A–F evidence (Draft)

2026-10-24, Asia/Shanghai. Baseline main `737ee9381ec0fdc57d26e605d557a844bdefd3d1`.
Branch `codex/c2-c3-authority-contract`; exact review commit: `git rev-parse HEAD`.
No production authority/issuer/egress implementation. Frozen Rev4/C0/C1/Decision v2 untouched.

## A. Scope and baseline

The isolated worktree started clean at baseline. CLAUDE.md, Rev4, C0,
input-artifact, Decision protocol/v2 contracts and issue #45 were read before
changes. No applicable AGENTS.md found. Existing backend baseline:

```text
cwd: backend
GOCACHE=/tmp/magi-joint-stage-43/b/go-cache go test -p 2 ./...
exit 0
log: /tmp/magi-joint-stage-43/b/baseline-go-test.log
```

An initial run without GOCACHE exited 1 at setup because the default cache was
read-only. This is an environment failure, not protocol RED. Explicit GOCACHE is
an environment workaround, not a contract or CI requirement. No MySQL DSN was
configured locally; existing MySQL-gated tests skip.

## B. Delta and requirement mapping

| #45 obligation | Reviewable artifact |
| --- | --- |
| invariants/threats first | authority-contract.md §1, A45-01..10 |
| typed VerifyAuthority/CommitIfCurrent and seven outcomes | scripts/authority-api.go (declarations only), contract §2 |
| operation authority ownership and producer/consumer DAG | contract §3 |
| token lifetime/replay/revocation, scopes, roots/rotation/recovery | contract §4, D45-01..08 |
| exact locks/time/linearization, journal/quiescence/closure/send | contract §5.1–5.3 SQL sketches, not migrations |
| FSM and crash/ABA/revoke/response-loss cases | contract §6 + synthetic fixtures |
| capabilities / LEGACY generation 0 / compatibility | contract §7 |
| executable static/schema checks and evidence limits | contract §8, checker/tests, CI backend step |

The checker invokes the existing Go C0/JSON Schema engine. Seven typed API
structures have required fields checked against the trace schema, and the API
compiles. The schema describes synthetic declarations, not production proof
formats. Four write-set variants are explicit; future enums cannot use a generic
mutation API and remain blocked until their typed write-set review.

## C. Genuine RED → GREEN

The first checker performed no semantic checks and returned STATIC_VALID for
well-shaped traces. Meaningful declarations failed for missing semantic behavior;
missing files/compiler errors were never counted as RED.

```text
cwd: repository root
python3 scripts/test-authority-contract.py
exit 1; Ran 1 test; FAILED (failures=41)
log: /tmp/magi-joint-stage-43/b/red-traces.log
```

Representative actual failures: epoch_revoke_after_verify expected STALE,
pending_injection_blocks_close expected CONFLICT, partial_close_write_set expected
WRITE_SET, witness_absent expected UNKNOWN; all incorrectly got STATIC_VALID.
The no-op checker/source remain in local evidence staging/build_contract.py,
not as any permissive production implementation.

A second real RED was observed before adding freshness semantics:
`GOCACHE=/tmp/magi-joint-stage-43/b/go-cache python3 scripts/test-authority-contract.py`
exited 1 with two failures (one millisecond over age and future observation);
log `/tmp/magi-joint-stage-43/b/red-freshness.log`. Equal-to-30000ms remains valid.
SQL now separates inclusive freshness from half-open expiry, and static steps
explicitly include `linearize` between revalidation and writes.

After constraints and expanded admission/governance/schema/freshness fixtures:

```text
GOCACHE=/tmp/magi-joint-stage-43/b/go-cache python3 scripts/check-authority-contract.py
exit 0; PASS 86 schema/static trace declarations
log: /tmp/magi-joint-stage-43/b/check-authority.log
GOCACHE=/tmp/magi-joint-stage-43/b/go-cache python3 scripts/test-authority-contract.py
exit 0; Ran 4 tests; OK
log: /tmp/magi-joint-stage-43/b/green-traces.log
```

Non-structural negatives first pass C0/Schema, then assert an exact semantic code.
Float/overflow fail C0 before Schema; unknown fields/operation/time precision fail
Schema. Broken Schema/typed-field drift raise infrastructure errors, never count
as denials. ORIGINAL lookup may survive revocation/expiry but cannot resend.

## D. Regression verification

Commands below ran in backend using the task GOCACHE and exited 0:

```text
python3 domain/canonical/testdata/verify_golden.py domain/canonical/testdata/golden_vectors.json
python3 domain/inputartifact/testdata/verify_domain_golden.py
python3 domain/inputartifact/testdata/verify_persona_golden.py
python3 domain/inputartifact/testdata/verify_context_golden.py
python3 ../scripts/check-context-schema-parity.py
python3 ../scripts/test-context-schema-parity.py
go run ../scripts/check-input-artifact-contract.go
python3 ../scripts/check-decision-artifact-v2.py
go vet -p 2 ./...
go vet ../scripts/check-authority-contract.go ../scripts/authority-api.go
```

Logs: `/tmp/magi-joint-stage-43/b/frozen-0.log` through `frozen-7.log`,
`frozen-status.json`, `final-go-vet.log`, `final-script-vet.log`.
New Go files were gofmt-formatted. The full final race command is
`GOCACHE=/tmp/magi-joint-stage-43/b/go-cache go test -race -p 2 ./...`;
log `/tmp/magi-joint-stage-43/b/final-go-race.log`, completed exit 0.
The new Go script types are compiled/vetted separately; backend has no runtime delta.

## E. Limits, risks and dependencies

NOT RUN: new authority MySQL schema/SQL, real lock/ABA/revoke races, kill at each
write, real commit reply loss, provider deliveries/confirmations, live signatures,
trust roots/clock guarantees, independent witness durability/placement,
credential/network isolation, protected SEND or remote recovery. BLOCKED on
independent C1-C/C2/C3 implementation and deployment acceptance. Existing MySQL CI
only covers the old protocol. Synthetic booleans do not prove actual permission,
read, signature, independent domain or transaction.

All-writer single-domain/single-transaction participation is an activation
precondition. Current claim/renew/terminal writers are unmodified; integration
must bridge atomically or stay disabled. SQL is untested design. No external
exactly-once or protection from a malicious credential-bearing Egress is promised.
Future typed write sets/schema versions need review. Conflicts with frozen specs
require Design Change before implementation.

## F. Review, CI and delivery status

Initial independent read-only review of `17917a98f5e968edc2531822fd1412a3e0b07631`:
CHANGES REQUIRED (B-01..B-04). The complete correction range through
`3d65567` subsequently received an independent scoped PASS with no remaining
P0/P1/P2 in those four obligations. Exact-head remote CI is recorded separately
after the final evidence commit. Keep Draft. No Ready/merge, issue/Epic/CAS-9
closure, terminal authorization, production permissions or protected send.


## Review correction wave — B-01 through B-04

Fix base: `17917a98f5e968edc2531822fd1412a3e0b07631`.
All four findings were verified against that head before changes. They concern
the new unmerged authority Draft only; frozen artifacts and backend remain intact.

| Finding | Correction and executable coverage |
| --- | --- |
| B-01 P1 | CompleteWritePlan binds fixed inputs, explicit transitions, mutation kinds and `magi-authority-derive/1`; plan_digest excludes generated outputs. Allocation/child result digests/lineage are constructed afterwards with no lineage self-reference. `test_B01_first_proposal_constructs_and_roundtrips_without_future_values` starts without a token for each of four variants, fixes the transcript, introduces a synthetic token/allocation, validates the first declaration, and roundtrips every result/lineage digest. Changed future sequence leaves the plan digest unchanged. No production Verify implementation is added. |
| B-02 P2 | Type/bind Journal HWM, provider settlement and pending transition, SEND action/gateway/version transition, exact current quota identity/version/remaining CAS and logical-operation VERIFY_EXISTING. Align SQL/provider locking with those fields. Permanent logical map is created only at PREPARE; SEND cannot create/rebind it. Separate negatives omit each required mutation label (semantic WRITE_SET), delete each concrete transition field (Schema), and replace only quota identity while retaining the same version/remaining (semantic STALE). |
| B-03 P2 | VerifyRequest.SendIntent binds exact action/intent/dependency digest, encoded request and business/logical operation. Compare action, typed SendPlan, dependency, witness and actual encoded request declaration to this request commitment. Paired detached-action, jointly swapped intent, and swapped actual request digest negatives retain the original token and reject. |
| B-04 P2 | Independent ReadOriginal/AuditReadRequest and current audit_access gate, with exact authenticated principal/source and case/round/key/target scope. DENIED/UNAVAILABLE/expired/identity mismatch negatives; approved current auditor can still read an expired/revoked old worker's immutable lineage, without old mutation roots being current or any resend. |

A scoped independent rereview found one residual B-02 binding gap. A schema-valid
mutation replacing only `quota_transition.quota_id` from `quota-1` to `quota-2`,
while retaining the same version and remaining balance and recomputing the plan
digest, was accepted as `STATIC_VALID`. `CurrentDeclaration` now includes a
required exact `quota_id`; the send checker requires it to equal the plan's
quota transition, the SQL compares the locked row into the conditional update,
and `test_B02_review_quota_identity_is_bound` requires `STALE` for that mutation.
This is the B-02-R1 correction.

### F51-01 artifact version / authority row version

Cross-contract review found that `ExactDependency.Version` was a safe integer even
though frozen input artifacts permit fixed non-SemVer strings. The corrected typed
contract uses two explicit fields:

- `artifact_version string` preserves exact C1-A/Decision v2 encodings such as
  `1.0.0` and `release/2026-10-24-r17`; mutable aliases fail Schema.
- `authority_row_version SafeUInt` identifies the separate Authority CAS/lineage
  row and is never derived from the artifact version.

The trace bases and dependency-substitution negatives now use the two fields. A
new regression recomputes the token transcript for both a frozen ContextManifest
version and a non-SemVer fixed version, then verifies Schema and static semantics.
It also proves that independently replacing the exact artifact version or the
authority row version leaves the trace schema-valid but changes the request
transcript and is rejected as `TOKEN_BINDING` against the original token. The
corrected MySQL sketch stores and locks both columns under the same dependency
identity. This is still a synthetic contract check; actual artifact lookup and
dependency-row locking remain future implementation evidence.

### RED

From repository root on the fix base, the original reviewer reproducer confirmed
all three schema-valid false acceptances:

```text
GOCACHE=/tmp/magi-joint-stage-43/b/go-cache python3 /tmp/magi-joint-stage-43/review-b-repro.py
exit 0 (diagnostic reproducer), all three semantics STATIC_VALID
log: /tmp/magi-joint-stage-43/b/review-repro-before.log
GOCACHE=/tmp/magi-joint-stage-43/b/go-cache python3 scripts/test-authority-review.py
exit 1; Ran 4 tests; FAILED (failures=9)
log: /tmp/magi-joint-stage-43/b/red-review.log
GOCACHE=/tmp/magi-joint-stage-43/b/go-cache python3 scripts/test-authority-review.py
exit 1; B-02-R1 observed STATIC_VALID where STALE was required
log: /tmp/magi-joint-stage-43/b/red-review-quota.log
```

The assertions failed because the old proposal contained prepared transaction
outputs, five omissions were accepted, send substitutions were accepted, and audit
denied/unavailable still passed. These were assertion failures, not import,
compiler, schema compilation or missing-function errors.

### GREEN and coverage boundary

From repository root after the coherent correction wave:

```text
GOCACHE=/tmp/magi-joint-stage-43/b/go-cache python3 scripts/test-authority-review.py
exit 0; Ran 8 tests; OK
log: /tmp/magi-joint-stage-43/b/green-review.log
GOCACHE=/tmp/magi-joint-stage-43/b/go-cache python3 scripts/test-authority-contract.py
exit 0; Ran 4 tests; OK
log: /tmp/magi-joint-stage-43/b/review-existing-contract.log
GOCACHE=/tmp/magi-joint-stage-43/b/go-cache python3 scripts/check-authority-contract.py
exit 0; PASS 108 schema/static trace declarations
log: /tmp/magi-joint-stage-43/b/check-authority-review.log
GOCACHE=/tmp/magi-joint-stage-43/b/go-cache python3 /tmp/magi-joint-stage-43/review-b-repro.py
exit 0; schema='' for all three; TOKEN_BINDING, TOKEN_BINDING, DENIED
log: /tmp/magi-joint-stage-43/b/review-repro-after.log
```

From backend:

```text
GOCACHE=/tmp/magi-joint-stage-43/b/go-cache go vet ../scripts/check-authority-contract.go ../scripts/authority-api.go
exit 0; log: /tmp/magi-joint-stage-43/b/review-script-vet.log
```

New review regression is wired into the same backend CI static gate. Existing
baseline/full backend race/vet and eight frozen gates from §A/D are prior results;
this script/schema/doc-only correction did not repeat unchanged backend suites.
Independent scoped rereview of `17917a98..3d65567` passed B-01 through B-04,
including the B-02-R1 quota identity correction, with no new P0/P1/P2. It reran
the focused 8-test review suite, four original contract tests, 108 declarations,
script vet and diff check. Exact-head CI remains controller-owned/pending.
`git diff --check` passes.
All construction rows/tokens/audit decisions are synthetic declarations; new
MySQL/issuer/provider/independent witness/egress remains NOT RUN/BLOCKED as in §E.
The final evidence-only commit records this independent outcome; the verified
contract code head is `3d65567`. Keep Draft and make Ready, merge, closure and
activation separate decisions.
