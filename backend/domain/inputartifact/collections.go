package inputartifact

import (
	"fmt"
	"slices"
	"unicode/utf16"
)

func lessUTF16(a, b string) bool {
	return slices.Compare(utf16.Encode([]rune(a)), utf16.Encode([]rune(b))) < 0
}
func ordered[T any](items []T, key func(T) string, path string) error {
	for i := 1; i < len(items); i++ {
		if !lessUTF16(key(items[i-1]), key(items[i])) {
			return invalid("COLLECTION", fmt.Sprintf("%s[%d]", path, i), "set must have strictly increasing unique keys; input is never reordered")
		}
	}
	return nil
}
func stringsOrdered(items []string, path string) error {
	return ordered(items, func(s string) string { return s }, path)
}
func refKey(r ExternalRef) string {
	return r.Namespace + "\x00" + r.ObjectID + "\x00" + r.Version + "\x00" + r.DigestAlg + "\x00" + r.ContentDigest
}
func validateCollections(d DomainProfile) error {
	if e := stringsOrdered(d.RiskPolicy.RequiredAttributes, "$.risk_policy.required_attributes"); e != nil {
		return e
	}
	if e := ordered(d.RiskPolicy.Rules, func(r RiskRule) string { return r.RuleID }, "$.risk_policy.rules"); e != nil {
		return e
	}
	for i, r := range d.RiskPolicy.Rules {
		path := fmt.Sprintf("$.risk_policy.rules[%d]", i)
		if e := ordered(r.AllOf, func(p RiskPredicate) string { return p.AttributeID }, path+".all_of"); e != nil {
			return e
		}
		if e := stringsOrdered(r.EvidenceRuleIDs, path+".evidence_rule_ids"); e != nil {
			return e
		}
	}
	if e := ordered(d.EvidenceRules, func(r EvidenceRule) string { return r.RuleID }, "$.evidence_rules"); e != nil {
		return e
	}
	for i, r := range d.EvidenceRules {
		path := fmt.Sprintf("$.evidence_rules[%d]", i)
		if e := stringsOrdered(r.SourceKinds, path+".source_kinds"); e != nil {
			return e
		}
		if e := stringsOrdered(r.InvalidatingEvents, path+".invalidating_events"); e != nil {
			return e
		}
		if e := ordered(r.MethodRefs, refKey, path+".method_refs"); e != nil {
			return e
		}
	}
	if e := ordered(d.ToolScope, func(t Tool) string { return t.ToolID }, "$.tool_scope"); e != nil {
		return e
	}
	for i, t := range d.ToolScope {
		p := fmt.Sprintf("$.tool_scope[%d]", i)
		if e := stringsOrdered(t.ResourceIDs, p+".resource_ids"); e != nil {
			return e
		}
		if e := stringsOrdered(t.RiskLevels, p+".risk_levels"); e != nil {
			return e
		}
	}
	if e := stringsOrdered(d.Selection.AllowedEarlySelectionPolicies, "$.selection.allowed_early_selection_policies"); e != nil {
		return e
	}
	found := false
	for _, p := range d.Selection.AllowedEarlySelectionPolicies {
		if p == d.Selection.DefaultEarlySelectionPolicy {
			found = true
		}
	}
	if !found {
		return invalid("REFERENCE", "$.selection.default_early_selection_policy", "default must be an allowed policy")
	}
	if e := ordered(d.SLATargets, func(t SLATarget) string { return t.MetricID }, "$.sla_targets"); e != nil {
		return e
	}
	return ordered(d.InstanceParameters, func(p InstanceParameter) string { return p.ParameterID }, "$.instance_parameters")
}
