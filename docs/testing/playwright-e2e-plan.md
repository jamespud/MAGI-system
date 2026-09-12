# Playwright E2E — batch 1 plan

Baseline `main@9f69872` (branch `codex/playwright-e2e`). This freezes the
infrastructure design for the first E2E batch. Plan only — no Playwright code, no
CI job, no application changes.

Decisions taken (from the recon review):

| # | Decision |
| --- | --- |
| 1 | **No OIDC in batch 1** — API-key login only; the cookie path gets its own batch |
| 2 | **In-repo fake OpenAI-compatible model server** — never a real provider |
| 3 | **nginx + production frontend build** — never the Vite dev server |
| 4 | **MySQL service container + an isolated schema per run** — never the dev compose stack |
| 5 | Four specs: `auth`, `case-switch`, `sse-resume`, `run-lifecycle` |

## 1. Topology

```
Playwright (host)
   |
   v   http://127.0.0.1:8080
nginx (nginx:1.27-alpine, docker/nginx/default.conf.template rendered)
   +-- /            -> frontend/dist   (production build, SPA fallback)
   +-- /api/        -> Go server       (real HTTP, real API key / cookie)
   +-- /health      -> Go server
   +-- /metrics, /a2a/, /.well-known/agent-card.json -> Go server
   +-- /auth/       -> NOT routed today (see section 7) - why OIDC is deferred
   v
Go server (built binary, E2E config via MAGI_* env)
   +-- MySQL       (service container; schema magi_e2e_<run> created and dropped per run)
   +-- fake model  (in-repo; base_url -> http://127.0.0.1:9000)
```

Details that keep this close to production while staying simple:

- The template proxies to the literal upstream `magi-server`; the E2E nginx
  container runs with `--network host --add-host magi-server:127.0.0.1`, so the
  template is used **unmodified** and `MAGI_HTTP_PORT` selects the upstream port.
- Only `/api` plus static assets are exercised, which is exactly the surface
  batch 1 needs.
- Nothing runs through Vite, so its `/api`-only proxy cannot mask a real routing
  problem.

## 2. Test lifecycle

```
globalSetup (once per run)
  1 build      frontend/dist (npm run build); Go server binary; fake model binary
  2 db         create schema magi_e2e_<run> on MAGI_E2E_MYSQL_DSN
  3 fake model start on 127.0.0.1:9000 (scenario: normal)
  4 backend    start with MAGI_DB_DSN=<schema>, MAGI_MODEL_* -> fake, MAGI_AUTH_* -> e2e token
  5 nginx      docker run nginx:1.27-alpine (dist + template + add-host)
  6 wait       GET /health until ready; on failure print the backend log tail

per spec
  7 new browser context    (fresh localStorage/cookies - no auth leakage between specs)
  8 scenario control       POST /_control/scenario on the fake model
  9 seed through the real API (fixtures/seed.ts)
 10 assert in the UI, and read state back from the API where state is the point

globalTeardown (always)
 11 stop nginx, backend, fake model
 12 drop schema magi_e2e_<run>
```

Rules:

- **Serial execution for batch 1** (`workers: 1`): the stack is shared and the run
  lifecycle spec is timing-sensitive.
- No `waitForTimeout` as an assertion; poll backend state
  (`GET /api/v1/cases/:id`) instead of sleeping.
- Seed through HTTP, not SQL, except where a deterministic event timeline is the
  point (then insert events and assert SSE replay).

## 3. Fixture ownership

```
e2e/
+-- fixtures/
|   +-- backend.ts   # owns the stack: build/start/stop, schema lifecycle, health wait, logs
|   +-- seed.ts      # owns data: create cases/events through the real API; returns ids
|   +-- browser.ts   # owns the client: context/page factory, API-key login, navigation helpers
+-- scenarios/
|   +-- auth.ts      # login / reload / bad key / auth-store outage (route-injected 503)
|   +-- cases.ts     # two distinguishable cases + a controllable A->B switch
|   +-- runs.ts      # create -> run -> pause -> resume -> terminal; failure -> re-run/fork
+-- auth.spec.ts
+-- case-switch.spec.ts
+-- sse-resume.spec.ts
+-- run-lifecycle.spec.ts
```

- One owner per concern, so later suites reuse `backend.ts`/`browser.ts` instead
  of re-implementing login and stack startup.
- `backend.ts` is the only place that knows ports, paths and env; specs never read
  `process.env` directly.
- Each spec namespaces its own data by the case id it seeds, and the schema is
  dropped at the end, so a re-run is always clean.

## 4. Fake model contract

Language: **Go**, in `tools/fake-openai/`, built with the repo toolchain — no new
runtime dependency, and its fixtures can be unit-tested by the Go suite.

### Protocol

| Surface | Behaviour |
| --- | --- |
| `POST /v1/chat/completions` | OpenAI-compatible. `stream: false` is the supported path: the runtime calls `Generate` (`domain/modelruntime/runtime.go`), not `Stream` |
| `POST /_control/scenario` | `{"name":"normal\|slow\|fail\|invalid","delay_ms":0}` — switches the next requests and returns the active scenario |
| `POST /_control/reset` | Back to `normal`, clears counters |
| `GET /health` | Readiness probe used by the stack fixture |

### How it decides which shape to return

The prompt itself is the discriminator — no hidden coupling to internals:

| Request marker (last user message) | Response content |
| --- | --- |
| `Output the DecisionTask JSON now.` | `DecisionTask` |
| `Output the FinalReportData JSON now.` (and the re-output variant) | `FinalReportData` |
| `Evidence gate passed. Now output the Vote JSON.` / `Reflection recorded. Now output the Vote JSON.` | `Vote` (role-specific) |
| `Evidence gate passed. Now output the Reflection JSON.` | `Reflection` (debate only; batch 1 avoids debate) |
| anything else (initial question, claim feedback, fix hint, compaction) | `EvidenceSummary`; compaction/unknown → plain-text fallback |

Role selection: the system prompt contains `You are the MAGI role <code>` plus the
role's objective dimensions, so the fake returns the matching per-role `Vote`.

**Failure must be loud**: an unclassifiable request returns HTTP 500 with a
descriptive body, so a mis-wired run fails visibly instead of producing a bad
state.

### Fixtures must be schema-valid and must not drift

- Batch 1 binds **no tools** and uses an E2E `magi:` config with empty
  `custom_rules`, so `RelaxEvidenceStandard` (no tools ⇒ no count/type
  requirements) leaves a trivially satisfiable gate. Without that, melchior's
  `utility_dimension_coverage` rule would demand two evidence type keys that a
  tool-less run cannot produce.
- A Go unit test loads the fake server's JSON fixtures and runs them through the
  real `validation.TypedValidator` for each entity, so a schema change breaks that
  test rather than the E2E suite.
- `Vote` fixtures are **per role** and must carry exactly that role's utility
  dimensions (`ValidateVoteDimensions` rejects missing or extra ones):
  melchior = correctness/efficiency/feasibility,
  balthasar = safety/reversibility/maintainability,
  casper = opportunity/innovation/user_value.
- All three agents vote the same way under `normal`, so round 1 is unanimous and
  no debate/reflection path is entered — that keeps batch 1 deterministic.

### Scenarios

| Scenario | Behaviour | Used by |
| --- | --- | --- |
| `normal` | schema-valid responses, no delay | auth, case-switch, SSE, happy-path run |
| `slow` | configurable per-response delay | run-lifecycle pause/resume (the run must stay RUNNING long enough) |
| `fail` | HTTP 500 | run failure → re-run/fork |
| `invalid` | HTTP 200 with malformed JSON | optional, not batch 1 |

## 5. Spec outline (behavioural contract)

- **`auth.spec.ts`** — sign in with the E2E static token; reload and assert still
  authenticated (localStorage replay plus `GET /cases` succeeding); replace the key
  with a bad one and assert the next request redirects to `/login`; inject a 503 on
  `/api/v1/status` via `page.route()` and assert the key is **kept** and no logout
  happens.
- **`case-switch.spec.ts`** — seed A and B with distinguishable questions; open A,
  start its stream, click B immediately; assert header, timeline and graph all
  belong to B; then release a delayed A response and assert B is unchanged. This is
  the browser-level version of the store-level race tests and the highest-value spec.
- **`sse-resume.spec.ts`** — drive a run, abort the SSE response with
  `page.route()`, let the client reconnect, and assert the timeline is complete,
  duplicate-free, and that the terminal state appears exactly once (real
  `Last-Event-ID` + durable replay).
- **`run-lifecycle.spec.ts`** — create → run (`slow`) → pause → resume → completed,
  asserting the UI matches `GET /api/v1/cases/:id` at each step; then `fail` →
  failure state → Re-run/fork.

## 6. CI execution plan

A new job **separate from the existing five** (a failure here means integration or
infrastructure drift, not a broken unit):

```
name: E2E (Playwright)
services: mysql:8.4            # same image/health pattern as MySQL migration compatibility
steps:
  1  checkout
  2  setup-go + cache (go.sum)
  3  setup-node + cache (frontend/package-lock.json) + npm ci
  4  build frontend dist (npm --prefix frontend run build)
  5  build go server (go build ./cmd/magi-server) and fake model (go build ./tools/fake-openai)
  6  create schema (MAGI_E2E_MYSQL_DSN; same pattern as bootstrap/mysql_migration_test.go)
  7  start fake model, backend, nginx (host network + --add-host magi-server:127.0.0.1)
  8  npx playwright test (workers=1, PLAYWRIGHT_BASE_URL=http://127.0.0.1:8080)
  9  upload trace/video on failure (actions/upload-artifact, if: failure())
 10  always stop processes and drop the schema
```

- Cache `~/.cache/ms-playwright` keyed on the Playwright version.
- Budget: stack build/start under ~3 min, four specs under ~2 min — keep the job
  under 10 minutes.
- **Not a required check initially**, same stance as the MySQL job: watch a few
  runs, then add it to the required list together with the others.
- Local reproduction: the same steps sit behind one entry point
  (`e2e/fixtures/backend.ts`), so a developer can run the suite against a local
  MySQL without CI.

## 7. Known gaps and follow-ups (explicitly out of batch 1)

- **nginx has no `/auth/` location at all** (`docker/nginx/default.conf.template`
  proxies `/api`, `/health`, `/metrics`, `/a2a/` and
  `/.well-known/agent-card.json` only) and Vite proxies only `/api`. OIDC login
  therefore cannot complete through either topology today; the OIDC batch must add
  that route — a production fix, not just test wiring.
- OIDC cookie login, fake issuer, session expiry/revocation UI — separate batch
  (`codex/playwright-oidc-e2e`).
- Tool-bound runs (fake tool calls), debate/reflection paths, report/consensus
  rendering, evaluation/benchmark/admin pages — not batch 1.
- Parallel workers and per-worker stacks — not batch 1 (`workers: 1`).
