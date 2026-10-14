package entity

import (
	"fmt"
	"time"
)

// Resolution is the final outcome of a DecisionCase.
type Resolution struct {
	ID                  string
	CaseID              string
	ExecutionGeneration int64
	Consensus           ConsensusResult
	FinalDecision       VoteDecision
	FinalReport         string
	KeyEvidenceIDs      []string
	KeyClaimIDs         []string
	// Dissent is the authoritative structured minority record derived from the
	// frozen round's ballots, persisted with the resolution so the report,
	// HTTP/A2A and export projections cannot independently re-invent (or hide)
	// minority positions (invariant REPORT-01, issue #13).
	//
	// Its nil-ness is the tri-state (see DissentState): nil means the record was
	// never persisted (legacy), an empty non-nil slice means an explicit
	// "no dissent", and a non-empty slice means minority positions exist.
	Dissent    []Dissent
	VoteIDs    []string
	Evaluation *Evaluation
	CreatedAt  time.Time
}

// DissentState classifies whether minority positions were recorded for a
// resolution. "No record" is not the same fact as "recorded that nobody
// dissented": a truthfulness contract must never present the former as the
// latter (invariant REPORT-01, issue #13).
type DissentState string

const (
	// DissentUnknown means the resolution predates structured dissent
	// persistence (or its record was never committed). Whether anyone dissented
	// cannot be proven from the stored data.
	DissentUnknown DissentState = "UNKNOWN"
	// DissentNone means the resolution explicitly recorded that no participant
	// dissented.
	DissentNone DissentState = "NONE"
	// DissentPresent means one or more minority positions were recorded.
	DissentPresent DissentState = "PRESENT"
)

// DissentStateOf classifies a dissent record by the nil-ness of its slice: nil
// means "never persisted", an empty non-nil slice means "explicitly no
// dissent", and a non-empty slice means "minority positions recorded".
func DissentStateOf(d []Dissent) DissentState {
	switch {
	case d == nil:
		return DissentUnknown
	case len(d) == 0:
		return DissentNone
	default:
		return DissentPresent
	}
}

// DissentState reports the persisted minority state of the resolution.
func (r *Resolution) DissentState() DissentState {
	if r == nil {
		return DissentUnknown
	}
	return DissentStateOf(r.Dissent)
}

// ValidateDissent rejects a dissent record that cannot be trusted as a
// structured minority position: a voting persona must be identified and the
// decision must be a defined vote value. A well-formed JSON array can still
// contain `null` or `{}` elements, which must never be reported as a real
// minority position.
func ValidateDissent(d Dissent) error {
	switch d.AgentCode {
	case MagiCodeMelchior, MagiCodeBalthasar, MagiCodeCasper:
	default:
		return fmt.Errorf("dissent agent code %q is not a voting persona", d.AgentCode)
	}
	if !IsValidVoteDecision(d.Decision) {
		return fmt.Errorf("dissent decision %q is not a valid vote decision", d.Decision)
	}
	return nil
}

// Dissent is a structured minority position recorded for auditability.
type Dissent struct {
	AgentCode   MagiCode
	Decision    VoteDecision
	Reasoning   string
	EvidenceIDs []string
	ClaimIDs    []string
	Conditions  []DecisionCondition
}

// ConsensusResult is the deterministic consensus outcome.
type ConsensusResult struct {
	Outcome    ConsensusOutcome
	Votes      []Vote
	Round      int
	Detail     string
	Conditions []DecisionCondition
}

type ConsensusOutcome string

const (
	ConsensusStrongApproval           ConsensusOutcome = "strong_approval"
	ConsensusStrongRejection          ConsensusOutcome = "strong_rejection"
	ConsensusMajorityApprovalDissent  ConsensusOutcome = "majority_approval_with_dissent"
	ConsensusMajorityRejectionDissent ConsensusOutcome = "majority_rejection_with_dissent"
	ConsensusDeadlock                 ConsensusOutcome = "deadlock"
	ConsensusInsufficientQuorum       ConsensusOutcome = "insufficient_quorum"
	ConsensusConditional              ConsensusOutcome = "conditional"
	// ConsensusIncomplete means the round is missing at least one expected
	// participant's ballot. A failed, timed-out, cancelled, missing or invalid
	// vote is not an abstention, so the round cannot produce a decision no
	// matter how many of the remaining ballots approve.
	ConsensusIncomplete ConsensusOutcome = "incomplete"
)
