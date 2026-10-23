package inputartifact

import (
	"fmt"
	"time"
)

// ValueSpec describes a local code's type and dimension, never its qualification.
type ValueSpec struct {
	Kind string
	Unit string
}
type Vocabulary struct {
	Attributes map[string]ValueSpec
	Metrics    map[string]string
	Parameters map[string]string
}

// DefaultVocabulary names the conservative local risk gates and the dimensions
// used by the contract examples. These spellings are implementation vocabulary,
// not authenticated method registrations or a complete ROLLOUT-LAB policy.
func DefaultVocabulary() Vocabulary {
	return Vocabulary{Attributes: map[string]ValueSpec{
		"bounded": {Kind: "boolean"}, "in_domain": {Kind: "boolean"}, "protected_effect": {Kind: "boolean"}, "recovery_verified": {Kind: "boolean"}, "reversible": {Kind: "boolean"}, "within_quota": {Kind: "boolean"},
	}, Metrics: map[string]string{"active-decision-p95": "millisecond", "automatic-coverage": "ratio", "indeterminate": "ratio", "protocol-completion": "ratio"}, Parameters: map[string]string{"canary-c2": "ratio", "error-increase": "percentage_point", "recovery-bound": "millisecond", "canary-c3": "ratio", "trial-duration": "millisecond", "terminal-review-freshness": "millisecond", "execution-limit": "millisecond", "active-task-limit": "count"}}
}
func validateDomain(d DomainProfile, opts ValidationOptions) ([]Dependency, error) {
	v := DefaultVocabulary()
	if opts.Vocabulary != nil {
		v = *opts.Vocabulary
	}
	// A caller cannot redefine a reserved safety attribute's meaning/type.
	for _, code := range []string{"bounded", "in_domain", "protected_effect", "recovery_verified", "reversible", "within_quota"} {
		spec := ValueSpec{Kind: "boolean"}
		if got, ok := v.Attributes[code]; !ok || got != spec {
			return nil, invalid("REFERENCE", "$.risk_policy.required_attributes", "reserved safety attributes must retain boolean type: "+code)
		}
	}
	if e := validateTimes(d); e != nil {
		return nil, e
	}
	if e := validateCollections(d); e != nil {
		return nil, e
	}
	if e := validateEvidence(d); e != nil {
		return nil, e
	}
	if e := validateRisk(d, v); e != nil {
		return nil, e
	}
	if e := validateValues(d, v); e != nil {
		return nil, e
	}
	if e := validateTools(d); e != nil {
		return nil, e
	}
	if e := validateBudget(d.Budget); e != nil {
		return nil, e
	}
	if e := validateWindows(d.Windows); e != nil {
		return nil, e
	}
	if d.DomainKey == "ROLLOUT-LAB" {
		if e := validateRollout(d); e != nil {
			return nil, e
		}
	}
	return pendingReferences(d)
}

const millisLayout = "2006-01-02T15:04:05.000Z"

func parseTime(s, path string) (time.Time, error) {
	t, e := time.Parse(millisLayout, s)
	if e != nil || len(s) != 24 || t.Format(millisLayout) != s {
		return time.Time{}, invalid("TIME", path, "calendar-valid UTC time with exactly three milliseconds required")
	}
	return t, nil
}
func validateTimes(d DomainProfile) error {
	if _, e := parseTime(d.FrozenAt, "$.frozen_at"); e != nil {
		return e
	}
	start, e := parseTime(d.Validity.NotBefore, "$.validity.not_before")
	if e != nil {
		return e
	}
	if d.Validity.ValidUntil != nil {
		end, e := parseTime(*d.Validity.ValidUntil, "$.validity.valid_until")
		if e != nil {
			return e
		}
		if !start.Before(end) {
			return invalid("TIME", "$.validity", "not_before must precede valid_until")
		}
	}
	return nil
}
func validateEvidence(d DomainProfile) error {
	kinds := map[string]bool{}
	for _, r := range d.EvidenceRules {
		kinds[r.AssertionKind] = true
	}
	for _, k := range []string{"FACT", "PREDICTION", "USER_PREFERENCE", "EXTERNAL_AUTHORITY"} {
		if !kinds[k] {
			return invalid("EVIDENCE", "$.evidence_rules", "missing assertion kind "+k)
		}
	}
	return nil
}
func dimension(q Quantity, unit, path string) error {
	if e := validateQuantity(q, path); e != nil {
		return e
	}
	if unit == "" {
		return invalid("REFERENCE", path, "code has no registered unit")
	}
	if q.Unit != unit {
		return invalid("QUANTITY", path+".unit", "expected registered dimension "+unit)
	}
	return nil
}
func validateValues(d DomainProfile, v Vocabulary) error {
	for i, p := range d.InstanceParameters {
		path := fmt.Sprintf("$.instance_parameters[%d].value", i)
		u, ok := v.Parameters[p.ParameterID]
		if !ok {
			return invalid("REFERENCE", path, "unknown parameter code")
		}
		if e := dimension(p.Value, u, path); e != nil {
			return e
		}
	}
	for i, s := range d.SLATargets {
		path := fmt.Sprintf("$.sla_targets[%d].bound", i)
		u, ok := v.Metrics[s.MetricID]
		if !ok {
			return invalid("REFERENCE", path, "unknown metric code")
		}
		if e := dimension(s.Bound, u, path); e != nil {
			return e
		}
	}
	return validateRational(d.Statistics.FamilyAlpha, "$.statistics.family_alpha")
}
func validateWindows(w Windows) error {
	if !(w.SubmissionMs < w.ObjectionMs && w.ObjectionMs < w.RevisionMs && w.RevisionMs < w.FreezeMs && w.FreezeMs <= w.ActiveDecisionMs) {
		return invalid("TIME", "$.windows", "phase deadlines must increase through freeze and active decision")
	}
	if w.ArbitrationAcceptMs > w.ArbitrationFinalMs {
		return invalid("TIME", "$.windows", "arbitration acceptance cannot follow final deadline")
	}
	return nil
}
