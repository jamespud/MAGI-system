package magi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/jamespud/magi/backend/application/metrics"
	"github.com/jamespud/magi/backend/domain/evidence"
	"github.com/jamespud/magi/backend/domain/port"
)

const (
	SearchProviderTavily  = "tavily"
	SearchProviderBrave   = "brave"
	defaultBraveSearchURL = "https://api.search.brave.com/res/v1/web/search"
)

// WebSearchProviderSpec declares one pluggable search backend. Providers are
// tried in configuration order; the first successful response wins.
type WebSearchProviderSpec struct {
	Provider string
	APIKey   string
	BaseURL  string
}

type namedSearchExecutor struct {
	name string
	exec port.ToolExecutorPort
}

// WebSearchToolExecutor routes the local web_search tool through an ordered
// provider chain and fails over on provider errors.
type WebSearchToolExecutor struct {
	providers []namedSearchExecutor
	metrics   *metrics.Registry
}

// NewWebSearchToolExecutor validates and wraps the configured search backends.
func NewWebSearchToolExecutor(specs []WebSearchProviderSpec, reg *metrics.Registry) (port.ToolExecutorPort, error) {
	if len(specs) == 0 {
		return nil, fmt.Errorf("web search provider chain is empty")
	}
	providers := make([]namedSearchExecutor, 0, len(specs))
	for i, spec := range specs {
		if strings.TrimSpace(spec.Provider) == "" {
			return nil, fmt.Errorf("search provider[%d]: provider is required", i)
		}
		if strings.TrimSpace(spec.APIKey) == "" {
			return nil, fmt.Errorf("search provider[%d] (%s): api_key is required", i, spec.Provider)
		}
		var executor port.ToolExecutorPort
		switch strings.ToLower(strings.TrimSpace(spec.Provider)) {
		case SearchProviderTavily:
			executor = NewTavilyToolExecutorWithURL(spec.APIKey, defaultString(spec.BaseURL, defaultTavilyURL))
		case SearchProviderBrave:
			executor = NewBraveSearchToolExecutorWithURL(spec.APIKey, defaultString(spec.BaseURL, defaultBraveSearchURL))
		default:
			return nil, fmt.Errorf("search provider[%d]: unsupported provider %q (use tavily or brave)", i, spec.Provider)
		}
		providers = append(providers, namedSearchExecutor{name: strings.ToLower(strings.TrimSpace(spec.Provider)), exec: executor})
	}
	return &WebSearchToolExecutor{providers: providers, metrics: reg}, nil
}

func (e *WebSearchToolExecutor) Execute(ctx context.Context, req port.ToolExecutionRequest) (*port.ToolExecutionResult, error) {
	if req.ToolName != "" && req.ToolName != "web_search" {
		return nil, fmt.Errorf("web search: unsupported tool %q", req.ToolName)
	}
	var args struct {
		Query string `json:"query"`
	}
	if err := json.Unmarshal([]byte(req.ArgumentsJSON), &args); err != nil {
		return nil, fmt.Errorf("web search: parse args: %w", err)
	}
	if strings.TrimSpace(args.Query) == "" {
		return nil, errors.New("web search: query cannot be empty")
	}

	var errs []error
	for i, provider := range e.providers {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		result, err := provider.exec.Execute(ctx, req)
		if err == nil {
			return result, nil
		}
		errs = append(errs, fmt.Errorf("search provider %s: %w", provider.name, err))
		if ctx.Err() != nil || i == len(e.providers)-1 {
			break
		}
		if e.metrics != nil {
			e.metrics.IncWebSearchFailover()
		}
		log.Printf("web search provider %s failed, trying %s: %v", provider.name, e.providers[i+1].name, err)
	}
	return nil, errors.Join(errs...)
}

// BraveSearchToolExecutor calls the Brave Search API and normalizes results to
// the shared web-search evidence shape.
type BraveSearchToolExecutor struct {
	apiKey  string
	baseURL string
	client  *http.Client
}

func NewBraveSearchToolExecutor(apiKey string) *BraveSearchToolExecutor {
	return NewBraveSearchToolExecutorWithURL(apiKey, defaultBraveSearchURL)
}

func NewBraveSearchToolExecutorWithURL(apiKey, baseURL string) *BraveSearchToolExecutor {
	return &BraveSearchToolExecutor{
		apiKey:  apiKey,
		baseURL: baseURL,
		client:  &http.Client{Timeout: 15 * time.Second},
	}
}

func (e *BraveSearchToolExecutor) Execute(ctx context.Context, req port.ToolExecutionRequest) (*port.ToolExecutionResult, error) {
	var args struct {
		Query string `json:"query"`
	}
	if err := json.Unmarshal([]byte(req.ArgumentsJSON), &args); err != nil {
		return nil, fmt.Errorf("brave: parse args: %w", err)
	}
	target, err := url.Parse(e.baseURL)
	if err != nil || target.Scheme == "" || target.Host == "" {
		return nil, fmt.Errorf("brave: invalid base URL %q", e.baseURL)
	}
	query := target.Query()
	query.Set("q", args.Query)
	query.Set("count", "3")
	target.RawQuery = query.Encode()

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet, target.String(), nil)
	if err != nil {
		return nil, fmt.Errorf("brave: new request: %w", err)
	}
	httpReq.Header.Set("Accept", "application/json")
	httpReq.Header.Set("X-Subscription-Token", e.apiKey)
	resp, err := e.client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("brave: http: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return nil, fmt.Errorf("brave: http status %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	var parsed struct {
		Web struct {
			Results []struct {
				Title       string `json:"title"`
				URL         string `json:"url"`
				Description string `json:"description"`
			} `json:"results"`
		} `json:"web"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&parsed); err != nil {
		return nil, fmt.Errorf("brave: decode: %w", err)
	}
	normalized := evidence.WebSearchResponse{Provider: SearchProviderBrave}
	for _, result := range parsed.Web.Results {
		normalized.Results = append(normalized.Results, evidence.WebSearchResult{
			Title: result.Title, URL: result.URL, Content: result.Description,
		})
	}
	raw, _ := json.Marshal(normalized)
	sourceURI := ""
	if len(normalized.Results) > 0 {
		sourceURI = normalized.Results[0].URL
	}
	return &port.ToolExecutionResult{
		Output: string(raw), Structured: &normalized, SourceURI: sourceURI,
	}, nil
}

func defaultString(value, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return strings.TrimSpace(value)
}
