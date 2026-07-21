# Real Tavily web_search Tool Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Give the three MAGI agents a real `web_search` tool backed by Tavily so they collect evidence (one record per search result), populating the Evidence Graph with evidence nodes + claim->evidence links.

**Architecture:** New `TavilyToolExecutor` (calls Tavily search API) + `LocalToolRegistry` (resolves the web_search definition) in the adapter layer; a `TavilyAdapter` (EvidenceAdapter) in domain/evidence parses Tavily results into one EvidenceCandidate per result. Bootstrap conditionally wires Tavily (when API key present) vs stub, and injects TavilyAdapter ahead of NativeAdapter in the agent loop's evidence registry.

**Tech Stack:** Go 1.24, `net/http`, `encoding/json`, GORM, Uber Fx, Hertz. Tests: `go test`, `net/http/httptest`.

## Global Constraints

- Go module `github.com/jamespud/magi/backend`, Go 1.24+. Domain (`domain/`) depends only on `domain/port` + eino; Coze impls in `adapter/`.
- No new external Go deps -- use `net/http` + `encoding/json` for the Tavily call.
- Structured-output structs need `json` tags (ADR-003).
- Conventional Commits (`feat:`, `fix:`, `test:`).
- Backend tests: `cd backend && go test ./...`; single test `go test ./path/ -run Name -v`.
- TDD: write failing test, watch it fail, minimal code, watch pass, commit.
- Tavily endpoint: `https://api.tavily.com/search` (POST JSON `{api_key, query, include_answer}`).

---

## File Structure

- `backend/domain/evidence/tavily_adapter.go` (create) -- `TavilyResponse`/`TavilyResult` types + `TavilyAdapter` (EvidenceAdapter).
- `backend/domain/evidence/tavily_adapter_test.go` (create) -- adapter unit tests.
- `backend/adapter/tavily_tool.go` (create) -- `TavilyToolExecutor` (ToolExecutorPort) + `LocalToolRegistry` (ToolRegistryPort).
- `backend/adapter/tavily_tool_test.go` (create) -- executor + registry tests.
- `backend/bootstrap/config.go` (modify) -- `Config.Tavily` + `MagiSpec.ToConfig` binds web_search.
- `backend/bootstrap/config_test.go` (create/modify) -- config parse + ToConfig tests.
- `backend/bootstrap/container.go` (modify) -- `provideToolRegistry`/`provideToolExecutor` + `provideAgentLoop` signature + TavilyAdapter injection.
- `backend/bootstrap/container_test.go` (modify) -- factory selection test.
- `backend/domain/runtime/agent_loop_test.go` (modify) -- integration test with stubbed Tavily HTTP.

---

## Task 1: TavilyAdapter + TavilyResponse types

**Files:**
- Create: `backend/domain/evidence/tavily_adapter.go`
- Create: `backend/domain/evidence/tavily_adapter_test.go`

**Interfaces:**
- Produces: `TavilyResponse` struct, `TavilyResult` struct, `TavilyAdapter` (implements `EvidenceAdapter`). `NewTavilyAdapter() *TavilyAdapter`. `TavilyAdapter.Supports(tool port.ToolDefinition) bool`, `TavilyAdapter.Extract(ctx, tool, result) ([]EvidenceCandidate, error)`.

- [ ] **Step 1: Write the failing test**

`backend/domain/evidence/tavily_adapter_test.go`:

```go
package evidence_test

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/jamespud/magi/backend/domain/entity"
	"github.com/jamespud/magi/backend/domain/evidence"
	"github.com/jamespud/magi/backend/domain/port"
)

func TestTavilyAdapter_SupportsOnlyWebSearch(t *testing.T) {
	a := evidence.NewTavilyAdapter()
	webSearch := port.ToolDefinition{Name: "web_search", Binding: entity.ToolBinding{ToolName: "web_search"}}
	other := port.ToolDefinition{Name: "calc"}
	if !a.Supports(webSearch) {
		t.Fatal("should support web_search")
	}
	if a.Supports(other) {
		t.Fatal("should not support other tools")
	}
}

func TestTavilyAdapter_ExtractOnePerResult(t *testing.T) {
	a := evidence.NewTavilyAdapter()
	resp := evidence.TavilyResponse{
		Answer: "yes",
		Results: []evidence.TavilyResult{
			{Title: "t1", URL: "https://a.example", Content: "content A", Score: 0.9},
			{Title: "t2", URL: "https://b.example", Content: "content B", Score: 0.8},
			{Title: "t3", URL: "https://c.example", Content: "content C", Score: 0.7},
		},
	}
	tool := port.ToolDefinition{Name: "web_search", Binding: entity.ToolBinding{Source: entity.ToolSourceLocal, ToolName: "web_search"}}
	cands, err := a.Extract(context.Background(), tool, &port.ToolExecutionResult{Structured: &resp})
	if err != nil {
		t.Fatalf("extract: %v", err)
	}
	if len(cands) != 3 {
		t.Fatalf("expected 3 candidates, got %d", len(cands))
	}
	if cands[0].Observation != "content A" || cands[0].SourceURI != "https://a.example" {
		t.Fatalf("candidate 0: %+v", cands[0])
	}
	for _, c := range cands {
		if c.Reliability.Final <= 0 {
			t.Fatalf("reliability not computed: %+v", c.Reliability)
		}
	}
}

func TestTavilyAdapter_ExtractFallsBackToRawWhenNoStructured(t *testing.T) {
	a := evidence.NewTavilyAdapter()
	tool := port.ToolDefinition{Name: "web_search", Binding: entity.ToolBinding{Source: entity.ToolSourceLocal, ToolName: "web_search"}}
	// Structured nil + Output is a JSON with no results -> fallback to one raw candidate.
	cands, err := a.Extract(context.Background(), tool, &port.ToolExecutionResult{Output: `{"answer":"","results":[]}`})
	if err != nil {
		t.Fatalf("extract: %v", err)
	}
	if len(cands) != 1 {
		t.Fatalf("expected 1 raw fallback candidate, got %d", len(cands))
	}
}

func TestTavilyAdapter_ExtractFromOutputJSON(t *testing.T) {
	a := evidence.NewTavilyAdapter()
	tool := port.ToolDefinition{Name: "web_search", Binding: entity.ToolBinding{Source: entity.ToolSourceLocal, ToolName: "web_search"}}
	body, _ := json.Marshal(evidence.TavilyResponse{
		Results: []evidence.TavilyResult{{URL: "https://x.example", Content: "X"}},
	})
	cands, _ := a.Extract(context.Background(), tool, &port.ToolExecutionResult{Output: string(body)})
	if len(cands) != 1 || cands[0].SourceURI != "https://x.example" {
		t.Fatalf("expected 1 candidate from output JSON, got %+v", cands)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd backend && go test ./domain/evidence/ -run TestTavilyAdapter -v`
Expected: FAIL -- `undefined: evidence.NewTavilyAdapter`, `evidence.TavilyResponse`.

- [ ] **Step 3: Write minimal implementation**

`backend/domain/evidence/tavily_adapter.go`:

```go
package evidence

import (
	"context"
	"encoding/json"

	"github.com/jamespud/magi/backend/domain/entity"
	"github.com/jamespud/magi/backend/domain/port"
)

// TavilyResult is one search result from the Tavily API.
type TavilyResult struct {
	Title   string  `json:"title"`
	URL     string  `json:"url"`
	Content string  `json:"content"`
	Score   float64 `json:"score"`
}

// TavilyResponse is the parsed Tavily search response. Shared with the
// executor (adapter/tavily_tool.go) which produces it.
type TavilyResponse struct {
	Answer  string         `json:"answer"`
	Results []TavilyResult `json:"results"`
}

// TavilyAdapter extracts one EvidenceCandidate per Tavily search result.
// It must be registered BEFORE NativeAdapter (which always Supports) in the
// EvidenceAdapterRegistry.
type TavilyAdapter struct{}

func NewTavilyAdapter() *TavilyAdapter { return &TavilyAdapter{} }

func (a *TavilyAdapter) Supports(tool port.ToolDefinition) bool {
	return tool.Name == "web_search"
}

func (a *TavilyAdapter) Extract(ctx context.Context, tool port.ToolDefinition, result *port.ToolExecutionResult) ([]EvidenceCandidate, error) {
	resp, ok := result.Structured.(*TavilyResponse)
	if !ok || resp == nil {
		// Fall back to parsing the Output JSON.
		var parsed TavilyResponse
		if err := json.Unmarshal([]byte(result.Output), &parsed); err == nil {
			resp = &parsed
		}
	}
	if resp == nil || len(resp.Results) == 0 {
		rel := ComputeReliability(ReliabilityInput{
			SourceType:           tool.Binding.Source,
			ExplicitReliability:  tool.Binding.Reliability,
			Directness:           DirectnessFromSource(tool.Binding.Source),
			Recency:              1.0,
			ExtractionConfidence: 0.3,
		})
		return []EvidenceCandidate{{Observation: result.Output, Reliability: rel}}, nil
	}
	out := make([]EvidenceCandidate, 0, len(resp.Results))
	for _, r := range resp.Results {
		rel := ComputeReliability(ReliabilityInput{
			SourceType:           tool.Binding.Source,
			ExplicitReliability:  tool.Binding.Reliability,
			Directness:           DirectnessFromSource(tool.Binding.Source),
			Recency:              1.0,
			ExtractionConfidence: 0.7,
		})
		out = append(out, EvidenceCandidate{Observation: r.Content, SourceURI: r.URL, Reliability: rel})
	}
	_ = entity.ToolSourceLocal // keep entity import if not otherwise used
	return out, nil
}
```

(If the `entity` import is unused after removing the placeholder line, drop it -- the linter will flag. The `tool.Binding.Source` is already `entity.ToolSource`, passed into `ReliabilityInput.SourceType`.)

- [ ] **Step 4: Run test to verify it passes**

Run: `cd backend && go test ./domain/evidence/ -run TestTavilyAdapter -v`
Expected: PASS (all 4 tests).

- [ ] **Step 5: Commit**

```bash
git add backend/domain/evidence/tavily_adapter.go backend/domain/evidence/tavily_adapter_test.go
git commit -m "feat(evidence): TavilyAdapter extracts one evidence per search result"
```

---

## Task 2: TavilyToolExecutor + LocalToolRegistry

**Files:**
- Create: `backend/adapter/tavily_tool.go`
- Create: `backend/adapter/tavily_tool_test.go`

**Interfaces:**
- Consumes: `evidence.TavilyResponse`, `evidence.TavilyResult` (Task 1); `port.ToolRegistryPort`, `port.ToolExecutorPort`, `port.ToolDefinition`, `port.ToolExecutionRequest`, `port.ToolExecutionResult`.
- Produces: `NewTavilyToolExecutor(apiKey string) *TavilyToolExecutor`, `NewTavilyToolExecutorWithURL(apiKey, baseURL string) *TavilyToolExecutor`, `NewLocalToolRegistry() *LocalToolRegistry`. `TavilyToolExecutor` implements `Execute(ctx, port.ToolExecutionRequest) (*port.ToolExecutionResult, error)`. `LocalToolRegistry` implements `List(ctx, []entity.ToolBinding) ([]port.ToolDefinition, error)`.

- [ ] **Step 1: Write the failing test**

`backend/adapter/tavily_tool_test.go`:

```go
package magi_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/jamespud/magi/backend/domain/entity"
	magi "github.com/jamespud/magi/backend/adapter"
	"github.com/jamespud/magi/backend/domain/evidence"
	"github.com/jamespud/magi/backend/domain/port"
)

func TestTavilyToolExecutor_CallsApiAndParses(t *testing.T) {
	var gotBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewDecoder(r.Body).Decode(&gotBody)
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(evidence.TavilyResponse{
			Answer: "yes",
			Results: []evidence.TavilyResult{
				{Title: "t1", URL: "https://a.example", Content: "A", Score: 0.9},
				{Title: "t2", URL: "https://b.example", Content: "B", Score: 0.8},
			},
		})
	}))
	defer srv.Close()

	exec := magi.NewTavilyToolExecutorWithURL("test-key", srv.URL)
	res, err := exec.Execute(context.Background(), port.ToolExecutionRequest{
		ToolName: "web_search", ArgumentsJSON: `{"query":"rust benchmarks"}`,
	})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if gotBody["api_key"] != "test-key" {
		t.Fatalf("api_key not sent: %+v", gotBody)
	}
	if gotBody["query"] != "rust benchmarks" {
		t.Fatalf("query not sent: %+v", gotBody)
	}
	tr, ok := res.Structured.(*evidence.TavilyResponse)
	if !ok || tr == nil || len(tr.Results) != 2 {
		t.Fatalf("Structured not parsed: %+v", res.Structured)
	}
	if res.SourceURI != "https://a.example" {
		t.Fatalf("SourceURI: %s", res.SourceURI)
	}
}

func TestTavilyToolExecutor_ReturnsErrorOnHTTP500(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(500)
	}))
	defer srv.Close()
	exec := magi.NewTavilyToolExecutorWithURL("k", srv.URL)
	_, err := exec.Execute(context.Background(), port.ToolExecutionRequest{
		ToolName: "web_search", ArgumentsJSON: `{"query":"x"}`,
	})
	if err == nil {
		t.Fatal("expected error on 500")
	}
}

func TestLocalToolRegistry_ListReturnsWebSearchDef(t *testing.T) {
	reg := magi.NewLocalToolRegistry()
	defs, err := reg.List(context.Background(), []entity.ToolBinding{
		{Source: entity.ToolSourceLocal, ToolName: "web_search"},
		{Source: entity.ToolSourceLocal, ToolName: "unknown_tool"},
	})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(defs) != 1 || defs[0].Name != "web_search" {
		t.Fatalf("expected 1 web_search def, got %+v", defs)
	}
}

// Compile-time: implementations satisfy the ports.
var _ port.ToolRegistryPort = (*magi.LocalToolRegistry)(nil)
var _ port.ToolExecutorPort = (*magi.TavilyToolExecutor)(nil)
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd backend && go test ./adapter/ -run 'TestTavilyToolExecutor|TestLocalToolRegistry' -v`
Expected: FAIL -- `undefined: magi.NewTavilyToolExecutorWithURL`, `magi.NewLocalToolRegistry`.

- [ ] **Step 3: Write minimal implementation**

`backend/adapter/tavily_tool.go`:

```go
package magi

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/jamespud/magi/backend/domain/entity"
	"github.com/jamespud/magi/backend/domain/evidence"
	"github.com/jamespud/magi/backend/domain/port"
)

const defaultTavilyURL = "https://api.tavily.com/search"

// webSearchArgsSchema is the JSON Schema for web_search arguments.
const webSearchArgsSchema = `{"type":"object","properties":{"query":{"type":"string","description":"the search query"}},"required":["query"],"additionalProperties":false}`

// LocalToolRegistry resolves the web_search tool definition for any binding
// that requests it. Other bindings are ignored (return no def).
type LocalToolRegistry struct{}

func NewLocalToolRegistry() *LocalToolRegistry { return &LocalToolRegistry{} }

func (r *LocalToolRegistry) List(ctx context.Context, bindings []entity.ToolBinding) ([]port.ToolDefinition, error) {
	out := make([]port.ToolDefinition, 0, len(bindings))
	for _, b := range bindings {
		if b.ToolName == "web_search" {
			out = append(out, port.ToolDefinition{
				Name:       "web_search",
				Desc:       "Search the web for up-to-date information. Returns content snippets and URLs.",
				ArgsSchema: []byte(webSearchArgsSchema),
				Source:     entity.ToolSourceLocal,
				Binding:    b,
			})
		}
	}
	return out, nil
}

// TavilyToolExecutor executes web_search by calling the Tavily search API.
type TavilyToolExecutor struct {
	apiKey  string
	baseURL string
	client  *http.Client
}

func NewTavilyToolExecutor(apiKey string) *TavilyToolExecutor {
	return NewTavilyToolExecutorWithURL(apiKey, defaultTavilyURL)
}

func NewTavilyToolExecutorWithURL(apiKey, baseURL string) *TavilyToolExecutor {
	return &TavilyToolExecutor{
		apiKey:  apiKey,
		baseURL: baseURL,
		client:  &http.Client{Timeout: 15 * time.Second},
	}
}

func (e *TavilyToolExecutor) Execute(ctx context.Context, req port.ToolExecutionRequest) (*port.ToolExecutionResult, error) {
	var args struct {
		Query string `json:"query"`
	}
	if err := json.Unmarshal([]byte(req.ArgumentsJSON), &args); err != nil {
		return nil, fmt.Errorf("tavily: parse args: %w", err)
	}
	body, err := json.Marshal(map[string]any{
		"api_key":        e.apiKey,
		"query":          args.Query,
		"include_answer": true,
	})
	if err != nil {
		return nil, fmt.Errorf("tavily: marshal body: %w", err)
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, e.baseURL, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("tavily: new request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	resp, err := e.client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("tavily: http: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("tavily: http status %d", resp.StatusCode)
	}
	var tr evidence.TavilyResponse
	if err := json.NewDecoder(resp.Body).Decode(&tr); err != nil {
		return nil, fmt.Errorf("tavily: decode: %w", err)
	}
	raw, _ := json.Marshal(tr)
	sourceURI := ""
	if len(tr.Results) > 0 {
		sourceURI = tr.Results[0].URL
	}
	return &port.ToolExecutionResult{
		Output:     string(raw),
		Structured: &tr,
		SourceURI:  sourceURI,
	}, nil
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `cd backend && go test ./adapter/ -run 'TestTavilyToolExecutor|TestLocalToolRegistry' -v`
Expected: PASS (all 3 tests).

- [ ] **Step 5: Commit**

```bash
git add backend/adapter/tavily_tool.go backend/adapter/tavily_tool_test.go
git commit -m "feat(adapter): TavilyToolExecutor + LocalToolRegistry for web_search"
```

---

## Task 3: Config (Tavily key + web_search binding)

**Files:**
- Modify: `backend/bootstrap/config.go`
- Create: `backend/bootstrap/config_test.go`

**Interfaces:**
- Produces: `Config.Tavily.APIKey`; `MagiSpec.ToConfig` sets `Tools: []entity.ToolBinding{{Source: entity.ToolSourceLocal, ToolName: "web_search"}}` on every agent.

- [ ] **Step 1: Write the failing test**

`backend/bootstrap/config_test.go` (package `bootstrap_test` -- but `ToConfig` is on `MagiSpec` which is exported; `Config` fields exported). Use internal package `bootstrap` to access unexported if needed; here all accessed types are exported so `bootstrap_test` works:

```go
package bootstrap_test

import (
	"testing"

	"github.com/jamespud/magi/backend/bootstrap"
	"github.com/jamespud/magi/backend/domain/entity"
)

func TestConfig_TavilyParsed(t *testing.T) {
	cfg, err := bootstrap.LoadConfig("conf/magi.yaml")
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if cfg.Tavily.APIKey == "" {
		t.Fatal("Tavily.APIKey should be parsed from magi.yaml")
	}
}

func TestMagiSpec_ToConfigBindsWebSearch(t *testing.T) {
	cfg, err := bootstrap.LoadConfig("conf/magi.yaml")
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	c := cfg.Magi.Melchior.ToConfig("melchior", cfg)
	if len(c.Tools) != 1 || c.Tools[0].ToolName != "web_search" {
		t.Fatalf("expected web_search bound, got %+v", c.Tools)
	}
	if c.Tools[0].Source != entity.ToolSourceLocal {
		t.Fatalf("expected local source, got %s", c.Tools[0].Source)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd backend && go test ./bootstrap/ -run 'TestConfig_TavilyParsed|TestMagiSpec_ToConfigBindsWebSearch' -v`
Expected: FAIL -- `cfg.Tavily undefined` and/or `c.Tools` empty.

- [ ] **Step 3: Add Tavily to Config + bind web_search in ToConfig**

In `backend/bootstrap/config.go`, add the `Tavily` field to `Config` (after `Database`):

```go
type Config struct {
	Model    struct {
		APIKey    string `yaml:"api_key"`
		BaseURL   string `yaml:"base_url"`
		ModelName string `yaml:"model_name"`
	} `yaml:"model"`
	Magi struct {
		MaxDebateRounds int      `yaml:"max_debate_rounds"`
		MaxSteps        int      `yaml:"max_steps"`
		TimeoutSeconds  int      `yaml:"timeout_seconds"`
		Melchior        MagiSpec `yaml:"melchior"`
		Balthasar       MagiSpec `yaml:"balthasar"`
		Casper          MagiSpec `yaml:"casper"`
	} `yaml:"magi"`
	Database struct {
		Driver string `yaml:"driver"`
		DSN    string `yaml:"dsn"`
	} `yaml:"database"`
	Tavily struct {
		APIKey string `yaml:"api_key"`
	} `yaml:"tavily"`
}
```

In `MagiSpec.ToConfig`, add `Tools` to the returned `entity.MagiConfig` (after `ReflectionPolicy`):

```go
		Tools: []entity.ToolBinding{
			{Source: entity.ToolSourceLocal, ToolName: "web_search"},
		},
```

- [ ] **Step 4: Add the tavily block to magi.yaml**

In `backend/conf/magi.yaml`, append:

```yaml
tavily:
  api_key: "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"
```

- [ ] **Step 5: Run test to verify it passes**

Run: `cd backend && go test ./bootstrap/ -run 'TestConfig_TavilyParsed|TestMagiSpec_ToConfigBindsWebSearch' -v`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add backend/bootstrap/config.go backend/bootstrap/config_test.go backend/conf/magi.yaml
git commit -m "feat(bootstrap): add Tavily api_key config + bind web_search to agents"
```

---

## Task 4: Bootstrap wiring (Tavily registry/executor + adapter injection)

**Files:**
- Modify: `backend/bootstrap/container.go`
- Modify: `backend/bootstrap/container_test.go`

**Interfaces:**
- Consumes: `adapter.NewLocalToolRegistry`, `adapter.NewTavilyToolExecutor`, `evidence.NewTavilyAdapter` (Tasks 1-3); `Config.Tavily.APIKey`.
- Produces: `provideToolRegistry(cfg) port.ToolRegistryPort`, `provideToolExecutor(cfg) port.ToolExecutorPort`; `provideAgentLoop` now takes `port.ToolRegistryPort`/`port.ToolExecutorPort` and injects a `TavilyAdapter`-aware registry.

- [ ] **Step 1: Write the failing test**

Add to `backend/bootstrap/container_test.go` (internal package `bootstrap_test` cannot call unexported `provideToolRegistry`; switch this file to package `bootstrap` internal, OR export the providers). Use internal package by renaming the test file's package to `bootstrap` is invasive. Instead, test the selection via exported helpers. Simplest: make the two providers exported (`ProvideToolRegistry`/`ProvideToolExecutor`). Add them exported and test:

```go
func TestProvideToolRegistry_SelectsByApiKey(t *testing.T) {
	withCfg := &bootstrap.Config{}
	withCfg.Tavily.APIKey = "k"
	with := bootstrap.ProvideToolRegistry(withCfg)
	if _, ok := with.(*magi.LocalToolRegistry); !ok {
		t.Fatalf("expected LocalToolRegistry when key set, got %T", with)
	}
	without := bootstrap.ProvideToolRegistry(&bootstrap.Config{})
	if _, ok := without.(*bootstrap.StubToolRegistry); !ok {
		t.Fatalf("expected StubToolRegistry when no key, got %T", without)
	}
}

func TestProvideToolExecutor_SelectsByApiKey(t *testing.T) {
	withCfg := &bootstrap.Config{}
	withCfg.Tavily.APIKey = "k"
	with := bootstrap.ProvideToolExecutor(withCfg)
	if _, ok := with.(*magi.TavilyToolExecutor); !ok {
		t.Fatalf("expected TavilyToolExecutor when key set, got %T", with)
	}
	without := bootstrap.ProvideToolExecutor(&bootstrap.Config{})
	if _, ok := without.(*bootstrap.StubToolExecutor); !ok {
		t.Fatalf("expected StubToolExecutor when no key, got %T", without)
	}
}
```

(Requires `import magi "github.com/jamespud/magi/backend/adapter"` in the test file. `Config.Tavily.APIKey` is set via field assignment to avoid matching the anonymous struct's yaml tag literally.)

- [ ] **Step 2: Run test to verify it fails**

Run: `cd backend && go test ./bootstrap/ -run 'TestProvideTool' -v`
Expected: FAIL -- `undefined: bootstrap.ProvideToolRegistry`.

- [ ] **Step 3: Implement providers + rewire provideAgentLoop**

In `backend/bootstrap/container.go`:

Add the exported providers (next to `provideDB`):

```go
// ProvideToolRegistry returns a LocalToolRegistry (resolves web_search) when a
// Tavily key is configured, else the no-op StubToolRegistry (no tools).
func ProvideToolRegistry(cfg *Config) port.ToolRegistryPort {
	if cfg.Tavily.APIKey != "" {
		return magi.NewLocalToolRegistry()
	}
	return &StubToolRegistry{}
}

// ProvideToolExecutor returns a TavilyToolExecutor when a Tavily key is
// configured, else the no-op StubToolExecutor.
func ProvideToolExecutor(cfg *Config) port.ToolExecutorPort {
	if cfg.Tavily.APIKey != "" {
		return magi.NewTavilyToolExecutor(cfg.Tavily.APIKey)
	}
	return &StubToolExecutor{}
}
```

Change `provideAgentLoop` to take the port interfaces and inject the TavilyAdapter-aware registry:

```go
func provideAgentLoop(
	modelPort *magi.ModelAdapter,
	toolReg port.ToolRegistryPort,
	toolExec port.ToolExecutorPort,
	val validation.Validator,
	gen validation.SchemaGenerator,
	broker *appserver.EventBroker,
) (*runtime.AgentLoop, error) {
	adapterRegistry := evidence.NewEvidenceAdapterRegistry(
		evidence.FullReliabilityResolver(),
		evidence.NewTavilyAdapter(),
		evidence.NewNativeAdapter(),
		evidence.NewRawObservationAdapter(),
	)
	return runtime.NewAgentLoop(runtime.AgentLoopDeps{
		ModelPort: modelPort, ToolReg: toolReg, ToolExec: toolExec,
		Validator: val, Gen: gen, EventPub: broker, Adapter: adapterRegistry,
	})
}
```

Add the imports `"github.com/jamespud/magi/backend/domain/evidence"` and ensure `port` is imported.

In the `fx.Provide(...)` list, replace:
```go
		func() *StubToolRegistry { return &StubToolRegistry{} },
		func() *StubToolExecutor { return &StubToolExecutor{} },
```
with:
```go
		ProvideToolRegistry,
		ProvideToolExecutor,
```

- [ ] **Step 4: Run test to verify it passes + build**

Run: `cd backend && go test ./bootstrap/ -run 'TestProvideTool' -v && go build ./...`
Expected: PASS; build clean.

- [ ] **Step 5: Run full suite (ensure provideAgentLoop signature change didn't break fx wiring)**

Run: `cd backend && go test ./...`
Expected: all packages PASS.

- [ ] **Step 6: Commit**

```bash
git add backend/bootstrap/container.go backend/bootstrap/container_test.go
git commit -m "feat(bootstrap): wire Tavily tool registry/executor + inject TavilyAdapter"
```

---

## Task 5: Integration test -- agent_loop produces evidence via stubbed Tavily

**Files:**
- Modify: `backend/domain/runtime/agent_loop_test.go`

**Interfaces:**
- Consumes: `TavilyToolExecutor` (with `NewTavilyToolExecutorWithURL` against an httptest server), `LocalToolRegistry`, `TavilyAdapter` (via the agent loop's adapter registry).
- Produces: a test proving the full tool-call -> evidence pipeline yields multiple evidence records.

- [ ] **Step 1: Write the failing test**

Append to `backend/domain/runtime/agent_loop_test.go`:

```go
func TestAgentLoop_WebSearchProducesMultipleEvidence(t *testing.T) {
	// Stub Tavily server returning 2 results.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(evidence.TavilyResponse{
			Results: []evidence.TavilyResult{
				{URL: "https://a.example", Content: "result A content", Score: 0.9},
				{URL: "https://b.example", Content: "result B content", Score: 0.8},
			},
		})
	}))
	defer srv.Close()

	gen := validation.NewReflectSchemaGenerator()
	val := validation.NewJSONSchemaValidator()
	toolSchema, _ := gen.FromStruct(struct{ Query string `json:"query"` }{})
	toolReg := &stubToolReg{defs: []port.ToolDefinition{{
		Name: "web_search", Desc: "search", ArgsSchema: toolSchema,
		Source: entity.ToolSourceLocal, Binding: entity.ToolBinding{Source: entity.ToolSourceLocal, ToolName: "web_search"},
	}}}
	toolExec := magi.NewTavilyToolExecutorWithURL("k", srv.URL)
	rec := &recordingEventPub{}
	loop, err := runtime.NewAgentLoop(runtime.AgentLoopDeps{
		ModelPort: &stubModelPort{m: &scriptedChatModel{responses: []*schema.Message{
			callMsg("c1", "web_search", `{"query":"rust"}`),
			finalMsg(summaryJSON("EV-001")),
			finalMsg(voteJSON("correctness")),
		}}},
		ToolReg:   toolReg,
		ToolExec:  toolExec,
		Validator: val, Gen: gen,
		EventPub:  rec,
	})
	if err != nil {
		t.Fatalf("new: %v", err)
	}
	res, err := loop.Run(context.Background(), evidenceCfg(1, 0), &runtime.AgentContext{CaseID: "c1", Task: entity.DecisionTask{CanonicalQuestion: "compute"}})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if res.Ledger == nil {
		t.Fatal("no ledger")
	}
	evs := res.Ledger.List()
	if len(evs) != 2 {
		t.Fatalf("expected 2 evidence records (one per Tavily result), got %d", len(evs))
	}
	if evs[0].Observation != "result A content" {
		t.Fatalf("evidence 0 observation: %s", evs[0].Observation)
	}
}
```

Add the new imports to the test file: `net/http`, `net/http/httptest`, `encoding/json`, `magi "github.com/jamespud/magi/backend/adapter"`, `"github.com/jamespud/magi/backend/domain/evidence"`. (`callMsg`, `finalMsg`, `summaryJSON`, `voteJSON`, `evidenceCfg`, `stubToolReg`, `stubModelPort`, `scriptedChatModel`, `recordingEventPub` already exist in the test file.)

- [ ] **Step 2: Run test to verify it fails**

Run: `cd backend && go test ./domain/runtime/ -run TestAgentLoop_WebSearchProducesMultipleEvidence -v`
Expected: FAIL -- the default adapter registry (NativeAdapter) produces 1 evidence from the whole Output, not 2. (The fix is wiring TavilyAdapter into the loop's registry -- but Task 4 already injects it in bootstrap, NOT in `runtime.NewAgentLoop`'s default. The default registry in agent_loop.go:84 is Native+Raw only. So this test fails until the default registry includes TavilyAdapter OR the test injects it.)

- [ ] **Step 3: Make the agent_loop default registry include TavilyAdapter**

In `backend/domain/runtime/agent_loop.go`, change the default adapter registry construction (the `if adapter == nil` branch) to prepend `TavilyAdapter`:

```go
	adapter := d.Adapter
	if adapter == nil {
		adapter = evidence.NewEvidenceAdapterRegistry(evidence.FullReliabilityResolver(),
			evidence.NewTavilyAdapter(),
			evidence.NewNativeAdapter(),
			evidence.NewRawObservationAdapter())
	}
```

(Bootstrap's explicit registry from Task 4 already does this; the default path now matches, so tests that don't inject an adapter also get Tavily support.)

- [ ] **Step 4: Run test to verify it passes**

Run: `cd backend && go test ./domain/runtime/ -run TestAgentLoop_WebSearchProducesMultipleEvidence -v`
Expected: PASS (2 evidence records).

- [ ] **Step 5: Run full suite**

Run: `cd backend && go test ./...`
Expected: all packages PASS.

- [ ] **Step 6: Commit**

```bash
git add backend/domain/runtime/agent_loop.go backend/domain/runtime/agent_loop_test.go
git commit -m "test(runtime): web_search via Tavily produces one evidence per result"
```

---

## Self-Review Notes

- **Spec coverage:** Section 1 (executor + registry) -> Task 2. Section 2 (TavilyAdapter + types) -> Task 1. Section 3 (config + bootstrap) -> Task 3 + Task 4. Integration -> Task 5. All spec sections covered; knowledge base explicitly out of scope (separate spec).
- **Placeholder scan:** No TBD/TODO; every code step has full code. Test stubs (`stubToolReg`, `recordingEventPub`, etc.) reference existing helpers in `agent_loop_test.go` (confirmed present in earlier tasks). The `entity.ToolSourceLocal` placeholder line in Task 1 Step 3 is called out for removal if the `entity` import goes unused.
- **Type consistency:** `evidence.TavilyResponse`/`TavilyResult` defined in Task 1, used by Task 2 (executor) and Task 5 (test). `NewTavilyToolExecutorWithURL(apiKey, baseURL)` signature consistent across Task 2/4/5. `ProvideToolRegistry`/`ProvideToolExecutor` exported names consistent between Task 4 test + impl. `LocalToolRegistry`/`TavilyToolExecutor` types consistent. The default adapter registry in Task 5 Step 3 matches Task 4's bootstrap registry (TavilyAdapter prepended).
- **Known friction:** Task 4's test sets `Config.Tavily.APIKey` via field assignment (not an anonymous-struct literal, which would require matching the yaml tag). Task 5 depends on Task 4's TavilyAdapter injection OR the default-registry change in Task 5 Step 3 (both done).
