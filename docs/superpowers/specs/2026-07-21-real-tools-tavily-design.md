# Real Tools (Tavily web_search) Design

**Date:** 2026-07-21
**Status:** Approved (brainstorming complete)
**Depends on:** Backend rich-data plan (merged) -- tool-call persistence + evidence/claim namespacing.

## Problem

The Evidence Graph is empty because agents collect no evidence. In standalone mode `StubToolRegistry.List` returns nil, so agents have no tools (`hasTools=false`), the evidence standard is relaxed, and agents reason from intrinsic knowledge. Their claims' `supports` arrays hold self-invented semantic labels (`"safety_concern"`) instead of EV-IDs, so there are no evidence nodes and no claim->evidence links. The graph shows only claim + vote nodes with claim->vote links.

The evidence extraction pipeline itself works: `EvidenceAdapterRegistry.Extract` -> `NativeAdapter` turns a tool result's `Output` into an evidence record. The missing piece is a real tool that returns real search results.

## Scope

**In scope:**
- A real `web_search` tool backed by the Tavily Search API, bound to all three agents.
- One evidence record per Tavily result (a `TavilyAdapter` parses the structured response into N `EvidenceCandidate`s).
- Config + bootstrap wiring: Tavily API key in config; conditional registry/executor (Tavily when key present, stub fallback when absent); `TavilyAdapter` injected into the agent loop's evidence adapter registry.

**Out of scope (separate spec):**
- Knowledge base + previous-conclusion retrieval (sub-project 2: `KnowledgePort` + `ContextBuilder` wiring).
- Additional tools (`url_fetch`, news search) -- YAGNI for now.
- Per-agent tool customization -- all three agents get `web_search`.

## Architecture

Three units, each independently testable:

```
MagiConfig.Tools=[web_search]
  -> LocalToolRegistry.List returns web_search ToolDefinition
    -> agent_loop binds tool to model; LLM emits web_search(query)
      -> TavilyToolExecutor.Execute calls Tavily API
        -> ToolExecutionResult{Output: json, Structured: TavilyResponse}
          -> TavilyAdapter.Extract -> one EvidenceCandidate per result
            -> ledger.Record each -> evidence rows (EV-ID + url)
              -> LLM cites EV-IDs in claim supports
                -> Evidence Graph: evidence nodes + claim->evidence links
```

## Section 1: Tavily tool executor + registry

**New file `backend/adapter/tavily_tool.go`:**

- `LocalToolRegistry` (implements `port.ToolRegistryPort`): holds the `web_search` `ToolDefinition` (Name=`web_search`, Desc, ArgsSchema=`{"type":"object","properties":{"query":{"type":"string"}},"required":["query"]}`, Source=ToolSourceLocal). `List(bindings)` returns the def once for each binding whose `ToolName=="web_search"`; other bindings ignored. `NewLocalToolRegistry()`.

- `TavilyToolExecutor` (implements `port.ToolExecutorPort`): `Execute` unmarshals `ArgumentsJSON` to `{Query string}`, `POST https://api.tavily.com/search` with JSON body `{api_key, query, include_answer: true}` (15s timeout via `net/http`), unmarshals the response into `TavilyResponse`, returns `ToolExecutionResult{Output: <raw json string>, Structured: <TavilyResponse>, SourceURI: <first result URL or "">}`. On HTTP/parse error returns the error (agent_loop emits `TOOL_CALL_FAILED`). `NewTavilyToolExecutor(apiKey string)`.

- `TavilyResponse` / `TavilyResult` types live in `domain/evidence/tavily_adapter.go` (next section) to avoid an import cycle; the executor imports them from there.

**Tests:** `TavilyToolExecutor` against an `httptest.Server` stubbing Tavily -- assert request body has `api_key`+`query`, response parsed into `Structured`, `SourceURI` set, error path returns error. `LocalToolRegistry.List` returns the web_search def for a web_search binding, empty for unknown.

## Section 2: TavilyAdapter (evidence extraction)

**New file `backend/domain/evidence/tavily_adapter.go`:**

- `TavilyResponse struct { Answer string; Results []TavilyResult }` and `TavilyResult struct { Title, URL, Content string; Score float64 }` -- shared types (executor imports these).
- `TavilyAdapter` (implements `EvidenceAdapter`):
  - `Supports(tool port.ToolDefinition) bool` -> `tool.Name == "web_search"`.
  - `Extract(ctx, tool, result) ([]EvidenceCandidate, error)`:
    - Resolve the response: prefer `result.Structured` asserted as `*TavilyResponse`; if nil, unmarshal `result.Output` into `TavilyResponse`.
    - If `len(Results) == 0`: fall back to one candidate `{Observation: result.Output}` (raw).
    - Else: one `EvidenceCandidate` per result: `Observation: r.Content`, `SourceURI: r.URL`, `Reliability: ComputeReliability(ReliabilityInput{SourceType: tool.Binding.Source, ExplicitReliability: tool.Binding.Reliability, Directness: DirectnessFromSource(tool.Binding.Source), Recency: 1.0, ExtractionConfidence: 0.7})`.
- **Priority:** `TavilyAdapter` is placed BEFORE `NativeAdapter` in the `EvidenceAdapterRegistry` (the registry returns the first `Supports`-ing adapter; NativeAdapter always supports, so TavilyAdapter must precede it). TavilyAdapter only supports web_search, so other tools still route to NativeAdapter.

**Tests:** `Supports` true for web_search, false otherwise. `Extract` on a 3-result `TavilyResponse` returns 3 candidates with matching Content/URL. Empty results falls back to 1 raw candidate.

## Section 3: config + bootstrap wiring

**Config (`backend/bootstrap/config.go` + `conf/magi.yaml`):**
- `Config` gains `Tavily struct { APIKey string \`yaml:"api_key"\` }`.
- `magi.yaml` gains:
  ```yaml
  tavily:
    api_key: "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"
  ```
- `MagiSpec.ToConfig` sets `Tools: []entity.ToolBinding{{Source: entity.ToolSourceLocal, ToolName: "web_search"}}` on every agent (the binding is a declaration; whether the tool resolves depends on the registry).

**Bootstrap (`backend/bootstrap/container.go`):**
- `provideAgentLoop` signature changes `toolReg *StubToolRegistry, toolExec *StubToolExecutor` -> `toolReg port.ToolRegistryPort, toolExec port.ToolExecutorPort`. It builds `evidence.NewEvidenceAdapterRegistry(evidence.FullReliabilityResolver(), evidence.NewTavilyAdapter(), evidence.NewNativeAdapter(), evidence.NewRawObservationAdapter())` and passes it as the `Adapter` dep (so TavilyAdapter is consulted first).
- New `provideToolRegistry(cfg *Config) port.ToolRegistryPort`: `cfg.Tavily.APIKey != ""` -> `adapter.NewLocalToolRegistry()`; else `&StubToolRegistry{}`.
- New `provideToolExecutor(cfg *Config) port.ToolExecutorPort`: `cfg.Tavily.APIKey != ""` -> `adapter.NewTavilyToolExecutor(cfg.Tavily.APIKey)`; else `&StubToolExecutor{}`.
- `fx.Provide` replaces the two `func() *StubXxx{...}` providers with `provideToolRegistry`, `provideToolExecutor`.

**Behavior:**
- With Tavily key: agents get `web_search`, real evidence flows, Evidence Graph populates with evidence nodes + claim->evidence links.
- Without key: `StubToolRegistry.List` returns nil -> `hasTools=false` -> relaxed mode (current behavior preserved).

**Tests:**
- `config_test`: parse `tavily.api_key`; `ToConfig` sets `Tools=[web_search]` on each agent.
- Factory selection: `provideToolRegistry`/`provideToolExecutor` return Tavily impls when key set, stubs when empty.
- Integration (existing `agent_loop_test` patterns + new): `TavilyAdapter` unit test; an agent_loop test with a stubbed Tavily HTTP server asserting multiple evidence records are produced and persisted.

## Risks

- **API key in config.** `magi.yaml` already stores the DeepSeek key the same way; `magi.local.yaml` (gitignored) can override. Same convention.
- **LLM EV-ID citation.** Whether claims' `supports` reference EV-IDs depends on the model following the prompt's `["EV-001"]` example. Even if the LLM omits them, evidence nodes still appear (only claim->evidence links would be sparse). DeepSeek has historically followed the format in this codebase.
- **Tavily rate limits / failures.** The executor returns errors on HTTP failure; agent_loop emits `TOOL_CALL_FAILED` and the loop's failure policy handles retries/termination. No special retry beyond what the loop already does.
- **Graceful fallback.** No Tavily key -> stub behavior -> no regression for users without a key.
