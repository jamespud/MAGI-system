package inputartifact

// validateRollout checks locally representable reference-instance constraints.
// External plans, evidence/method bindings and publishing remain unresolved;
// these checks cannot certify a deployment or classify actual candidates.
func validateRollout(d DomainProfile) error {
	b := d.Budget
	if b.RoleToolCalls > 6 || b.TaskToolCalls > 24 || b.RoleOutputTokens > 6000 || b.TaskOutputTokens > 24000 || b.TaskInputTokens > 200000 {
		return invalid("BUDGET", "$.budget", "ROLLOUT-LAB-1.0 resource ceiling exceeded")
	}
	if d.Selection.CandidateLimit != 4 {
		return invalid("INSTANCE_POLICY", "$.selection.candidate_limit", "ROLLOUT-LAB requires K_max=4")
	}
	if d.RiskPolicy.R3Execution != "DISABLED" {
		return invalid("INSTANCE_POLICY", "$.risk_policy.r3_execution", "ROLLOUT-LAB prohibits R3 execution")
	}
	w := d.Windows
	if w.ReadRetries > 1 || w.FormatRepairsPerRole > 1 {
		return invalid("BUDGET", "$.windows", "ROLLOUT-LAB retries and repairs at most one")
	}
	if w.SubmissionMs != 60000 || w.ObjectionMs != 120000 || w.RevisionMs != 150000 || w.FreezeMs != 180000 || w.ActiveDecisionMs != 300000 || w.ArbitrationAcceptMs != 3600000 || w.ArbitrationFinalMs != 86400000 || w.TaskValidityMs != 86400000 || w.ReadTimeoutMs != 10000 {
		return invalid("INSTANCE_POLICY", "$.windows", "ROLLOUT-LAB-1.0 fixed windows differ")
	}
	for _, t := range d.ToolScope {
		if t.Mode == "WRITE" && t.AdapterMode != "FENCED_IDEMPOTENT" {
			return invalid("TOOL_POLICY", "$.tool_scope", "ROLLOUT-LAB only permits FENCED_IDEMPOTENT writes")
		}
	}
	if d.Statistics.ClaimCount != 23 || d.Statistics.FamilyAlpha != (Rational{Numerator: 1, Denominator: 20}) {
		return invalid("INSTANCE_POLICY", "$.statistics", "reference family requires 23 claims and alpha 1/20")
	}
	r := d.Retention
	if r.FullInputMs != 7776000000 || r.IntegrityMetadataMs != 31536000000 || r.CacheMs > 86400000 || r.BackupErasureMs > 604800000 {
		return invalid("INSTANCE_POLICY", "$.retention", "reference retention or deletion boundary differs")
	}
	if e := rolloutEvidence(d); e != nil {
		return e
	}
	if e := rolloutParameters(d); e != nil {
		return e
	}
	return rolloutSLA(d)
}
func rolloutEvidence(d DomainProfile) error {
	telemetry := false
	for _, r := range d.EvidenceRules {
		var ttl int64
		switch r.AssertionKind {
		case "FACT":
			ttl = 86400000
		case "PREDICTION":
			ttl = 3600000
		case "USER_PREFERENCE":
			ttl = 86400000
		case "EXTERNAL_AUTHORITY":
			ttl = 300000
		}
		// 'fact' is this implementation's local telemetry code. Other FACT rules
		// may describe recovery, whose maximum is 24h. Qualification is still pending.
		if r.RuleID == "fact" {
			if r.AssertionKind != "FACT" {
				return invalid("INSTANCE_POLICY", "$.evidence_rules", "telemetry fact code has wrong assertion kind")
			}
			ttl = 120000
			telemetry = true
		}
		if r.TTLMs > ttl {
			return invalid("INSTANCE_POLICY", "$.evidence_rules", "reference evidence lifetime exceeded: "+r.RuleID)
		}
	}
	if !telemetry {
		return invalid("INSTANCE_POLICY", "$.evidence_rules", "local reference telemetry rule 'fact' required")
	}
	return nil
}
func rolloutParameters(d DomainProfile) error {
	// The code names are local registry choices, not new Rev4 authority fields.
	expected := []InstanceParameter{
		{ParameterID: "active-task-limit", Value: Quantity{Value: 20, Unit: "count"}},
		{ParameterID: "canary-c2", Value: Quantity{Value: 5, Unit: "ratio", Scale: 2}},
		{ParameterID: "canary-c3", Value: Quantity{Value: 10, Unit: "ratio", Scale: 2}},
		{ParameterID: "error-increase", Value: Quantity{Value: 5, Unit: "percentage_point", Scale: 1}},
		{ParameterID: "execution-limit", Value: Quantity{Value: 990000, Unit: "millisecond"}},
		{ParameterID: "recovery-bound", Value: Quantity{Value: 60000, Unit: "millisecond"}},
		{ParameterID: "terminal-review-freshness", Value: Quantity{Value: 30000, Unit: "millisecond"}},
		{ParameterID: "trial-duration", Value: Quantity{Value: 900000, Unit: "millisecond"}},
	}
	for _, want := range expected {
		found := false
		for _, p := range d.InstanceParameters {
			if p.ParameterID == want.ParameterID {
				found = true
				cmp, e := CompareQuantities(p.Value, want.Value)
				if e != nil {
					return e
				}
				if cmp != 0 {
					return invalid("INSTANCE_POLICY", "$.instance_parameters", "reference threshold differs: "+p.ParameterID)
				}
			}
		}
		if !found {
			return invalid("INSTANCE_POLICY", "$.instance_parameters", "missing local reference parameter "+want.ParameterID)
		}
	}
	return nil
}
func rolloutSLA(d DomainProfile) error {
	expected := []struct {
		id, op string
		q      Quantity
	}{
		{"active-decision-p95", "LE", Quantity{Value: 240000, Unit: "millisecond"}},
		{"automatic-coverage", "GE", Quantity{Value: 90, Unit: "ratio", Scale: 2}},
		{"indeterminate", "LE", Quantity{Value: 2, Unit: "ratio", Scale: 2}},
		{"protocol-completion", "GE", Quantity{Value: 99, Unit: "ratio", Scale: 2}},
	}
	for _, want := range expected {
		found := false
		for _, s := range d.SLATargets {
			if s.MetricID == want.id {
				found = true
				cmp, e := CompareQuantities(s.Bound, want.q)
				if e != nil {
					return e
				}
				if s.Operator != want.op || (want.op == "LE" && cmp > 0) || (want.op == "GE" && cmp < 0) {
					return invalid("INSTANCE_POLICY", "$.sla_targets", "reference SLA weakened: "+s.MetricID)
				}
			}
		}
		if !found {
			return invalid("INSTANCE_POLICY", "$.sla_targets", "missing local reference SLA "+want.id)
		}
	}
	return nil
}
