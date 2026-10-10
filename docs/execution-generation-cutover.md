# ExecutionGeneration writer cutover (T6 / #24)

T6 supports an offline cutover. Generation-unaware and generation-aware
writers must never share live write credentials. Rolling mixed-version upgrades
are unsupported. T1–T5 ownership, terminal settlement and retention semantics
remain unchanged.

## Authority and prerequisites

The database has one `decision_writer_contract` row, id 1. Contract version 1
is compatible with this binary. Installation is BLOCKED. Admit and Claim hold a
shared InnoDB gate lock before existing locks; closing the gate takes the
exclusive lock and advances the epoch. A committed close prohibits subsequent
admission and Claims, including a replay using an earlier claim token.

Closing admission does not invalidate active owners. They may finish under the
existing Job → Case protocol. Drain them with a bounded deadline or explicitly
Cancel their Cases before stopping workers. During a failed offline drain,
use an explicit `decision-admin cancel-case --case CASE_ID`; cancellation remains
external authority and does not require a worker owner. Do not rewrite generations or
delete historical Artifacts to make a drain appear complete.

Runtime startup verifies schema, contract version, enabled epoch, irreversible
rollback marker and `CURRENT_USER()` before recovery or scheduler startup.
Runtime performs no AutoMigrate. A malformed column, nullable/default mismatch,
missing or reordered index, non-InnoDB table, broken event cursor, missing
singleton, incompatible version or blocked gate refuses writer startup.

Production repositories reject ownerless authoritative Case transitions,
Artifact/checkpoint writes and completion/status/failure events, including G0.
Direct durable `Orchestrate()` and synchronous Decision fallback fail with
`ErrExecutionOwnerRequired`. Benchmarks use the normal RunManager
Admit → Claim → owner execution lifecycle and read the exact settled generation.
Pure memory callers and explicit historical fixtures retain legacy behavior.
G0 completed history is readable; unfinished history enters G1 on its first
new Claim and does not become current Artifact input.

There are three identities:

- An inventory of old application identities, such as `legacy_writer@%`.
- A fresh runtime identity, such as `generation_writer@%`, with per-table
  SELECT/INSERT/UPDATE/DELETE on application tables and SELECT only on the
  contract table. It has no DDL, global privileges, roles or grant option.
- An offline admin/migrator with DDL and the narrowly controlled account/session
  privileges needed for cutover. Never distribute this credential to runtime.

The supplied tool manages one legacy identity per cutover. Before using it,
consolidate the inventory or exclude every additional old identity independently.
All old deployment targets, host processes, CronJobs, autoscalers and database
connections must be inventoried. Recreate strategy in Kubernetes is helpful but
does not replace credential exclusion or scaling old targets to zero.

A DBA who gives the new credentials to an old executable can bypass this trust
boundary. Schema metadata cannot identify a binary from its SQL. T6 proves old
deployment identity exclusion, not request-level binary attestation.

## Build and offline schema preparation

Pin the target commit and build `decision-admin` from that same checkout:

```bash
cd backend
go build -o /secure/tools/decision-admin ./cmd/decision-admin
```

Build the backend image separately, record its immutable digest and assign
`MAGI_WRITER_IMAGE=registry/magi-server@sha256:...`. Compose and the Helm
backend require a pinned writer image; ordinary `deploy.sh up` verifies writer
capability and uses `--no-build`.

Supply credentials through a private environment/file loader; never place DSNs
or passwords in command arguments, shell tracing, issue bodies or logs.
`MAGI_ADMIN_DSN` is the admin connection, `MAGI_WRITER_DSN` is the new runtime
connection. Each command has a bounded `--timeout`.

For a genuinely empty database only:

```bash
decision-admin init
decision-admin verify-schema
```

Initialization is explicit and refuses a nonempty database. If initialization
fails partway, keep writers stopped and investigate; it is not an automatic
repair command.

For an existing T5 deployment, while every old writer is stopped:

```bash
decision-admin install-contract
decision-admin verify-schema
```

`install-contract` verifies the complete S16/S25–S28 contract before creating
S29. An existing S29 table is verified without repair; a partial table or missing
singleton refuses continuation. S29 can alternatively be applied once using
`docker/atlas/migrations/magi_s29_decision_writer_contract.sql`. Its schema is
tested against the management path.

If any earlier contract is incomplete, apply the corresponding explicit
migration/repair offline. Inspect the current stage and use the established
S16 repair procedure for interrupted event backfill. Do not replay
non-idempotent DDL or run startup AutoMigrate over S27 provenance. This tool
does not guess missing historical migration stages. No migration failure may
be followed by enable.

S29 installation on an existing database conservatively records
`legacy_rollback_forbidden=true`: earlier positive-generation writes may
already have occurred, even if those rows were later deleted. Fresh initialization
can start with false; the first enable sets it irreversibly before any new Claim.
No supported admin action clears it.

## Coordinated Compose cutover

Freeze external traffic and **all** Decision launchers (HTTP, Recurring,
Benchmark, A2A and external replicas). Stop automatic restarts/autoscaling of
the old deployment. The script requires executable, reviewed inventory hooks;
each hook must return failure if its inventory cannot be proved complete:

- `MAGI_CUTOVER_FREEZE`: freezes admission sources.
- `MAGI_CUTOVER_STOP_ALL`: stops every inventoried old instance. It must be
  idempotent and safe on failure containment.
- `MAGI_CUTOVER_OPEN`: opens only verified new-version traffic.

Set `MAGI_DECISION_ADMIN`, admin/writer DSNs, `MAGI_WRITER_IMAGE`,
`MAGI_WRITER_DB_USER`, `MAGI_WRITER_DB_PASSWORD`,
`MAGI_NEW_WRITER_ACCOUNT` and `MAGI_LEGACY_WRITER_ACCOUNT`.
Provision the fresh account offline after schema preparation:

```bash
decision-admin provision --writer generation_writer@%
```

The password arrives only as `MAGI_NEW_WRITER_PASSWORD`. Provision refuses
an existing account, avoiding inherited roles/global grants. On an upgrade,
provision after S29 installation so the control table receives SELECT only.
For the script's first T6 upgrade, freeze/stop and install S29 manually before
provisioning, then leave ingress frozen and run the remaining coordinated flow.

Review the plan and execute during the maintenance window:

```bash
scripts/decision-cutover.sh --dry-run
scripts/decision-cutover.sh
```

The script performs these stages:

1. Freeze all launchers. Block S29 admission when present.
2. Observe zero RUNNING Jobs, bounded by `MAGI_CUTOVER_DRAIN_SECONDS` (300
   seconds by default). A failed observation or timeout fails the cutover.
3. Stop all old deployments and Compose server/web. Make a consistent backup.
   The server is already stopped, so `backup.sh` cannot restart it through
   its normal “restore previously running server” trap.
4. Install/verify S29 without changing earlier contract structures.
5. Configure separate identities; lock the old account, revoke all privileges
   and grant option, kill every existing connection with its username. Roles
   or other retained grants cause rejection. Password rotation alone is not
   accepted as exclusion.
6. Verify schema, exact identity, allowed new writer privileges, old account
   lock/grants/session absence, and zero RUNNING Jobs.
7. Block again to obtain the current epoch, then enable that exact epoch.
   The exclusive lock prevents Admit/Claim from entering mid-transition.
   Start only the new pinned image, await health and reopen ingress.

The script has no handler that starts an old service. A failing stage keeps
ingress frozen, stops deployment targets and best-effort blocks admission.
If the control schema itself is broken, the absent/unusable gate remains
fail closed; repair offline. Inspect `decision-admin status` before retrying.

For other deployment systems, perform the same stages explicitly. Scale all old
writers to zero, exclude all old identities and sessions, bind the pinned new
image to a fresh Secret, then enable and start. Do not use ordinary Helm
rollout or restore automation to resurrect old credentials.

## Recovery, ambiguity and rollback

`block`, `configure` and `enable` advance a monotonic epoch. Enable requires
the exact blocked epoch, compatible schema, excluded legacy account and no
RUNNING Jobs. A stale operator cannot reopen a later cutover. Runtime cannot
alter its gate or schema.

MySQL account DDL commits independently. Exclusion can fail partway; admission
stays BLOCKED and the command must be inspected/retried while writers remain
stopped. A lost management COMMIT reply is resolved by authoritative
`status` reread. Do not blindly replay enable or open ingress on an uncertain
response. An enabled status alone is not permission to resume an old binary.

After the irreversible marker is set, use a generation-aware repair or forward
upgrade. Never restore an old authoritative binary onto the live database.
A pre-enable, genuinely empty deployment rollback still requires proof of
compatible backups and complete writer exclusion. Current MAX(generation) is
never sufficient evidence for rollback permission.

Monitor the first new Claim, Case/Job generation equality, terminal
Resolution/Event provenance and Job settlement after reopening. Keep the
backup, image digest, contract epoch, identity exclusion/session evidence and
exact-head CI results with the deployment record. Production rehearsal and
application-specific freeze hooks remain operator responsibilities; automated
tests exercise disposable schemas and identities only.

## Executable acceptance

`MAGI_TEST_MYSQL_DSN` activates the real MySQL 8.4 tests. They create isolated
schemas/accounts and require a test administrator, never a production DSN.

- T6-01–04: runtime never repairs missing S25 columns, S26 types/registry/index,
  S27 Artifact/direct Case/checkpoint contract, S28 Event or S16 sequence/cursor.
  Positive compatible startup is also read-only.
- T6-05–06: missing/versioned/blocked/wrong-identity gate denies Admit and Claim,
  including stable-token replay; barriers around actual shared locks prove both
  close-first and transaction-first interleavings.
- T6-07: failed schema prevents startup dependents; blocked recovery/Benchmark/
  scheduler produces no new execution or Claim.
- T6-08: compile the pinned pre-T1 adapter at
  `ee935d079da44f4a5ec8dd5ce3549a06cec3faab`. Its real UpdateStatus and
  CleanupCaseArtifacts succeed before exclusion, then fail on the retained
  connection and on reconnect after revoke/lock/kill. Full authoritative
  row snapshots are unchanged.
- T6-09/09B: G0 history remains readable, first new Claim gets G1 and excludes
  G0 current input; durable ownerless execution fails; Benchmark settles
  through the existing owner lifecycle.
- T6-10: S29 parity/idempotence, failed migration stays blocked, stale enable is
  denied, new writer cannot change control or schema, irreversible marker
  survives removal of positive-generation data. Script tests inject stage
  failures and prove no premature start or ingress reopening.
- T6-11: existing T1–T5 MySQL regressions and backend race/vet gates remain.

The dedicated CI check is **Decision writer cutover (MySQL)**. It checks out
full history, builds the actual old adapter probe and runs the T6 suite with
race detection. Existing MySQL migration and ownership suites continue
separately. Skipped sandbox/model-provider tests are not counted as live
sandbox acceptance.

Parent #17 should retain the T1–T6 Issue/PR/merge/evidence map and remain open
until the merged integration HEAD passes all required checks. No new GC or
external-effect exactly-once guarantee is introduced here.
