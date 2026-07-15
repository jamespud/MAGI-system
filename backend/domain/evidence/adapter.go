package evidence

import (
	"context"
	"fmt"

	"github.com/jamespud/magi/backend/domain/entity"
	"github.com/jamespud/magi/backend/domain/port"
)

// EvidenceCandidate is a potential evidence record extracted from a tool result.
type EvidenceCandidate struct {
	Observation string
	SourceURI   string
	Reliability entity.ReliabilityScore
}

// EvidenceAdapter extracts evidence candidates from a tool result (ADR-005).
// Priority: Native > ToolParser > LLMExtractor > Raw (S7: Native + Raw implemented).
type EvidenceAdapter interface {
	Supports(tool port.ToolDefinition) bool
	Extract(ctx context.Context, tool port.ToolDefinition, result *port.ToolExecutionResult) ([]EvidenceCandidate, error)
}

// EvidenceAdapterRegistry tries adapters by priority; first Supports wins.
type EvidenceAdapterRegistry struct {
	adapters []EvidenceAdapter
	resolver ReliabilityResolver
}

func NewEvidenceAdapterRegistry(resolver ReliabilityResolver, adapters ...EvidenceAdapter) *EvidenceAdapterRegistry {
	if resolver == nil {
		resolver = DefaultReliabilityResolver()
	}
	return &EvidenceAdapterRegistry{adapters: adapters, resolver: resolver}
}

func (r *EvidenceAdapterRegistry) Extract(ctx context.Context, tool port.ToolDefinition, result *port.ToolExecutionResult) ([]EvidenceCandidate, error) {
	for _, a := range r.adapters {
		if a.Supports(tool) {
			return a.Extract(ctx, tool, result)
		}
	}
	rel := r.resolver(tool.Binding)
	return []EvidenceCandidate{{Observation: result.Output, Reliability: rel}}, nil
}

// NativeAdapter checks for structured tool output first; falls back to raw.
// Priority: above RawObservationAdapter in the registry.
type NativeAdapter struct{}

func NewNativeAdapter() *NativeAdapter { return &NativeAdapter{} }

func (a *NativeAdapter) Supports(tool port.ToolDefinition) bool { return true }

func (a *NativeAdapter) Extract(ctx context.Context, tool port.ToolDefinition, result *port.ToolExecutionResult) ([]EvidenceCandidate, error) {
	rel := ComputeReliability(ReliabilityInput{
		SourceType:           tool.Binding.Source,
		ExplicitReliability:  tool.Binding.Reliability,
		Directness:           DirectnessFromSource(tool.Binding.Source),
		Recency:              1.0, // freshly collected
		ExtractionConfidence: 1.0, // native = deterministic structured extraction
	})
	obs := result.Output
	if result.Structured != nil {
		obs = fmt.Sprintf("%+v", result.Structured)
	}
	return []EvidenceCandidate{{Observation: obs, Reliability: rel}}, nil
}

// RawObservationAdapter always supports; wraps the whole output as one candidate.
type RawObservationAdapter struct{}

func NewRawObservationAdapter() *RawObservationAdapter { return &RawObservationAdapter{} }

func (a *RawObservationAdapter) Supports(tool port.ToolDefinition) bool { return true }

func (a *RawObservationAdapter) Extract(ctx context.Context, tool port.ToolDefinition, result *port.ToolExecutionResult) ([]EvidenceCandidate, error) {
	rel := ComputeReliability(ReliabilityInput{
		SourceType:           tool.Binding.Source,
		ExplicitReliability:  tool.Binding.Reliability,
		Directness:           DirectnessFromSource(tool.Binding.Source),
		Recency:              1.0, // freshly collected
		ExtractionConfidence: 0.3, // raw = unstructured fallback
	})
	return []EvidenceCandidate{{Observation: result.Output, Reliability: rel}}, nil
}
