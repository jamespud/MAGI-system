# Case Page Quality Fixes — Design

- Date: 2026-07-22
- Status: Approved
- Scope: Frontend only (`frontend/src/`)

## Problem

Four quality issues on the case workspace page discovered after real-data testing:

1. **StatusBadge mismatch** — AgentPanel badge shows runtime status (COMPLETED/ERROR) not vote result (APPROVE/REJECT), and `apiStatusToAgentStatus` misses `cancelled` mapping.
2. **Run button stale** — `disabled={running || resolved}` only covers HTTP request duration; case runs async so button is clickable during agent loop.
3. **Timeline hardcoded** — `currentStep = 0` regardless of case progress.
4. **Evidence Graph sparse** — Nodes render but no connections because LLM may not provide `supports` arrays in claims; buildGraph has no fallback linking.

## Design (approved)

### 1. StatusBadge → vote result

**File: `src/components/workspace/AgentPanel.tsx`**

StatusBadge now shows vote stance (via `stanceLabel` + `stanceColor`) when agent has voted; "RUNNING" with agent color while active; "IDLE" when not started.

**Also fix** `src/stores/agentStore.ts`: `apiStatusToAgentStatus` add `case 'cancelled': return 'error'`.

### 2. Run button state-driven

**File: `src/pages/DecisionWorkspace.tsx`**

Replace `disabled={running || resolved}` with `runButtonState(currentCase.status)`:

| Case status group | disabled | label |
|---|---|---|
| Active (INVESTIGATING … EVALUATING) | true | "Running..." |
| RESOLVED | true | "Resolved" |
| FAILED / DEADLOCKED / CANCELLED / TIMED_OUT | false | "Re-run" |
| default (DRAFT etc.) | false | "Run Decision" |

Remove `running` local state (no longer needed).

### 3. Timeline dynamic

**File: `src/components/workspace/ConsensusPanel.tsx`**

Replace hardcoded `currentStep = 0` with `timelineStep(status)`:

```ts
function timelineStep(status: CaseStatus): number {
  switch (status) {
    case 'INVESTIGATING','EVIDENCE_GATING','COLLECTING_VOTES','CONSENSUS_CHECK': return 0;
    case 'DEBATING': return 1;
    case 'REFLECTING': return 2;
    case 'REVOTING': return 3;
    case 'RESOLVING','GENERATING_REPORT','SAVING_MEMORY','EVALUATING',
         'RESOLVED','DEADLOCKED','FAILED','CANCELLED','TIMED_OUT': return 4;
    default: return 0;
  }
}
```

Timeline steps: `['Round 1', 'Debate', 'Reflection', 'Round 2', 'Resolved']`. Direct-to-RESOLVED cases light all 5; round-2 cases step through.

### 4. Evidence Graph implicit links

**File: `src/components/evidence/EvidenceGraph.tsx` — `buildGraph`**

After existing explicit connections (claim.supports → evidence, claim.contradicts → claim, claim → vote), add implicit agent-based links:

- evidence → collector agent's claims (grouped by `created_by`)
- evidence → collector agent's vote

This guarantees every agent's evidence→claims→vote forms at least a tree structure even when LLM omits `supports`.

```ts
// build agent→claims index
const agentClaims: Record<string, string[]> = {};
for (const c of claims) {
  (agentClaims[c.created_by] ??= []).push(c.id);
}
// implicit: evidence → agent's claims + vote
for (const e of evidence) {
  const voteId = `vote-${e.collected_by}`;
  if (nodeIds.has(voteId)) {
    links.push({ source: e.id, target: voteId, type: 'supports' });
  }
  for (const clId of agentClaims[e.collected_by] ?? []) {
    if (nodeIds.has(clId)) {
      links.push({ source: e.id, target: clId, type: 'supports' });
    }
  }
}
```

## Testing

- **AgentPanel test**: verify that agent with vote renders stance badge, agent without vote with `isRunning` renders RUNNING badge.
- **DecisionWorkspace test**: verify Run button label/disabled per status.
- **ConsensusPanel test**: verify timeline step per status.
- **EvidenceGraph test**: verify implicit links appear when explicit supports are empty.

## Out of Scope

- No backend changes.
- No new dependencies.
- No changes to other components.

## Files Touched

- Modify: `src/components/workspace/AgentPanel.tsx`
- Modify: `src/stores/agentStore.ts`
- Modify: `src/pages/DecisionWorkspace.tsx`
- Modify: `src/components/workspace/ConsensusPanel.tsx`
- Modify: `src/components/evidence/EvidenceGraph.tsx`
