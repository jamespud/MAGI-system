package evidence

import (
	"fmt"
	"strings"

	"github.com/jamespud/magi/backend/domain/entity"
)

type GateViolation struct {
	Code, Message, Field, Current, Required string
}

type GateResult struct {
	Passed     bool
	Violations []GateViolation
}

type EvidenceGate struct {
	ruleRegistry RuleRegistry
}

func NewEvidenceGate() *EvidenceGate {
	return &EvidenceGate{ruleRegistry: DefaultRuleRegistry()}
}

func NewEvidenceGateWithRegistry(registry RuleRegistry) *EvidenceGate {
	return &EvidenceGate{ruleRegistry: registry}
}

func (g *EvidenceGate) Evaluate(summary *entity.EvidenceSummary, ledger *EvidenceLedger, standard entity.EvidenceStandard, collector string) *GateResult {
	res := &GateResult{Passed: true}
	if summary == nil {
		res.Passed = false
		res.Violations = append(res.Violations, GateViolation{Code: "NIL_SUMMARY", Message: "evidence summary is nil"})
		return res
	}
	allIDs := make(map[string]bool)
	typeCounts := make(map[string]int)
	for typ, ids := range summary.EvidenceByType {
		for _, id := range ids {
			allIDs[id] = true
			typeCounts[typ]++
		}
	}
	if len(allIDs) < standard.MinEvidenceCount {
		res.Passed = false
		res.Violations = append(res.Violations, GateViolation{Code: "INSUFFICIENT_EVIDENCE", Message: fmt.Sprintf("need >= %d, got %d", standard.MinEvidenceCount, len(allIDs)), Current: fmt.Sprintf("%d", len(allIDs)), Required: fmt.Sprintf("%d", standard.MinEvidenceCount)})
	}
	if typeCounts["quantitative"] < standard.MinQuantitativeCount {
		res.Passed = false
		res.Violations = append(res.Violations, GateViolation{Code: "INSUFFICIENT_QUANTITATIVE", Message: fmt.Sprintf("need >= %d quantitative, got %d", standard.MinQuantitativeCount, typeCounts["quantitative"]), Current: fmt.Sprintf("%d", typeCounts["quantitative"]), Required: fmt.Sprintf("%d", standard.MinQuantitativeCount)})
	}
	for _, rt := range standard.RequiredTypes {
		if typeCounts[rt.Type] < rt.MinCount {
			res.Passed = false
			res.Violations = append(res.Violations, GateViolation{Code: "MISSING_REQUIRED_TYPE", Message: fmt.Sprintf("type %q need >= %d, got %d", rt.Type, rt.MinCount, typeCounts[rt.Type]), Field: rt.Type})
		}
	}
	if len(summary.Claims) < standard.RequiredClaimCount {
		res.Passed = false
		res.Violations = append(res.Violations, GateViolation{Code: "INSUFFICIENT_CLAIMS", Message: fmt.Sprintf("need >= %d claims, got %d", standard.RequiredClaimCount, len(summary.Claims))})
	}
	collectorCheck := ""
	if standard.RequireOwnCollected {
		collectorCheck = collector
	}
	for id := range allIDs {
		if !ledger.ExistsCollected(id, collectorCheck) {
			res.Passed = false
			res.Violations = append(res.Violations, GateViolation{Code: "EVIDENCE_NOT_FOUND", Message: fmt.Sprintf("EV-ID %q not found or not own", id), Field: id})
			continue
		}
		rel, ok := ledger.Reliability(id)
		if !ok || rel < standard.MinReliability {
			res.Passed = false
			res.Violations = append(res.Violations, GateViolation{Code: "RELIABILITY_BELOW_THRESHOLD", Message: fmt.Sprintf("EV-ID %q reliability %.2f below %.2f", id, rel, standard.MinReliability), Field: id})
		}
	}
	for i, c := range summary.Claims {
		for _, sid := range c.Supports {
			if !ledger.ExistsCollected(sid, collectorCheck) {
				res.Passed = false
				res.Violations = append(res.Violations, GateViolation{Code: "CLAIM_UNSUPPORTED", Message: fmt.Sprintf("claim #%d supports non-existent EV-ID %q", i, sid), Field: sid})
			}
		}
	}
	// Evaluate custom rules
	for _, rule := range standard.CustomRules {
		fn, ok := g.ruleRegistry[rule.Code]
		if !ok {
			res.Passed = false
			res.Violations = append(res.Violations, GateViolation{
				Code:    "UNKNOWN_CUSTOM_RULE",
				Message: fmt.Sprintf("no custom rule registered for code %q", rule.Code),
			})
			continue
		}
		if v := fn(summary, ledger, collector); v != nil {
			res.Passed = false
			res.Violations = append(res.Violations, *v)
		}
	}
	return res
}

// CustomRuleFunc is a deterministic Go function that checks an evidence gate rule.
type CustomRuleFunc func(summary *entity.EvidenceSummary, ledger *EvidenceLedger, collector string) *GateViolation

// RuleRegistry maps rule codes to their check functions.
type RuleRegistry map[string]CustomRuleFunc

// DefaultRuleRegistry returns the registry of all known custom rules.
func DefaultRuleRegistry() RuleRegistry {
	return RuleRegistry{
		"worst_case_claim_required":  checkWorstCaseClaimRequired,
		"reversibility_assessment":   checkReversibilityAssessment,
		"opportunity_cost_claim":     checkOpportunityCostClaim,
		"time_window_assessment":     checkTimeWindowAssessment,
		"primary_source_required":    checkPrimarySourceRequired,
		"utility_dimension_coverage": checkUtilityDimensionCoverage,
	}
}

func claimContainsKeyword(claims []entity.EvidenceSummaryClaim, keywords []string) bool {
	for _, c := range claims {
		lower := strings.ToLower(c.Statement)
		for _, kw := range keywords {
			if strings.Contains(lower, kw) {
				return true
			}
		}
	}
	return false
}

func checkWorstCaseClaimRequired(summary *entity.EvidenceSummary, _ *EvidenceLedger, _ string) *GateViolation {
	if claimContainsKeyword(summary.Claims, []string{"worst case", "worst-case"}) {
		return nil
	}
	return &GateViolation{Code: "WORST_CASE_CLAIM_MISSING", Message: "no worst-case claim found in summary claims"}
}

func checkReversibilityAssessment(summary *entity.EvidenceSummary, _ *EvidenceLedger, _ string) *GateViolation {
	if claimContainsKeyword(summary.Claims, []string{"reversible", "reversibility", "rollback", "undo"}) {
		return nil
	}
	return &GateViolation{Code: "REVERSIBILITY_MISSING", Message: "no reversibility assessment found in summary claims"}
}

func checkOpportunityCostClaim(summary *entity.EvidenceSummary, _ *EvidenceLedger, _ string) *GateViolation {
	if claimContainsKeyword(summary.Claims, []string{"opportunity cost", "opportunity", "trade-off", "tradeoff"}) {
		return nil
	}
	return &GateViolation{Code: "OPPORTUNITY_COST_MISSING", Message: "no opportunity cost claim found in summary claims"}
}

func checkTimeWindowAssessment(summary *entity.EvidenceSummary, _ *EvidenceLedger, _ string) *GateViolation {
	if claimContainsKeyword(summary.Claims, []string{"time window", "timing", "window of opportunity", "deadline"}) {
		return nil
	}
	return &GateViolation{Code: "TIME_WINDOW_MISSING", Message: "no time window assessment found in summary claims"}
}

func checkPrimarySourceRequired(summary *entity.EvidenceSummary, ledger *EvidenceLedger, _ string) *GateViolation {
	for _, ids := range summary.EvidenceByType {
		for _, evID := range ids {
			ev, ok := ledger.Get(evID)
			if ok && ev != nil && (ev.SourceType == entity.EvidenceSourceLocal || ev.SourceType == entity.EvidenceSourceCodeRun) {
				return nil
			}
		}
	}
	return &GateViolation{Code: "PRIMARY_SOURCE_MISSING", Message: "no primary/technical source evidence found"}
}

func checkUtilityDimensionCoverage(summary *entity.EvidenceSummary, _ *EvidenceLedger, _ string) *GateViolation {
	if len(summary.Claims) >= 2 && len(summary.EvidenceByType) >= 2 {
		return nil
	}
	return &GateViolation{Code: "DIMENSION_COVERAGE_WEAK", Message: "insufficient utility dimension coverage: need >=2 claims and >=2 evidence types"}
}
