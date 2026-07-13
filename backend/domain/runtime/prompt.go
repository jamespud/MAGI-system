package runtime

import (
	"fmt"
	"strings"

	"github.com/jamespud/magi/backend/domain/entity"
)

// BuildAgentSystemPrompt assembles the full system prompt from MagiConfig +
// evidence/vote schemas + optional debate context (Reconsider mode).
// hasTools indicates whether the agent has tools available; when false, the prompt
// instructs the agent to reason from intrinsic knowledge instead of tool calls.
func BuildAgentSystemPrompt(cfg *entity.MagiConfig, summarySchema, voteSchema []byte, debate *DebateContext, hasTools bool) string {
	if cfg == nil {
		return ""
	}
	var b strings.Builder
	b.WriteString(cfg.Persona)
	if len(cfg.Objective.Dimensions) > 0 {
		b.WriteString("\n\nObjective function (your value axes; weigh decisions accordingly):")
		for _, d := range cfg.Objective.Dimensions {
			fmt.Fprintf(&b, "\n- %s (weight %.2f): %s", d.Code, d.Weight, d.Description)
		}
	}
	if cfg.RiskTendency != "" {
		fmt.Fprintf(&b, "\n\nRisk tendency: %s", string(cfg.RiskTendency))
	}
	es := cfg.EvidenceStandard
	fmt.Fprintf(&b, "\n\nEvidence standard: min evidence=%d, min quantitative=%d, min reliability=%.2f, required claim count=%d.",
		es.MinEvidenceCount, es.MinQuantitativeCount, es.MinReliability, es.RequiredClaimCount)
	if hasTools {
		b.WriteString("\n\nWorkflow: gather evidence via tool calls; when ready, output an EvidenceSummary JSON (no tool calls) citing real EV-IDs; after the gate passes, output a Vote JSON.")
		b.WriteString("\n\nYou may also submit claims incrementally during the gather phase:")
		b.WriteString("\n  Output {\"type\":\"claim_submission\",\"claims\":[{\"statement\":\"...\",\"supports\":[\"EV-001\"],\"contradicts\":[]}]}")
		b.WriteString("\n  Claims with valid EV-ID references will be recorded in the Claim Graph.")
	} else {
		b.WriteString("\n\nWorkflow: You have no tools available. Reason from your intrinsic knowledge to analyze the decision.")
		b.WriteString(" Output an EvidenceSummary JSON with your analysis claims (empty evidence_by_type is fine).")
		b.WriteString(" After the summary, output a Vote JSON with your decision.")
	}
	b.WriteString("\n\nEvidenceSummary JSON schema:\n")
	b.Write(summarySchema)
	b.WriteString("\n\nVote JSON schema:\n")
	b.Write(voteSchema)
	b.WriteString("\n\nValid decision values: \"approve\", \"reject\", \"abstain\", \"conditional_approve\".")
	b.WriteString("\nYou MUST use one of these exact values for the decision field.")
	if debate != nil {
		b.WriteString("\n\n--- RECONSIDERATION ---\n")
		b.WriteString("You are in reconsideration mode. Review the debate context and the majority/minority arguments.")
		b.WriteString(" You may gather new evidence via tool calls, then output a new EvidenceSummary, then a new Vote.")
		b.WriteString(" Your previous vote is included for reference. You must cite new EV-IDs or accept/reject specific claims to justify any position change.")
		if debate.PreviousVote != nil {
			fmt.Fprintf(&b, "\nYour previous vote: decision=%s, confidence=%.0f.", debate.PreviousVote.Decision, debate.PreviousVote.Confidence)
		}
	}
	return b.String()
}

// ValidateVoteDimensions is the deterministic post-check (ADR-003/#3).
func ValidateVoteDimensions(vote *entity.Vote, obj entity.ObjectiveFunction) error {
	if vote == nil {
		return fmt.Errorf("vote is nil")
	}
	validDims := make(map[string]bool, len(obj.Dimensions))
	for _, d := range obj.Dimensions {
		validDims[d.Code] = true
	}
	seen := make(map[string]bool, len(vote.UtilityScores))
	for _, us := range vote.UtilityScores {
		if !validDims[us.DimensionCode] {
			return fmt.Errorf("utility dimension %q not in objective function", us.DimensionCode)
		}
		if us.Score < 0 || us.Score > 100 {
			return fmt.Errorf("utility score for %q out of range [0,100]: %v", us.DimensionCode, us.Score)
		}
		if seen[us.DimensionCode] {
			return fmt.Errorf("duplicate utility dimension %q", us.DimensionCode)
		}
		seen[us.DimensionCode] = true
	}
	for d := range validDims {
		if !seen[d] {
			return fmt.Errorf("missing utility dimension %q", d)
		}
	}
	return nil
}
