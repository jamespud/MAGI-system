# Frontend-Backend Integration Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Wire the MAGI frontend from 100% mock data to real HTTP API calls against the backend MySQL-persisted decision case endpoints, covering list, create, and detail.

**Architecture:** Tracer-bullet approach — four sequential phases (S0–S3), each delivering one independently testable API connection. Backend changes add MySQL config + GORM wiring + DTO expansion. Frontend changes add an API client layer, async store actions, and remove direct mock imports from components.

**Tech Stack:** Go 1.24, Hertz, Uber Fx, GORM + MySQL driver, React 18, TypeScript, Zustand, fetch API, Vite proxy

## Global Constraints

- Scope: Level A — minimal end-to-end wire-up (list + create + detail of decision cases)
- Database: MySQL at `127.0.0.1:3307`, user `magi`, password `magi123`, database `magi`
- DTO JSON keys: `background` (maps entity `Context`), `constraints[].label` (maps entity `Constraint.Key`)
- Frontend MSW: keep as dev fallback, `onUnhandledRequest: 'bypass'`
- Field naming: backend snake_case JSON, frontend camelCase TypeScript

---

## Phase S0: Backend Infrastructure

### Task S0-1: Add database config to YAML and Config struct

**Files:**
- Modify: `backend/conf/magi.yaml` (append)
- Modify: `backend/bootstrap/config.go` (append to Config struct)

**Interfaces:**
- Produces: `cfg.Database.Driver` (string), `cfg.Database.DSN` (string) — consumed by S0-2

- [ ] **Step 1: Add database block to magi.yaml**

Append two blank lines then the database block to `backend/conf/magi.yaml`:

```yaml

database:
  driver: mysql
  dsn: "magi:magi123@tcp(127.0.0.1:3307)/magi?charset=utf8mb4&parseTime=True"
```

- [ ] **Step 2: Add Database struct to Config**

In `backend/bootstrap/config.go`, add a `Database` field inside the `Config` struct after the `Magi` field (after the closing `` `yaml:"magi"` `` of the Magi block):

```go
	Database struct {
		Driver string `yaml:"driver"`
		DSN    string `yaml:"dsn"`
	} `yaml:"database"`
```

- [ ] **Step 3: Verify config loads**

```bash
cd backend && go build ./...
```

Expected: no compilation errors

---

### Task S0-2: Add GORM MySQL provider and Repository injection

**Files:**
- Modify: `backend/bootstrap/container.go`

**Interfaces:**
- Consumes: `cfg.Database.DSN`, `cfg.Database.Driver` from S0-1
- Produces: `*gorm.DB` (via `provideDB`), `port.Repository` (via `provideRepository`)

- [ ] **Step 1: Add imports to container.go**

In `backend/bootstrap/container.go`, add to the import block:

```go
	"fmt"

	"gorm.io/driver/mysql"
	"gorm.io/gorm"
```

Add `"fmt"` alongside the other stdlib imports, and the gorm imports after the existing third-party imports.

- [ ] **Step 2: Add provideDB and provideRepository functions**

Add after the existing `StubToolExecutor` type and its method (after line 184):

```go
func provideDB(cfg *Config) (*gorm.DB, error) {
	db, err := gorm.Open(mysql.Open(cfg.Database.DSN), &gorm.Config{})
	if err != nil {
		return nil, fmt.Errorf("failed to connect database: %w", err)
	}
	if err := db.AutoMigrate(&magi.CaseModel{}); err != nil {
		return nil, fmt.Errorf("failed to migrate: %w", err)
	}
	return db, nil
}

func provideRepository(db *gorm.DB) port.Repository {
	return magi.NewRepository(db)
}
```

- [ ] **Step 3: Register providers in fx.Provide**

Inside the `fx.Provide(` block, add two providers before the `// Agent runtime` comment:

```go
			// Database
			provideDB,
			provideRepository,
```

- [ ] **Step 4: Inject repository into DecisionService**

Change `provideDecisionService` (currently takes `orch`, `cfg`) to:

```go
func provideDecisionService(
	orch *orchestration.Orchestrator,
	repo port.Repository,
	cfg *Config,
) *decision.Service {
	return decision.NewService(orch, decision.ServiceConfig{
		MaxDebateRounds: cfg.Magi.MaxDebateRounds,
	}, decision.WithCaseRepo(repo.CaseRepo()))
}
```

- [ ] **Step 5: Verify compilation**

```bash
cd backend && go build ./...
```

Expected: no compilation errors

- [ ] **Step 6: Commit**

```bash
git add backend/conf/magi.yaml backend/bootstrap/config.go backend/bootstrap/container.go
git commit -m "feat(bootstrap): add MySQL config, GORM provider, and repository injection"
```

---

### Task S0-3: Expand DTO CaseResponse and CreateCaseRequest

**Files:**
- Modify: `backend/server/dto/dto.go`

**Interfaces:**
- Produces: `CaseResponse` (11 fields), `ConstraintDTO`, `ConsensusDTO`, expanded `CreateCaseRequest`

- [ ] **Step 1: Replace CaseResponse and add new DTO types**

Replace the existing `CaseResponse` struct with:

```go
type CaseResponse struct {
	ID          string          `json:"id"`
	Question    string          `json:"question"`
	Background  string          `json:"background"`
	Constraints []ConstraintDTO `json:"constraints"`
	Status      string          `json:"status"`
	Consensus   *ConsensusDTO   `json:"consensus,omitempty"`
	Confidence  float64         `json:"confidence"`
	Round       int             `json:"round"`
	CreatedAt   string          `json:"created_at"`
	UpdatedAt   string          `json:"updated_at"`
}

type ConstraintDTO struct {
	Label string `json:"label"`
	Value string `json:"value"`
}

type ConsensusDTO struct {
	Approve        int    `json:"approve"`
	Reject         int    `json:"reject"`
	Abstain        int    `json:"abstain"`
	Majority       string `json:"majority"`
	NeedReflection bool   `json:"need_reflection"`
}
```

- [ ] **Step 2: Replace CreateCaseRequest**

Replace the existing `CreateCaseRequest` with:

```go
type CreateCaseRequest struct {
	Question    string          `json:"question"`
	Background  string          `json:"background,omitempty"`
	Constraints []ConstraintDTO `json:"constraints,omitempty"`
}
```

- [ ] **Step 3: Update FromCase function**

Replace the existing `FromCase` function with:

```go
func FromCase(c *entity.DecisionCase) CaseResponse {
	constraints := make([]ConstraintDTO, len(c.Constraints))
	for i, ct := range c.Constraints {
		constraints[i] = ConstraintDTO{Label: ct.Key, Value: ct.Value}
	}
	return CaseResponse{
		ID:          c.ID,
		Question:    c.Question,
		Background:  c.Context,
		Constraints: constraints,
		Status:      string(c.Status),
		Round:       0,
		CreatedAt:   c.CreatedAt.Format(time.RFC3339),
		UpdatedAt:   c.UpdatedAt.Format(time.RFC3339),
	}
}
```

Add `"time"` to the import block.

- [ ] **Step 4: Verify compilation**

```bash
cd backend && go build ./...
```

Expected: no compilation errors

- [ ] **Step 5: Commit**

```bash
git add backend/server/dto/dto.go
git commit -m "feat(dto): expand CaseResponse with background, constraints, consensus, timestamps"
```

---

### Task S0-4: Update handler and service for new request fields

**Files:**
- Modify: `backend/server/handler/decision.go`
- Modify: `backend/application/decision/service.go`

**Interfaces:**
- Consumes: `CreateCaseRequest.Background`, `CreateCaseRequest.Constraints` from S0-3
- Produces: Updated `Service.Create` that accepts background and constraints

- [ ] **Step 1: Update handler Create method**

In `backend/server/handler/decision.go`, replace the `Create` method with:

```go
func (h *DecisionHandler) Create(ctx context.Context, c *app.RequestContext) {
	var req dto.CreateCaseRequest
	if err := c.BindAndValidate(&req); err != nil {
		c.JSON(consts.StatusBadRequest, dto.ErrorResponse{Error: "invalid request: " + err.Error()})
		return
	}
	constraints := make([]entity.Constraint, len(req.Constraints))
	for i, ct := range req.Constraints {
		constraints[i] = entity.Constraint{Key: ct.Label, Value: ct.Value}
	}
	case_, err := h.svc.Create(ctx, req.Question, req.Background, constraints)
	if err != nil {
		c.JSON(consts.StatusInternalServerError, dto.ErrorResponse{Error: err.Error()})
		return
	}
	c.JSON(consts.StatusCreated, dto.FromCase(case_))
}
```

Add `"github.com/jamespud/magi/backend/domain/entity"` to the import block.

- [ ] **Step 2: Update service Create method**

In `backend/application/decision/service.go`, replace the `Create` method with:

```go
// Create creates a new DecisionCase from a question, optional background, and constraints.
func (s *Service) Create(ctx context.Context, question, background string, constraints []entity.Constraint) (*entity.DecisionCase, error) {
	case_ := &entity.DecisionCase{
		ID:              fmt.Sprintf("case-%d", time.Now().Unix()),
		Question:        question,
		Context:         background,
		Constraints:     constraints,
		MaxDebateRounds: s.cfg.MaxDebateRounds,
		Status:          entity.CaseStatusDraft,
		CreatedAt:       time.Now(),
	}
	if s.caseRepo != nil {
		if err := s.caseRepo.Create(ctx, case_); err != nil {
			return nil, fmt.Errorf("failed to persist case: %w", err)
		}
	}
	return case_, nil
}
```

- [ ] **Step 3: Verify compilation**

```bash
cd backend && go build ./...
```

Expected: no compilation errors

- [ ] **Step 4: Smoke-test with curl**

Start MySQL then server, then:

```bash
curl -s -X POST http://localhost:8080/api/v1/cases \
  -H 'Content-Type: application/json' \
  -d '{"question":"Should we adopt Rust?","background":"Java backend team"}'
```

Expected: JSON with `id`, `question`, `background`, `status: "DRAFT"`, `created_at`

```bash
curl -s http://localhost:8080/api/v1/cases
```

Expected: JSON array containing the case

- [ ] **Step 5: Commit**

```bash
git add backend/server/handler/decision.go backend/application/decision/service.go
git commit -m "feat(decision): accept background and constraints in Create, persist to repo"
```

---

## Phase S1: GET /api/v1/cases — Case List

### Task S1-1: Create frontend API client

**Files:**
- Create: `frontend/src/api/client.ts`

**Interfaces:**
- Produces: `api.getCases()`, `api.getCase(id)`, `api.createCase(question, background)` — consumed by S1-2, S2-1

- [ ] **Step 1: Create the API client file**

Create `frontend/src/api/client.ts`:

```typescript
// API response types (snake_case as returned by backend)
interface ApiCaseResponse {
  id: string;
  question: string;
  background: string;
  constraints: { label: string; value: string }[];
  status: string;
  consensus: {
    approve: number;
    reject: number;
    abstain: number;
    majority: string;
    need_reflection: boolean;
  } | null;
  confidence: number;
  round: number;
  created_at: string;
  updated_at: string;
}

const BASE_URL = '/api/v1';

async function request<T>(path: string, options?: RequestInit): Promise<T> {
  const res = await fetch(`${BASE_URL}${path}`, {
    headers: { 'Content-Type': 'application/json' },
    ...options,
  });
  if (!res.ok) {
    const err = await res.json().catch(() => ({ error: res.statusText }));
    throw new Error(err.error || `HTTP ${res.status}`);
  }
  return res.json();
}

export const api = {
  getCases: () => request<ApiCaseResponse[]>('/cases'),

  getCase: (id: string) => request<ApiCaseResponse>(`/cases/${id}`),

  createCase: (question: string, background?: string) =>
    request<ApiCaseResponse>('/cases', {
      method: 'POST',
      body: JSON.stringify({ question, background }),
    }),
};

export type { ApiCaseResponse };
```

- [ ] **Step 2: Verify TypeScript compilation**

```bash
cd frontend && npx tsc --noEmit
```

Expected: no type errors

- [ ] **Step 3: Commit**

```bash
git add frontend/src/api/client.ts
git commit -m "feat(frontend): add API client for cases endpoints"
```

---

### Task S1-2: Add fetchCases action to caseStore

**Files:**
- Modify: `frontend/src/stores/caseStore.ts`

**Interfaces:**
- Consumes: `api.getCases()` from S1-1
- Produces: `useCaseStore.getState().fetchCases()` — consumed by S1-3, S3

- [ ] **Step 1: Update imports**

In `frontend/src/stores/caseStore.ts`, the existing import:
```typescript
import type { Case, CaseSummary } from '@/types/case';
```

Change to:
```typescript
import type { Case, CaseStatus, CaseSummary } from '@/types/case';
```

Add a new import line after existing imports:
```typescript
import { api, type ApiCaseResponse } from '@/api/client';
```

- [ ] **Step 2: Add mapToSummary helper**

Add before the `CaseState` interface:

```typescript
function mapToSummary(c: ApiCaseResponse): CaseSummary {
  return {
    id: c.id,
    question: c.question,
    status: c.status as CaseStatus,
    round: c.round,
    createdAt: c.created_at,
    pinned: false,
  };
}
```

- [ ] **Step 3: Add fetchCases to interface and store**

Add to the `CaseState` interface:

```typescript
  fetchCases: () => Promise<void>;
```

Add to the store object:

```typescript
  fetchCases: async () => {
    set({ loading: true, error: null });
    try {
      const list = await api.getCases();
      set({ cases: list.map(mapToSummary), loading: false });
    } catch (e) {
      set({ error: (e as Error).message, loading: false });
    }
  },
```

- [ ] **Step 4: Verify TypeScript compilation**

```bash
cd frontend && npx tsc --noEmit
```

Expected: no type errors

- [ ] **Step 5: Commit**

```bash
git add frontend/src/stores/caseStore.ts
git commit -m "feat(frontend): add fetchCases async action to caseStore"
```

---

### Task S1-3: Wire LeftNav to auto-fetch cases

**Files:**
- Modify: `frontend/src/components/layout/LeftNav.tsx`

**Interfaces:**
- Consumes: `useCaseStore.getState().fetchCases()` from S1-2

- [ ] **Step 1: Add imports**

In `frontend/src/components/layout/LeftNav.tsx`, add:

```typescript
import { useEffect } from 'react';
import { useCaseStore } from '@/stores';
```

The first line should change from `import { NavLink } from 'react-router-dom';` to include both imports.

- [ ] **Step 2: Add useEffect to component body**

After `export default function LeftNav({ cases = [] }: LeftNavProps) {`, add:

```typescript
  useEffect(() => {
    useCaseStore.getState().fetchCases();
  }, []);
```

- [ ] **Step 3: Verify TypeScript compilation**

```bash
cd frontend && npx tsc --noEmit
```

Expected: no type errors

- [ ] **Step 4: Commit**

```bash
git add frontend/src/components/layout/LeftNav.tsx
git commit -m "feat(frontend): auto-fetch cases in LeftNav on mount"
```

---

## Phase S2: POST + GET /api/v1/cases/:id — Create & Detail

### Task S2-1: Add fetchCase and createCase to caseStore

**Files:**
- Modify: `frontend/src/stores/caseStore.ts`

**Interfaces:**
- Consumes: `api.getCase()`, `api.createCase()` from S1-1
- Produces: `useCaseStore.getState().fetchCase(id)`, `useCaseStore.getState().createCase(q, bg, constraints)` — consumed by S2-2

- [ ] **Step 1: Add mapToCase helper**

Add after `mapToSummary` in `frontend/src/stores/caseStore.ts`:

```typescript
function mapToCase(c: ApiCaseResponse): Case {
  return {
    id: c.id,
    question: c.question,
    background: c.background,
    constraints: (c.constraints || []).map(ct => ({ label: ct.label, value: ct.value })),
    status: c.status as CaseStatus,
    round: c.round,
    consensus: c.consensus ? {
      approve: c.consensus.approve,
      reject: c.consensus.reject,
      abstain: c.consensus.abstain,
      majority: c.consensus.majority as 'Approve' | 'Reject' | 'Tie',
      needReflection: c.consensus.need_reflection,
    } : null,
    confidence: c.confidence,
    createdAt: c.created_at,
    updatedAt: c.updated_at,
  };
}
```

`Case` is already imported from S1-2 (`Case, CaseStatus, CaseSummary`). No additional import changes needed.

- [ ] **Step 2: Add fetchCase and createCase to interface and store**

Add to `CaseState` interface:

```typescript
  fetchCase: (id: string) => Promise<void>;
  createCase: (question: string, background?: string) => Promise<ApiCaseResponse>;
```

Add to store object:

```typescript
  fetchCase: async (id: string) => {
    set({ loading: true, error: null });
    try {
      const c = await api.getCase(id);
      set({ case: mapToCase(c), loading: false });
    } catch (e) {
      set({ error: (e as Error).message, loading: false });
    }
  },

  createCase: async (question: string, background?: string) => {
    set({ loading: true, error: null });
    try {
      const c = await api.createCase(question, background);
      set({ loading: false });
      return c;
    } catch (e) {
      set({ error: (e as Error).message, loading: false });
      throw e;
    }
  },
```

- [ ] **Step 3: Verify TypeScript compilation**

```bash
cd frontend && npx tsc --noEmit
```

Expected: no type errors

- [ ] **Step 4: Commit**

```bash
git add frontend/src/stores/caseStore.ts
git commit -m "feat(frontend): add fetchCase and createCase async actions"
```

---

### Task S2-2: Rewire DecisionWorkspace to use real API

**Files:**
- Modify: `frontend/src/pages/DecisionWorkspace.tsx`

**Interfaces:**
- Consumes: store `fetchCase`, `createCase`, `fetchCases` from S2-1
- Consumes: `useParams`, `useNavigate` from react-router-dom

- [ ] **Step 1: Replace DecisionWorkspace.tsx**

Replace the entire file content with:

```typescript
import { useEffect, useRef } from 'react';
import { useParams, useNavigate } from 'react-router-dom';
import { useCaseStore, useAgentStore, useEventStore } from '@/stores';
import { createMockAgents, createMockEvents } from '@/mock/data';
import CaseHeader from '@/components/workspace/CaseHeader';
import AgentTrio from '@/components/workspace/AgentTrio';
import ConsensusPanel from '@/components/workspace/ConsensusPanel';
import DecisionInput from '@/components/workspace/DecisionInput';
import { EvidenceGraph } from '@/components/evidence';
import { Button } from '@/components/ui';

export default function DecisionWorkspace() {
  const { caseId } = useParams<{ caseId: string }>();
  const navigate = useNavigate();
  const currentCase = useCaseStore((s) => s.case);
  const loading = useCaseStore((s) => s.loading);
  const error = useCaseStore((s) => s.error);
  const initAgents = useRef(false);

  // Load mock agents/events once
  useEffect(() => {
    if (initAgents.current) return;
    initAgents.current = true;
    useAgentStore.getState().loadAgents(createMockAgents());
    useEventStore.getState().loadEvents(createMockEvents());
  }, []);

  // Fetch case or reset when route changes
  useEffect(() => {
    if (caseId) {
      useCaseStore.getState().fetchCase(caseId);
    } else {
      useCaseStore.setState({ case: null });
    }
  }, [caseId]);

  const handleCreate = async (question: string) => {
    try {
      const c = await useCaseStore.getState().createCase(question);
      navigate(`/case/${c.id}`);
    } catch {
      // error already set in store
    }
  };

  if (!caseId) {
    return (
      <div className="flex h-full items-center justify-center p-8">
        <div className="w-full max-w-lg">
          <h2 className="font-mono text-lg font-semibold text-text-primary mb-4">New Decision</h2>
          <form
            onSubmit={(e) => {
              e.preventDefault();
              const form = e.currentTarget;
              const input = form.elements.namedItem('question') as HTMLInputElement;
              if (input.value.trim()) handleCreate(input.value.trim());
            }}
          >
            <input
              name="question"
              className="w-full bg-raised border border-border-dim rounded px-3 py-2 text-sm text-text-primary font-mono placeholder:text-text-muted focus:outline-none focus:border-accent mb-3"
              placeholder="What decision should MAGI analyze?"
              autoFocus
            />
            <div className="flex gap-2">
              <Button type="submit" disabled={loading}>
                {loading ? 'Creating...' : 'Create Case'}
              </Button>
            </div>
          </form>
          {error && <p className="text-red-400 text-xs mt-2 font-mono">{error}</p>}
        </div>
      </div>
    );
  }

  if (loading || !currentCase) {
    return (
      <div className="flex h-full items-center justify-center p-8">
        <span className="font-mono text-text-muted">{loading ? 'Loading...' : 'Case not found'}</span>
      </div>
    );
  }

  return (
    <div className="h-full overflow-y-auto">
      <DecisionInput />
      <CaseHeader />
      <AgentTrio />
      <ConsensusPanel />
      <EvidenceGraph />
    </div>
  );
}
```

- [ ] **Step 2: Verify TypeScript compilation**

```bash
cd frontend && npx tsc --noEmit
```

Expected: no type errors

- [ ] **Step 3: Commit**

```bash
git add frontend/src/pages/DecisionWorkspace.tsx
git commit -m "feat(frontend): rewire DecisionWorkspace to use real case API, keep agents/events mock"
```

---

## Phase S3: Final Verification

### Task S3-1: Full build and smoke test

**Files:** (no code changes — verification only)

- [ ] **Step 1: Build backend**

```bash
cd backend && go build ./...
```

Expected: no errors

- [ ] **Step 2: Build frontend**

```bash
cd frontend && npx tsc --noEmit && npm run build
```

Expected: no type errors, build succeeds

- [ ] **Step 3: End-to-end API test**

With MySQL running (`make middleware`) and server running (`make server`):

```bash
# Create a case
curl -s -X POST http://localhost:8080/api/v1/cases \
  -H 'Content-Type: application/json' \
  -d '{"question":"Should we adopt Rust?","background":"Java backend team of 5"}'

# List cases (should include the new one)
curl -s http://localhost:8080/api/v1/cases | python3 -m json.tool

# Get single case (replace ID from output above)
curl -s http://localhost:8080/api/v1/cases/<case-id> | python3 -m json.tool
```

All should return valid JSON. Create returns status `DRAFT`. List includes the created case. Get returns the case with all fields.

- [ ] **Step 4: Start frontend and verify in browser**

```bash
cd frontend && npm run dev
```

Open `http://localhost:5173` — verify:
- LeftNav shows the case created via curl
- Click the case → navigates to `/case/<id>` → shows detail (question, status, background)
- Navigate to `/` → shows create form → submit → creates case → redirects to detail

- [ ] **Step 5: Commit**

```bash
git status
git add -A
git commit -m "chore: final verification of frontend-backend integration"
```

---
