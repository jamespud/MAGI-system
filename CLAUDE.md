# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project

MAGI is an evidence-driven multi-agent decision engine (inspired by Evangelion's MAGI). Three agents — **Melchior** (scientist), **Balthasar** (protector), **Casper** (innovator) — each with a distinct objective function, evidence standard, and risk tendency, independently investigate a decision question, then vote, debate, reflect, and re-vote toward a consensus resolution. A **Commander** LLM handles only language tasks. The frozen target architecture and design philosophy are documented in `docs/harness-eva-design.md`; all code targets that design.

Go module at `backend/` (`github.com/jamespud/magi/backend`), Go 1.24+.

## Commands

Makefile targets run from repo root:

```bash
make setup       # interactive wizard: collect model/embedding/search keys -> .env + magi.yaml
make install     # install Go + npm dependencies
make dev         # hot-reload: middleware + Vite (:5173) + Go server (:8080)
make start       # production-ish: middleware + nginx (:80) + Go binary
make backend     # Go server only
make frontend    # Vite dev server
make up          # containerized full stack
make docker-start  # middleware (MySQL + Milvus + ES)
make test        # backend Go tests + frontend vitest
make fmt vet tidy lint   # Go quality targets
make stop        # stop local services
```

Direct Go (from `backend/`):

```bash
go build ./cmd/magi-server
go test ./...
go test ./domain/validation/ -run TestTypedValidator -v   # single test
go vet ./...
```

Config: `backend/conf/magi.yaml` (override path with `MAGI_CONFIG` env var). Secrets come from `.env` (`MAGI_*`) at runtime — `make dev`/`make start` and `make up` all load `.env`. The `model` block points at any OpenAI-compatible endpoint (default DeepSeek).

## Critical: Coze Studio dependency

`backend/go.mod` depends on the published `github.com/coze-dev/coze-studio/backend`
module (see `backend/Dockerfile`); the only replace directive is the required
`github.com/apache/thrift => v0.13.0` pin that must stay in sync with coze-studio.
MAGI reuses Coze Studio's model builder, plugin/tool registry, knowledge store, and
sandbox — but only through `adapter/`, never by importing Coze domain types into
`domain/`.

The code sandbox is wired in `bootstrap/container.go`: `codeRunnerAdapter`
builds `infra/coderunner/impl/sandbox.Config` from the `code_runner` config and
injects `sandbox.NewRunner` into `CodeRunnerAdapter` (no dependency on the Coze
global). `backend/sandbox.py` is a vendored copy of Coze's sandbox orchestrator
(provenance in its header); the Docker image ships `python3` + `deno` and warms
the `jsr:@langchain/pyodide-sandbox` cache. MCP servers are config-driven
(`backend/adapter/mcp`), tool names are `mcp_<server>_<tool>`.

## Architecture

Four layers, strictly one-way dependencies:

```
Application -> Orchestration -> Agent Runtime -> Port/Adapter -> Coze Infrastructure
```

`domain/` depends only on `domain/port` interfaces + eino; Coze implementations live in `adapter/`. Reverse dependencies are forbidden (design §2). ADRs are referenced in code comments (ADR-002 … ADR-010) but not stored as files here — the code is authoritative and the design rationale lives in `docs/harness-eva-design.md`.

Design philosophy and EVA mapping: `docs/harness-eva-design.md`.

### Governing principle: LLM = semantics, code = rules

LLMs: understand the question, propose claims, explain evidence, write reflections, draft reports.
Deterministic Go: permission checks, schema validation, EV-ID authenticity, evidence gate, utility-dimension legality, vote counting, consensus judgment, state transitions, loop limits, timeouts.

When adding capability, decide first which side it belongs to. The Commander (an LLM) is deliberately not a "god agent" — it must not count votes, judge consensus, bypass the evidence gate, or drive state transitions (see `domain/service/commander.go`, design §6).

### Orchestration is a deterministic FSM

`domain/orchestration/orchestrator.go` drives each case through ~18 `CaseStatus` states (`domain/entity/case.go`): DRAFT → NORMALIZING → CONTEXT_BUILDING → RETRIEVING_MEMORY → INVESTIGATING → EVIDENCE_GATING → COLLECTING_VOTES → CONSENSUS_CHECK → (RESOLVING | DEBATING → REFLECTING → REVOTING → CONSENSUS_CHECK) → GENERATING_REPORT → SAVING_MEMORY → EVALUATING → RESOLVED. Every transition is a `switch` case; nothing is LLM-decided. First-round 2:1 (majority-with-dissent) goes to debate; only post-revote 2:1 may resolve (design §15).

`Dispatcher` (`dispatcher.go`) fans the three agents out concurrently with `sync.WaitGroup` — each gets isolated working memory; the Evidence Ledger is shared append-only. Per-agent errors become `LoopStatusError` results handled by `FailurePolicy`, never panics.

### Agent Runtime is a hand-written loop (not Eino ReAct)

`domain/runtime/agent_loop.go` is MAGI's own loop — do not replace it with Eino ReAct/compose. Each step the model returns one of: tool call, claim submission, evidence summary, or vote. Flow: gather (tools → Evidence Ledger → Claims) → EvidenceSummary → **Evidence Gate** (deterministic) → Vote (structurally validated). Termination is code-enforced: valid vote, max steps, timeout, context cancel, token budget, repeated validation/tool/gate failures (`termination.go`).

When no tools are bound (`cfg.Tools` empty), evidence requirements are relaxed to zero so the agent can reason from intrinsic knowledge and still produce a valid vote — this is how the standalone CLI runs.

### Evidence Ledger is the spine

`domain/evidence/`: Tool result → `EvidenceRecord` → supports → `Claim` → Vote/Reasoning. Reliability is a deterministic modifier formula (base + directness + recency + corroboration + extraction), never a single LLM-emitted number. Each agent has its own `EvidenceStandard` enforced by `EvidenceGate` (Melchior needs quantitative evidence; Balthasar needs a worst-case claim; Casper needs an opportunity-cost claim). `domain/claim/graph.go` tracks supports/contradicts so debate targets specific conflicting claims, not free-form prose.

### Validation = JSON Schema as the runtime contract (ADR-003)

`domain/validation/`: Go struct → `eino-contrib/jsonschema.Reflect()` → JSON Schema → `santhosh-tekuri/jsonschema/v6` validates every LLM structured output (DecisionTask, EvidenceSummary, ClaimSubmission, Vote, Reflection, FinalReport). `TypedValidator[T]` validates then typed-unmarshals.

**Gotcha (recent bug source):** any struct used as an LLM structured-output contract MUST have `json` tags on every field. Without them, `Reflect()` emits PascalCase property names while the LLM emits lowercase; with `additionalProperties: false`, validation fails. When adding a structured-output struct or field: add `json:"..."` tags and update the Commander prompt to describe the exact object shape (e.g. "each with code + description", not just "a list"). See commit history (`fix: add json tags ...`) and `docs/superpowers/plans/`.

### Ports & dual-mode model

`domain/port/` defines `ModelPort`, `ToolRegistryPort`, `ToolExecutorPort`, `KnowledgePort`, `Repository`, `EventPublisher`, etc. `adapter/model_adapter.go` builds models two ways: **direct mode** (APIKey + ModelName → eino-ext openai client; used by the standalone CLI) or **Coze mode** (ModelID > 0 → `modelbuilder.BuildModelByID`; integrated deployment). `main.go` wires stub tool registry/executor and a nil case repo — it exercises the full domain logic standalone; the MySQL path (`docker/`, `docker/atlas/migrations/`) is for the integrated deployment.

### Events, memory, evaluation, replay

`KnowledgePort` is implemented by `adapter/rag/HybridKnowledgeAdapter` (not Coze crossknowledge): case-memory projections are rendered to long text, chunked into a 1800/900/300 parent-child hierarchy, embedded via an OpenAI-compatible endpoint, and indexed into Milvus (300-level vectors) + MySQL (content + hierarchy) + Elasticsearch (300-level BM25). Retrieve fuses Milvus + ES via RRF, then promotes unanimous 300-groups up to 900/1800. Store is async (worker pool). When Milvus/ES addresses are empty, fake indexes are used (standalone-safe). See `docs/superpowers/specs/2026-07-26-rag-pipeline-design.md`.

Every transition publishes a `MagiEvent` (`domain/entity/event.go`, ADR-008) via `EventPublisher` → event store / SSE / replay. `domain/service/replay.go` reconstructs a case from its event stream. `domain/memory/` builds agent context (working/case/long-term) and projects resolved cases for future RAG retrieval. `domain/service/evaluation.go` scores tool/evidence/agent/consensus/system metrics, including counterfactual stability.

## Conventions

- Conventional Commits (`fix:`, `feat:`, `test:`, `chore:`).
- Larger change plans live under `docs/superpowers/plans/` using `- [ ]` checkbox steps.
- Code comments and identifiers are English; design docs (`docs/harness-eva-design.md`) are Chinese.
