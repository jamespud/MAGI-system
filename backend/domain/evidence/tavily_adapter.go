package evidence

import (
	"context"
	"encoding/json"

	"github.com/jamespud/magi/backend/domain/port"
)

// WebSearchResult is the provider-neutral result shape used by local web
// search executors and the evidence adapter.
type WebSearchResult struct {
	Title   string  `json:"title"`
	URL     string  `json:"url"`
	Content string  `json:"content"`
	Score   float64 `json:"score"`
}

// WebSearchResponse is the normalized search payload consumed by the evidence
// pipeline.
type WebSearchResponse struct {
	Answer   string            `json:"answer"`
	Results  []WebSearchResult `json:"results"`
	Provider string            `json:"provider,omitempty"`
}

// Tavily aliases preserve the existing public evidence API while allowing all
// search providers to share the normalized runtime shape.
type (
	TavilyResult   = WebSearchResult
	TavilyResponse = WebSearchResponse
)

// WebSearchAdapter extracts one EvidenceCandidate per normalized search
// result. It must be registered BEFORE NativeAdapter (which always Supports)
// in the EvidenceAdapterRegistry.
type WebSearchAdapter struct{}

// NewWebSearchAdapter returns the provider-neutral web search adapter.
func NewWebSearchAdapter() *WebSearchAdapter { return &WebSearchAdapter{} }

// NewTavilyAdapter preserves the historical constructor name.
func NewTavilyAdapter() *WebSearchAdapter { return &WebSearchAdapter{} }

func (a *WebSearchAdapter) Supports(tool port.ToolDefinition) bool {
	return tool.Name == "web_search"
}

func (a *WebSearchAdapter) Extract(ctx context.Context, tool port.ToolDefinition, result *port.ToolExecutionResult) ([]EvidenceCandidate, error) {
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
	return out, nil
}
