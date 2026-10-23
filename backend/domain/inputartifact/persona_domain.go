package inputartifact

import (
	"fmt"
	"regexp"
	"slices"
	"strings"
)

var exactDomainID = regexp.MustCompile(`^dprf_[0-9a-f]{64}$`)

// requirementKinds is a conservative local type mapping from Rev4 8 / 17.
// It checks declarations, not source/method qualification or actual evidence.
// Predictions can inform feasibility/impact, never observed baseline, recovery,
// authenticated preference or actual control/consent/burden records.
func requirementKinds(id string) []string {
	switch id {
	case "EFFECT_BASELINE", "RECOVERY_VERIFICATION":
		return []string{"FACT"}
	case "FEASIBILITY_COUNTEREVIDENCE", "FAILURE_IMPACT":
		return []string{"FACT", "PREDICTION"}
	case "AUTHENTICATED_GOAL_PREFERENCE":
		return []string{"USER_PREFERENCE"}
	case "CONTROL_CONSENT_BURDEN":
		return []string{"EXTERNAL_AUTHORITY", "FACT"}
	}
	return nil
}

func validatePersonaDomain(p PersonaSpec, opts PersonaValidationOptions) ([]Dependency, string, Budget, bool, error) {
	var pending []Dependency
	status := "UNVERIFIABLE_REFERENCE"
	if opts.DomainProfileID != "" && !exactDomainID.MatchString(opts.DomainProfileID) {
		return nil, "", Budget{}, false, invalid("REFERENCE", "$.domain_profile_id", "exact immutable dprf_ identity required")
	}
	// Even without domain bytes, a contradiction among declared selected IDs is
	// locally decidable and must not be hidden behind the missing dependency.
	declaredID := opts.DomainProfileID
	for i, x := range p.DomainExtensions {
		if declaredID == "" {
			declaredID = x.DomainProfileID
		}
		if x.DomainProfileID != declaredID {
			return nil, "", Budget{}, false, invalid("REFERENCE", fmt.Sprintf("$.domain_extensions[%d].domain_profile_id", i), "extension must reference the selected exact domain version")
		}
	}
	var caps []Budget
	if opts.DomainProfile == nil {
		if opts.RequireDomain {
			return nil, "", Budget{}, false, invalid("UNVERIFIABLE_REFERENCE", "$.domain_profile_id", "selected DomainProfile bytes unavailable")
		}
		pending = append(pending, Dependency{Kind: "UNVERIFIABLE_REFERENCE", Path: "$.domain_profile_id", Detail: "exact selected DomainProfile and its evidence rules/budgets unavailable; all cross-artifact relations remain unverified"})
	} else {
		domain := *opts.DomainProfile
		if opts.DomainProfileID == "" || domain.ArtifactID() == "" || opts.DomainProfileID != domain.ArtifactID() {
			return nil, "", Budget{}, false, invalid("REFERENCE", "$.domain_profile_id", "nonzero validated DomainProfile must match caller-selected exact ID")
		}
		d := domain.Body()
		if (d.Validity.ValidUntil != nil && p.Validity.NotBefore >= *d.Validity.ValidUntil) || (p.Validity.ValidUntil != nil && d.Validity.NotBefore >= *p.Validity.ValidUntil) {
			return nil, "", Budget{}, false, invalid("REFERENCE", "$.validity", "Persona and Domain declared validity intervals do not overlap")
		}
		status = "CHECKED_OFFLINE"
		rules := map[string]EvidenceRule{}
		for _, r := range d.EvidenceRules {
			rules[r.RuleID] = r
		}
		check := func(items []Investigation, path string) error {
			for i, r := range items {
				at := fmt.Sprintf("%s[%d]", path, i)
				kinds := requirementKinds(r.RequirementID)
				for j, id := range r.AcceptableEvidenceRuleIDs {
					rule, ok := rules[id]
					if !ok {
						return invalid("REFERENCE", fmt.Sprintf("%s.acceptable_evidence_rule_ids[%d]", at, j), "evidence rule absent from selected exact domain")
					}
					if len(kinds) > 0 && !slices.Contains(kinds, rule.AssertionKind) {
						return invalid("EVIDENCE", at+".acceptable_evidence_rule_ids", "assertion kind cannot satisfy formal investigation "+r.RequirementID)
					}
				}
				if len(kinds) == 0 {
					status = "PARTIAL_OFFLINE"
					pending = append(pending, Dependency{Kind: "UNVERIFIABLE_REQUIREMENT_BINDING", Path: at, Detail: "extension investigation code has no frozen local assertion-type mapping; external approved meaning and method binding unverified"})
				}
				pending = append(pending, Dependency{Kind: "UNVERIFIABLE_APPLICABILITY", Path: at, Detail: "rule existence/type only: actual source authentication, method, environment/scope, same-environment recovery, freshness, independent record/substitution and read-set evidence not verified"})
			}
			return nil
		}
		if e := check(p.MinimumInvestigation, "$.minimum_investigation"); e != nil {
			return nil, "", Budget{}, false, e
		}
		for i, x := range p.DomainExtensions {
			if e := check(x.MinimumInvestigation, fmt.Sprintf("$.domain_extensions[%d].minimum_investigation", i)); e != nil {
				return nil, "", Budget{}, false, e
			}
		}
		// A locally valid DomainProfile is neither qualified nor authorized. Carry
		// every unresolved domain dependency forward with its original path.
		for _, dep := range domain.PendingDependencies() {
			dep.Path = "$.domain_profile" + strings.TrimPrefix(dep.Path, "$")
			pending = append(pending, dep)
		}
		caps = append(caps, d.Budget)
	}
	caps = append(caps, opts.AdditionalBudgets...)
	var budget Budget
	hasBudget := len(caps) > 0
	if hasBudget {
		b, e := ComposeBudgets(caps[0], caps[1:]...)
		if e != nil {
			return nil, "", Budget{}, false, e
		}
		budget, e = ComposePersonaV1Budget(b)
		if e != nil {
			return nil, "", Budget{}, false, e
		}
		if budget.CostLimit != nil {
			pending = append(pending, Dependency{Kind: "UNVERIFIABLE_BILLING_MAPPING", Path: "$.budget.cost_limit", Detail: "supplied cost ceilings are intersected exactly; trusted currency/exponent mapping and actual charges remain unverified"})
		}
		count := len(p.MinimumInvestigation)
		for _, x := range p.DomainExtensions {
			count += len(x.MinimumInvestigation)
		}
		if budget.RoleToolCalls < int64(count) {
			pending = append(pending, Dependency{Kind: "RUNTIME_BUDGET_REVIEW_REQUIRED", Path: "$.budget", Detail: "direct minimum investigations exceed supplied call caps; runtime must prove permissible independent-record reuse or abstain/defer; no investigation was executed"})
		}
	}
	pending = append(pending, Dependency{Kind: "UNVERIFIABLE_RELEASE_HISTORY", Path: "$.compatibility", Detail: "release registration, compatible-version continuity, external behavioral acceptance and revocation history are not verified"})
	return pending, status, budget, hasBudget, nil
}
