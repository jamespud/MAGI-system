package entity

import "time"

// Resolution is the final outcome of a DecisionCase.
type Resolution struct {
	ID             string
	CaseID         string
	Consensus      ConsensusResult
	FinalDecision  VoteDecision
	FinalReport    string
	KeyEvidenceIDs []string
	KeyClaimIDs    []string
	VoteIDs        []string
	Evaluation     *Evaluation
	CreatedAt      time.Time
}

// ConsensusResult is the deterministic consensus outcome.
type ConsensusResult struct {
	Outcome ConsensusOutcome
	Votes   []Vote
	Round   int
	Detail  string
}

type ConsensusOutcome string

const (
	ConsensusStrongApproval          ConsensusOutcome = "strong_approval"
	ConsensusStrongRejection         ConsensusOutcome = "strong_rejection"
	ConsensusMajorityApprovalDissent ConsensusOutcome = "majority_approval_with_dissent"
	ConsensusMajorityRejectionDissent ConsensusOutcome = "majority_rejection_with_dissent"
	ConsensusDeadlock                ConsensusOutcome = "deadlock"
	ConsensusInsufficientQuorum      ConsensusOutcome = "insufficient_quorum"
	ConsensusConditional             ConsensusOutcome = "conditional"
)
