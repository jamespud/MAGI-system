# Execution generation retention (T5)

Issue #23 adopts retention by default: **correctness must hold when cleanup
never runs**. A retry obtains a new generation, resets Case status through the
T4 owned transaction, and executes using T3 generation-scoped artifacts. Old
rows remain history. Retention is indefinite until an explicit storage policy
authorizes a separate reclamation mechanism.

## Retired capabilities

`ArtifactCleaner`, `CleanupCaseArtifacts(caseID)`, the RunManager cleaner
dependency, and cleanup-failure settlement are removed. The new repository
cannot be asserted to the old cleaner signature. This is an intentional source
compatibility break for consumers of that optional capability; retries no
longer accept a cleaner. Production bootstrap already supplies none.

Retry-reset ownership conflicts and unknown COMMIT outcomes keep the T4
behavior: stop the attempt without replaying an uncertain transaction. Cleanup
is never a recovery operation or a prerequisite for execution.

## Retention policy

| Data | Default policy |
| --- | --- |
| AgentRun, Evidence, Claim, Vote, DebateRound | Keep all generations, including generation 0 |
| Reflection, ToolCall | Keep direct provenance and historical AgentRun relationships |
| Checkpoint | Keep each `(run_id, execution_generation)` snapshot |
| Resolution and cited artifacts | Keep the committed result and its references |
| Event, EventCursor | Keep audit history and sequence state |
| DecisionJob, ClaimToken | Keep ownership and recovery facts |
| MemoryProjection, Approval | Outside retry cleanup scope |

Current execution uses generation-scoped reads. History `ListByCase` retains
superseded rows and their generations. Checkpoint loading still requires the
active owner and exact generation. Historical checkpoints remain persisted
after settlement, even though the settled owner cannot load them as active
execution state. AgentRun history also preserves token and cost accounting.

Generation 0 denotes legacy or unknown provenance. S27's historical rows with
`case_id=''` remain unchanged. Neither RunID parsing nor a guessed AgentRun
association assigns them a Case or execution generation.

Explicit user-authorized `CaseRepo.Delete` remains whole-Case deletion. It is
separate from retry lifecycle and retains its existing behavior.

## Verification boundary

The real MySQL tests use a channel barrier to pause an old cleanup request
before takeover, then allow G2 to write artifacts and atomically commit its
Resolution, completion Event and Job settlement. After releasing the request,
the old capability is absent and full persisted rows remain identical.
A separate request made after ownership loss proves the same absence. Both
tests were RED against the pre-T5 repository: its old method deleted seven
artifact families, including committed Resolution references.

These tests prove **removal of deletion authority**, not transaction fencing
for physical GC. They also snapshot another Case's two generations. A real
RunManager retry retains both generations across all seven artifact families
and checkpoints, verifies current/history reads and indirect relationships,
and preserves unknown legacy rows and billing history.

## Future physical reclamation

No physical GC, TTL, scheduler or schema migration is introduced. If a storage
policy later requires reclamation, its API must identify an active durable
owner and explicit target generations strictly older than the active one.
Generation 0 is excluded by default. It must lock Job then Case, validate the
full owner and database-time lease in the deletion transaction, protect all
committed Resolution/audit dependencies and relationships, and handle unknown
COMMIT outcomes without blind replay.

Any such implementation needs fresh real-DELETE MySQL race, rollback and
reply-loss tests. The disabled-cleaner tests cannot serve as evidence of GC
linearization. T6 remains responsible for deployment capability gates and
excluding older binaries that still contain destructive cleanup; this change
does not retroactively revoke code already running in an old process.
