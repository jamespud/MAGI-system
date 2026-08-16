// Package prompt owns the built-in prompt templates and the placeholder
// renderer (P2 D12). The runtime uses these as fallbacks; a DB-backed prompt
// store can override them per key without a code change.
package prompt

import (
	"strings"
)

// Keys for the versioned prompt registry.
const (
	KeyCommanderNormalize  = "commander.normalize"
	KeyCommanderReport     = "commander.report"
	KeyAgentWorkflowTools  = "agent.workflow_tools"
	KeyAgentWorkflowNoTools = "agent.workflow_notools"
)

// Default returns the built-in templates keyed by registry key.
func Default() map[string]string {
	return map[string]string{
		KeyCommanderNormalize: `{{PERSONA}}

Normalize this decision question into a DecisionTask JSON. You MUST include ALL fields:
- canonical_question: the standardized, unambiguous form of the question
- decision_type: the type of decision (adopt/migrate/launch/strategic/generic)
- background: relevant context for understanding the decision
- dimensions: key decision dimensions, each with code + description (at least 2)
- information_needs: information gaps to fill, each with topic + rationale
- success_criteria: how to evaluate the decision, each with code + description
- unknowns: what is currently unknown or uncertain

Question: {{QUESTION}}
Context: {{CONTEXT}}
Constraints: {{CONSTRAINTS}}`,
		KeyCommanderReport: `{{PERSONA}}

Generate a FinalReportData JSON for this decision. You MUST include ALL fields:
- decision: the final decision (approve/reject/conditional_approve)
- summary: a one-paragraph summary of the decision
- key_reasons: main reasoning points (array of strings)
- risks: risks or dissenting points (array of strings)
- next_steps: recommended next steps (array of strings)
- key_evidence_ids: array of evidence IDs this decision relies on
- key_claim_ids: array of claim IDs this decision relies on

Question: {{QUESTION}}. Consensus: {{CONSENSUS}}. Votes: {{VOTES}}.
Available evidence IDs: {{EVIDENCE_IDS}}
Available claim IDs: {{CLAIM_IDS}}
When evidence or claim IDs are available you MUST cite at least one of them in key_evidence_ids/key_claim_ids.`,
		KeyAgentWorkflowTools: `Workflow: gather evidence via tool calls; limit yourself to AT MOST 3 tool calls. Once you have gathered enough evidence, STOP calling tools and output an EvidenceSummary JSON (no tool calls) citing real EV-IDs; after the gate passes, output a Vote JSON. Do not keep searching past 3 calls -- converge to a decision.

You may also submit claims incrementally during the gather phase:
  Output {"type":"claim_submission","claims":[{"statement":"...","supports":["EV-001"],"contradicts":[]}]}
  Claims with valid EV-ID references will be recorded in the Claim Graph.`,
		KeyAgentWorkflowNoTools: `Workflow: You have no tools available. Reason from your intrinsic knowledge to analyze the decision.
 Output an EvidenceSummary JSON with your analysis claims (empty evidence_by_type is fine).
 After the summary, output a Vote JSON with your decision.`,
	}
}

// Render replaces {{KEY}} placeholders with the provided values. Unknown
// placeholders are left intact so the caller can detect a malformed override.
func Render(template string, vars map[string]string) string {
	out := template
	for k, v := range vars {
		out = strings.ReplaceAll(out, "{{"+k+"}}", v)
	}
	return out
}
