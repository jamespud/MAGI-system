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

Independent read-only review: PENDING controller dispatch. Exact-head remote CI:
PENDING controller push/Draft PR. This worker performed no remote action. Keep
Draft. No Ready/merge, issue/Epic/CAS-9 closure, terminal authorization, production
permissions or protected send. Controller stage ledger records later review/CI
URLs and exact final hash; this report claims no future result.
