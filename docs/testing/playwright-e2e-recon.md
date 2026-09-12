# Playwright E2E — coverage recon

Baseline `main@9f69872`. This is a recon only: no Playwright code, no CI job, no
application changes. It answers three questions — what the current frontend
suite already proves, which browser-level risks are therefore unproven, and what
infrastructure an E2E suite needs in order to exercise the **real** path
(browser → frontend → HTTP → backend → MySQL).

## 1. What the existing suite proves, and how

32 vitest files / 188 tests, all in jsdom with module-level mocks: no real HTTP
request, no real cookie, no real SSE connection, no real navigation.

| Area | File | What it pins | Mock boundary |
| --- | --- | --- | --- |
| HTTP layer | `frontend/src/api/__tests__/client.test.ts` | 401 clears the stored key and dispatches `magi:unauthorized`; `verifyAuth` tri-state (valid/invalid/unavailable) | `fetch` stubbed |
| SSE consumer | `frontend/src/api/stream.test.ts` | frame parsing, `id:` watermark, `Last-Event-ID` on reconnect, EOF reconnect, CRLF framing, gap detection, 401 stop | fake `fetch` + hand-built `ReadableStream` |
| Event mapping | `frontend/src/api/eventMapper.test.ts` | backend event type → display type | none |
| Case store | `frontend/src/stores/__tests__/caseStore.test.ts` | stale `fetchCase` discard, mutation scoping, list patching | `api` mocked |
| Workspace page | `frontend/src/pages/__tests__/DecisionWorkspace.test.tsx` | create form, `fetchCase` invoked, agents/events load, run button, fork + navigate, loading gate | `@/stores`, `@/api/client`, `@/api/stream` all mocked |
| Login page | `frontend/src/pages/__tests__/Login.test.tsx` | valid / invalid / unavailable; candidate key never persisted before verification | `api.verifyAuth` mocked |
| Panels, graph, nav, timeline | component tests | rendering and selectors from seeded store state | stores mocked |

Consequences that shape the E2E design:

- Nothing exercises the router (`createBrowserRouter`) or a real navigation.
- Nothing exercises a real session — no cookie, no `X-API-Key` on the wire.
- Nothing exercises a real SSE connection; the stream is synthetic.
- The cross-case race we fixed is pinned **only** at store/component level with
  mocked stores, so the browser-level ordering is still unproven.

## 2. How the app actually runs (the path an E2E must use)

- `make dev` → `scripts/serve.sh --dev`: middleware containers + Vite (:5173) +
  Go server (:8080), with `VITE_PROXY_TARGET` wiring Vite to the backend.
- Vite proxies **only `/api`** (`frontend/vite.config.ts`). `/auth/oidc/*` is not
  proxied, so an OIDC login flow through the dev server needs the production path
  (nginx) or a proxy entry.
- MSW is **off** unless `VITE_USE_MSW=true` (`frontend/src/mock/browser.ts`), so
  the default dev app already talks to the real backend — a mock backend would
  duplicate the existing vitest coverage.
- No `test:e2e` script, no Playwright dependency, no `e2e/` directory.
- Two credential kinds plus OIDC: `auth.static_tokens` (service credentials) and
  DB-issued user keys. The browser stores a user API key in `localStorage` and
  sends `X-API-Key`; OIDC sets an HttpOnly `magi_session` cookie that the backend
  re-validates per request (503 if the user store is unreadable).
- **There is no explicit session probe.** The first authenticated call on load is
  `LeftNav`'s `fetchCases()` (`GET /api/v1/cases`); nothing calls `/me` except
  Settings on demand, and `AppShell` only reacts to `magi:unauthorized` (401) by
  navigating to `/login`. So "reload keeps you signed in" depends on whichever
  credential the browser replays (localStorage key or OIDC cookie), and
  "401 redirects to /login" is driven by that first request — both real,
  currently unverified browser behaviours.
- 503 is deliberately *not* unauthorized: `client.ts` throws a generic error and
  keeps the stored key, and `stream.ts` treats it as a retryable failure. Worth
  pinning so a future change cannot turn an auth-store outage into a logout.
- Case switching is `NavLink to={/case/${id}}` rendered by `LeftNav` →
  `PaginatedSection`; the timeline reads the global `eventStore`.

## 3. Missing browser-level risks

| Flow | Risk | Existing test | Need E2E |
| --- | --- | --- | --- |
| Login + reload (API key or OIDC cookie) | session not restored after reload; cookie path never exercised | unit with mocked `verifyAuth`; no cookie/HTTP | yes |
| 401 mid-session | no redirect to `/login`, or redirect loop | unit only | yes |
| 503 auth-store outage | must not log the user out or look like a bad key | unit (`verifyAuth` tri-state) | yes |
| A→B rapid case switch | B renders A's case/agents/events; A's SSE writes into B | store/component unit | yes |
| Late A response after B selected | same, ordering dependent | store unit only | yes |
| SSE drop → reconnect → resume | duplicated or missing timeline events; lost terminal state | unit with synthetic stream | yes |
| Run lifecycle create → run → pause → resume → completed | UI diverges from the backend state machine | store-level pause/resume only; no UI coverage | yes |
| Run failure → rerun/fork | wrong error state; fork navigation | component (mocked) | yes (failure path needs no model) |
| Report/consensus after completion | final artifacts only after terminal refetch | none at browser level | maybe |

## 4. Proposed E2E suite

```
frontend/e2e/
├ fixtures/
│  ├ backend.ts        # start/stop the real Go server against an isolated schema
│  └ seed.ts           # create cases/events through the real HTTP API
├ auth.spec.ts
├ case-switch.spec.ts
├ sse-resume.spec.ts
└ run-lifecycle.spec.ts
```

- `auth.spec.ts` — sign in with a real key, reload and assert still authenticated;
  corrupt/revoke the key and assert the next request redirects to `/login`;
  simulate a 503 on `/api/v1/status` and assert the key is **kept** and no logout
  happens.
- `case-switch.spec.ts` — seed two cases with distinguishable timelines, open A,
  click B immediately, assert header/timeline/graph all belong to B and that A's
  stream (aborted mid-flight) cannot repopulate B; also delay A's `/agents`
  response past B's and assert the same.
- `sse-resume.spec.ts` — seed durable events, abort the SSE response with
  `page.route()`, let the client reconnect, and assert the timeline is complete
  and duplicate-free and that the terminal state still arrives (the backend
  already supports `Last-Event-ID` + durable replay).
- `run-lifecycle.spec.ts` — create → run → pause → resume → terminal, asserting
  the UI state matches the backend state read back through the API (not just the
  rendered label); plus the failure path → Re-run/fork.

## 5. Required test infrastructure (listed only, not implemented)

- Playwright + browsers, a `test:e2e` script, and a config decision: drive the
  Vite dev server (real app, real proxy) or the built bundle behind nginx.
- Real backend: Go server plus MySQL, reusing the pattern from the new
  `MySQL migration compatibility` job (service container + DSN) with an isolated
  schema per run, and a test config that declares `auth.static_tokens`.
- **Deterministic model for the success path.** There is no stub/fake model in
  non-test code: the agent loop and commander require six structured outputs
  (`DecisionTask`, `FinalReportData`, `EvidenceSummary`, `Vote`,
  `ClaimSubmission`, `Reflection`) plus optional tool calls, all validated
  against JSON Schema. Either a scripted OpenAI-compatible fake server addressed
  via `MAGI_MODEL_BASE_URL`, or a real provider (cost/flake). The failure path
  needs no model (defaults are 3 attempts with a 1s base backoff).
- Seeding/teardown through the real API (`POST /api/v1/cases`,
  `POST /api/v1/cases/:id/run`) and/or direct event inserts for deterministic
  timelines; per-run schema creation like `bootstrap/mysql_migration_test.go`.
- Network fault injection (`page.route()` abort/delay) for the SSE scenarios.
- Timing control for pause/resume: the run must be long enough to pause
  deterministically (e.g. a fake model that delays), and assertions should poll
  backend state rather than sleep.
- OIDC: either a fake issuer (discovery + token + userinfo) or defer the cookie
  path to a later batch — note the `/auth` proxy gap above must be solved first.
- CI: a job separate from the existing five, with the MySQL service, the browser
  cache, and trace/video artifacts on failure.

## 6. Non-goals

- Re-testing rendering, stores or parsers that the vitest suite already covers.
- Mocking the backend: the point of this suite is real HTTP against the real
  server and database.
- Every page: admin users/audit, evaluation, dataset, templates, benchmark and
  knowledge pages are out of scope unless a flow above needs them.

## 7. Open questions before implementation

1. OIDC cookie login in the first E2E batch, or API-key login only (OIDC needs a
   fake issuer plus fixing the `/auth` proxy gap)?
2. Deterministic model: build a small scripted OpenAI-compatible fake server in
   the repo, or point E2E at a real provider?
3. Which runtime should Playwright drive — the Vite dev server, or the built
   bundle behind nginx (closer to production, includes `/auth`)?
4. Reuse the MySQL service-container pattern with an isolated schema per run, or
   reuse the compose middleware stack?
