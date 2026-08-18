package entity

import "fmt"

// StateTransition is one allowed case-status transition in the decision FSM.
type StateTransition struct {
	From string `json:"from"`
	To   string `json:"to"`
}

// FSMBlueprint is the editable orchestration blueprint: the set of legal
// case-status transitions. It is the governance surface for the deterministic
// FSM; execution still follows the Go orchestrator while the blueprint is used
// for audit and transition validation.
type FSMBlueprint struct {
	Transitions []StateTransition `json:"transitions"`
}

// DefaultFSMBlueprint returns the legal transitions of the current
// orchestrator state machine.
func DefaultFSMBlueprint() FSMBlueprint {
	return FSMBlueprint{Transitions: []StateTransition{
		{From: string(CaseStatusDraft), To: string(CaseStatusNormalizing)},
		{From: string(CaseStatusNormalizing), To: string(CaseStatusContextBuilding)},
		{From: string(CaseStatusContextBuilding), To: string(CaseStatusRetrievingMemory)},
		{From: string(CaseStatusRetrievingMemory), To: string(CaseStatusInvestigating)},
		{From: string(CaseStatusInvestigating), To: string(CaseStatusEvidenceGating)},
		{From: string(CaseStatusEvidenceGating), To: string(CaseStatusCollectingVotes)},
		{From: string(CaseStatusCollectingVotes), To: string(CaseStatusConsensusCheck)},
		{From: string(CaseStatusConsensusCheck), To: string(CaseStatusResolving)},
		{From: string(CaseStatusConsensusCheck), To: string(CaseStatusDebating)},
		{From: string(CaseStatusConsensusCheck), To: string(CaseStatusDeadlocked)},
		{From: string(CaseStatusDebating), To: string(CaseStatusReflecting)},
		{From: string(CaseStatusReflecting), To: string(CaseStatusRevoting)},
		{From: string(CaseStatusRevoting), To: string(CaseStatusConsensusCheck)},
		{From: string(CaseStatusResolving), To: string(CaseStatusGeneratingReport)},
		{From: string(CaseStatusGeneratingReport), To: string(CaseStatusSavingMemory)},
		{From: string(CaseStatusSavingMemory), To: string(CaseStatusEvaluating)},
		{From: string(CaseStatusEvaluating), To: string(CaseStatusResolved)},
		// Retry edges: failed/timeout/cancelled runs restart from draft.
		{From: string(CaseStatusFailed), To: string(CaseStatusDraft)},
		{From: string(CaseStatusTimedOut), To: string(CaseStatusDraft)},
		{From: string(CaseStatusCancelled), To: string(CaseStatusDraft)},
		{From: string(CaseStatusDeadlocked), To: string(CaseStatusDraft)},
	}}
}

// allowed reports whether the transition is present in the blueprint.
func (b FSMBlueprint) allowed(from, to string) bool {
	for _, t := range b.Transitions {
		if t.From == from && t.To == to {
			return true
		}
	}
	return false
}

// ValidatePath checks a sequence of case statuses against the blueprint and
// returns the violating transitions.
func (b FSMBlueprint) ValidatePath(path []string) []string {
	var violations []string
	for i := 0; i+1 < len(path); i++ {
		from, to := path[i], path[i+1]
		if from == to {
			continue
		}
		if !b.allowed(from, to) {
			violations = append(violations, fmt.Sprintf("%s -> %s is not allowed by the FSM blueprint", from, to))
		}
	}
	return violations
}
