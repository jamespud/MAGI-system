package evidence

import (
	"context"
	"encoding/json"

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
	return out, nil
}
