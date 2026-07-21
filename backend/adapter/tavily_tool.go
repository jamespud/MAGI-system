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
