package runtime

import (
	"math"
	"testing"

	"github.com/cloudwego/eino/schema"

	"github.com/jamespud/magi/backend/domain/entity"
)

func TestExtractUsageReadsProviderCost(t *testing.T) {
	msg := &schema.Message{
		ResponseMeta: &schema.ResponseMeta{Usage: &schema.TokenUsage{
			PromptTokens: 100, CompletionTokens: 20, TotalTokens: 120,
		}},
		Extra: map[string]any{entity.ModelCostExtraKey: 0.25},
	}
	got := extractUsage(msg)
	if got.TotalTokens != 120 || got.CostUSD != 0.25 {
		t.Fatalf("usage = %+v, want provider-accurate cost", got)
	}
}

func TestAddUsagePreservesCost(t *testing.T) {
	got := addUsage(
		&entity.Usage{TotalTokens: 100, CostUSD: 0.2},
		&entity.Usage{TotalTokens: 20, CostUSD: 0.1},
	)
	if got.TotalTokens != 120 || math.Abs(got.CostUSD-0.3) > 1e-12 {
		t.Fatalf("usage = %+v, want summed cost", got)
	}
}
