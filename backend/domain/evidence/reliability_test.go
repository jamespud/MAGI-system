package evidence_test

import (
	"testing"

	"github.com/jamespud/magi/backend/domain/entity"
	"github.com/jamespud/magi/backend/domain/evidence"
)

func TestComputeReliability_DefaultWeights(t *testing.T) {
	input := evidence.ReliabilityInput{
		SourceType:          entity.ToolSourceLocal,
		Directness:          1.0,
		Recency:             1.0,
		CorroborationCount:  3,
		ExtractionConfidence: 1.0,
	}
	s := evidence.ComputeReliability(input)
	// base=0.9, dir=1.0, rec=1.0, corr=0.5+0.3=0.8, ext=1.0
	// final = 0.9*0.35 + 1.0*0.25 + 1.0*0.15 + 0.8*0.15 + 1.0*0.10
	//       = 0.315 + 0.25 + 0.15 + 0.12 + 0.10 = 0.935
	if s.Base != 0.9 {
		t.Fatalf("base: got %v, want 0.9", s.Base)
	}
	if s.Final < 0.93 || s.Final > 0.94 {
		t.Fatalf("final: got %v, want ~0.935", s.Final)
	}
	if s.Directness != 1.0 {
		t.Fatalf("directness: got %v, want 1.0", s.Directness)
	}
	if s.Recency != 1.0 {
		t.Fatalf("recency: got %v, want 1.0", s.Recency)
	}
	if s.Corroboration != 0.8 {
		t.Fatalf("corroboration: got %v, want 0.8", s.Corroboration)
	}
	if s.Extraction != 1.0 {
		t.Fatalf("extraction: got %v, want 1.0", s.Extraction)
	}
}

func TestComputeReliability_UsesDefaults(t *testing.T) {
	input := evidence.ReliabilityInput{SourceType: entity.ToolSourceKnowledge}
	s := evidence.ComputeReliability(input)
	if s.Directness == 0 {
		t.Fatalf("directness should have default, got 0")
	}
	if s.Recency == 0 {
		t.Fatalf("recency should have default, got 0")
	}
	if s.Corroboration == 0 {
		t.Fatalf("corroboration should have default, got 0")
	}
	if s.Extraction == 0 {
		t.Fatalf("extraction should have default, got 0")
	}
	if s.Final == 0 {
		t.Fatalf("final should be non-zero")
	}
}

func TestComputeReliability_ExplicitOverride(t *testing.T) {
	rel := 0.99
	input := evidence.ReliabilityInput{
		SourceType:          entity.ToolSourceLocal,
		ExplicitReliability: &rel,
		Directness:          0.5,
	}
	s := evidence.ComputeReliability(input)
	if s.Base != 0.99 {
		t.Fatalf("base: got %v, want 0.99 (explicit override)", s.Base)
	}
}

func TestComputeReliability_ClampsFinal(t *testing.T) {
	input := evidence.ReliabilityInput{
		SourceType:          entity.ToolSourceCodeRunner,
		Directness:          2.0,
		Recency:             2.0,
		CorroborationCount:  100,
		ExtractionConfidence: 2.0,
	}
	s := evidence.ComputeReliability(input)
	if s.Final < 0 || s.Final > 1.0 {
		t.Fatalf("final out of [0,1]: %v", s.Final)
	}
}

func TestFullReliabilityResolver(t *testing.T) {
	resolver := evidence.FullReliabilityResolver()
	s := resolver(entity.ToolBinding{Source: entity.ToolSourceCodeRunner})
	if s.Base != 0.95 {
		t.Fatalf("coderunner base: got %v, want 0.95", s.Base)
	}
	if s.Directness == 0 || s.Recency == 0 || s.Extraction == 0 {
		t.Fatalf("modifiers should have defaults: %+v", s)
	}
	if s.Final == 0 {
		t.Fatalf("final should be non-zero: %v", s.Final)
	}
}

func TestDirectnessFromSource(t *testing.T) {
	cases := []struct {
		source entity.ToolSource
		want   float64
	}{
		{entity.ToolSourceLocal, 1.0},
		{entity.ToolSourceCodeRunner, 1.0},
		{entity.ToolSourcePlugin, 0.8},
		{entity.ToolSourceWorkflow, 0.7},
		{entity.ToolSourceKnowledge, 0.6},
	}
	for _, c := range cases {
		got := evidence.DirectnessFromSource(c.source)
		if got != c.want {
			t.Errorf("DirectnessFromSource(%s)=%v want=%v", c.source, got, c.want)
		}
	}
}
