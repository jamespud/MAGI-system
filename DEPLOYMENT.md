# MAGI Deployment & Verification Guide

MAGI is a governed, evidence-driven multi-agent decision harness. This guide
covers containerized deployment and the production end-to-end verification
that requires real model/plugin/workflow credentials.

## Prerequisites

- Docker with compose plugin
- Go 1.24+ (for local builds/tests)
- Node.js 18+ (for the frontend)
- An OpenAI-compatible model API key, or a Coze model id
- Optional: Tavily API key (web_search), Coze plugin/workflow services

## Quick start (containerized stack)

```bash
make prepare                       # bootstrap deps, .env, dirs
cp backend/conf/magi.yaml.example backend/conf/magi.yaml
# edit backend/conf/magi.yaml: model.api_key/model_name (+ auth, limits as needed)
make web-up                        # mysql + magi-server + nginx (port 80)
```

- API: `http://localhost/api/v1`
- OpenAPI: `http://localhost/openapi.json`
- Metrics: `http://localhost/metrics`
- Health/readiness: `/health`, `/ready` (readiness pings the database)

Stop with `make web-down`; logs with `make web-logs`.

### Metrics endpoint exposure

`GET /metrics` is intentionally unauthenticated (Prometheus scraping cannot
send bearer tokens). Do not expose it directly to the public internet: put it
behind a reverse proxy that allows only the Prometheus server (e.g. nginx
`allow` rules or a separate scrape port). Example alert rules live in
`deploy/prometheus-alerts.example.yml`.

### Multi-tenant boundaries and sandbox egress

- **Per-user limits are cumulative and independent**: run concurrency
  (`limits.max_concurrent_runs_per_user`), token/cost budgets
  (`limits.max_tokens_per_user`, `limits.max_cost_usd_per_user`), and tool
  rate limits (`tool_quota`) each cap a user without affecting others. All are
  backed by shared DB state so they hold across replicas.
- **Sandbox egress is deny-by-default**: `code_runner.allow_net` maps to Deno
  permission flags; an empty list means no network access. For a controlled
  egress proxy, run MAGI behind a proxy and populate `allow_net` with only the
  proxy's address.
- **MCP credentials**: external MCP servers are configured in `mcp.servers`;
  HTTP servers accept static auth via `headers` (e.g. `Authorization`). OAuth
  flows are not built in — terminate OAuth at a gateway and inject the token
  via headers.
- **Coze integration mode**: plugin/workflow tools require Coze `DefaultSVC`
  services. In standalone deployments those tools degrade gracefully; embed
  MAGI in the Coze process when plugin/workflow capability is required.

## Configuration

### Schema management

**AutoMigrate is the source of truth for the database schema.** The server runs
`db.AutoMigrate(AllModels()...)` at startup, so upgrading the binary is
sufficient for additive changes. `docker/atlas/migrations/` contains baseline
SQL snapshots (s6-s14); they are documentation, not the applied migration
path, and can drift from the GORM models. When they do, regenerate them from
`backend/adapter/model.go` rather than hand-editing.

`backend/conf/magi.yaml` (or env overrides via `MAGI_*`):

| Section | Purpose |
| --- | --- |
| `model` | `api_key` + `base_url` + `model_name` (direct) or `model_id` (Coze mode); `price_per_m_*_usd` for cost accounting |
| `auth` | `enabled: true` + `api_keys` (name/key/user_id/role). Startup fails fast if enabled without valid keys |
| `limits` | `max_concurrent_runs_per_user` |
| `code_runner` | enable + timeout/length/language/danger-pattern guardrails on top of the Coze WASM sandbox; `allow_*` lists map to Deno permission flags (empty = deny all), `memory_limit_mb` (default 100) |
| `mcp` | `servers[]` with `name`/`transport` (`stdio` or `http`)/`command`+`args` or `url`/`env`/`timeout_seconds` (default 60); tools appear as `mcp_<server>_<tool>` |
| `tool_policy` | `require_approval` (default `code_runner`) and `auto_approved` |
| `magi` budget | `approval_timeout_seconds` (default 3600), `token_budget` (150000), `compaction_threshold` (0.7) |
| `tracing` | `enabled` + `service_name` (log sink; OTLP-ready) |
| `tool_quota` | `default_per_minute` + per-tool limits |
| `benchmark` | `runs_per_item`, `regression_threshold` |

Secrets are injected at runtime: `MAGI_MODEL_API_KEY`, `MAGI_TAVILY_API_KEY`,
`MAGI_AUTH_ENABLED`, `MAGI_AUTH_API_KEYS` (`userID:role:name:key;...`),
`MAGI_DB_DSN`, `MAGI_DB_DRIVER`.

## Verification checklist (real credentials required)

## Quality gates before release

MAGI ships three complementary gates. Run them before any release that changes
prompts, tools, role contracts or the agent loop; treat a failed gate as a
release blocker.

1. **Ground-truth benchmark + regression threshold** — build a dataset of
   expected decisions, then run a benchmark. The run reports
   `accuracy`, `weighted_accuracy`, `stability` (consistency across N repeats
   per item) and fails with `regression_failed: true` when accuracy falls below
   `benchmark.regression_threshold` (e.g. 0.8). This catches decision
   regressions cheaply.
   ```bash
   curl -s -X POST http://localhost/api/v1/datasets -H "Authorization: Bearer <key>" \
     -H "Content-Type: application/json" -d '{"name":"release-gate"}'
   # add items, then:
   curl -s -X POST http://localhost/api/v1/datasets/<id>/runs?runs_per_item=3 -H "Authorization: Bearer <key>"
   curl -s http://localhost/api/v1/benchmarks/<runID> -H "Authorization: Bearer <key>"
   ```
2. **LLM-as-a-Judge semantic gate** — after a benchmark, judge the resolved
   cases for report quality / evidence consistency / reflection validity
   (`POST /api/v1/evaluation/:id/judge`, then `GET .../judge`). Establish a
   baseline score on the current release; block releases whose mean `overall`
   drops below it by more than a few points.
3. **Counterfactual stability** — with `runs_per_item >= 3`, the benchmark's
   per-item consistency rate is itself the counterfactual stability measure;
   a stability drop (e.g. below 0.7) indicates prompt/model sensitivity that
   should be investigated before release.

1. Stack is healthy:
   ```bash
   curl -s http://localhost/ready     # {"status":"ready"}
   curl -s http://localhost/metrics   # counters present
   ```

2. Authentication: create an API key in config, then
   ```bash
   curl -s -X POST http://localhost/api/v1/cases \
     -H "Authorization: Bearer <key>" \
     -H "Content-Type: application/json" \
     -d '{"question":"Should we adopt Rust for the core service?"}'
   ```

3. Run the case and poll until `RESOLVED`:
   ```bash
   curl -s -X POST http://localhost/api/v1/cases/<id>/run -H "Authorization: Bearer <key>"
   curl -s http://localhost/api/v1/cases/<id> -H "Authorization: Bearer <key>"
   curl -s http://localhost/api/v1/cases/<id>/report -H "Authorization: Bearer <key>"
   ```
   The final report must cite at least one collected evidence/claim ID; if the
   model never cites evidence, the case fails deliberately.

4. Tools and safety:
   - `GET /api/v1/tools` lists resolved tools.
   - Code execution requires approval unless `code_runner` is in
     `tool_policy.auto_approved`; language/length/pattern/timeout guardrails
     apply before any sandbox call.
   - Gated tools create approval requests visible at `GET /api/v1/approvals`;
     approving resumes the parked run, rejecting feeds the decision back to the agent.
   - The sandbox reuses Coze's Deno + Pyodide WASM runner: the image must
     contain `python3`, `deno`, and `/app/sandbox.py` (vendored from
     coze-studio), with the `jsr:@langchain/pyodide-sandbox` cache warmed at
     build time. Code must define `async def main(args)` returning a dict.
   - MCP servers are config-driven; unreachable servers are skipped in
     `GET /api/v1/tools` and surface an error when called.
   - Plugin bindings are per-user via `/api/v1/plugins`.

5. Evaluation loop:
   - `POST /api/v1/datasets/<id>/runs?runs=N&threshold=T` repeats each item N times
     and fails the run below the accuracy threshold; run detail reports stability.
   - `POST /api/v1/evaluation/<caseId>/judge` runs the semantic judge.
   ```bash
   curl -s -X POST http://localhost/api/v1/datasets \
     -H "Authorization: Bearer <key>" -H "Content-Type: application/json" \
     -d '{"name":"launch-eval"}'
   curl -s -X POST http://localhost/api/v1/datasets/<id>/items \
     -H "Authorization: Bearer <key>" -H "Content-Type: application/json" \
     -d '{"items":[{"question":"ship A?","expected_decision":"approve"}]}'
   curl -s -X POST http://localhost/api/v1/datasets/<id>/runs -H "Authorization: Bearer <key>"
   curl -s http://localhost/api/v1/benchmarks/<runId> -H "Authorization: Bearer <key>"
   ```

6. Conversation + scheduler:
   ```bash
   curl -s -X POST http://localhost/api/v1/assistant \
     -H "Authorization: Bearer <key>" -H "Content-Type: application/json" \
     -d '{"message":"Should we migrate the database?"}'
   curl -s -X POST http://localhost/api/v1/recurring \
     -H "Authorization: Bearer <key>" -H "Content-Type: application/json" \
     -d '{"name":"daily","question":"keep the stack?","interval_seconds":86400}'
   ```

7. Traceability: every HTTP response carries `X-Trace-ID`; spans are written
   to server logs when `tracing.enabled: true`. Events are persisted in the
   event store and available via `/api/v1/cases/<id>/events`.

## Troubleshooting

- **Startup fails fast**: read the error — model must be configured, auth keys
  valid when enabled, limits non-negative.
- **Case stuck in a state**: check `/api/v1/cases/<id>/events`; each transition
  publishes an event. Restarting the server requeues expired jobs, resumes
  checkpointed agent runs per case/agent/round, and cleans stale attempt artifacts
  so the retry does not duplicate evidence/claims/votes.
- **Report fails with citation error**: the model did not cite collected
  evidence; retry the case (or improve prompts) — this is intentional.
- **Plugin/workflow unavailable**: MAGI standalone degrades gracefully when
  Coze `DefaultSVC` services are not registered; embed MAGI in the Coze process
  or wire the services to use them.
- **Sandbox unavailable**: check the image has `python3`/`deno` and
  `/app/sandbox.py`; `code_runner.enabled` must be true. Runtime errors like
  "deno: command not found" mean the image was built before the sandbox deps.
- **MCP tool missing from `/api/v1/tools`**: the server is unreachable or
  failed `initialize`/`tools/list`; check the server URL/command and its logs.
  Tool names are `mcp_<server>_<tool>` (names normalized to `[a-z0-9_]`).

## Automated verification (no external credentials)

```bash
make test                            # backend go tests + frontend vitest
cd backend && go test -race ./...
docker compose -f docker/docker-compose-dev.yml config   # compose validity
```

`backend/server/e2e_test.go` exercises the full harness flow (auth, cases,
dataset evaluation, plugins, recurring, assistant, metrics, admin usage)
against a real SQLite-backed stack with a stubbed orchestrator.
