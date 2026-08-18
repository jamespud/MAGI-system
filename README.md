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
- **Real tool integration** - Tavily `web_search` returns live web evidence, external MCP servers expose their tools as `mcp_<server>_<tool>`, and a WASM code sandbox (Coze's Deno + Pyodide runner) executes Python in isolation; one tool result -> one evidence record.
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

The frozen target architecture and design philosophy are documented in `docs/harness-eva-design.md`; all code targets that design.
The EVA lineage and design philosophy — hard voting vs. judge-based synthesis,
personality-as-executable-boundary, dissent as a first-class outcome — is documented
in `docs/harness-eva-design.md`.

> **Coze reuse:** `backend/go.mod` depends on the published `coze-dev/coze-studio` module from the Go proxy (see `backend/Dockerfile`). MAGI reuses Coze Studio's model builder, plugin/tool registry, knowledge store, and sandbox - but only through `adapter/`, never by importing Coze domain types into `domain/`. The code sandbox reuses Coze's `infra/coderunner/impl/sandbox` (Deno + Pyodide WASM); `backend/sandbox.py` is a vendored copy of Coze's sandbox orchestrator (provenance pinned in its header), and the Docker image ships `python3` + `deno` with a warmed `jsr:@langchain/pyodide-sandbox` cache.

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
| `make prepare` | Bootstrap: check deps, init `.env`, install Go + npm dependencies |
| `make db-up` / `make db-down` | Start/stop MySQL middleware on `127.0.0.1:3307` |
| `make debug` | MySQL + Go backend + nginx on `:80` |
| `make backend` / `make frontend` | Run Go server (`:8080`) / Vite dev (`:5173`) |
| `make build` | Build `bin/magi` + `frontend/dist/` + Docker images |
| `make test` | Go tests + frontend tests |
| `make fmt` / `make vet` / `make tidy` / `make lint` | Go quality tools |
| `make web-up` / `make web-down` | Containerized full stack (mysql + server + nginx + RAG) |
| `make rag_up` / `make rag_down` | RAG middleware (Milvus + ES) |
| `make stop` | Stop all dev/web containers |
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

**Backend:** Go 1.25 · Hertz (HTTP) · GORM/MySQL · Uber Fx (DI) · eino (LLM) · santhosh-tekuri/jsonschema/v6 · GORM AutoMigrate (schema source of truth; `docker/atlas/migrations/` holds baseline snapshots).

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

- The standalone CLI mode referenced in some older docs is not currently wired; run via the HTTP server + frontend.

## License

Personal project. See the repository for details.

## Harness Capabilities

Beyond the core decision loop, MAGI ships as a governed, deployable AI harness:

- **Durable async execution** — decision jobs persist with leases, retries, cancellation, and startup recovery; agent runs checkpoint and resume.
- **Human-in-the-loop approvals** — gated tools create persisted approval requests (`/api/v1/approvals`); the run parks and resumes after approve/reject, and the decision is recorded on the tool call for audit.
- **Cross-retry checkpoint resume** — agent checkpoints are keyed per case/agent/round (not per attempt), so durable retries and restarts resume the interrupted step instead of restarting from scratch.
- **Context compaction** — before hitting the token budget the agent history is summarized (with a deterministic fallback) so long runs continue.
- **Multi-tenant API** — API-key auth (constant-time compare) with per-user ownership on cases, datasets, plugin bindings, and recurring templates. Health/docs/metrics stay public.
- **Governance & safety** — per-user run concurrency limits, token cost accounting, Prometheus `/metrics`, code-runner guardrails (language/length/danger patterns/timeout) on top of the Coze WASM sandbox, an autonomous tool-approval gate (high-impact tools require admin auto-approval), prompt-injection framing of tool output, and secret redaction before events/audit/model messages leave the process.
- **Observability** — OpenTelemetry spans on HTTP requests and decision runs with `X-Trace-ID` propagation (log sink by default, OTLP-ready), plus readiness/DB ping and config fail-fast validation.
- **Evidence-backed outputs** — the final report must cite at least one collected evidence/claim ID when evidence exists, or the case fails.
- **User-scoped tools** — each user manages their own Coze plugin bindings; agent runs resolve the user's enabled tools dynamically.
- **Ground-truth evaluation** — datasets of expected decisions run asynchronously through the full orchestrator and report accuracy / weighted accuracy with per-item results.
- **Semantic judge** — `POST /evaluation/:id/judge` scores report quality, evidence consistency and reflection validity (LLM-as-a-Judge), persisted per case.
- **Counterfactual stability & regression gate** — benchmark items repeat N times (`runs_per_item`), report stability, and fail the run when accuracy drops below `regression_threshold`.
- **Multi-instance operation** — per-user run limits and the recurring scheduler use shared DB state; API keys may be stored hashed (`key_hash`).
- **MCP resilience** — HTTP auth headers and reconnect-with-backoff for external MCP servers.
- **Tool quotas & observability** — per-user tool rate limits, run-duration histograms, cost metrics, OTLP export, and per-step/per-tool spans.
- **Proactive scheduling** — recurring decision templates fire automatically at intervals through the async run manager.
- **Conversational entry** — `POST /api/v1/assistant` turns a natural-language question into a full decision run with report.
- **Admin operations** — role-gated usage aggregates (cases/runs/tokens/cost per user) at `GET /api/v1/admin/usage`.
- **Kubernetes delivery** - Helm chart with backend/frontend deployments, SSE-aware ingress, probes, HPA, PDB, and externally managed secrets (`deploy/magi/`).

### API map (v1)

| Method | Path | Purpose |
| --- | --- | --- |
| POST | `/assistant` | Run a decision from a message |
| POST/GET/PATCH/DELETE | `/cases`, `/cases/:id` (`?page=&page_size=`; `PATCH` pin/archive; `DELETE` cascade; +`/run`, `/cancel`, `/fork`, `/report`, `/export`, `/agents`, `/evidence`, `/claims`, `/votes`, `/events`, `/timeline`, `/trace`, `/stream`) | Decision lifecycle and artifacts |
| GET | `/datasets`, `/datasets/:id`, `/:id/items`, `/:id/runs`, `/benchmarks/:runID` (+`DELETE /datasets/:id`) | Ground-truth evaluation |
| GET | `/memory`, `/memory/:id`, `/memory/export` | Case-memory search (semantic + LIKE) and full export |
| GET | `/evaluation/:id/export` | Export evaluation + judge verdict |
| GET/POST | `/approvals`, `/approvals/:id`, `/approvals/:id/approve`, `/approvals/:id/reject` | Human-in-the-loop tool approvals |
| GET/POST/PATCH/DELETE | `/plugins`, `/plugins/:id` | User plugin bindings |
| GET/POST/PATCH/DELETE | `/recurring`, `/recurring/:id` (+`/:id/run`) | Recurring templates |
| POST | `/evaluation`, `/evaluation/:id`, `/benchmark` | Evaluation |
| GET | `/admin/usage`, `/me/usage` | Admin usage aggregate / own usage + budget |
| GET/PUT/POST | `/admin/prompts`, `/admin/prompts/:key` (+`/restore`) | Versioned prompt registry (P2) |
| POST/GET/DELETE | `/admin/users`, `/admin/users/:id` | User management (admin) |
| GET/POST | `/admin/users/:id/keys` | List / issue user API keys (admin) |
| POST | `/admin/keys/:id/revoke`, `/admin/keys/:id/rotate` | Revoke / rotate an API key |
| GET | `/me` (+`POST /me/keys`) | Current principal + self-issued keys |
| POST/GET/DELETE | `/knowledge`, `/knowledge/:id` | User knowledge base (RAG documents) |
| GET | `/metrics`, `/health`, `/ready`, `/version`, `/openapi.json` | Ops |

### Configuration highlights

```yaml
model: { api_key: "...", model_name: "..." }   # or model_id for Coze mode
auth: { enabled: true, api_keys: [...] }        # static bootstrap keys (DB keys managed via /admin/users)
limits: { max_concurrent_runs_per_user: 3 }
code_runner: { enabled: true, timeout_seconds: 30, max_code_chars: 4000, ... }
tool_policy: { require_approval: ["code_runner"], auto_approved: [] }
magi: { approval_timeout_seconds: 3600, token_budget: 150000, compaction_threshold: 0.7 }
# per-role model overrides (inherit unset fields from the global `model` block)
magi.melchior.model: { model_name: "...", api_key: "...", base_url: "..." }
commander.model: { model_name: "..." }
judge.model: { model_name: "..." }
mcp:
  servers:
    - { name: "example", transport: "stdio", command: "/usr/local/bin/mcp-server", timeout_seconds: 60 }
```

Each of the three Magi roles (and the Commander / semantic Judge) may override
the global `model` on a per-field basis under `magi.<role>.model`, `commander.model`
and `judge.model` respectively — unset fields inherit from the global block. This
enables genuine cognitive diversity by running different roles on different models
(see `backend/conf/magi.yaml.example`).

`code_runner` maps its `allow_env/allow_read/allow_write/allow_net/allow_run/allow_ffi` lists into the Deno sandbox permission flags (empty = deny all). `Config.Validate()` runs at startup and fails fast on missing model, invalid auth, negative limits, or invalid MCP server definitions (empty/duplicate names, bad transport, missing command/url); `/ready` performs a live database ping.

## Development

```bash
cd backend && go test ./... && go test -race ./...
```

The end-to-end harness flow (`server/e2e_test.go`) exercises auth, case lifecycle, dataset evaluation, plugin bindings, recurring triggers, the assistant endpoint, metrics, and admin usage against a real SQLite-backed stack with a stubbed orchestrator.
