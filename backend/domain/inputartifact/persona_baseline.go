package inputartifact

import (
	"fmt"
	"slices"
	"strings"
)

type personaBaseline struct {
	id, core, reviews, investigations, evidence, buckets string
	support, reject, abstain, correction                 string
}

// These literals implement C1-A 4.2 / Rev4 17.2–17.4. They are unexported,
// returned by value and have no configurable registry or caller override.
func baselineFor(role string) (personaBaseline, error) {
	switch role {
	case "melchior":
		return personaBaseline{
			id:             "MELCHIOR-1.0",
			core:           "TARGET_EFFECT RESOURCE_OPPORTUNITY_COST IMPLEMENTATION_CONVENIENCE",
			reviews:        "BASELINE EFFECT_SIZE SCOPE RESOURCE_TIME_COST KEY_ASSUMPTIONS ALTERNATIVES NO_ACTION",
			investigations: "EFFECT_BASELINE FEASIBILITY_COUNTEREVIDENCE",
			evidence:       "REPRODUCIBLE_TASK_MEASUREMENT VERIFIED_RESOURCE_RECORD APPLICABLE_EXPERIENCE MARKED_MODEL_SPECULATION",
			buckets:        "EFFECT_BASELINE COUNTEREVIDENCE_FEASIBILITY CANDIDATE_COMPARISON CORRECTION_RESERVE",
			support:        "EFFECT_SUPPORTED FEASIBILITY_RESOLVED CHARTER_SATISFIED",
			reject:         "EFFECT_REFUTED CHARTER_COST_EXCEEDED UNACCEPTABLE_DEFECT",
			abstain:        "EFFECT_UNVERIFIABLE FEASIBILITY_UNVERIFIABLE EVIDENCE_CONFLICT",
			correction:     "BASELINE_ERROR MEASUREMENT_INVALID KEY_COST_CHANGED COUNTEREXAMPLE CONFIRMED_GOAL_REVISED",
		}, nil
	case "balthasar":
		return personaBaseline{
			id:             "BALTHASAR-1.0",
			core:           "IRREVERSIBLE_SPILLOVER RECOVERY_RESPONSIBILITY LONG_TERM_RESILIENCE",
			reviews:        "AFFECTED_SUBJECTS FAILURE_MODES MAX_LOSS IMPACT_SCOPE RECOVERY_EVIDENCE RESPONSIBILITY STOP_CONDITIONS NO_ACTION_RISK",
			investigations: "FAILURE_IMPACT RECOVERY_VERIFICATION",
			evidence:       "SAME_ENVIRONMENT_RECOVERY_MEASUREMENT RESOURCE_AUTHORITY_BOUNDARY COMPARABLE_FAILURE_HISTORY UNVERIFIED_RECOVERY_PROMISE",
			buckets:        "FAILURE_IMPACT RECOVERY_VERIFICATION NO_ACTION_RESPONSIBILITY CORRECTION_RESERVE",
			support:        "RISK_ACCEPTED_WITHIN_BOUNDARY DECISIVE_RECOVERY_SATISFIED RESPONSIBILITY_CLEAR",
			reject:         "UNACCEPTABLE_HARM RECOVERY_UNAVAILABLE CHARTER_RISK_EXCEEDED",
			abstain:        "CONSEQUENCE_UNKNOWN RECOVERY_UNKNOWN BOUNDARY_UNCLEAR DECISIVE_CONFLICT",
			correction:     "RECOVERY_VERDICT_CHANGED IMPACT_CHANGED RESPONSIBILITY_CONFIRMED NEW_COUNTEREVIDENCE NO_ACTION_RISK_CHANGED",
		}, nil
	case "casper":
		return personaBaseline{
			id:             "CASPER-1.0",
			core:           "EXPLICIT_GOAL_CONSENT CONTROL_EXIT BURDEN_ADOPTION",
			reviews:        "EXPLICIT_GOAL MARKED_INFERENCE REQUIRED_CONSENT CONTROL_CHANGE EXIT BURDEN_TRANSFER REAL_GOAL_ADOPTION",
			investigations: "AUTHENTICATED_GOAL_PREFERENCE CONTROL_CONSENT_BURDEN",
			evidence:       "AUTHENTICATED_SUBJECT_RECORD APPLICABLE_CONSENT_AUTHORITY_CHAIN VERIFIABLE_OPERATIONAL_BURDEN INFERRED_PREFERENCE_NO_AUTHORITY",
			buckets:        "GOAL_PREFERENCE CONTROL_CONSENT BURDEN_ALTERNATIVE CORRECTION_RESERVE",
			support:        "CONFIRMED_GOAL_CONTROL_BURDEN REQUIRED_CONSENT_COMPLETE",
			reject:         "EXPLICIT_GOAL_CONFLICT UNAUTHORIZED_CONTROL_LOSS REJECTED_BURDEN",
			abstain:        "KEY_PREFERENCE_UNKNOWN CONSENT_UNKNOWN EXIT_UNKNOWN PREFERENCE_CONFLICT",
			correction:     "PREFERENCE_CONFIRMED_WITHDRAWN AUTHORITY_CHANGED BURDEN_EVIDENCE_CHANGED INFERENCE_MISLABELED",
		}, nil
	}
	return personaBaseline{}, invalid("BASELINE", "$.persona_id", "unknown formal persona")
}
func exactCodes(actual []string, expected, path string, set bool) error {
	want := strings.Fields(expected)
	if set {
		if e := stringsOrdered(actual, path); e != nil {
			return e
		}
		// Only the frozen expected ASCII code set is sorted. Input is never repaired.
		slices.Sort(want)
	}
	if !slices.Equal(actual, want) {
		return invalid("BASELINE", path, "must match the frozen v1.0 baseline codes and order; additions belong in domain_extensions")
	}
	return nil
}
func validatePersonaLocal(p PersonaSpec) error {
	b, e := baselineFor(p.PersonaID)
	if e != nil {
		return e
	}
	if p.BaselineID != b.id {
		return invalid("BASELINE", "$.baseline_id", "persona_id must match its formal v1.0 baseline")
	}
	if e = validateTimes(DomainProfile{FrozenAt: p.FrozenAt, Validity: p.Validity}); e != nil {
		return e
	}
	for _, check := range []struct {
		actual         []string
		expected, path string
		set            bool
	}{
		{p.CorePriority, b.core, "$.core_priority", false},
		{p.MandatoryReviews, b.reviews, "$.mandatory_reviews", true},
		{p.EvidencePriority, b.evidence, "$.evidence_priority", false},
		{p.SupportConditions, b.support, "$.support_conditions", true},
		{p.RejectConditions, b.reject, "$.reject_conditions", true},
		{p.AbstainConditions, b.abstain, "$.abstain_conditions", true},
		{p.CorrectionTriggers, b.correction, "$.correction_triggers", true},
	} {
		if e = exactCodes(check.actual, check.expected, check.path, check.set); e != nil {
			return e
		}
	}
	if e = validateInvestigations(p.MinimumInvestigation, "$.minimum_investigation"); e != nil {
		return e
	}
	ids := make([]string, len(p.MinimumInvestigation))
	for i, r := range p.MinimumInvestigation {
		ids[i] = r.RequirementID
	}
	if e = exactCodes(ids, b.investigations, "$.minimum_investigation", true); e != nil {
		return e
	}
	buckets := strings.Fields(b.buckets)
	var total int64
	for i, a := range p.CallAllocation {
		want := int64(1)
		if i < 2 {
			want = 2
		}
		if a.BucketID != buckets[i] || a.Calls != want {
			return invalid("BASELINE", fmt.Sprintf("$.call_allocation[%d]", i), "fixed ordered investigation buckets require 2,2,1,1 calls")
		}
		total += a.Calls
	}
	if total != p.ToolCallLimit {
		return invalid("BUDGET", "$.call_allocation", "allocation must sum to tool_call_limit")
	}
	return validatePersonaExtensions(p)
}
func validateInvestigations(items []Investigation, path string) error {
	if e := ordered(items, func(r Investigation) string { return r.RequirementID }, path); e != nil {
		return e
	}
	for i, r := range items {
		if e := stringsOrdered(r.AcceptableEvidenceRuleIDs, fmt.Sprintf("%s[%d].acceptable_evidence_rule_ids", path, i)); e != nil {
			return e
		}
	}
	return nil
}
func validatePersonaExtensions(p PersonaSpec) error {
	if e := ordered(p.DomainExtensions, func(x PersonaExtension) string { return x.ExtensionID }, "$.domain_extensions"); e != nil {
		return e
	}
	reviews := map[string]bool{}
	requirements := map[string]bool{}
	for _, id := range p.MandatoryReviews {
		reviews[id] = true
	}
	for _, r := range p.MinimumInvestigation {
		requirements[r.RequirementID] = true
	}
	for i, x := range p.DomainExtensions {
		path := fmt.Sprintf("$.domain_extensions[%d]", i)
		if e := stringsOrdered(x.ReviewItemIDs, path+".review_item_ids"); e != nil {
			return e
		}
		if e := validateInvestigations(x.MinimumInvestigation, path+".minimum_investigation"); e != nil {
			return e
		}
		for _, id := range x.ReviewItemIDs {
			if reviews[id] {
				return invalid("EXTENSION", path+".review_item_ids", "extension must add unique reviews, never shadow core or another extension")
			}
			reviews[id] = true
		}
		for _, r := range x.MinimumInvestigation {
			if requirements[r.RequirementID] {
				return invalid("EXTENSION", path+".minimum_investigation", "extension must add unique investigations, never shadow core or another extension")
			}
			requirements[r.RequirementID] = true
		}
	}
	return nil
}
