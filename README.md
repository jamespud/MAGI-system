# MAGI

> An evidence-driven multi-agent decision engine, inspired by the three Magi in *Neon Genesis Evangelion*.

MAGI takes a hard decision question and gives you a reasoned, evidence-backed recommendation. Three independent AI agents - **Melchior** (the scientist), **Balthasar** (the protector), and **Casper** (the innovator) - each investigate the question under a distinct objective function and evidence standard, then vote, debate, reflect, and re-vote toward a consensus resolution. A deterministic state machine orchestrates the whole process; LLMs handle the language, Go code enforces the rules.

```
            ┌────────────┐   ┌────────────┐   ┌────────────┐
            │  Melchior  │   │ Balthasar  │   │   Casper   │
            │ scientist  │   │ protector  │   │ innovator  │
            └─────┬──────┘   └─────┬──────┘   └─────┬──────┘
                  │ investigate (tools -> evidence -> claims) │
                  ▼                ▼                          ▼
            ┌─────────────────────────────────────────────────────┐
            │  vote -> 2:1? debate -> reflect -> re-vote -> consensus │
            └──────────────────────────┬──────────────────────────┘
                                       ▼
                          final report + confidence + round
```

## Why MAGI

Most "multi-agent" demos let the LLM do everything - including counting its own votes and deciding when it's done. MAGI refuses that. The governing principle is **LLM = semantics, code = rules**:

- LLMs understand the question, propose claims, explain evidence, write reflections, draft reports.
- Deterministic Go does permission checks, schema validation, evidence-authenticity checks, the evidence gate, utility-dimension legality, vote counting, consensus judgment, state transitions, loop limits, and timeouts.

The Commander (an LLM) is deliberately *not* a "god agent" - it cannot count votes, judge consensus, bypass the evidence gate, or drive state transitions.

## Features

- **Three adversarial agents with different risk tendencies** - Melchior (neutral, needs quantitative evidence), Balthasar (conservative, needs a worst-case claim), Casper (aggressive, needs an opportunity-cost claim). Each enforces its own `EvidenceStandard`.
- **Executable role contracts** - each Magi must emit role-specific structured analysis: Melchior's technical feasibility, Balthasar's residual-risk/rollback assessment, or Casper's opportunity/timing assessment. Go gates these fields and blocks approvals outside each role's decision boundary.
- **Hand-written agent loop** (not Eino ReAct) - gather -> evidence summary -> **evidence gate** (deterministic) -> vote. Termination is code-enforced: valid vote, max steps, timeout, token budget, repeated validation/gate failures.
- **Evidence Ledger as the spine** - `tool result -> EvidenceRecord -> Claim -> Vote`. Reliability is a deterministic modifier formula (base + directness + recency + corroboration + extraction), never a single LLM-emitted number.
- **Real tool integration** - Tavily `web_search` returns live web evidence; one search result -> one evidence record.
- **Deterministic FSM orchestration** - ~20 case states (`DRAFT -> NORMALIZING -> … -> INVESTIGATING -> EVIDENCE_GATING -> COLLECTING_VOTES -> CONSENSUS_CHECK -> DEBATING -> REFLECTING -> REVOTING -> GENERATING_REPORT -> RESOLVED`). First-round 2:1 goes to debate; only post-revote 2:1 may resolve.
- **Claim graph** - claims track supports/contradicts, so debate targets specific conflicting claims, not free-form prose.
- **Live UI** - React workspace with per-agent panels, consensus panel, timeline, and an interactive Evidence Graph (zoom/pan, radial vote->claim->evidence hierarchy) fed by real API data over SSE.
- **Events, memory, evaluation, replay** - every transition publishes a `MagiEvent`; resolved cases are projected for future RAG retrieval; a counterfactual stability score is computed.

## Quick Start

### Prerequisites

- Go 1.24+
- Node.js 18+ (npm)
- Docker (for MySQL)
- An OpenAI-compatible model API key (DeepSeek, OpenAI, Doubao/Ark, …)

### 1. Configure the model

```bash
cp backend/conf/magi.yaml.example backend/conf/magi.yaml
```

Edit `backend/conf/magi.yaml` and set your model credentials:

```yaml
model:
  api_key: "sk-your-api-key"
  base_url: "https://api.deepseek.com"     # any OpenAI-compatible endpoint
  model_name: "deepseek-v4-flash"
```

(Optional) add a Tavily key to enable real web search:

```yaml
tavily:
  api_key: "tvly-dev-..."
```

### 2. Start MySQL + backend + frontend

```bash
make prepare        # one-time: check deps, install Go + npm dependencies
make debug          # start MySQL (docker) + Go server + Vite dev server, in parallel
```

Open the frontend at **http://localhost:5173**. The Vite dev server proxies `/api` to the Go server on `:8080`.

### 3. Run a decision

In the UI, type a question (e.g. *"Should we migrate the backend from Java to Rust?"*) and click **Run Decision**. Watch the three agents investigate, hit the evidence gate, vote, debate if split, and converge - then read the final report.

Or drive it via the API:

```bash
# Create a case
curl -s -X POST localhost:8080/api/v1/cases \
  -H 'Content-Type: application/json' \
  -d '{"question":"Should we adopt a monorepo?"}' | jq .

# Run it (async - returns 202, streams progress over SSE)
curl -s -X POST localhost:8080/api/v1/cases/<CASE_ID>/run

# Poll / stream until RESOLVED, then fetch artifacts
curl -s localhost:8080/api/v1/cases/<CASE_ID>/report    | jq .   # final report
curl -s localhost:8080/api/v1/cases/<CASE_ID>/agents    | jq .   # per-agent runs
curl -s localhost:8080/api/v1/cases/<CASE_ID>/evidence  | jq .
curl -s localhost:8080/api/v1/cases/<CASE_ID>/votes     | jq .
```

## Configuration

| File | Purpose |
|------|---------|
| `backend/conf/magi.yaml` | Main config (model, agents, evidence standards, loop policy). Copy from `magi.yaml.example`. |
| `backend/conf/magi.local.yaml` | Local overrides (gitignored). Merged on top of `magi.yaml`. |
| `MAGI_CONFIG` env var | Override the config path (default `conf/magi.yaml`). |

The `magi:` block defines each agent's `persona`/`persona_def`, utility `dimensions` (with weights), `risk_tendency`, executable `role_policy`, `evidence` standard (`min_evidence_count`, `required_types`, `custom_rules`), and global `max_debate_rounds` / `max_steps` / `timeout_seconds` / `max_tool_calls`.

## Architecture

Four layers, strictly one-way dependencies:

```
Application  ->  Orchestration  ->  Agent Runtime  ->  Port/Adapter  ->  Coze Infrastructure
```

- **`domain/`** depends only on `domain/port` interfaces + eino. Coze implementations live in `adapter/`. Reverse dependencies are forbidden.
- **Orchestration** (`domain/orchestration/orchestrator.go`) drives each case through the FSM; every transition is a `switch` case, nothing LLM-decided. The `Dispatcher` fans the three agents out concurrently with `sync.WaitGroup` - isolated working memory each, shared append-only Evidence Ledger. Per-agent errors become `LoopStatusError` results handled by `FailurePolicy`, never panics.
- **Agent Runtime** (`domain/runtime/agent_loop.go`) is MAGI's own loop - do not replace it with Eino ReAct/compose.
- **Validation** (`domain/validation/`) - Go struct -> JSON Schema -> validates every LLM structured output (DecisionTask, EvidenceSummary, ClaimSubmission, Vote, Reflection, FinalReport) via `santhosh-tekuri/jsonschema/v6`.

The frozen target architecture is `magi-design.md` (Chinese); all code targets that design.

> **Build caveat:** `backend/go.mod` has a `replace` directive pointing `coze-dev/coze-studio` at a local path (`/home/spud/proj/coze-studio/backend`). The build **fails** if that path is absent. MAGI reuses Coze Studio's model builder, plugin/tool registry, knowledge store, and sandbox - but only through `adapter/`, never by importing Coze domain types into `domain/`. For production, swap the replace to a published fork.

## Project Structure

```
backend/
  cmd/magi-server/     # fx entry point (HTTP server)
  bootstrap/           # DI wiring, config loading
  server/              # Hertz HTTP handlers, SSE, router, DTOs
  application/         # decision service, async RunManager
  domain/
    orchestration/     # FSM orchestrator + dispatcher
    runtime/           # hand-written agent loop, response parser, prompts
    evidence/          # Evidence Ledger, reliability modifiers, adapters (Tavily)
    entity/            # Case, AgentRun, Evidence, Claim, Vote, Event, …
    validation/        # JSON Schema generation + validation (ADR-003)
    consensus/ debate/ memory/ service/ port/
  adapter/             # Coze model builder, Tavily tool, GORM repos
  conf/                # magi.yaml.example
  docker/atlas/migrations/  # SQL migrations (s6, s7, s8)
frontend/
  src/
    pages/             # DecisionWorkspace
    components/        # workspace (AgentPanel, ConsensusPanel), evidence (EvidenceGraph), layout
    stores/            # Zustand: case, agent, event, ui
    api/               # REST client + SSE stream consumer
    lib/               # stance helper (color/label normalization)
scripts/               # dev.sh, build.sh, env.sh
docs/superpowers/      # specs + implementation plans
```

## API Reference

| Method | Path | Description |
|--------|------|-------------|
| `POST` | `/api/v1/cases` | Create a case (question + optional background) |
| `POST` | `/api/v1/cases/:id/run` | Run asynchronously (202; 409 if already running) |
| `POST` | `/api/v1/cases/:id/cancel` | Cancel a running case |
| `GET`  | `/api/v1/cases` | List cases |
| `GET`  | `/api/v1/cases/:id` | Case detail (status, consensus, confidence, round) |
| `GET`  | `/api/v1/cases/:id/report` | Final report + resolution |
| `GET`  | `/api/v1/cases/:id/agents` | Per-agent snapshot (run status, tool calls, evidence, claims, vote) |
| `GET`  | `/api/v1/cases/:id/evidence` | All evidence records |
| `GET`  | `/api/v1/cases/:id/claims` | All claims (supports/contradicts) |
| `GET`  | `/api/v1/cases/:id/votes` | All votes |
| `GET`  | `/api/v1/cases/:id/events` | Event stream (for replay/audit) |
| `GET`  | `/api/v1/cases/:id/stream` | SSE: live progress + historical catch-up |

## Commands

All run from the repo root.

| Command | Description |
|---------|-------------|
| `make prepare` | Bootstrap: check deps, init `.env.local`, install Go + npm dependencies |
| `make docker-up` | Start MySQL on `127.0.0.1:3307` |
| `make debug` | Start MySQL + Go backend + Vite frontend (parallel) |
| `make backend` | Start Go server only |
| `make frontend` | Start Vite dev server only |
| `make build` | Build `bin/magi` + `frontend/dist/` |
| `make migrate` | Apply Atlas SQL migrations (`docker/atlas/migrations/`) |
| `make test` | Run Go tests with coverage |
| `make lint` / `make fmt` / `make vet` / `make tidy` | Go quality tools |
| `make clean` | Remove build artifacts |

Frontend (from `frontend/`): `npm run dev` · `npm run build` · `npm test` (Vitest) · `npm run lint`.

## Testing

```bash
# Backend
make test                          # or: cd backend && go test ./...
go test ./domain/runtime/ -run TestAgentLoop_MaxToolCalls -v   # single test

# Frontend
cd frontend && npm test
```

The agent loop is heavily tested with scripted models, including failure policies, evidence-gate self-heal, the MaxToolCalls cutoff, and a validator that catches orphaned `tool_calls` (the 400 "insufficient tool messages" class of bug).

## Tech Stack

**Backend:** Go 1.24 · Hertz (HTTP) · GORM/MySQL · Uber Fx (DI) · eino (LLM) · santhosh-tekuri/jsonschema/v6 · Atlas (migrations).

**Frontend:** React 18 · TypeScript 5.6 · Vite 6 · Zustand 5 · d3 v7 (graph) · Tailwind v4 · Vitest · MSW.

## How a Decision Runs

1. **Create** -> case is `DRAFT`.
2. **Normalize / build context** -> the Commander produces a canonical question.
3. **Investigate** (concurrent) -> each agent runs its loop: calls tools, records evidence, forms claims, produces an `EvidenceSummary`.
4. **Evidence gate** (deterministic) -> each agent's evidence is checked against its `EvidenceStandard` (count, reliability, required types, custom rules). Fail -> back to gather.
5. **Collect votes** -> each agent emits a structurally-validated `Vote` (stance + confidence + per-dimension scores + reasoning).
6. **Consensus check** -> unanimous -> resolve. First-round 2:1 -> **debate**.
7. **Debate -> reflect -> re-vote** -> debate targets conflicting claims; reflection requires justification; re-vote.
8. **Post-revote 2:1 may resolve**; otherwise deadlock.
9. **Generate report -> save memory -> evaluate** -> `RESOLVED` (or `DEADLOCKED`/`FAILED`).

## Known Limitations / Future Work

- **Knowledge base + prior-conclusion retrieval** - `KnowledgePort` and `ContextBuilder` are wired but not yet retrieving past resolved cases during investigation.
- **Coze local replace directive** - must be swapped to a published fork for production builds (see Architecture).
- The standalone CLI mode referenced in some docs is not currently wired; run via the HTTP server + frontend.

## License

Personal project. See the repository for details.

## Harness Capabilities

Beyond the core decision loop, MAGI ships as a governed, deployable AI harness:

- **Durable async execution** — decision jobs persist with leases, retries, cancellation, and startup recovery; agent runs checkpoint and resume.
- **Multi-tenant API** — API-key auth (constant-time compare) with per-user ownership on cases, datasets, plugin bindings, and recurring templates. Health/docs/metrics stay public.
- **Governance & safety** — per-user run concurrency limits, token cost accounting, Prometheus `/metrics`, code-runner guardrails (language/length/danger patterns/timeout), an autonomous tool-approval gate (high-impact tools require admin auto-approval), prompt-injection framing of tool output, and secret redaction before events/audit/model messages leave the process.
- **Observability** — OpenTelemetry spans on HTTP requests and decision runs with `X-Trace-ID` propagation (log sink by default, OTLP-ready), plus readiness/DB ping and config fail-fast validation.
- **Evidence-backed outputs** — the final report must cite at least one collected evidence/claim ID when evidence exists, or the case fails.
- **User-scoped tools** — each user manages their own Coze plugin bindings; agent runs resolve the user's enabled tools dynamically.
- **Ground-truth evaluation** — datasets of expected decisions run asynchronously through the full orchestrator and report accuracy / weighted accuracy with per-item results.
- **Proactive scheduling** — recurring decision templates fire automatically at intervals through the async run manager.
- **Conversational entry** — `POST /api/v1/assistant` turns a natural-language question into a full decision run with report.
- **Admin operations** — role-gated usage aggregates (cases/runs/tokens/cost per user) at `GET /api/v1/admin/usage`.

### API map (v1)

| Method | Path | Purpose |
| --- | --- | --- |
| POST | `/assistant` | Run a decision from a message |
| POST/GET | `/cases`, `/cases/:id` (+`/run`, `/cancel`, `/report`, `/agents`, `/evidence`, `/claims`, `/votes`, `/events`, `/timeline`, `/trace`, `/stream`) | Decision lifecycle and artifacts |
| GET | `/datasets`, `/datasets/:id`, `/:id/items`, `/:id/runs`, `/benchmarks/:runID` | Ground-truth evaluation |
| GET/POST/PATCH/DELETE | `/plugins`, `/plugins/:id` | User plugin bindings |
| GET/POST/PATCH/DELETE | `/recurring`, `/recurring/:id` (+`/:id/run`) | Recurring templates |
| POST | `/evaluation`, `/evaluation/:id`, `/benchmark` | Evaluation |
| GET | `/admin/usage` | Admin usage aggregate |
| GET | `/metrics`, `/health`, `/ready`, `/version`, `/openapi.json` | Ops |

### Configuration highlights

```yaml
model: { api_key: "...", model_name: "..." }   # or model_id for Coze mode
auth: { enabled: true, api_keys: [...] }        # per-user API keys
limits: { max_concurrent_runs_per_user: 3 }
code_runner: { enabled: true, timeout_seconds: 30, max_code_chars: 4000, ... }
tool_policy: { require_approval: ["code_runner"], auto_approved: [] }
```

`Config.Validate()` runs at startup and fails fast on missing model, invalid auth, or negative limits; `/ready` performs a live database ping.

## Development

```bash
cd backend && go test ./... && go test -race ./...
```

The end-to-end harness flow (`server/e2e_test.go`) exercises auth, case lifecycle, dataset evaluation, plugin bindings, recurring triggers, the assistant endpoint, metrics, and admin usage against a real SQLite-backed stack with a stubbed orchestrator.
