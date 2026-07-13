package evidence

import "github.com/jamespud/magi/backend/domain/entity"

// ReliabilityResolver maps a ToolBinding to a ReliabilityScore.
type ReliabilityResolver func(binding entity.ToolBinding) entity.ReliabilityScore

// DefaultReliabilityResolver computes Base by source-type, Final=Base.
// Other factors (Directness/Recency/Corroboration/Extraction) are 0 in S2,
// filled in S3+.
func DefaultReliabilityResolver() ReliabilityResolver {
	return func(b entity.ToolBinding) entity.ReliabilityScore {
		base := 0.7
		switch b.Source {
		case entity.ToolSourceLocal:
			base = 0.9
		case entity.ToolSourceKnowledge:
			base = 0.85
		case entity.ToolSourceCodeRunner:
			base = 0.95
		}
		if b.Reliability != nil {
			base = *b.Reliability
		}
		return entity.ReliabilityScore{Base: base, Final: base}
	}
}

// ReliabilityInput bundles all context needed for a full reliability computation.
type ReliabilityInput struct {
	SourceType           entity.ToolSource
	ExplicitReliability  *float64
	Directness           float64
	Recency              float64
	CorroborationCount   int
	ExtractionConfidence float64
}

const (
	weightBase          = 0.35
	weightDirectness    = 0.25
	weightRecency       = 0.15
	weightCorroboration = 0.15
	weightExtraction    = 0.10
)

// ComputeReliability computes a full ReliabilityScore using a weighted average.
// Final is clamped to [0, 1].
func ComputeReliability(input ReliabilityInput) entity.ReliabilityScore {
	base := 0.7
	switch input.SourceType {
	case entity.ToolSourceLocal:
		base = 0.9
	case entity.ToolSourceKnowledge:
		base = 0.85
	case entity.ToolSourceCodeRunner:
		base = 0.95
	case entity.ToolSourcePlugin:
		base = 0.75
	case entity.ToolSourceWorkflow:
		base = 0.80
	}
	if input.ExplicitReliability != nil {
		base = *input.ExplicitReliability
	}

	dir := input.Directness
	if dir == 0 {
		dir = 0.7
	}
	rec := input.Recency
	if rec == 0 {
		rec = 0.5
	}
	corr := 0.5 + float64(input.CorroborationCount)*0.1
	if corr > 1.0 {
		corr = 1.0
	}
	ext := input.ExtractionConfidence
	if ext == 0 {
		ext = 0.8
	}

	final := base*weightBase + dir*weightDirectness + rec*weightRecency + corr*weightCorroboration + ext*weightExtraction
	if final > 1.0 {
		final = 1.0
	}
	if final < 0.0 {
		final = 0.0
	}

	return entity.ReliabilityScore{
		Base:          base,
		Directness:    dir,
		Recency:       rec,
		Corroboration: corr,
		Extraction:    ext,
		Final:         final,
	}
}

// FullReliabilityResolver returns a ReliabilityResolver that computes all
// five modifiers from a ReliabilityInput. Callers that do not yet supply
// Directness/Recency/Corroboration/Extraction get sensible defaults so
// Final is a meaningful composite.
func FullReliabilityResolver() ReliabilityResolver {
	return func(b entity.ToolBinding) entity.ReliabilityScore {
		return ComputeReliability(ReliabilityInput{
			SourceType:          b.Source,
			ExplicitReliability: b.Reliability,
			Directness:          0.7,
			Recency:             0.5,
			CorroborationCount:  0,
			ExtractionConfidence: 0.8,
		})
	}
}
