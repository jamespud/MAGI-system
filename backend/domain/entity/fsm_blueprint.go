package entity

import "fmt"

// StateTransition is one allowed case-status transition in the decision FSM.
type StateTransition struct {
	From string `json:"from"`
	To   string `json:"to"`
	// Action is the orchestrator action executed when entering To. It is the
	// NLAH binding between the editable blueprint and the deterministic Go
	// handler: the runtime fails fast if the declared action is unknown or
	// does not match the handler bound to the target status.
	Action string `json:"action,omitempty"`
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
	transitions := []StateTransition{
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
	}
	for i := range transitions {
		if transitions[i].Action == "" {
			transitions[i].Action = ActionForStatus(transitions[i].To)
		}
	}
	return FSMBlueprint{Transitions: transitions}
}

// ActionForStatus returns the canonical action name the orchestrator executes
// when it enters the given status. Empty means the status has no dedicated
// action in the decision engine.
func ActionForStatus(status string) string {
	switch status {
	case string(CaseStatusDraft):
		return "start"
	case string(CaseStatusNormalizing):
		return "normalize"
	case string(CaseStatusContextBuilding):
		return "build_context"
	case string(CaseStatusRetrievingMemory):
		return "retrieve_memory"
	case string(CaseStatusInvestigating):
		return "investigate"
	case string(CaseStatusEvidenceGating):
		return "gate_evidence"
	case string(CaseStatusCollectingVotes):
		return "collect_votes"
	case string(CaseStatusConsensusCheck):
		return "check_consensus"
	case string(CaseStatusDebating):
		return "debate"
	case string(CaseStatusReflecting):
		return "reflect"
	case string(CaseStatusRevoting):
		return "revote"
	case string(CaseStatusResolving):
		return "resolve"
	case string(CaseStatusGeneratingReport):
		return "generate_report"
	case string(CaseStatusSavingMemory):
		return "save_memory"
	case string(CaseStatusEvaluating):
		return "evaluate"
	case string(CaseStatusResolved):
		return "complete"
	case string(CaseStatusDeadlocked):
		return "deadlock"
	}
	return ""
}

// KnownFSMActions returns the action names the orchestrator supports.
func KnownFSMActions() map[string]bool {
	names := []string{
		"start", "normalize", "build_context", "retrieve_memory", "investigate",
		"gate_evidence", "collect_votes", "check_consensus", "debate", "reflect",
		"revote", "resolve", "generate_report", "save_memory", "evaluate",
		"complete", "deadlock",
	}
	out := make(map[string]bool, len(names))
	for _, n := range names {
		out[n] = true
	}
	return out
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

// ActionFor returns the declared action for a transition, or "" when the
// transition is absent or has no action declared (legacy blueprints).
func (b FSMBlueprint) ActionFor(from, to string) string {
	for _, t := range b.Transitions {
		if t.From == from && t.To == to {
			return t.Action
		}
	}
	return ""
}

// UnknownActions returns the transitions that declare an action the
// orchestrator does not support. Empty actions are tolerated (legacy).
func (b FSMBlueprint) UnknownActions() []string {
	known := KnownFSMActions()
	var out []string
	for _, t := range b.Transitions {
		if t.Action != "" && !known[t.Action] {
			out = append(out, fmt.Sprintf("%s -> %s declares unknown action %q", t.From, t.To, t.Action))
		}
	}
	return out
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
