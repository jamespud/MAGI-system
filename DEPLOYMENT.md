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
`magi_model_failovers_total` counts automatic moves from one model provider
to the next; alert on any increase because a healthy primary should not fail
continuously.
`magi_web_search_failovers_total` applies the same rule to search providers.

### Self-improving harness (human-in-the-loop)

Failed cases can be analyzed into deterministic, categorized improvement
suggestions (`POST /admin/selfimprove/analyze` with `{case_id}`). The analyzer
reads the case events and agent-run errors, classifies the failure
(gate/tool/model/timeout), and proposes a rule. Gate failures additionally
propose a prompt change. Suggestions stay `open` until an admin applies them
(`POST /admin/selfimprove/suggestions/:id/apply`), which writes any proposed
prompt to the versioned prompt registry and marks the suggestion `applied`.
Nothing is applied automatically; the operator is the approval gate.

### Editable role contracts (NLAH control surface)

The per-role evidence-gate policy (required assessment type, residual-risk /
technical / opportunity score thresholds, weighted-utility gate, debate
directive) is stored in the `role_policy` table and editable over
`GET/PUT /admin/role-policies/:code` (reset with
`POST .../reset`). Stored specs override the built-in defaults when agent
configs are assembled at startup, so tuning a role's decision boundary is a
versioned, auditable config change rather than a code change.

### Online golden regression

Completed production cases can be promoted to the online-golden set
(`POST /admin/golden` with `{case_id}`; list/delete via `GET/DELETE
/admin/golden...`). `POST /admin/golden/sync` appends all golden cases to the
built-in decision sanity suite, so the periodic automated regression gate
(`benchmark.auto_interval_seconds`) continuously validates real production
decisions against the current harness.

### Self-improving automation

`selfimprove.auto_apply_enabled` turns the analysis loop fully automatic
behind a threshold: after each automated regression, the oldest open
suggestion of any category that has reached `auto_apply_threshold` is applied
to the versioned prompt registry (the harness evolves its own rules). Keep it
disabled unless you have audited the prompt-registry change path.

### Default observability stack

With the web stack running, start the bundled Prometheus + Alertmanager +
Grafana stack in one command:

```bash
make monitoring-up
# Prometheus:   http://localhost:9090
# Alertmanager: http://localhost:9093
# Grafana:      http://localhost:3000  (admin / admin; change GRAFANA_ADMIN_PASSWORD)
```

Prometheus scrapes `magi-server:8080/metrics` over the `magi-web_default`
network, loads the alert rules from `deploy/prometheus-alerts.example.yml`, and
forwards them to Alertmanager. Grafana auto-provisions the Prometheus data
source and a prebuilt "MAGI Overview" dashboard (request rate, active/failed
runs, tool/model/search failures and failovers, model cost) from
`docker/grafana/`. Stop with `make monitoring-down`.

### Users and API keys

- **Static bootstrap keys** (`auth.api_keys` in config) authenticate at
  startup and remain valid for the process lifetime. Prefer creating
  runtime users over the admin API instead of editing config.
- **DB-backed users** are created with `POST /api/v1/admin/users` (admin
  only). Each user gets a bootstrap API key whose plaintext is shown exactly
  once; only its SHA-256 hash is stored (`api_keys.key_hash`).
- **Key lifecycle** over the admin API: list/issue per user,
  `POST /admin/keys/:id/revoke` disables a key, `POST /admin/keys/:id/rotate`
  revokes and issues a replacement. `GET /me` shows the current principal and
  lets users self-issue keys (`POST /me/keys`).
- The auth middleware checks static keys first, then DB keys by hash,
  updating `last_used_at` for observability. Revoked or deleted keys stop
  authenticating immediately.
- **Roles**: `admin` (users, API keys, all routes), `operator` (usage,
  prompts, benchmark seed and eval summary), and `user` (own workspace).
  Routes are gated with `RequireAnyRole(...)`; user and API-key management
  stays admin-only.

### OIDC single sign-on

`auth.oidc.enabled: true` enables authorization-code login against any OIDC
issuer. `GET /auth/oidc/login` redirects to the provider; the callback
exchanges the code, fetches userinfo, matches the identity by email (or
auto-provisions a user-role account when `self_registration` is true), and
sets an HMAC-signed `magi_session` cookie that the auth middleware accepts
alongside API keys. `auth.self_registration` exposes `POST /auth/register`
for public one-time-key account creation. The one-time state store is
in-memory, so multi-replica deployments should terminate OIDC at a gateway or
share state.

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

## Kubernetes deployment (Helm)

The Helm chart deploys two stateless workloads: `magi-backend` and
`magi-frontend`. MySQL, Milvus, and Elasticsearch are intentionally external
dependencies so storage durability, backup policy, and scaling remain under
platform control.

```bash
# Build and publish both images first (see deploy/magi/README.md).
helm lint deploy/magi --strict \
  --set-string secret.values.dbDSN='magi:magi123@tcp(mysql:3306)/magi?charset=utf8mb4&parseTime=True' \
  --set-string secret.values.modelApiKey=render-only

helm upgrade --install magi deploy/magi \
  --namespace magi --create-namespace \
  --values magi-values.yaml
```

The chart provides:

- Backend/frontend Deployments, Services, readiness/liveness probes and resources
- A least-privilege ServiceAccount with token automount disabled
- Runtime Secret integration (chart-created or externally managed)
- A non-secret configuration ConfigMap
- SSE-aware nginx configuration (`proxy_buffering off`, long read timeout)
- Ingress with SSE-friendly annotations, optional TLS
- Optional HPA and PodDisruptionBudget for horizontal scaling
- A rendered example manifest at `deploy/k8s/magi.yaml`

Keep `backend.replicaCount=1` for the first install while AutoMigrate initializes
the schema. After a successful rollout, increase replicas or enable the backend
HPA. Shared DB scheduler locks, durable jobs, run counters, and SSE DB polling
support multiple backend replicas. A production values file should use
`secret.create=false` plus `secret.existingSecret`; see
[`deploy/magi/README.md`](deploy/magi/README.md).

The raw manifest contains deliberately invalid placeholders (`CHANGE_ME` and
`example.com` images). Render a fresh manifest from your own values instead of
applying it unchanged.

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
| `model` | `api_key` + `base_url` + `model_name` (direct) or `model_id` (Coze mode); ordered `providers[]` supplies automatic failover; `price_per_m_*_usd` for provider-accurate cost accounting |
| `search` | Ordered `providers[]` for Tavily/Brave; the first successful response wins and provider failures increment `magi_web_search_failovers_total` |
| `auth` | `enabled: true` + `api_keys` (name/key/user_id/role) act as static bootstrap credentials. Runtime users/keys are managed over the admin API (`/admin/users`) and stored in DB (`users` / `api_keys`, SHA-256-hashed, plaintext shown once at issuance) |
| `limits` | `max_concurrent_runs_per_user` |
| `code_runner` | enable + timeout/length/language/danger-pattern guardrails on top of the Coze WASM sandbox; `allow_*` lists map to Deno permission flags (empty = deny all), `memory_limit_mb` (default 100) |
| `code_runner.docker` | Optional Docker sandbox runtime: `docker run --rm --network none --pids-limit 64` with `memory_mb`/`cpus`/`timeout_seconds` limits; reuses the shared language/length/blocked-pattern policy; requires a local docker CLI |
| `db_tool` | Built-in `db_query` tool: single read-only SELECT inside a read-only transaction; `driver`/`dsn` default to the `database` block, `max_rows`/`max_query_chars`/`timeout_seconds` bound each call, `blocked_prefixes` adds write-statement rejections |
| `file_tool` | Built-in read-only `file_query` tool: allow-listed absolute `roots`, traversal rejection, `max_file_bytes` / `max_list_items` bounds |
| `repo_tool` | Built-in read-only `repo_query` tool: grep substring / list files inside allow-listed roots, `includes` extension filter, `max_results` / `max_file_bytes` bounds, symlink-escape rejection |
| `web_tool` | Built-in restricted `web_fetch` tool: allow-listed domains only, SSRF DNS guard for domain names, `max_bytes` / `timeout_seconds` bounds, HTML-to-text extraction, non-text content rejection |
| `delegate_tool` | Built-in `delegate` tool (default disabled): spawn an independent sub-investigation subagent on the shared agent loop and return its collected evidence |
| `feedback_tool` | Built-in `check_output` deterministic feedback sensor (default enabled): JSON Schema lint + field constraint rules over the model's own output; violations are fed back for self-correction and counted in `magi_feedback_violations_total` |
| `benchmark` | `runs_per_item` / `regression_threshold` for manual runs; `auto_interval_seconds` schedules an automated regression of the built-in sanity suite, with `auto_runs_per_item` / `auto_regression_threshold` overrides; failures increment `magi_benchmark_regression_failures_total` |
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

## Backup and disaster recovery

### Create a full-stack backup

```bash
make backup
# or with explicit retention/output settings:
MAGI_BACKUP_RETAIN=30 MAGI_BACKUP_DIR=/secure/magi-backups scripts/backup.sh
```

`scripts/backup.sh` pauses only the application server, then creates:

- a single-transaction MySQL logical dump of all MAGI tables;
- archives of `magi-milvus-data`, `magi-es-data`, `magi-etcd-data`, and
  `magi-minio-data`;
- `SHA256SUMS`, a manifest, and one portable `magi-backup-<timestamp>.tar.gz`.

Retention removes only old `magi-backup-*.tar.gz` files in the output
directory. Store bundles off-host and encrypt them: they contain case content,
knowledge, memories, checkpoint state, and credential hashes. A production RPO
is determined by how often this command is scheduled (for example, nightly with
cron, systemd timers, or your platform scheduler).

### Verify and restore

Inspect a bundle without touching running services:

```bash
scripts/restore.sh --bundle backups/magi-backup-<timestamp>.tar.gz --dry-run
```

Restore is destructive. It stops the application, drops and recreates the
configured MySQL database, restores the SQL dump, replaces RAG volume contents,
and restarts the stack:

```bash
scripts/restore.sh \
  --bundle backups/magi-backup-<timestamp>.tar.gz \
  --confirm
curl http://localhost/ready
```

Use `--skip-rag` only for a deliberate database-only recovery. Test a restore
into an isolated environment after every backup-process or Docker-volume layout
change; an untested backup is not a recovery plan.

For Kubernetes, use the persistent-volume backup strategy supported by the
platform (for example Velero with restic/Kopia), plus managed MySQL PITR or
scheduled `mysqldump`. The Helm chart intentionally leaves durable storage and
backup policy to MySQL/RAG providers. Preserve the same namespace/secret and
database naming conventions in recovery runbooks.

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
cd backend && go test -race ./... && go vet ./...
docker compose -f docker/docker-compose-dev.yml config   # compose validity
helm lint deploy/magi --strict       # add render-only secret values
docker run --rm -i ghcr.io/yannh/kubeconform:v0.6.7 -strict -summary \
  < deploy/k8s/magi.yaml
```

`backend/server/e2e_test.go` exercises the full harness flow (auth, cases,
dataset evaluation, plugins, recurring, assistant, metrics, admin usage)
against a real SQLite-backed stack with a stubbed orchestrator.

## P2 additions (2026-08-16)

### HTTP rate limiting
`http_rate_limit: { enabled, per_user_per_minute, per_ip_per_minute }` applies a
per-minute in-memory limit to all `/api/v1` routes, keyed by authenticated user
id (client IP in open mode). It returns `429` + `Retry-After`. The limiter is
per-instance; in multi-replica deployments keep it disabled and rely on the
DB-backed run concurrency and tool quotas, which already hold across replicas.

### Metrics exposure control
`metrics.auth_required: true` moves `/metrics` out of the public path and
requires the admin role (open mode still passes). Default remains public for
Prometheus scraping; keep it behind a reverse-proxy allow-list in production.

### Versioned prompt registry
Prompt templates are seeded from built-in defaults and stored in the
`prompt_template` table. Admin API:

```
GET  /api/v1/admin/prompts              # all versions
GET  /api/v1/admin/prompts/:key         # active version
PUT  /api/v1/admin/prompts/:key         # {content} -> new version becomes active
POST /api/v1/admin/prompts/:key/restore # reset to the built-in default
```

Keys: `commander.normalize`, `commander.report`, `agent.workflow_tools`,
`agent.workflow_notools`. Runtime falls back to built-ins when the table is
empty. Prompts use `{{PLACEHOLDER}}` tokens; malformed overrides degrade to the
built-in rather than failing the run.

### Case list pagination
`GET /api/v1/cases?page=&page_size=` is scoped to the caller and returns
`{cases,total,page,page_size}`; `PATCH /cases/:id` toggles `pinned`/`archived`
and `DELETE /cases/:id` removes a case with all artifacts in one transaction.
`DELETE /datasets/:id` cancels in-flight runs and removes the dataset, items,
runs and results.
