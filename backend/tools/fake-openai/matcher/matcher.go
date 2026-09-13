// Package matcher classifies an OpenAI chat-completion request into the
// structured output the MAGI backend is asking for.
//
// This is the ONLY place in the fake model that inspects prompts. Keeping the
// wording here (with tests) means a prompt change breaks one package's tests
// instead of silently mis-answering through scattered string checks.
package matcher

import "strings"

// Kind is a structured output the backend requests.
type Kind string

const (
	KindDecisionTask    Kind = "decision_task"
	KindFinalReport     Kind = "final_report"
	KindEvidenceSummary Kind = "evidence_summary"
	KindVote            Kind = "vote"
	KindReflection      Kind = "reflection"
	// KindCompaction is the working-memory summarizer; its answer is plain text,
	// not a schema-validated structure.
	KindCompaction Kind = "compaction"
)

// Request is the minimal view of a chat completion the matcher needs.
type Request struct {
	// System is the system message (used for role detection).
	System string
	// User is the LAST user message, which names the expected output.
	User string
}

// AgentRoles are the three MAGI agents.
var AgentRoles = []string{"melchior", "balthasar", "casper"}

// Classify returns the requested output kind and, for role-scoped outputs, the
// agent role (empty when the request is not role-scoped). The prompt markers
// below mirror the backend's nudges; see the matcher tests.
func Classify(req Request) (Kind, string) {
	user := strings.ToLower(req.User)
	switch {
	case strings.Contains(user, "summarize the following agent working-memory history"):
		return KindCompaction, ""
	case strings.Contains(user, "decisiontask json"):
		return KindDecisionTask, ""
	case strings.Contains(user, "finalreportdata json"):
		return KindFinalReport, ""
	case strings.Contains(user, "reflection json"):
		return KindReflection, RoleIn(req.System)
	case strings.Contains(user, "vote json"):
		return KindVote, RoleIn(req.System)
	default:
		// The first user message is the decision question, and claim feedback /
		// fix hints arrive before the summary is accepted.
		return KindEvidenceSummary, RoleIn(req.System)
	}
}

// RoleIn reports which MAGI role authored the system prompt, or "" when the
// prompt is not agent-scoped (for example the Commander's normalization and
// report calls).
func RoleIn(system string) string {
	s := strings.ToLower(system)
	for _, role := range AgentRoles {
		if strings.Contains(s, "you are the magi role "+role) {
			return role
		}
	}
	for _, role := range AgentRoles {
		if strings.Contains(s, "you are "+role) {
			return role
		}
	}
	return ""
}
