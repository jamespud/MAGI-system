package entity

// Evaluation is the scored outcome of a completed DecisionCase across the five
// design categories (Tool/Evidence/Agent/Consensus/System) plus counterfactual
// stability. Computed by service.Evaluate from LoopResult data.
type Evaluation struct {
	// Tool category
	ToolSuccessRate   float64
	AvgToolCalls      float64
	ToolParamFailures int

	// Evidence category
	EvidenceCount     int
	AvgReliability    float64
	UniqueSourceTypes int

	// Agent category
	GateFailures       int
	MaxStepsExceeded   int
	ValidationFailures int

	// Consensus category
	FirstRoundConsensus bool
	ConsensusRound      int
	ConsensusOutcome    ConsensusOutcome

	// System category
	TotalTokens       int64
	AvgTokensPerAgent float64
	TotalSteps        int
	TotalToolCalls    int

	// Stability
	CounterfactualStability float64
}
