package consensus

// ConsensusPolicy configures the deterministic consensus engine (ADR-009).
type ConsensusPolicy struct {
	Quorum                     int  // minimum effective votes (default 2)
	FirstSplitGoesToDebate     bool // first-round 2:1 should enter debate (default true)
	ResolveOnReconsiderMajority bool // reconsider 2:1 may resolve (default true)
	ConditionalAsApprove       bool // CONDITIONAL_APPROVE counts as APPROVE (default true)
}

func DefaultConsensusPolicy() ConsensusPolicy {
	return ConsensusPolicy{
		Quorum:                     2,
		FirstSplitGoesToDebate:     true,
		ResolveOnReconsiderMajority: true,
		ConditionalAsApprove:       true,
	}
}
