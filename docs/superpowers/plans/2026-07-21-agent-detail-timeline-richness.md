# Agent Detail + Timeline Richness Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make agent panels show real tool calls / evidence / claims (persisted + exposed via `/agents`) and make the timeline render event payload detail, closing the richness gap vs the original mock UI.

**Architecture:** New `ToolCall` entity + `ToolCallModel` (s8 `magi_tool_call` table) + `ToolCallRepository`; orchestrator persists `LoopResult.Trace.Steps[].ToolCalls` in `persistArtifacts` with namespaced IDs + remapped evidence_id; `/agents` endpoint joins tool_calls + evidence + claims + votes per agent; frontend `loadAgentsFromApi` maps the enriched shape into `AgentSnapshot`; `BottomTimeline` derives specific labels from `event.payload`.

**Tech Stack:** Go 1.24, GORM/MySQL, Hertz, Uber Fx (backend); React 18, TypeScript 5.6, Zustand, Vitest (frontend). Tests: `go test`, `npm test`.

## Global Constraints

- Go module `github.com/jamespud/magi/backend`, Go 1.24+. Domain (`domain/`) depends only on `domain/port` + eino; Coze impls in `adapter/`.
- GORM mysql dialector sets `DefaultStringSize: 191` (via `bootstrap.MysqlDialector`) -- do not regress.
- Structured-output structs need `json` tags on every field (ADR-003).
- Conventional Commits (`feat:`, `fix:`, `test:`, `chore:`).
- Backend tests: `cd backend && go test ./...`; single test `go test ./path/ -run Name -v`.
- Frontend tests: `cd frontend && npm test`; typecheck `npx tsc -b --noEmit`.
- TDD: write failing test, watch it fail, minimal code, watch pass, commit.

---

## File Structure

**Backend:**
- `backend/domain/entity/tool_call.go` (create) -- `ToolCall` entity.
- `backend/adapter/model.go` (modify) -- `ToolCallModel` + add to `AllModels()`.
- `backend/domain/port/repository.go` (modify) -- `ToolCallRepository` interface + `Repository.ToolCallRepo()`.
- `backend/adapter/repository.go` (modify) -- `toolCallRepo` impl + wire into `magiRepository`.
- `backend/domain/orchestration/orchestrator.go` (modify) -- persist tool calls in `persistArtifacts`.
- `backend/domain/orchestration/orchestrator_test.go` (modify) -- extend stub repo + persistence test.
- `backend/server/dto/dto.go` (modify) -- `AgentSnapshotDTO` arrays + `ToolCallDTO` + `FromToolCall`.
- `backend/application/decision/service.go` (modify) -- `ToolCalls` accessor + `WithToolCallRepo`.
- `backend/server/handler/artifact.go` (modify) -- rewrite `Agents` to return arrays.
- `backend/server/handler/artifact_test.go` (modify) -- assert arrays per agent.
- `backend/bootstrap/container.go` (modify) -- `WithToolCallRepo` wiring.

**Frontend:**
- `frontend/src/api/client.ts` (modify) -- `ApiAgentSnapshot` arrays + `ApiToolCall`.
- `frontend/src/stores/agentStore.ts` (modify) -- `loadAgentsFromApi` maps enriched shape.
- `frontend/src/stores/__tests__/agentStore.test.ts` (modify) -- `loadAgentsFromApi` test.
- `frontend/src/components/layout/BottomTimeline.tsx` (modify) -- `formatEventMessage`.
- `frontend/src/components/layout/__tests__/BottomTimeline.test.tsx` (create) -- `formatEventMessage` test.

---

## Task 1: ToolCall entity + model + repository

**Files:**
- Create: `backend/domain/entity/tool_call.go`
- Modify: `backend/adapter/model.go`
- Modify: `backend/domain/port/repository.go`
- Modify: `backend/adapter/repository.go`
- Test: `backend/bootstrap/container_test.go` (extend `TestAllModels_MigrateWithoutError`)

**Interfaces:**
- Produces: `entity.ToolCall` struct; `port.ToolCallRepository` (`Create(ctx, *entity.ToolCall) error`, `ListByCase(ctx, caseID string) ([]*entity.ToolCall, error)`); `magiRepository.ToolCallRepo()`; `ToolCallModel` in `AllModels()`.

- [ ] **Step 1: Write the failing test**

Append to `backend/bootstrap/container_test.go`:

```go
func TestAllModels_IncludesToolCallModel(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.AutoMigrate(magi.AllModels()...); err != nil {
		t.Fatalf("auto-migrate: %v", err)
	}
	if !db.Migrator().HasTable("magi_tool_call") {
		t.Fatal("magi_tool_call table not created")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd backend && go test ./bootstrap/ -run TestAllModels_IncludesToolCallModel -v`
Expected: FAIL -- `magi_tool_call table not created` (ToolCallModel not in AllModels yet).

- [ ] **Step 3: Create the ToolCall entity**

`backend/domain/entity/tool_call.go`:

```go
package entity

import "time"

// ToolCall is one tool invocation by an agent during a run (persisted to
// magi_tool_call, ADR/S8). Mirrors runtime.ToolCallRecord plus case/run linkage.
type ToolCall struct {
	ID         string
	CaseID     string
	AgentRunID string
	ToolCallID string // the LLM-assigned tool-call id
	ToolName   string
	Arguments  string
	Valid      bool
	Result     string
	Err        string
	EvidenceID string // namespaced persisted EV-ID this call produced (may be empty)
	DurationMs int64
	CreatedAt  time.Time
}
```

- [ ] **Step 4: Add ToolCallModel + register in AllModels**

In `backend/adapter/model.go`, append after `MemoryProjectionModel`:

```go
type ToolCallModel struct {
	ID         string `gorm:"primaryKey"`
	AgentRunID string `gorm:"index"`
	ToolCallID string
	ToolName   string
	Arguments  string `gorm:"type:text"`
	Valid      bool
	Result     string `gorm:"type:text"`
	Err        string `gorm:"type:text"`
	EvidenceID string
	DurationMs int64
	CreatedAt  time.Time
}

func (ToolCallModel) TableName() string { return "magi_tool_call" }
```

In `AllModels()`, add `&ToolCallModel{}` to the slice (after `&MemoryProjectionModel{}`):

```go
func AllModels() []any {
	return []any{
		&CaseModel{}, &AgentRunModel{}, &EvidenceModel{}, &ClaimModel{},
		&VoteModel{}, &ResolutionModel{}, &EventModel{},
		&DebateRoundModel{}, &ReflectionModel{}, &MemoryProjectionModel{},
		&ToolCallModel{},
	}
}
```

- [ ] **Step 5: Add ToolCallRepository port**

In `backend/domain/port/repository.go`, add the interface and the accessor on `Repository`:

```go
type ToolCallRepository interface {
	Create(ctx context.Context, t *entity.ToolCall) error
	ListByCase(ctx context.Context, caseID string) ([]*entity.ToolCall, error)
}
```

Add `ToolCallRepo() ToolCallRepository` to the `Repository` interface (after `CheckpointRepo()`).

- [ ] **Step 6: Implement toolCallRepo + wire into magiRepository**

In `backend/adapter/repository.go`, add the accessor on `magiRepository` (next to the other `XxxRepo()` methods):

```go
func (r *magiRepository) ToolCallRepo() port.ToolCallRepository { return &toolCallRepo{db: r.db} }
```

Append the impl (after `memoryRepo`):

```go
// --- ToolCallRepository ---

type toolCallRepo struct{ db *gorm.DB }

func (r *toolCallRepo) Create(ctx context.Context, t *entity.ToolCall) error {
	m := ToolCallModel{
		ID: t.ID, AgentRunID: t.AgentRunID, ToolCallID: t.ToolCallID, ToolName: t.ToolName,
		Arguments: t.Arguments, Valid: t.Valid, Result: t.Result, Err: t.Err,
		EvidenceID: t.EvidenceID, DurationMs: t.DurationMs, CreatedAt: t.CreatedAt,
	}
	return r.db.WithContext(ctx).Create(&m).Error
}

func (r *toolCallRepo) ListByCase(ctx context.Context, caseID string) ([]*entity.ToolCall, error) {
	var models []ToolCallModel
	err := r.db.WithContext(ctx).
		Joins("JOIN magi_agent_run ON magi_agent_run.id = magi_tool_call.agent_run_id").
		Where("magi_agent_run.case_id = ?", caseID).
		Order("magi_tool_call.created_at ASC").
		Find(&models).Error
	if err != nil {
		return nil, err
	}
	out := make([]*entity.ToolCall, len(models))
	for i, m := range models {
		out[i] = &entity.ToolCall{
			ID: m.ID, AgentRunID: m.AgentRunID, ToolCallID: m.ToolCallID, ToolName: m.ToolName,
			Arguments: m.Arguments, Valid: m.Valid, Result: m.Result, Err: m.Err,
			EvidenceID: m.EvidenceID, DurationMs: m.DurationMs, CreatedAt: m.CreatedAt,
		}
	}
	return out, nil
}
```

- [ ] **Step 7: Run test to verify it passes**

Run: `cd backend && go test ./bootstrap/ -run TestAllModels -v && go build ./...`
Expected: PASS; build clean.

- [ ] **Step 8: Commit**

```bash
git add backend/domain/entity/tool_call.go backend/adapter/model.go backend/domain/port/repository.go backend/adapter/repository.go backend/bootstrap/container_test.go
git commit -m "feat(entity): add ToolCall entity + ToolCallRepository (magi_tool_call table)"
```

---

## Task 2: orchestrator persists tool calls

**Files:**
- Modify: `backend/domain/orchestration/orchestrator.go` (in `persistArtifacts`)
- Modify: `backend/domain/orchestration/orchestrator_test.go` (extend stubRepo + test)

**Interfaces:**
- Consumes: `entity.ToolCall`, `port.ToolCallRepository` (Task 1), `runtime.ToolCallRecord` (via `LoopResult.Trace.Steps`).
- Produces: `persistArtifacts` writes `magi_tool_call` rows per agent run.

- [ ] **Step 1: Extend the stubRepo to record tool calls**

In `backend/domain/orchestration/orchestrator_test.go`, add a `toolCalls` slice to `stubRepo`:

```go
type stubRepo struct {
	mu          sync.Mutex
	cases       []*entity.DecisionCase
	statuses    map[string]entity.CaseStatus
	agentRuns   []*entity.AgentRun
	evidence    []*entity.EvidenceRecord
	claims      []*entity.Claim
	votes       []*entity.Vote
	resolutions []*entity.Resolution
	toolCalls   []*entity.ToolCall
}
```

Add the accessor and sub-repo (next to the other stubs):

```go
func (s *stubRepo) ToolCallRepo() port.ToolCallRepository { return &stubToolCallRepo{s: s} }

type stubToolCallRepo struct{ s *stubRepo }
func (r *stubToolCallRepo) Create(ctx context.Context, t *entity.ToolCall) error {
	r.s.mu.Lock(); defer r.s.mu.Unlock()
	cp := *t
	r.s.toolCalls = append(r.s.toolCalls, &cp)
	return nil
}
func (r *stubToolCallRepo) ListByCase(ctx context.Context, caseID string) ([]*entity.ToolCall, error) {
	return nil, nil
}
```

- [ ] **Step 2: Make the mock produce a tool-call record**

The mock's `Run` builds a `LoopResult` with a `Trace`. Add a `ToolCallRecord` to the trace step. In the mock `Run` (around the `return &runtime.LoopResult{...}`), change the `Trace` field to include a tool call:

```go
	return &runtime.LoopResult{
		Vote:   vote,
		Status: runtime.LoopStatusCompleted,
		Ledger: ledger,
		Trace: &runtime.LoopTrace{Steps: []*runtime.Step{{
			IsFinal: true,
			ToolCalls: []runtime.ToolCallRecord{{
				ToolCallID: "call-1", ToolName: "calc", Arguments: `{"a":1,"b":2}`,
				Valid: true, Result: "3", Duration: 5 * time.Millisecond,
			}},
		}}},
		Usage: &entity.Usage{TotalTokens: 100},
	}, nil
```

Ensure `"time"` is imported in the test file (it is, from the integration test).

- [ ] **Step 3: Write the failing test**

Extend `TestOrchestrate_PersistsArtifacts` -- add after the evidence/votes/resolutions assertions (inside `repo.mu.Lock()` block):

```go
	if len(repo.toolCalls) != 3 {
		t.Fatalf("expected 3 tool calls persisted (1 per agent), got %d", len(repo.toolCalls))
	}
	seenTC := map[string]bool{}
	for _, tc := range repo.toolCalls {
		if seenTC[tc.ID] {
			t.Fatalf("duplicate persisted tool call ID: %s", tc.ID)
		}
		seenTC[tc.ID] = true
		if tc.AgentRunID == "" {
			t.Fatalf("tool call %s missing AgentRunID", tc.ID)
		}
		if tc.ToolName != "calc" {
			t.Fatalf("tool call ToolName: %s", tc.ToolName)
		}
	}
```

- [ ] **Step 4: Run test to verify it fails**

Run: `cd backend && go test ./domain/orchestration/ -run TestOrchestrate_PersistsArtifacts -v`
Expected: FAIL -- `expected 3 tool calls persisted, got 0` (persistence not yet added).

- [ ] **Step 5: Persist tool calls in persistArtifacts**

In `backend/domain/orchestration/orchestrator.go`, inside `persistArtifacts`, after the claims loop (still inside `if r.Ledger != nil { ... }` is wrong -- tool calls live on `r.Trace`, not the ledger). Add a separate block after the `if r.Ledger != nil { ... }` block, before the vote remap:

```go
		// Persist tool-call records from the run trace. The PK is a namespaced
		// counter (the LLM's ToolCallID is stored separately and may collide
		// across agents); evidence_id is remapped to the persisted EV-ID.
		if r.Trace != nil {
			toolIdx := 0
			for _, st := range r.Trace.Steps {
				for _, tc := range st.ToolCalls {
					toolIdx++
					evID := tc.EvidenceID
					if remapped, ok := evRemap[evID]; ok {
						evID = remapped
					}
					toolCall := &entity.ToolCall{
						ID:         fmt.Sprintf("%s-tc%d", prefix, toolIdx),
						CaseID:     case_.ID,
						AgentRunID: run.ID,
						ToolCallID: tc.ToolCallID,
						ToolName:   tc.ToolName,
						Arguments:  tc.Arguments,
						Valid:      tc.Valid,
						Result:     tc.Result,
						Err:        tc.Err,
						EvidenceID: evID,
						DurationMs: tc.Duration.Milliseconds(),
						CreatedAt:  now,
					}
					_ = o.repo.ToolCallRepo().Create(ctx, toolCall)
				}
			}
		}
```

(`prefix` is already in scope from the ledger block above; if `r.Ledger` is nil the `prefix` variable is never declared -- so hoist `prefix` declaration before the `if r.Ledger != nil` block. Move `prefix := fmt.Sprintf("%s-%s-r%d-%s", case_.ID, code, round, phase)` to just before `if r.Ledger != nil {`.)

- [ ] **Step 6: Run test to verify it passes**

Run: `cd backend && go test ./domain/orchestration/... -v`
Expected: PASS (all orchestration tests, including the existing debate + integration tests).

- [ ] **Step 7: Commit**

```bash
git add backend/domain/orchestration/orchestrator.go backend/domain/orchestration/orchestrator_test.go
git commit -m "feat(orchestration): persist tool-call records per agent run"
```

---

## Task 3: Enrich `/agents` endpoint with arrays

**Files:**
- Modify: `backend/server/dto/dto.go`
- Modify: `backend/application/decision/service.go`
- Modify: `backend/server/handler/artifact.go`
- Modify: `backend/server/handler/artifact_test.go`
- Modify: `backend/bootstrap/container.go`

**Interfaces:**
- Consumes: `entity.ToolCall` + `ToolCallRepository` (Task 1), `entity.EvidenceRecord`/`Claim`/`Vote` (existing).
- Produces: `AgentSnapshotDTO` with `tool_calls[]`/`evidence[]`/`claims[]`/`step`; `Service.ToolCalls(ctx, caseID)`; `WithToolCallRepo`.

- [ ] **Step 1: Write the failing test**

In `backend/server/handler/artifact_test.go`, replace the existing `TestArtifactHandler_Evidence*` helpers' `memEvidenceRepo` pattern is fine; add a new test for the enriched Agents endpoint. First add a `memToolCallRepo` and seed data, then:

```go
type memToolCallRepo struct{ items []*entity.ToolCall }

func (r *memToolCallRepo) Create(ctx context.Context, t *entity.ToolCall) error { r.items = append(r.items, t); return nil }
func (r *memToolCallRepo) ListByCase(ctx context.Context, caseID string) ([]*entity.ToolCall, error) {
	var out []*entity.ToolCall
	for _, t := range r.items {
		if t.CaseID == caseID {
			out = append(out, t)
		}
	}
	return out, nil
}

func TestArtifactHandler_AgentsReturnsArrays(t *testing.T) {
	evRepo := &memEvidenceRepo{items: []*entity.EvidenceRecord{
		{ID: "EV-m1", CaseID: "c1", Observation: "obs", Reliability: entity.ReliabilityScore{Final: 0.9}, CollectedBy: entity.MagiCode("melchior"), CreatedAt: time.Now()},
	}}
	clRepo := &memClaimRepo{items: []*entity.Claim{
		{ID: "CL-m1", CaseID: "c1", Statement: "claim text", CreatedBy: entity.MagiCode("melchior"), CreatedAt: time.Now()},
	}}
	tcRepo := &memToolCallRepo{items: []*entity.ToolCall{
		{ID: "tc1", CaseID: "c1", AgentRunID: "run-m1", ToolCallID: "call-1", ToolName: "calc", Arguments: "{}", Valid: true, Result: "3", DurationMs: 5, CreatedAt: time.Now()},
	}}
	runRepo := &memAgentRunRepo{items: []*entity.AgentRun{
		{ID: "run-m1", CaseID: "c1", MagiCode: entity.MagiCode("melchior"), Round: 1, Status: entity.AgentRunStatusCompleted, StartedAt: time.Now()},
	}}
	svc := decision.NewService(nil, decision.ServiceConfig{},
		decision.WithEvidenceRepo(evRepo),
		decision.WithClaimRepo(clRepo),
		decision.WithAgentRunRepo(runRepo),
		decision.WithToolCallRepo(tcRepo))
	h := handler.NewArtifactHandler(svc)

	r := hzserver.Default(hzserver.WithHostPorts("127.0.0.1:0"))
	r.GET("/cases/:id/agents", h.Agents)

	w := ut.PerformRequest(r.Engine, "GET", "/cases/c1/agents", nil)
	resp := w.Result()
	if resp.StatusCode() != 200 {
		t.Fatalf("status: %d body=%s", resp.StatusCode(), string(resp.Body()))
	}
	body := string(resp.Body())
	if !contains(body, `"tool_calls"`) || !contains(body, `"calc"`) {
		t.Fatalf("response missing tool_calls/calc: %s", body)
	}
	if !contains(body, `"evidence"`) || !contains(body, "EV-m1") {
		t.Fatalf("response missing evidence array: %s", body)
	}
	if !contains(body, `"claims"`) || !contains(body, "claim text") {
		t.Fatalf("response missing claims array: %s", body)
	}
}
```

Add `memClaimRepo` and `memAgentRunRepo` stubs the same way as `memEvidenceRepo` (Create + ListByCase returning items where `CaseID`/`case match`). `memAgentRunRepo.ListByCase` returns items where `CaseID == caseID`.

- [ ] **Step 2: Run test to verify it fails**

Run: `cd backend && go test ./server/handler/ -run TestArtifactHandler_AgentsReturnsArrays -v`
Expected: FAIL -- `WithToolCallRepo` undefined and/or response missing `tool_calls` (current `Agents` returns counts only).

- [ ] **Step 3: Add DTOs + FromToolCall**

In `backend/server/dto/dto.go`, replace the `AgentSnapshotDTO` struct:

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

(Remove the old `EvidenceCount`/`ClaimCount` fields.)

Append the converter:

```go
func FromToolCall(t *entity.ToolCall) ToolCallDTO {
	return ToolCallDTO{
		ToolCallID: t.ToolCallID,
		ToolName:   t.ToolName,
		Arguments:  t.Arguments,
		Result:     t.Result,
		Err:        t.Err,
		EvidenceID: t.EvidenceID,
		DurationMs: t.DurationMs,
	}
}
```

- [ ] **Step 4: Add Service.ToolCalls + WithToolCallRepo**

In `backend/application/decision/service.go`, add a `toolCallRepo port.ToolCallRepository` field to `Service`, the option, and the accessor:

```go
// WithToolCallRepo injects a ToolCallRepository for the /agents endpoint.
func WithToolCallRepo(repo port.ToolCallRepository) Option {
	return func(s *Service) { s.toolCallRepo = repo }
}
```

Add `toolCallRepo port.ToolCallRepository` to the `Service` struct. Add the accessor (next to `AgentRuns`):

```go
// ToolCalls returns all tool-call records for a case.
func (s *Service) ToolCalls(ctx context.Context, caseID string) ([]*entity.ToolCall, error) {
	if s.toolCallRepo == nil {
		return nil, nil
	}
	return s.toolCallRepo.ListByCase(ctx, caseID)
}
```

- [ ] **Step 5: Rewrite ArtifactHandler.Agents**

In `backend/server/handler/artifact.go`, replace the `Agents` method body:

```go
func (h *ArtifactHandler) Agents(ctx context.Context, c *app.RequestContext) {
	id := c.Param("id")
	runs, _ := h.svc.AgentRuns(ctx, id)
	evs, _ := h.svc.Evidence(ctx, id)
	cls, _ := h.svc.Claims(ctx, id)
	vs, _ := h.svc.Votes(ctx, id)
	tcs, _ := h.svc.ToolCalls(ctx, id)

	evByAgent := map[string][]dto.EvidenceDTO{}
	for _, e := range evs {
		code := string(e.CollectedBy)
		evByAgent[code] = append(evByAgent[code], dto.FromEvidence(e))
	}
	clByAgent := map[string][]dto.ClaimDTO{}
	for _, cl := range cls {
		code := string(cl.CreatedBy)
		clByAgent[code] = append(clByAgent[code], dto.FromClaim(cl))
	}
	tcByRun := map[string][]dto.ToolCallDTO{}
	for _, tc := range tcs {
		tcByRun[tc.AgentRunID] = append(tcByRun[tc.AgentRunID], dto.FromToolCall(tc))
	}
	voteByAgent := map[string]*dto.VoteDTO{}
	for _, v := range vs {
		key := agentCodeFromRun(runs, v)
		if key == "" {
			continue
		}
		vd := dto.FromVote(v)
		if cur, ok := voteByAgent[key]; !ok || v.Round >= cur.Round {
			voteByAgent[key] = &vd
		}
	}
	// runID by agent code (latest round)
	runIDByAgent := map[string]string{}
	runRoundByAgent := map[string]int{}
	for _, r := range runs {
		code := string(r.MagiCode)
		if _, ok := runIDByAgent[code]; !ok || r.Round >= runRoundByAgent[code] {
			runIDByAgent[code] = r.ID
			runRoundByAgent[code] = r.Round
		}
	}

	out := make(map[string]dto.AgentSnapshotDTO, len(runs))
	for _, r := range runs {
		code := string(r.MagiCode)
		runID := runIDByAgent[code]
		snap := dto.AgentSnapshotDTO{
			AgentCode: code,
			Status:    string(r.Status),
			Round:     r.Round,
			Step:      len(tcByRun[runID]),
			ToolCalls: tcByRun[runID],
			Evidence:  evByAgent[code],
			Claims:    clByAgent[code],
		}
		if v, ok := voteByAgent[code]; ok {
			snap.Vote = v
		}
		if snap.ToolCalls == nil {
			snap.ToolCalls = []dto.ToolCallDTO{}
		}
		if snap.Evidence == nil {
			snap.Evidence = []dto.EvidenceDTO{}
		}
		if snap.Claims == nil {
			snap.Claims = []dto.ClaimDTO{}
		}
		out[code] = snap
	}
	c.JSON(consts.StatusOK, out)
}
```

- [ ] **Step 6: Wire WithToolCallRepo in bootstrap**

In `backend/bootstrap/container.go` `provideDecisionService`, add `decision.WithToolCallRepo(repo.ToolCallRepo()),` to the `NewService` options (next to the other `With*Repo` calls).

- [ ] **Step 7: Run tests to verify pass**

Run: `cd backend && go test ./server/handler/ -run TestArtifactHandler -v && go build ./...`
Expected: PASS; build clean.

- [ ] **Step 8: Commit**

```bash
git add backend/server/dto/dto.go backend/application/decision/service.go backend/server/handler/artifact.go backend/server/handler/artifact_test.go backend/bootstrap/container.go
git commit -m "feat(server): /agents returns tool_calls/evidence/claims arrays per agent"
```

---

## Task 4: Frontend loadAgentsFromApi maps enriched shape

**Files:**
- Modify: `frontend/src/api/client.ts`
- Modify: `frontend/src/stores/agentStore.ts`
- Modify: `frontend/src/stores/__tests__/agentStore.test.ts`

**Interfaces:**
- Consumes: backend `AgentSnapshotDTO` (Task 3).
- Produces: `loadAgentsFromApi` populates `AgentSnapshot.toolCalls/evidence/claims/step` from real data.

- [ ] **Step 1: Write the failing test**

Append to `frontend/src/stores/__tests__/agentStore.test.ts`:

```ts
import { api } from '@/api/client';

vi.mock('@/api/client', () => ({ api: {} }));

describe('loadAgentsFromApi', () => {
  beforeEach(() => {
    useAgentStore.getState().resetAgents();
  });

  it('maps enriched API snapshot into AgentSnapshot arrays', () => {
    const snap = {
      melchior: {
        agent_code: 'melchior',
        status: 'completed',
        round: 1,
        step: 2,
        tool_calls: [
          { tool_call_id: 'call-1', tool_name: 'calc', arguments: '{}', result: '3', duration_ms: 5 },
        ],
        evidence: [
          { id: 'EV-m1', source: 'local', observation: 'obs', reliability: 0.9, collected_by: 'melchior', timestamp: 't' },
        ],
        claims: [
          { id: 'CL-m1', text: 'claim text', supports: [], contradicts: [], created_by: 'melchior' },
        ],
        vote: { id: 'v1', agent_code: 'melchior', stance: 'approve', confidence: 80, reasoning: 'r', round: 1 },
      },
    };
    useAgentStore.getState().loadAgentsFromApi(snap as never);

    const m = useAgentStore.getState().agents.melchior;
    expect(m?.status).toBe('completed');
    expect(m?.step).toBe(2);
    expect(m?.toolCalls).toHaveLength(1);
    expect(m?.toolCalls[0].name).toBe('calc');
    expect(m?.evidence).toHaveLength(1);
    expect(m?.evidence[0].id).toBe('EV-m1');
    expect(m?.claims).toHaveLength(1);
    expect(m?.claims[0].text).toBe('claim text');
    expect(m?.vote?.stance).toBe('approve');
  });

  it('handles empty arrays without throwing', () => {
    useAgentStore.getState().loadAgentsFromApi({
      casper: { agent_code: 'casper', status: 'running', round: 1, step: 0, tool_calls: [], evidence: [], claims: [] },
    } as never);
    const c = useAgentStore.getState().agents.casper;
    expect(c?.toolCalls).toEqual([]);
    expect(c?.evidence).toEqual([]);
    expect(c?.claims).toEqual([]);
    expect(c?.vote).toBeNull();
  });
});
```

Add `import { vi } from 'vitest'` to the imports if not present.

- [ ] **Step 2: Run test to verify it fails**

Run: `cd frontend && npm test -- agentStore.test.ts`
Expected: FAIL -- `loadAgentsFromApi` maps the old thin shape (no tool_calls/evidence/claims arrays populated; `step` from API not wired).

- [ ] **Step 3: Extend ApiAgentSnapshot + ApiToolCall**

In `frontend/src/api/client.ts`, replace `ApiAgentSnapshot` and add `ApiToolCall`:

```ts
interface ApiToolCall {
  tool_call_id: string;
  tool_name: string;
  arguments: string;
  result: string;
  err?: string;
  evidence_id?: string;
  duration_ms: number;
}

interface ApiAgentSnapshot {
  agent_code: string;
  status: string;
  round: number;
  step: number;
  tool_calls: ApiToolCall[];
  evidence: ApiEvidence[];
  claims: ApiClaim[];
  vote?: ApiVote;
}
```

(Remove `evidence_count`/`claim_count`.) Add `ApiToolCall` to the `export type { ... }` list.

- [ ] **Step 4: Rewrite loadAgentsFromApi**

In `frontend/src/stores/agentStore.ts`, replace the `loadAgentsFromApi` method:

```ts
  loadAgentsFromApi: (snap) => {
    const agents = { ...empty } as Record<AgentId, AgentSnapshot | null>;
    for (const [k, v] of Object.entries(snap)) {
      const id = k as AgentId;
      const vote: AgentVote | null = v.vote
        ? { stance: v.vote.stance as AgentVote['stance'], confidence: v.vote.confidence, reasoning: v.vote.reasoning }
        : null;
      agents[id] = {
        agentId: id,
        status: apiStatusToAgentStatus(v.status),
        step: v.step ?? 0,
        maxSteps: 12,
        thought: '',
        toolCalls: (v.tool_calls ?? []).map((tc) => ({
          name: tc.tool_name,
          params: tc.arguments ? safeParseArgs(tc.arguments) : {},
          result: tc.result || null,
          timestamp: '',
        })),
        evidence: (v.evidence ?? []).map((e) => ({ id: e.id, source: e.source, reliability: e.reliability })),
        claims: (v.claims ?? []).map((cl) => ({ id: cl.id, text: cl.text, supports: cl.supports, contradicts: cl.contradicts })),
        vote,
      };
    }
    set({ agents });
  },
```

Add the helpers above the `create(...)` call (next to `apiStatusToAgentStatus`):

```ts
function safeParseArgs(args: string): Record<string, string> {
  try {
    const parsed = JSON.parse(args);
    if (parsed && typeof parsed === 'object') {
      const out: Record<string, string> = {};
      for (const [k, v] of Object.entries(parsed)) out[k] = String(v);
      return out;
    }
  } catch {
    // not JSON -- return the raw string under a single key
  }
  return args ? { raw: args } : {};
}
```

The `AgentState` interface already has `loadAgentsFromApi` typed as `(snap: Record<string, ApiAgentSnapshot>) => void` (from the prior plan); confirm the signature matches. If it was typed differently, update it to `(snap: Record<string, ApiAgentSnapshot>) => void`.

- [ ] **Step 5: Run test to verify pass + typecheck**

Run: `cd frontend && npm test -- agentStore.test.ts && npx tsc -b --noEmit`
Expected: PASS; tsc clean.

- [ ] **Step 6: Commit**

```bash
git add frontend/src/api/client.ts frontend/src/stores/agentStore.ts frontend/src/stores/__tests__/agentStore.test.ts
git commit -m "feat(frontend): map enriched /agents shape into AgentSnapshot arrays"
```

---

## Task 5: Timeline renders event payload detail

**Files:**
- Modify: `frontend/src/components/layout/BottomTimeline.tsx`
- Create: `frontend/src/components/layout/__tests__/BottomTimeline.test.ts`

**Interfaces:**
- Consumes: `MagiEvent` (with `type` + `data` payload) from `eventStore`.
- Produces: `formatEventMessage(event: MagiEvent): string` exported for testing.

- [ ] **Step 1: Write the failing test**

`frontend/src/components/layout/__tests__/BottomTimeline.test.ts`:

```ts
import { describe, it, expect } from 'vitest';
import { formatEventMessage } from '../BottomTimeline';
import type { MagiEvent } from '@/types/event';

function ev(type: MagiEvent['type'], data?: Record<string, unknown>, message = 'fallback'): MagiEvent {
  return { id: 'e1', type, timestamp: 't', message, data };
}

describe('formatEventMessage', () => {
  it('uses tool_name for TOOL_CALL', () => {
    expect(formatEventMessage(ev('TOOL_CALL', { tool_name: 'web_search' }))).toBe('called web_search');
  });

  it('uses evidence_id for EVIDENCE_CREATED', () => {
    expect(formatEventMessage(ev('EVIDENCE_CREATED', { evidence_id: 'EV-001' }))).toBe('evidence EV-001');
  });

  it('uses stance + confidence for VOTE_SUBMITTED', () => {
    expect(formatEventMessage(ev('VOTE_SUBMITTED', { stance: 'approve', confidence: 80 }))).toBe('voted approve (80%)');
  });

  it('uses outcome for CONSENSUS_CHANGED', () => {
    expect(formatEventMessage(ev('CONSENSUS_CHANGED', { outcome: 'strong_approval' }))).toBe('consensus: strong_approval');
  });

  it('falls back to event.message when payload lacks the field', () => {
    expect(formatEventMessage(ev('TOOL_CALL', {}, 'Tool call requested'))).toBe('Tool call requested');
  });

  it('falls back to event.message for types without payload logic', () => {
    expect(formatEventMessage(ev('ROUND_START', undefined, 'Round started'))).toBe('Round started');
  });
});
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd frontend && npm test -- BottomTimeline.test.ts`
Expected: FAIL -- `formatEventMessage` not exported.

- [ ] **Step 3: Add formatEventMessage + use it in the timeline**

In `frontend/src/components/layout/BottomTimeline.tsx`, add the exported helper near the top (after the `EVENT_TO_FILTER` map):

```ts
export function formatEventMessage(event: MagiEvent): string {
  const d = (event.data ?? {}) as Record<string, unknown>;
  switch (event.type) {
    case 'TOOL_CALL':
      if (d.tool_name) return `called ${d.tool_name}`;
      break;
    case 'EVIDENCE_CREATED':
      if (d.evidence_id) return `evidence ${d.evidence_id}`;
      break;
    case 'VOTE_SUBMITTED':
      if (d.stance) return `voted ${d.stance}${d.confidence != null ? ` (${d.confidence}%)` : ''}`;
      break;
    case 'CONSENSUS_CHANGED':
      if (d.outcome) return `consensus: ${d.outcome}`;
      break;
    default:
      break;
  }
  return event.message;
}
```

Ensure `MagiEvent` is imported (add `import type { EventType, EventFilter, MagiEvent } from '@/types/event';` -- merge with the existing `import type { EventType, EventFilter }` line).

In the render, replace `{event.message}` with `{formatEventMessage(event)}` (inside the `filteredEvents.map` block).

- [ ] **Step 4: Run test to verify pass + full suite + typecheck**

Run: `cd frontend && npm test && npx tsc -b --noEmit`
Expected: all tests PASS; tsc clean.

- [ ] **Step 5: Commit**

```bash
git add frontend/src/components/layout/BottomTimeline.tsx frontend/src/components/layout/__tests__/BottomTimeline.test.ts
git commit -m "feat(frontend): timeline renders event payload detail via formatEventMessage"
```

---

## Self-Review Notes

- **Spec coverage:** Section 1 (tool-call persistence) -> Task 1 (entity/model/repo) + Task 2 (orchestrator persist). Section 2 (`/agents` enrichment) -> Task 3. Section 3 (frontend mapping + timeline) -> Task 4 + Task 5. Thought field out of scope (confirmed). All spec sections covered.
- **Placeholder scan:** No TBD/TODO; every code step has full code; test stubs (`memClaimRepo`, `memAgentRunRepo`) are described with their method bodies inline. The `memClaimRepo`/`memAgentRunRepo` in Task 3 Step 1 reference the `memEvidenceRepo` pattern -- the implementer mirrors that pattern (Create appends, ListByCase filters by CaseID). Made explicit.
- **Type consistency:** `entity.ToolCall` fields (Task 1) match `ToolCallModel` fields and `FromToolCall` (Task 3) and the orchestrator persist (Task 2). `ApiAgentSnapshot` (Task 4) matches backend `AgentSnapshotDTO` (Task 3): `tool_calls`/`evidence`/`claims`/`step`. `formatEventMessage(event: MagiEvent)` signature matches the test + the timeline call site. `prefix` variable hoisted in Task 2 Step 5 (called out).
- **Known friction:** Task 2 Step 5 requires hoisting the `prefix` declaration out of the `if r.Ledger != nil` block so the tool-call block (which runs regardless of ledger) can use it. Called out explicitly in the step.
- **Risk:** `step = len(tool_calls)` is a proxy (no per-step trace persistence); acceptable per spec. Under stub executor, `evidence[]` may be empty but tool_calls + claims render.
