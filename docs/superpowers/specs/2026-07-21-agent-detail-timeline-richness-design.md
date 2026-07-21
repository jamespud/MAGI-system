# Agent Detail + Timeline Richness Design

**Date:** 2026-07-21
**Status:** Approved (brainstorming complete)
**Depends on:** Backend rich-data plan (`2026-07-20-backend-rich-data-async.md`) — merged

## Problem

After wiring the frontend to real backend data, the workspace is thinner than the original mock UI in two ways:

1. **Agent panels lack detail.** `AgentPanel` (expanded) renders `agent.toolCalls[]`, `agent.thought`, `agent.evidence[]`, `agent.claims[]`, `agent.vote`. The real `/agents` endpoint returns only `agent_code, status, round, evidence_count, claim_count, vote?` — counts, not arrays. `loadAgentsFromApi` fills `toolCalls/evidence/claims` as empty arrays and `thought` as `""`. So expanding an agent shows "0 Tools / 0 Evidence / 0 Claims" and no tool-call records, even after a real run. Evidence and claims ARE persisted (per-agent filterable via `collected_by` / `created_by`); tool-call records are NOT persisted at all.

2. **Timeline events are generic.** `BottomTimeline` displays `event.message`, which the backend derives as a constant per type ("Tool call requested", "Evidence created"). The structured detail (tool name, evidence id, vote stance) lives in `event.payload` but is never rendered.

The original rich mock shape is preserved at `frontend/src/mock/data.reference.ts` (`@ts-nocheck`, reference-only) as the target.

## Scope

**In scope:**
- Persist tool-call records per agent run (Gap 2 — toolCalls).
- Enrich `/agents` to return `tool_calls[]`, `evidence[]`, `claims[]` arrays per agent (Gap 2 — evidence/claims detail; Thought explicitly **out of scope** — the real agent loop emits structured JSON, not freeform reasoning, so a synthetic thought adds no value).
- Frontend `loadAgentsFromApi` maps the enriched shape into `AgentSnapshot` so `AgentPanel` shows real tool calls / evidence / claims.
- Timeline renders `event.payload` detail (Gap 1).

**Out of scope:**
- Agent "Thought" field (decided: skip).
- Real tool integration (web_search etc.) — that is what would populate evidence and graph connections; tracked separately as Gap 3. With the stub executor, evidence arrays may still be sparse, but tool calls and claims will render.
- Evidence Graph connection logic (already builds links from claims' supports/contradicts; richer when real tools arrive).

## Architecture

Three layers, each independently testable:

```
agent_loop (ToolCallRecord per step, in LoopResult.Trace)
    -> orchestrator.persistArtifacts writes magi_tool_call rows
        -> /agents endpoint joins tool_calls + evidence + claims + votes per agent
            -> frontend loadAgentsFromApi maps into AgentSnapshot
                -> AgentPanel renders; BottomTimeline renders payload detail
```

## Section 1: Backend tool-call persistence

**New files / changes:**

- `backend/adapter/model.go` — add `ToolCallModel` matching the s8 `magi_tool_call` table:
  ```
  id VARCHAR(64) PK, agent_run_id VARCHAR(64) index, tool_call_id, tool_name,
  arguments TEXT, valid TINYINT, result TEXT, err TEXT, evidence_id, duration_ms INT
  ```
  Add to `AllModels()`.

- `backend/domain/port/repository.go` — add `ToolCallRepository`:
  - `Create(ctx, *entity.ToolCall) error`
  - `ListByCase(ctx, caseID string) ([]*entity.ToolCall, error)`

- `backend/domain/entity/` — add `ToolCall` entity (fields mirror `runtime.ToolCallRecord` + `CaseID`/`AgentRunID`).

- `backend/adapter/repository.go` — `toolCallRepo` impl + wire into `magiRepository.ToolCallRepo()`.

- `backend/domain/orchestration/orchestrator.go` — in `persistArtifacts`, after evidence/claims, iterate `r.Trace.Steps` and persist each `ToolCallRecord` as a `ToolCall` with:
  - namespaced ID `<caseID>-<code>-r<round>-<phase>-<toolCallID>` (globally unique, same scheme as claims)
  - `CaseID`, `AgentRunID = run.ID`
  - `EvidenceID` passed through (already namespaced? — no: the in-memory `tcr.EvidenceID` is the ledger's `EV-001`; remap through `evRemap` to the persisted evidence ID so the tool_call -> evidence link stays consistent)
  - duration as ms

**Data flow:** `agent_loop` accumulates `ToolCallRecord` into `Step.ToolCalls` → `LoopResult.Trace` → orchestrator persists. The in-memory trace is not mutated (copies persisted, same pattern as evidence/claims).

## Section 2: `/agents` endpoint enrichment

**DTO** (`backend/server/dto/dto.go`):
```go
type AgentSnapshotDTO struct {
    AgentCode string        `json:"agent_code"`
    Status    string        `json:"status"`
    Round     int           `json:"round"`
    Step      int           `json:"step"`
    ToolCalls []ToolCallDTO `json:"tool_calls"`
    Evidence  []EvidenceDTO `json:"evidence"`
    Claims    []ClaimDTO    `json:"claims"`
    Vote      *VoteDTO      `json:"vote,omitempty"`
}
type ToolCallDTO struct {
    ToolCallID string `json:"tool_call_id"`
    ToolName   string `json:"tool_name"`
    Arguments  string `json:"arguments"`
    Result     string `json:"result"`
    Err        string `json:"err,omitempty"`
    EvidenceID string `json:"evidence_id,omitempty"`
    DurationMs int64  `json:"duration_ms"`
}
```
(Drops `evidence_count` / `claim_count` — arrays replace them; frontend uses `.length`.)

**Service**: add `Service.ToolCalls(ctx, caseID)` via `WithToolCallRepo`.

**`ArtifactHandler.Agents` rewrite**: fetch agent_runs + evidence + claims + votes + tool_calls (each `ListByCase` once), aggregate per agent code:
- evidence filtered by `collected_by`
- claims filtered by `created_by`
- tool_calls joined to agent runs via `agent_run_id` → agent code
- vote = latest round for the agent
- step = `len(tool_calls)` as a proxy (trace step count isn't persisted; tool-call count is a reasonable approximation for display)

**bootstrap**: `provideDecisionService` adds `WithToolCallRepo(repo.ToolCallRepo())`.

## Section 3: Frontend agent panel + timeline

**API types** (`frontend/src/api/client.ts`): `ApiAgentSnapshot` gains `step`, `tool_calls: ApiToolCall[]`, `evidence: ApiEvidence[]`, `claims: ApiClaim[]`; drops `evidence_count`/`claim_count`.

**`agentStore.loadAgentsFromApi` rewrite**: map `tool_calls` → `ToolCall[]` (`{name, params, result, timestamp}`), `evidence` → `EvidenceRef[]` (`{id, source, reliability}`), `claims` → `ClaimRef[]` (`{id, text, supports, contradicts}`), `step` from API, `thought: ''`. `AgentPanel` then renders real arrays with no further change.

**Timeline** (`frontend/src/components/layout/BottomTimeline.tsx`): add `formatEventMessage(event: MagiEvent): string` deriving a specific label from `event.type` + `event.data` (payload), falling back to `event.message`:
- `TOOL_CALL` + `data.tool_name` → `called {tool_name}`
- `EVIDENCE_CREATED` + `data.evidence_id` → `evidence {evidence_id}`
- `VOTE_SUBMITTED` + `data.stance`/`data.confidence` → `voted {stance} ({confidence}%)`
- `CONSENSUS_CHANGED` + `data.outcome` → `consensus: {outcome}`
- `DEBATE_START` → `debate started` (round from `data.round` if present)
- fallback → `event.message`

## Testing

TDD throughout (red-green-refactor per the test-driven-development skill):

- **Backend unit**: `ToolCallModel` migrates (sqlite in-memory); `persistArtifacts` persists tool calls with namespaced IDs + remapped evidence_id (stub repo with snapshot); `ArtifactHandler.Agents` returns toolCalls/evidence/claims per agent (in-memory repo seeded with multi-agent data).
- **Frontend unit**: `loadAgentsFromApi` maps enriched shape into `AgentSnapshot` (arrays populated, step set); `formatEventMessage` derives correct labels from type+payload and falls back to `event.message`.

## Risks

- **`step` is a proxy.** Real per-step trace isn't persisted; `len(tool_calls)` approximates progress. Acceptable for display; if exact step is needed later, persist `Step.Index` on the tool-call row.
- **Evidence sparsity under stub executor.** With `StubToolExecutor`, few/no evidence records are produced, so `evidence[]` may be empty and Evidence Graph nodes sparse. This is expected until real tools are integrated (Gap 3, separate). Tool calls and claims still render.
- **`evidence_id` remap consistency.** Tool-call rows reference evidence by the namespaced persisted ID (remapped through `evRemap`), so tool_call → evidence links survive the case-scoped ID scheme.
