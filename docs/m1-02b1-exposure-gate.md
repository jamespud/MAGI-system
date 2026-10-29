# M1-02B-1: `JOURNAL_EXPOSURE_GATE_V2` Authority Core

> Issue [#47](https://github.com/jamespud/MAGI-system/issues/47) / Stage
> [#43](https://github.com/jamespud/MAGI-system/issues/43)
>
> Status: **Authority core implemented / default-off / no consumable permit**.
> The production constructor is not wired. No remote Provider is called, and no
> real Witness or real Provider fence is claimed verified.

## A. Baseline and scope

- Baseline: `main=4d969869410e64ffb3816e3d72475e0987e78e23`.
- Branch: `codex/m1-02b1-exposure-gate`.
- The Gate reuses `magi_authority_head`, the C2 verifier, ExecutionGeneration,
  Owner/lease and the existing MySQL READ COMMITTED transaction boundary.
- Added the S34 additive migration and a default-off Gate Authority Core.
- No Provider APPLY, HTTP request, Settlement, Evidence Closure, ballot,
  Egress, production authority, or M1 pass is added.
- The frozen S33 Admission table, sequence allocation and statement shapes are
  not replaced. Renewed Admission uniqueness is preserved.

## B. Requirement mapping

| Approved constraint | Implementation / evidence |
| --- | --- |
| Gate and Admission share one sequence domain | Gate increments `last_sequence` and `last_gate_sequence` from the locked Head snapshot; `last_gate_sequence <= last_sequence` remains enforced. |
| Gate must be later than its Admission | Gate locks and reads the exact Admission; no Gate override can lower the Head sequence. Tests assert Admission+1 and a later common revocation. |
| Distinguish attempt and commit | `magi_authority_exposure_gate_attempt_v2` records terminal NEW or BLOCKED attempts; only `magi_authority_exposure_gate_commit_v2` consumes a sequence. |
| Replay returns original record | Same attempt, same canonical body and token returns the original lineage; changed body/token is `EXPOSURE_GATE_ATTEMPT_CONFLICT`. |
| A BLOCKED attempt cannot become success | The attempt primary key is immutable; replay returns the original BLOCKED record. A repaired request must use a new attempt identity. |
| Revocation before Gate | C2 revalidation inside the transaction returns REVOKED and no Gate is written. |
| Gate before revocation | Gate retains its in-flight record; revocation gets a later common sequence. Provider APPLY must perform its own fence/revocation check. |
| No remote network in the transaction | The adapter accepts only immutable capability and witness evidence; it contains no HTTP client. |
| C2 witness failure closes Gate | Missing/malformed witness or unavailable independent verifier produces a terminal BLOCKED attempt with zero sequence/permit. No consumable permit exists in this slice. |
| Production default off | `AuthorityRepositoryConfig.Enabled` is still required. Test-only verifier injection is implemented through `_test.go`; no production bootstrap sets the verifier. |

## C. State machine

```text
admission committed
  -> attempt A starts
     -> prerequisite missing / evidence unavailable
        -> terminal BLOCKED attempt (no sequence, no permit forever)
     -> C2, scope, owner, generation, lease, query revalidation all valid
        -> authority sequence N+1
        -> GateCommit lineage N+1
        -> non-consumable provider reservation remains RESERVED
        -> APPLY is still NOT RUN
```

`GateAttempt` is a durable record of an attempt, not an authorization object.
`GateCommit` is an immutable common-order record. Neither makes the remote Provider
APPLY atomic with MySQL.

## D. Lock order and failure behavior

```text
lock magi_authority_head
lock terminal attempt row, or token row when absent
re-authenticate C2 (root, proof, scopes, epochs, DB time)
lock owner job + case, then re-read DB time and owner predicates
lock exact Admission and fence
recheck generation, fence lease, capability binding
verify immutable attestation/witness evidence
advance common Head sequence + last_gate_sequence
write attempt + GateCommit + GateToken + GateLineage + Attestation
```

- Absent rows are not assumed to be protected by `SELECT ... FOR UPDATE`.
  Attempt ID, commit ID, Gate ID, sequence, and token have database unique keys.
  Concurrent insertion is resolved by holding the common Head first and then
  deterministically replaying the durable attempt.
- Every write boundary is under the same transaction. The test-only fault
  injection asserts rollback of the Head sequence, Gate rows, token, lineage,
  attestation and fence state at each write point.
- After COMMIT, an injected reply-loss fault returns `UNKNOWN`; a subsequent
  call returns the original committed lineage, never a new sequence.

## E. Actual RED and GREEN evidence

The following failures were observed before their corresponding implementation
was complete:

1. Domain Gate tests failed to build with:
   `undefined: authoritycommit.ValidateBlockedAttempt` and
   `undefined: authoritycommit.ValidateExposureGate`.
2. Real MySQL failed with:
   `Table '...magi_authority_exposure_gate_attempt_v2' doesn't exist`.
3. While wiring the S34 schema, MySQL rejected the forbidden fence-state
   transition (`chk_ac_fence_blocked`), so this slice retains the S33 fence as
   `DECLARED_BLOCKED` and does not fake a Gate-to-fence state change.
4. The first adapter gate test failed because the Gate C2 proof resource did not
   match the fixture's Gate resource; a dedicated Gate proof was added, then the
   test reached sequence allocation.
5. A stale Generation was initially classified `DENIED`; it now returns the
   precise `STALE/EXECUTION_GENERATION_STALE` refusal.

The final focused GREEN commands used:

```bash
cd backend
GOCACHE=/tmp/magi-m1-02b1-cache \
  go test ./domain/authority ./domain/authoritycommit -count=1

MAGI_TEST_MYSQL_DSN='root:root@tcp(127.0.0.1:33306)/magi_wave2?charset=utf8mb4&parseTime=True' \
GOCACHE=/tmp/magi-m1-02b1-cache \
  go test ./adapter -run '^TestAuthorityCommitMySQL_' -count=1
```

The real MySQL suite reports PASS without skips for the newly run Gate tests.
The focused domain suite also passes. Exact PR-head CI is **NOT RUN** because no
push or PR is performed by this task.

## F. Known limitations and blocked capabilities

- **No real Provider fence**: a capability declaration is not an enforceable
  remote fence. Production exposure remains disabled.
- **No independent Witness**: the test verifier proves transaction wiring only.
  It is not a production witness, does not establish deployment independence,
  and cannot be cited as a verified Witness.
- **No consumable Permit**: GateCommit and reservation rows are deliberately
  non-consumable. There is no Gate→Provider APPLY path in this slice.
- **No Settlement**: `APPLIED_VERIFIED`, `UNVERIFIABLE`, pending reduction and
  Provider original reconciliation are NOT RUN.
- **No three-role closure**: Evidence Closure, sealed ballots and DecisionRecord
  are outside this slice.
- **Migration operational gap**: S34 is additive but has not been exercised as a
  full deployment upgrade/rollback on production data; only isolated MySQL tests
  and schema DDL are evidence.


## Final verification record

The final implementation was verified with the following actual commands:

```text
GOCACHE=/tmp/magi-m1-02b1-cache go test ./... -count=1 -timeout=5m
PASS

MAGI_TEST_MYSQL_DSN=... GOCACHE=/tmp/magi-m1-02b1-cache   go test ./adapter -run '^TestAuthorityCommitMySQL_ExposureGate' -count=1 -timeout=5m
PASS (all Gate tests, including real MySQL concurrency, revocation, every write fault,
      reply-loss replay, cross-case binding, lease containment and token reuse)

GOCACHE=/tmp/magi-m1-02b1-cache go test -race ./domain/authority ./domain/authoritycommit -count=1
PASS

GOCACHE=/tmp/magi-m1-02b1-cache go vet ./adapter ./domain/authority ./domain/authoritycommit
PASS

GOCACHE=/tmp/magi-m1-02b1-cache python3 scripts/check-journal-admit-v2.py
PASS 46 JOURNAL_ADMIT v2 static declarations

GOCACHE=/tmp/magi-m1-02b1-cache python3 scripts/test-journal-admit-v2.py
PASS (15 tests)
```

Baseline comparison: `TestA2ASubmission_ConcurrentSameMessageOnMySQL` and
`TestA2ASnapshot_ConsistentOnMySQL` were run on untouched baseline
`4d969869` and passed there; they also passed when rerun in isolation on this
branch. A full parallel adapter run on this machine exposed unrelated
MySQL connection-contention/timeout noise. A full bootstrap run failed because
the suite's fixed schema name already existed (`Error 1007 database exists`);
this is an environment/test-isolation limitation, not a Gate assertion. The
targeted Gate MySQL suite did not skip and passed.

The new regression checks include: exact Admission request digest binding,
Admission owner/Case/round/generation/scoped epoch comparison, capability
`max_lease_until` containment of the fence lease, same-token rejection across
attempts, COMMIT reply loss returning the original Gate lineage, and S34 upgrade
over an existing S33 row. S34 adds foreign keys from GateCommit to both Admission
and GateAttempt.

Production exposure remains disabled; there is no consumable permit, remote
Provider APPLY, real Witness verification or Settlement in this slice.


## Review follow-up: F59-01 / F59-02 / F59-03

The independent review found three P1 boundaries. All were reproduced and fixed
without changing the common-order architecture.

### F59-01: C2 expiry while waiting for Admission/Fence locks

RED observed on real MySQL:

```text
expired gate={Outcome:VALID Code:EXPOSURE_GATED ...}
```

The whole-request C2 proof was rechecked before `lockExposureGateAdmission`, but
not after waiting for the Admission row lock. The adapter now executes
`verifyProofInTx` again with fresh DB time after Admission/Fence locks and before
the final Owner/fence checks and sequence allocation.

GREEN test:
`TestAuthorityCommitMySQL_ExposureGateProofExpiryDuringAdmissionWaitRejects`.
It holds the real Admission row lock past the proof deadline, releases it, then
asserts `EXPIRED/PROOF_EXPIRED`, unchanged Head (`1,1,0,0`), and zero Gate
attempt/commit/token/lineage/attestation rows.

### F59-02: historical Gate lineage needed an audit boundary

Operational replay of an existing Gate attempt no longer returns history. It
returns `DENIED/AUDIT_AUTHORIZATION_REQUIRED` with no Attempt lineage. Historical
material is available only through
`ReplayJournalExposureGateV2` with a current `JOURNAL_AUDIT_READ` proof.

Covered paths:

- ordinary commit retry returns no lineage;
- a valid independent audit proof recovers the original Attempt/sequence;
- an operational Gate proof is not accepted as audit authorization;
- revoking the audit scope rejects replay;
- revoking only the operational proof scope does not prevent the still-current
  audit proof from reading history.

The audit path never reassigns a sequence or reissues a permit.

### F59-03: test verifier injection was compiled into production

`SetExposureGateAttestationVerifierForTest` was removed from the normal package
and now exists only in `authority_commit_export_test.go`. A compile-surface test
parses the production Go file set and rejects any production top-level declaration
with that name.

### Final follow-up command evidence

```text
MAGI_TEST_MYSQL_DSN=... go test ./adapter \
  -run '^TestAuthorityCommitMySQL_ExposureGate|^TestAuthorityCommitMySQL_S34|^TestExposureGateVerifierInjectionAbsentFromProductionPackage$'
PASS (30.1s)

go test -race ./domain/authority ./domain/authoritycommit
PASS

go vet ./adapter ./domain/authority ./domain/authoritycommit
PASS
```

The required full adapter MySQL gate and exact-head CI were rerun after these
changes; only the final output recorded in the PR may be cited as exact-head CI
evidence.


### Targeted regression note

After the three fixes, the CI-equivalent full adapter MySQL command on the local
host still reported two unrelated A2A failures:

```text
FAIL TestA2ASubmission_ConcurrentSameMessageOnMySQL
FAIL TestA2ASnapshot_ConsistentOnMySQL
```

No `TestAuthorityCommitMySQL_ExposureGate*`, S34, verifier-surface or domain test
failed in that run. The two A2A tests also passed on untouched baseline
`4d969869` as well as in isolated reruns on this branch; the full local run
remains an environment-sensitive baseline signal and is not described as green.
