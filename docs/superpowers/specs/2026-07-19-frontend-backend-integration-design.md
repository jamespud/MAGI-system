# Frontend-Backend Integration Design (Tracer-bullet)

- **Date**: 2026-07-19
- **Scope**: Level A — minimal end-to-end wire-up
- **Approach**: Tracer-bullet — one API endpoint at a time, verified before proceeding
- **Database**: MySQL (existing docker-compose), GORM + existing `adapter/repository.go`

## Architecture

```
Browser → React → Zustand Store (async actions) → API Client (fetch)
    → Vite proxy (:5173 → :8080)
    → Hertz Handler → Application Service → Domain Orchestrator
    → GORM Repository → MySQL (:3307)
```

## Steps Overview

| Step | Scope | Verification |
|------|-------|-------------|
| S0 | Backend infrastructure: config + GORM MySQL + repo injection + DTO expansion + migration | `GET /health` ok, `case_models` table exists |
| S1 | `GET /api/v1/cases` — list endpoint end-to-end | LeftNav sidebar shows cases from DB |
| S2 | `POST /api/v1/cases` + `GET /api/v1/cases/:id` — create + detail | Submit question → case created → detail page rendered |
| S3 | Cleanup: remove direct mock imports from DecisionWorkspace, keep MSW as dev fallback | App works with MSW disabled |

S0 is a hard prerequisite for all subsequent steps.

## Files Involved

### Backend (8 files)
- `conf/magi.yaml` — add `database` block
- `backend/bootstrap/config.go` — add `Database` struct to `Config`
- `backend/bootstrap/container.go` — add `provideDB`, `provideRepository`; inject repo into services
- `backend/adapter/repository.go` — already implemented, reference only
- `backend/server/dto/dto.go` — extend `CaseResponse` and `CreateCaseRequest`
- `backend/server/handler/decision.go` — minor updates for new DTO fields
- `backend/application/decision/service.go` — accept new fields in `Create`
- `backend/go.mod` — promote `gorm.io/driver/mysql` to direct dependency

### Frontend (6 files)
- `src/api/client.ts` — new file: fetch-based API client
- `src/stores/caseStore.ts` — add `fetchCases`, `fetchCase`, `createCase` async actions
- `src/pages/DecisionWorkspace.tsx` — remove mock imports, use store actions
- `src/components/layout/LeftNav.tsx` — self-fetch cases on mount
- `src/components/workspace/CaseHeader.tsx` — read from store (already does, minor field mapping)
- `src/components/workspace/DecisionInput.tsx` — wire submit to `createCase` action

---

## S0 — Backend Infrastructure

### Config

**`conf/magi.yaml`** — new top-level block:
```yaml
database:
  driver: mysql
  dsn: "magi:magi123@tcp(127.0.0.1:3307)/magi?charset=utf8mb4&parseTime=True"
```

**`backend/bootstrap/config.go`** — new struct field in `Config`:
```go
Database struct {
    Driver string `yaml:"driver"`
    DSN    string `yaml:"dsn"`
} `yaml:"database"`
```

### Bootstrap

**`backend/bootstrap/container.go`** — three changes:

1. New provider `provideDB(cfg *Config) (*gorm.DB, error)`:
   - Opens MySQL connection via `gorm.Open(mysql.Open(cfg.Database.DSN))`
   - Runs `AutoMigrate(&adapter.CaseModel{})`
   - Returns `*gorm.DB`

2. New provider `provideRepository(db *gorm.DB) port.Repository`:
   - Returns `adapter.NewRepository(db)`

3. Modify `provideDecisionService` to accept `port.Repository` and inject via `decision.WithCaseRepo(repo.CaseRepo())`

Also add `import "gorm.io/driver/mysql"` and `import "gorm.io/gorm"`.

### DTO Expansion

**`backend/server/dto/dto.go`** — `CaseResponse` extended from 5 fields to 11:
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

`FromCase()` updated to map all fields from `entity.DecisionCase`.

### Verification
- `make middleware` starts MySQL
- `make server` starts without errors
- `GET /health` returns 200
- MySQL `magi.case_models` table exists

---

## S1 — GET /api/v1/cases

Backend requires no additional changes after S0.

### Frontend

**`src/api/client.ts`** (new file):
```typescript
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
  getCases: () => request<CaseResponse[]>('/cases'),
  getCase: (id: string) => request<CaseResponse>(`/cases/${id}`),
  createCase: (question: string, background?: string) =>
    request<CaseResponse>('/cases', {
      method: 'POST',
      body: JSON.stringify({ question, background }),
    }),
};
```

**`src/stores/caseStore.ts`** — add `fetchCases`:
```typescript
fetchCases: async () => {
  set({ loading: true, error: null });
  try {
    const list = await api.getCases();
    const summaries: CaseSummary[] = list.map(c => ({
      id: c.id, question: c.question,
      status: c.status as CaseStatus,
      round: c.round, createdAt: c.created_at, pinned: false,
    }));
    set({ cases: summaries, loading: false });
  } catch (e) {
    set({ error: (e as Error).message, loading: false });
  }
},
```

**`src/components/layout/LeftNav.tsx`** — fetch on mount:
```typescript
useEffect(() => {
  useCaseStore.getState().fetchCases();
}, []);
```

### Verification
- Frontend LeftNav shows empty state when DB is empty
- After creating a case (S2), LeftNav shows the case in sidebar

---

## S2 — POST + GET /api/v1/cases/:id

### Backend

**`dto.CreateCaseRequest`** extended:
```go
type CreateCaseRequest struct {
    Question    string          `json:"question"`
    Background  string          `json:"background,omitempty"`
    Constraints []ConstraintDTO `json:"constraints,omitempty"`
}
```

**`decision.Service.Create`** — accept and persist `background` and `constraints` on the `DecisionCase`.

**`handler/decision.go`** `Create` method — pass `req.Background` and map DTO constraints.

### Frontend

**`caseStore`** — add `fetchCase` and `createCase`:
```typescript
fetchCase: async (id: string) => {
  set({ loading: true });
  const c = await api.getCase(id);
  set({ case: mapToCase(c), loading: false });
},
createCase: async (question: string) => {
  const c = await api.createCase(question);
  return c;
},
```

Where `mapToCase` converts the API `CaseResponse` to the frontend `Case` interface.

**`DecisionWorkspace.tsx`** — remove direct mock imports (`createMockCase`, etc.). Instead:
- Read `caseId` from route params
- If `caseId` → `fetchCase(caseId)` on mount
- If no `caseId` (index route `/`) → show `DecisionInput` for creating new case

**`DecisionInput.tsx`** — wire the submit button to `createCase`, on success navigate to `/case/:newId`.

### Verification
- Navigate to `/` → see DecisionInput form
- Type question → submit → redirect to `/case/:newId`
- Case detail page shows question, status (DRAFT), background, constraints
- Refresh page → data persists (MySQL)
- LeftNav sidebar shows the new case in list

---

## S3 — Cleanup

### Frontend

**`DecisionWorkspace.tsx`**:
- Remove `import { createMockCase, createMockAgents, createMockEvents, createMockCaseList } from '@/mock/data'`
- Remove `loaded` ref and mock-injecting `useEffect`
- Replace with route-param-based fetch logic from S2

**`EvidenceGraph.tsx`** and **`RightInspector.tsx`**: For now, keep their mock data usage. These are part of the detail view that will be connected in a future iteration.

**MSW**: Keep `browser.ts` and handlers as-is. They serve as a dev fallback. The API client routes through Vite proxy to real backend; MSW interceptors are bypassed when no matching handler exists or can be disabled via env var.

### Verification
- Start MSW disabled → app works entirely from real API
- Start MSW enabled → real API still used (since MSW handlers are minimal and `onUnhandledRequest: 'bypass'`)

---

## DTO / Type Mapping

### Backend DTO → Frontend Type

| Backend `CaseResponse` | Frontend `Case` | Notes |
|------------------------|-----------------|-------|
| `id` (string) | `id` (string) | Direct |
| `question` (string) | `question` (string) | Direct |
| `background` (string) | `background` (string) | Direct |
| `constraints` (ConstraintDTO[]) | `constraints` (Constraint[]) | Direct |
| `status` (string) | `status` (CaseStatus) | Cast needed |
| `consensus` (ConsensusDTO?) | `consensus` (ConsensusState?) | Field name mapping |
| `confidence` (float64) | `confidence` (number) | Direct |
| `round` (int) | `round` (number) | Direct |
| `created_at` (string) | `createdAt` (string) | snake_case → camelCase |
| `updated_at` (string) | `updatedAt` (string) | snake_case → camelCase |
| — | `pinned` (boolean) | Frontend-only, default false |

---

## Out of Scope (Future Iterations)

- SSE streaming (`/api/v1/cases/:id/stream`) — agent execution real-time updates
- Agent state, evidence, claims, votes — detail sub-resources
- Memory, Replay, Evaluation pages — currently PlaceholderPage
- Tool listing — currently stub
- Case run/cancel/report endpoints — backend implemented but frontend not connected
