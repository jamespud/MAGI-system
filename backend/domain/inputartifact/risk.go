package inputartifact

import (
	"bytes"
	"encoding/json"
	"fmt"
)

func validateRisk(d DomainProfile, v Vocabulary) error {
	required := map[string]bool{}
	for i, id := range d.RiskPolicy.RequiredAttributes {
		if _, ok := v.Attributes[id]; !ok {
			return invalid("REFERENCE", fmt.Sprintf("$.risk_policy.required_attributes[%d]", i), "unregistered risk attribute")
		}

		spec := v.Attributes[id]
		switch spec.Kind {
		case "boolean", "token":
			if spec.Unit != "" {
				return invalid("REFERENCE", fmt.Sprintf("$.risk_policy.required_attributes[%d]", i), "non-quantity code cannot declare a unit")
			}
		case "quantity":
			if e := validateQuantity(Quantity{Unit: spec.Unit}, fmt.Sprintf("$.risk_policy.required_attributes[%d]", i)); e != nil {
				return e
			}
		default:
			return invalid("REFERENCE", fmt.Sprintf("$.risk_policy.required_attributes[%d]", i), "unknown registered value type")
		}
		required[id] = true
	}
	for _, id := range []string{"bounded", "in_domain", "protected_effect", "recovery_verified", "reversible", "within_quota"} {
		if !required[id] {
			return invalid("RISK_POLICY", "$.risk_policy.required_attributes", "missing critical attribute "+id)
		}
	}
	evidence := map[string]bool{}
	for _, r := range d.EvidenceRules {
		evidence[r.RuleID] = true
	}
	for i, r := range d.RiskPolicy.Rules {
		path := fmt.Sprintf("$.risk_policy.rules[%d]", i)
		for j, id := range r.EvidenceRuleIDs {
			if !evidence[id] {
				return invalid("REFERENCE", fmt.Sprintf("%s.evidence_rule_ids[%d]", path, j), "missing evidence rule")
			}
		}
		gates := map[string]bool{}
		for j, p := range r.AllOf {
			pp := fmt.Sprintf("%s.all_of[%d]", path, j)
			spec, ok := v.Attributes[p.AttributeID]
			if !ok || !required[p.AttributeID] {
				return invalid("REFERENCE", pp+".attribute_id", "attribute must be registered and required")
			}
			if e := validatePredicate(p, spec, pp); e != nil {
				return e
			}
			if r.Level != "R3" && p.Operator == "UNKNOWN" {
				return invalid("RISK_POLICY", path, "unknown attributes cannot justify a lower risk class")
			}
			if p.Operator == "EQ" && spec.Kind == "boolean" {
				gates[p.AttributeID] = bytes.Equal(p.Expected, []byte("true"))
			}
		}
		// Conservative sufficient conditions for lower-class rules. Classification
		// must still apply all universal gates and MAX_APPLICABLE at runtime.
		needs := map[string]bool{}
		switch r.Level {
		case "R0":
			needs["protected_effect"] = false
		case "R1", "R2":
			needs = map[string]bool{"protected_effect": true, "bounded": true, "in_domain": true, "recovery_verified": true, "reversible": true, "within_quota": true}
		}
		if r.Level == "R2" {
			delete(needs, "within_quota")
		}
		for _, id := range []string{"protected_effect", "bounded", "in_domain", "recovery_verified", "reversible", "within_quota"} {
			if want, ok := needs[id]; ok {
				got, present := gates[id]
				if !present || got != want {
					return invalid("RISK_POLICY", path, "lower risk rule lacks required safety gate "+id)
				}
			}
		}
	}
	return nil
}
func validatePredicate(p RiskPredicate, spec ValueSpec, path string) error {
	if p.Operator == "KNOWN" || p.Operator == "UNKNOWN" {
		if len(p.Expected) != 0 {
			return invalid("PREDICATE", path+".expected", "KNOWN/UNKNOWN must omit expected")
		}
		return nil
	}
	if len(p.Expected) == 0 {
		return invalid("PREDICATE", path+".expected", "comparison requires expected")
	}
	if p.Operator != "EQ" && spec.Kind != "quantity" {
		return invalid("PREDICATE", path+".operator", "LE/GE require a registered quantity")
	}
	switch spec.Kind {
	case "boolean":
		var x bool
		if e := json.Unmarshal(p.Expected, &x); e != nil || bytes.Equal(p.Expected, []byte("null")) {
			return invalid("PREDICATE", path+".expected", "expected boolean")
		}
	case "token":
		var x string
		if e := json.Unmarshal(p.Expected, &x); e != nil {
			return invalid("PREDICATE", path+".expected", "expected token")
		}
	case "quantity":
		var q Quantity
		if e := json.Unmarshal(p.Expected, &q); e != nil {
			return invalid("PREDICATE", path+".expected", "expected quantity")
		}
		return dimension(q, spec.Unit, path+".expected")
	default:
		return invalid("PREDICATE", path, "unsupported registered value type")
	}
	return nil
}
