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

// dbQueryArgsSchema is the JSON Schema for db_query arguments.
const dbQueryArgsSchema = `{"type":"object","properties":{"query":{"type":"string","description":"single read-only SELECT statement"}},"required":["query"],"additionalProperties":false}`

// LocalToolRegistry resolves built-in local tool definitions (web_search,
// db_query) for any binding that requests them. Other bindings are ignored.
// An empty enabled set preserves the historical "all local tools" behavior.
type LocalToolRegistry struct {
	enabled map[string]bool
}

// NewLocalToolRegistry returns a registry restricted to the named local
// tools. With no arguments every local tool is enabled.
func NewLocalToolRegistry(enabled ...string) *LocalToolRegistry {
	set := make(map[string]bool, len(enabled))
	for _, name := range enabled {
		if name != "" {
			set[name] = true
		}
	}
	return &LocalToolRegistry{enabled: set}
}

func (r *LocalToolRegistry) isEnabled(name string) bool {
	if len(r.enabled) == 0 {
		return true
	}
	return r.enabled[name]
}

func (r *LocalToolRegistry) List(ctx context.Context, bindings []entity.ToolBinding) ([]port.ToolDefinition, error) {
	out := make([]port.ToolDefinition, 0, len(bindings))
	for _, b := range bindings {
		switch {
		case b.ToolName == "web_search" && r.isEnabled("web_search"):
			out = append(out, port.ToolDefinition{
				Name:       "web_search",
				Desc:       "Search the web for up-to-date information. Returns content snippets and URLs.",
				ArgsSchema: []byte(webSearchArgsSchema),
				Source:     entity.ToolSourceLocal,
				Binding:    b,
			})
		case b.ToolName == DBQueryToolName && r.isEnabled(DBQueryToolName):
			out = append(out, port.ToolDefinition{
				Name:       DBQueryToolName,
				Desc:       "Run a single read-only SELECT query against the configured database. Returns rows as JSON.",
				ArgsSchema: []byte(dbQueryArgsSchema),
				Source:     entity.ToolSourceLocal,
				Binding:    b,
			})
		case b.ToolName == FeedbackToolName && r.isEnabled(FeedbackToolName):
			out = append(out, port.ToolDefinition{
				Name:       FeedbackToolName,
				Desc:       "Run deterministic checks on your own output: JSON Schema lint and field constraint rules. Returns ok plus a list of violations.",
				ArgsSchema: []byte(feedbackArgsSchema),
				Source:     entity.ToolSourceLocal,
				Binding:    b,
			})
		case b.ToolName == FileToolName && r.isEnabled(FileToolName):
			out = append(out, port.ToolDefinition{
				Name:       FileToolName,
				Desc:       "Read a file or list a directory inside the configured allow-listed roots. Returns content or entries as JSON.",
				ArgsSchema: []byte(fileArgsSchema),
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
		"max_results":    3,
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
	tr.Provider = SearchProviderTavily
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
