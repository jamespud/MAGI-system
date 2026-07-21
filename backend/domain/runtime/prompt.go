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
func BuildAgentSystemPrompt(cfg *entity.MagiConfig, summarySchema, voteSchema, reflectionSchema []byte, debate *DebateContext, hasTools bool) string {
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
	// Guide the LLM on exactly what the deterministic gate checks, so it can
	// produce a passing EvidenceSummary on the first try.
	if len(es.RequiredTypes) > 0 {
		b.WriteString("\n\nIn your EvidenceSummary's evidence_by_type, you MUST classify evidence under these type keys (the gate requires them):")
		for _, rt := range es.RequiredTypes {
			fmt.Fprintf(&b, "\n- %q: at least %d EV-ID(s)", rt.Type, rt.MinCount)
		}
	}
	if guidance := customRuleGuidance(es.CustomRules); guidance != "" {
		b.WriteString("\n\nYour EvidenceSummary MUST also satisfy these requirements:")
		b.WriteString(guidance)
	}
	if hasTools {
		b.WriteString("\n\nWorkflow: gather evidence via tool calls; limit yourself to AT MOST 3 tool calls. Once you have gathered enough evidence, STOP calling tools and output an EvidenceSummary JSON (no tool calls) citing real EV-IDs; after the gate passes, output a Vote JSON. Do not keep searching past 3 calls -- converge to a decision.")
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
		b.WriteString(" You may gather new evidence via tool calls, then output a new EvidenceSummary, then a Reflection, then a new Vote.")
		b.WriteString(" Your previous vote is included for reference. You must cite new EV-IDs or accept/reject specific claims to justify any position change.")
		b.WriteString("\n\nAfter your EvidenceSummary passes the gate, output a Reflection JSON describing your position change, then a Vote JSON.")
		b.WriteString(" The Reflection must justify any position change with at least one of: new EV-ID, accepted claim, rejected claim, or utility dimension re-evaluation.")
		b.WriteString("\nReflection JSON schema:\n")
		b.Write(reflectionSchema)
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

// customRuleGuidance maps the deterministic gate's custom rule codes to
// human-readable instructions for the LLM, so it can satisfy them on the
// first try. Returns "" if no recognized rules.
var ruleGuidanceMap = map[string]string{
	"worst_case_claim_required":  "Include at least one claim mentioning a 'worst case' scenario.",
	"reversibility_assessment":   "Include at least one claim addressing reversibility (use the word 'reversible' or 'reversibility').",
	"opportunity_cost_claim":     "Include at least one claim discussing opportunity cost or trade-offs (use 'opportunity cost' or 'trade-off').",
	"time_window_assessment":     "Include at least one claim assessing the time window or timing (use 'time window' or 'timing').",
	"primary_source_required":    "Cite at least one primary/technical source in your evidence (web_search results count as primary sources).",
	"utility_dimension_coverage": "Provide at least 2 claims and classify your evidence under at least 2 different type keys in evidence_by_type.",
}

func customRuleGuidance(rules []entity.EvidenceRule) string {
	var out strings.Builder
	for _, r := range rules {
		if g, ok := ruleGuidanceMap[r.Code]; ok {
			fmt.Fprintf(&out, "\n- %s", g)
		}
	}
	return out.String()
}
