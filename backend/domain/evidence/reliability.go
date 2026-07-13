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
