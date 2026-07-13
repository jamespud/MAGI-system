package evidence

import (
	"fmt"

	"github.com/jamespud/magi/backend/domain/entity"
)

type GateViolation struct {
	Code, Message, Field, Current, Required string
}

type GateResult struct {
	Passed     bool
	Violations []GateViolation
}

type EvidenceGate struct{}

func NewEvidenceGate() *EvidenceGate { return &EvidenceGate{} }

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
	return res
}
