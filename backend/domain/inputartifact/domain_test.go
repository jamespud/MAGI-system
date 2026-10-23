package inputartifact

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/jamespud/magi/backend/domain/execution"
)

func fixture(t *testing.T) DomainProfile {
	t.Helper()
	var d DomainProfile
	if err := json.Unmarshal(read(t, "testdata/domain_profile.json"), &d); err != nil {
		t.Fatal(err)
	}
	return d
}
func read(t *testing.T, path string) []byte {
	t.Helper()
	b, e := os.ReadFile(path)
	if e != nil {
		t.Fatal(e)
	}
	return b
}
func encode(t *testing.T, v any) []byte {
	t.Helper()
	b, e := json.Marshal(v)
	if e != nil {
		t.Fatal(e)
	}
	return b
}
func validate(t *testing.T, d DomainProfile) ValidatedDomainProfile {
	t.Helper()
	v, e := ValidateDomainProfile(encode(t, d), "", ValidationOptions{})
	if e != nil {
		t.Fatal(e)
	}
	if len(v.PendingDependencies()) == 0 {
		t.Fatal("synthetic references incorrectly qualified")
	}
	return v
}
func reject(t *testing.T, edit func(*DomainProfile), kind, path string) {
	t.Helper()
	d := fixture(t)
	validate(t, d)
	edit(&d)
	v, e := ValidateDomainProfile(encode(t, d), "", ValidationOptions{})
	var ve *Error
	if !errors.As(e, &ve) || ve.Kind != kind || !strings.Contains(ve.Path, path) {
		t.Fatalf("want %s at %s; got %v", kind, path, e)
	}
	if v.ArtifactID() != "" {
		t.Fatal("failure leaked validated identity")
	}
}
func TestDomainGolden(t *testing.T) {
	raw := read(t, "testdata/domain_profile.json")
	v, e := ValidateDomainProfile(raw, "", ValidationOptions{})
	if e != nil {
		t.Fatal(e)
	}
	var g struct {
		Digest string `json:"digest"`
		ID     string `json:"artifact_id"`
	}
	if e = json.Unmarshal(read(t, "testdata/golden.json"), &g); e != nil {
		t.Fatal(e)
	}
	if v.ArtifactID() != g.ID || v.Digest() != g.Digest || !bytes.Equal(v.CanonicalBytes(), read(t, "testdata/domain_profile.canonical.json")) {
		t.Fatal("independent golden mismatch")
	}
	if _, e = ReadDomainProfile(v.CanonicalBytes(), g.ID, ValidationOptions{}); e != nil {
		t.Fatal(e)
	}
	// Neither a mutable body nor returned byte/dependency slices may alter identity.
	b := v.Body()
	b.RiskPolicy.Rules[0].Level = "R3"
	cb := v.CanonicalBytes()
	cb[0] = '!'
	deps := v.PendingDependencies()
	deps[0].Path = "fake"
	if v.Body().RiskPolicy.Rules[0].Level != "R0" || v.CanonicalBytes()[0] != '{' || v.PendingDependencies()[0].Path == "fake" {
		t.Fatal("validated result aliases caller state")
	}
	for _, edit := range []func(*DomainProfile){func(d *DomainProfile) { d.CreatedBy.SubjectID = "other" }, func(d *DomainProfile) { d.GovernancePolicyRef.ContentDigest = strings.Repeat("1", 64) }, func(d *DomainProfile) { d.Version = "1.0.1" }, func(d *DomainProfile) { d.Validity.ValidUntil = ptr("2027-01-01T00:00:00.000Z") }} {
		d := fixture(t)
		edit(&d)
		if validate(t, d).ArtifactID() == v.ArtifactID() {
			t.Fatal("authority mutation did not change identity")
		}
	}
}
func ptr(s string) *string { return &s }
func TestDomainStrictInputAndReadback(t *testing.T) {
	raw := read(t, "testdata/domain_profile.json")
	v, e := ValidateDomainProfile(raw, "", ValidationOptions{})
	if e != nil {
		t.Fatal(e)
	}
	for name, b := range map[string][]byte{"fence": append([]byte("```json\n"), raw...), "duplicate": bytes.Replace(raw, []byte(`"artifact_type":`), []byte(`"version":"1.0","artifact_type":`), 1), "float": bytes.Replace(raw, []byte(`"role_tool_calls": 6`), []byte(`"role_tool_calls": 6.0`), 1)} {
		t.Run(name, func(t *testing.T) {
			if _, e := ValidateDomainProfile(b, "", ValidationOptions{}); e == nil {
				t.Fatal("strict parser accepted invalid input")
			}
		})
	}
	if _, e = ReadDomainProfile(raw, v.ArtifactID(), ValidationOptions{}); e == nil {
		t.Fatal("noncanonical stored bytes accepted")
	}
	if _, e = ReadDomainProfile(v.CanonicalBytes(), "", ValidationOptions{}); e == nil {
		t.Fatal("readback without expected identity accepted")
	}
	var env Envelope
	env = Envelope{ArtifactID: v.ArtifactID(), Digest: v.Digest(), CanonicalProfile: "magi-cj/1", DigestAlg: "sha256", Body: json.RawMessage(v.CanonicalBytes())}
	if _, e = ReadDomainProfileEnvelope(encode(t, env), v.ArtifactID(), ValidationOptions{}); e != nil {
		t.Fatal(e)
	}
	env.Digest = strings.Repeat("f", 64)
	if _, e = ReadDomainProfileEnvelope(encode(t, env), v.ArtifactID(), ValidationOptions{}); e == nil {
		t.Fatal("tampered envelope digest accepted")
	}
	env.Digest = v.Digest()
	env.Body = raw
	if _, e = ReadDomainProfileEnvelope(encode(t, env), v.ArtifactID(), ValidationOptions{}); e == nil {
		t.Fatal("noncanonical nested body accepted")
	}
	if _, e = ValidateDomainProfile(raw, "", ValidationOptions{Limits: ResourceLimits{MaxBytes: 10}}); e == nil {
		t.Fatal("resource ceiling ignored")
	}
}
func TestDomainRejectsUnknownSchemaVersion(t *testing.T) {
	reject(t, func(d *DomainProfile) { d.SchemaVersion = "2.0" }, "SCHEMA", "schema_version")
}
func TestDomainRejectsUnknownRiskAttribute(t *testing.T) {
	reject(t, func(d *DomainProfile) { d.RiskPolicy.Rules[0].AllOf[0].AttributeID = "unregistered" }, "REFERENCE", "attribute_id")
}
func TestDomainRejectsMissingEvidenceRule(t *testing.T) {
	reject(t, func(d *DomainProfile) { d.RiskPolicy.Rules[0].EvidenceRuleIDs = []string{"missing"} }, "REFERENCE", "evidence_rule_ids")
}
func TestDomainRejectsDuplicateRuleID(t *testing.T) {
	reject(t, func(d *DomainProfile) { d.RiskPolicy.Rules[1].RuleID = d.RiskPolicy.Rules[0].RuleID }, "COLLECTION", "rules")
}
func TestDomainRejectsUnsortedRuleCollection(t *testing.T) {
	reject(t, func(d *DomainProfile) { r := d.RiskPolicy.Rules; r[0], r[1] = r[1], r[0] }, "COLLECTION", "rules")
}
func TestDomainRejectsRiskPolicyDowngrade(t *testing.T) {
	reject(t, func(d *DomainProfile) { d.RiskPolicy.Rules[2].Level = "R0" }, "RISK_POLICY", "rules")
	reject(t, func(d *DomainProfile) { d.RiskPolicy.UnknownLevel = "R0" }, "SCHEMA", "unknown_level")
	reject(t, func(d *DomainProfile) { d.RiskPolicy.Downgrade = "SAME_ROUND" }, "SCHEMA", "downgrade")
	reject(t, func(d *DomainProfile) { d.RiskPolicy.Aggregation = "FIRST_MATCH" }, "SCHEMA", "aggregation")
	reject(t, func(d *DomainProfile) { d.RiskPolicy.R3Execution = "AUTOMATIC" }, "SCHEMA", "r3_execution")
}
func TestDomainRejectsWeakenedR2R3Quorum(t *testing.T) {
	for _, r := range []string{"R2", "R3"} {
		t.Run(r, func(t *testing.T) {
			reject(t, func(d *DomainProfile) { d.Selection.ApprovalVotes[r] = 2 }, "SCHEMA", "approval_votes")
		})
	}
}
func TestDomainRejectsInvalidPredicateExpected(t *testing.T) {
	reject(t, func(d *DomainProfile) { d.RiskPolicy.Rules[2].AllOf[0].Expected = json.RawMessage(`true`) }, "PREDICATE", "expected")
	reject(t, func(d *DomainProfile) { d.RiskPolicy.Rules[0].AllOf[0].Expected = nil }, "PREDICATE", "expected")
	reject(t, func(d *DomainProfile) { d.RiskPolicy.Rules[0].AllOf[0].Operator = "LE" }, "PREDICATE", "operator")
}
func TestDomainRejectsRatioOutOfRange(t *testing.T) {
	reject(t, func(d *DomainProfile) { d.InstanceParameters[0].Value.Value = 101 }, "QUANTITY", "value")
}
func TestDomainRejectsQuantityUnitMismatch(t *testing.T) {
	reject(t, func(d *DomainProfile) { d.InstanceParameters[0].Value.Unit = "percentage_point" }, "QUANTITY", "unit")
}
func TestDomainRejectsRationalNotReduced(t *testing.T) {
	reject(t, func(d *DomainProfile) { d.Statistics.FamilyAlpha = Rational{Numerator: 5, Denominator: 100} }, "RATIONAL", "family_alpha")
}
func TestDomainRejectsPrecisionOverflow(t *testing.T) {
	reject(t, func(d *DomainProfile) { d.Budget.TaskInputTokens = 9007199254740992 }, "CANONICAL", "task_input_tokens")
}
func TestDomainRejectsInvalidCalendarTime(t *testing.T) {
	reject(t, func(d *DomainProfile) { d.FrozenAt = "2026-02-30T00:00:00.000Z" }, "TIME", "frozen_at")
}
func TestDomainRejectsInvalidValidityInterval(t *testing.T) {
	reject(t, func(d *DomainProfile) { d.Validity.ValidUntil = ptr(d.Validity.NotBefore) }, "TIME", "validity")
}
func TestDomainRejectsAmbiguousVersionReference(t *testing.T) {
	for _, v := range []string{"latest", "stable", "main", "v1-latest", "CURRENT", "1.x"} {
		t.Run(v, func(t *testing.T) {
			reject(t, func(d *DomainProfile) { d.GovernancePolicyRef.Version = v }, "VERSION", "version")
		})
	}
	d := fixture(t)
	d.GovernancePolicyRef.Version = "policy-v1"
	validate(t, d)
}
func TestDomainRejectsIncorrectExpectedArtifactID(t *testing.T) {
	d := fixture(t)
	v := validate(t, d)
	if _, e := ValidateDomainProfile(encode(t, d), "dprf_"+strings.Repeat("0", 64), ValidationOptions{}); e == nil {
		t.Fatal("wrong identity accepted")
	}
	if _, e := ValidateDomainProfile(encode(t, d), v.ArtifactID(), ValidationOptions{}); e != nil {
		t.Fatal(e)
	}
}
func TestDomainRejectsUnsupportedToolPolicy(t *testing.T) {
	reject(t, func(d *DomainProfile) { d.ToolScope[0].Mode = "WRITE" }, "TOOL_POLICY", "tool_scope")
	reject(t, func(d *DomainProfile) { d.ToolScope[0].AdapterMode = "UNSAFE" }, "TOOL_POLICY", "tool_scope")
	d := fixture(t)
	d.ToolScope[0].Mode = "WRITE"
	d.ToolScope[0].AdapterMode = "FENCED_IDEMPOTENT"
	d.ToolScope[0].RiskLevels = []string{"R1"}
	validate(t, d)
}
func TestDomainRejectsUnauthorizedReferenceClaim(t *testing.T) {
	d := fixture(t)
	v := validate(t, d)
	if len(v.PendingDependencies()) < 15 {
		t.Fatal("unverified external dependencies omitted")
	}
	if _, e := ValidateDomainProfile(encode(t, d), "", ValidationOptions{RequireCompleteQualification: true}); e == nil {
		t.Fatal("nonempty digests/self-declared publisher granted qualification")
	}
	raw := bytes.Replace(encode(t, d), []byte(`"artifact_type":`), []byte(`"approved":true,"artifact_type":`), 1)
	if _, e := ValidateDomainProfile(raw, "", ValidationOptions{}); e == nil {
		t.Fatal("publisher approval claim accepted")
	}
}
func TestDomainSeparatesGenericAndRolloutBudgets(t *testing.T) {
	d := fixture(t)
	d.Budget.RoleToolCalls = 12
	d.Budget.RoleOutputTokens = 12000
	d.Windows.ReadRetries = 2
	d.Windows.FormatRepairsPerRole = 2
	validate(t, d)
	effective, e := ComposePersonaV1Budget(d.Budget)
	if e != nil {
		t.Fatal(e)
	}
	if effective.RoleToolCalls != 6 || effective.RoleOutputTokens != 6000 {
		t.Fatal("domain inflated Persona v1 budget")
	}
	d.DomainKey = "ROLLOUT-LAB"
	if _, e := ValidateDomainProfile(encode(t, d), "", ValidationOptions{}); e == nil {
		t.Fatal("rollout instance limits ignored")
	}
	d = rolloutFixture(t)
	validate(t, d)
	d.Budget.RoleToolCalls = 4
	v := validate(t, d)
	effective, e = ComposePersonaV1Budget(v.Body().Budget)
	if e != nil || effective.RoleToolCalls != 4 {
		t.Fatal("stricter domain cap ignored")
	}
}
func TestDomainLegacyIdentityRemainsUnchanged(t *testing.T) {
	validate(t, fixture(t))
	if got := execution.ToolIdempotencyKey("inv-abc", "search", []byte(`{"q":"magi","limit":1}`)); got != "1ef9b021e21fb4a3215d3885bb063cab0cf3b3ead0ba92947bef00734cb43358" {
		t.Fatal(got)
	}
}
func TestDomainExactArithmetic(t *testing.T) {
	a := Quantity{Value: 9007199254740991, Unit: "millisecond"}
	b := a
	cmp, e := CompareQuantities(a, b)
	if e != nil || cmp != 0 {
		t.Fatal(cmp, e)
	}
	// Large scaled count values cannot be used to launder precision or dimensions.
	if _, e = CompareQuantities(a, Quantity{Value: 1, Unit: "ratio", Scale: 1}); e == nil {
		t.Fatal("mixed dimensions")
	}
	cmp, e = CompareQuantities(Quantity{Value: 100000000, Unit: "ratio", Scale: 9}, Quantity{Value: 1, Unit: "ratio", Scale: 1})
	if e != nil || cmp != 0 {
		t.Fatal(cmp, e)
	}
	r, e := PerClaimAlpha(Rational{Numerator: 1, Denominator: 20}, 3)
	if e != nil || r.Numerator != 1 || r.Denominator != 60 {
		t.Fatal(r, e)
	}
	if _, e = PerClaimAlpha(Rational{Numerator: 1, Denominator: 9007199254740991}, 64); e == nil {
		t.Fatal("overflowing serialized rational accepted")
	}
}
func TestDomainSchemaMirror(t *testing.T) {
	if !bytes.Equal(contractSchema, read(t, "../../../docs/contracts/input-artifacts/schema.json")) {
		t.Fatal("embedded schema drift")
	}
}

func TestDomainEvidenceAndCollectionBoundaries(t *testing.T) {
	cases := []struct {
		name string
		edit func(*DomainProfile)
	}{
		{"duplicate-evidence-id", func(d *DomainProfile) { d.EvidenceRules[1].RuleID = d.EvidenceRules[0].RuleID }},
		{"duplicate-tool-id", func(d *DomainProfile) { x := d.ToolScope[0]; x.Version = "2"; d.ToolScope = append(d.ToolScope, x) }},
		{"duplicate-metric-id", func(d *DomainProfile) { x := d.SLATargets[0]; x.Bound.Value++; d.SLATargets = append(d.SLATargets, x) }},
		{"duplicate-parameter-id", func(d *DomainProfile) {
			x := d.InstanceParameters[0]
			x.Value.Value++
			d.InstanceParameters = append(d.InstanceParameters, x)
		}},
		{"duplicate-attribute", func(d *DomainProfile) {
			d.RiskPolicy.Rules[1].AllOf[1].AttributeID = d.RiskPolicy.Rules[1].AllOf[0].AttributeID
		}},
		{"unsorted-evidence", func(d *DomainProfile) {
			d.EvidenceRules[0], d.EvidenceRules[1] = d.EvidenceRules[1], d.EvidenceRules[0]
		}},
		{"unsorted-events", func(d *DomainProfile) {
			d.EvidenceRules[0].InvalidatingEvents = []string{"REVOKED", "ENVIRONMENT_CHANGED"}
		}},
		{"unsorted-sources", func(d *DomainProfile) { d.EvidenceRules[0].SourceKinds = []string{"z", "a"} }},
		{"unsorted-resources", func(d *DomainProfile) { d.ToolScope[0].ResourceIDs = []string{"z", "a"} }},
		{"missing-assertion-kind", func(d *DomainProfile) { d.EvidenceRules[0].AssertionKind = "FACT" }},
		{"missing-default-policy", func(d *DomainProfile) { d.Selection.AllowedEarlySelectionPolicies = []string{"HUMAN_WAIVER"} }},
		{"missing-critical-attribute", func(d *DomainProfile) { d.RiskPolicy.RequiredAttributes = d.RiskPolicy.RequiredAttributes[1:] }},
		{"zero-denominator", func(d *DomainProfile) { d.Statistics.FamilyAlpha.Denominator = 0 }},
		{"probability-above-one", func(d *DomainProfile) { d.Statistics.FamilyAlpha = Rational{Numerator: 21, Denominator: 20} }},
		{"discrete-scale", func(d *DomainProfile) { d.SLATargets[0].Bound.Scale = 1 }},
		{"wrong-sla-unit", func(d *DomainProfile) { d.SLATargets[0].Bound = Quantity{Value: 1, Unit: "ratio"} }},
		{"unknown-parameter", func(d *DomainProfile) { d.InstanceParameters[2].ParameterID = "unknown" }},
		{"timezone-offset", func(d *DomainProfile) { d.FrozenAt = "2026-10-22T00:00:00.000+00:00" }},
		{"leap-second", func(d *DomainProfile) { d.FrozenAt = "2026-10-22T00:00:60.000Z" }},
		{"no-milliseconds", func(d *DomainProfile) { d.FrozenAt = "2026-10-22T00:00:00Z" }},
		{"reversed-windows", func(d *DomainProfile) { d.Windows.FreezeMs = d.Windows.RevisionMs }},
		{"unknown-release-version", func(d *DomainProfile) { d.Version = "latest" }},
		{"mutable-idp-version", func(d *DomainProfile) { d.CreatedBy.IdentitySourceVersion = "latest" }},
		{"tool-definition-version", func(d *DomainProfile) { d.ToolScope[0].DefinitionRef.Version = "2" }},
		{"r3-disabled-write", func(d *DomainProfile) {
			d.ToolScope[0].Mode = "WRITE"
			d.ToolScope[0].AdapterMode = "FENCED_IDEMPOTENT"
			d.ToolScope[0].RiskLevels = []string{"R3"}
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			d := fixture(t)
			validate(t, d)
			tc.edit(&d)
			if _, e := ValidateDomainProfile(encode(t, d), "", ValidationOptions{}); e == nil {
				t.Fatal("invalid semantic body accepted")
			}
		})
	}
}
func TestDomainExactMoneyAndBudgetIntersection(t *testing.T) {
	d := fixture(t)
	d.Budget.CostLimit = &Money{MinorUnits: 500, Currency: "USD", MinorUnitExponent: 2}
	v := validate(t, d)
	found := false
	for _, dep := range v.PendingDependencies() {
		if dep.Kind == "UNVERIFIABLE_BILLING_MAPPING" {
			found = true
		}
	}
	if !found {
		t.Fatal("cost shape claimed as verified billing")
	}
	caps := d.Budget
	caps.RoleToolCalls = 3
	caps.TaskToolCalls = 2
	caps.TaskOutputTokens = 4000
	caps.CostLimit = &Money{MinorUnits: 200, Currency: "USD", MinorUnitExponent: 2}
	b, e := ComposeBudgets(d.Budget, caps)
	if e != nil || b.RoleToolCalls != 2 || b.RoleOutputTokens != 4000 || b.CostLimit.MinorUnits != 200 {
		t.Fatal(b, e)
	}
	b.CostLimit.MinorUnits = 1
	if caps.CostLimit.MinorUnits != 200 || d.Budget.CostLimit.MinorUnits != 500 {
		t.Fatal("budget aliases source")
	}
	caps.CostLimit.Currency = "EUR"
	if _, e = ComposeBudgets(d.Budget, caps); e == nil {
		t.Fatal("implicit exchange conversion")
	}
	caps.CostLimit.Currency = "USD"
	caps.CostLimit.MinorUnitExponent = 3
	if _, e = ComposeBudgets(d.Budget, caps); e == nil {
		t.Fatal("different minor-unit mapping accepted")
	}
	if _, e = PerClaimAlpha(Rational{Numerator: 1, Denominator: 9007199254740991}, 9007199254740991); e == nil {
		t.Fatal("cross multiplication silently overflowed")
	}
	// Legitimate calendar leap day and exact probability boundaries.
	d.FrozenAt = "2024-02-29T12:00:00.000Z"
	validate(t, d)
	for _, r := range []Rational{{Numerator: 0, Denominator: 1}, {Numerator: 1, Denominator: 1}} {
		d.Statistics.FamilyAlpha = r
		validate(t, d)
	}
}
func TestDomainRegisteredQuantityPredicate(t *testing.T) {
	d := fixture(t)
	vocab := DefaultVocabulary()
	vocab.Attributes["exposure"] = ValueSpec{Kind: "quantity", Unit: "ratio"}
	d.RiskPolicy.RequiredAttributes = append(d.RiskPolicy.RequiredAttributes[:1], append([]string{"exposure"}, d.RiskPolicy.RequiredAttributes[1:]...)...)
	// Keep the contract's producer ordering; the validator must not sort it.
	d.RiskPolicy.Rules[2].AllOf = []RiskPredicate{{AttributeID: "exposure", Operator: "GE", Expected: json.RawMessage(`{"value":5,"unit":"ratio","scale":2}`)}}
	if _, e := ValidateDomainProfile(encode(t, d), "", ValidationOptions{Vocabulary: &vocab}); e != nil {
		t.Fatal(e)
	}
	d.RiskPolicy.Rules[2].AllOf[0].Expected = json.RawMessage(`{"value":5,"unit":"percentage_point","scale":2}`)
	if _, e := ValidateDomainProfile(encode(t, d), "", ValidationOptions{Vocabulary: &vocab}); e == nil {
		t.Fatal("predicate dimension laundering")
	}
}
func TestDomainPendingDependencyCompleteness(t *testing.T) {
	d := fixture(t)
	v := validate(t, d)
	paths := map[string]bool{}
	for _, dep := range v.PendingDependencies() {
		paths[dep.Path] = true
	}
	for _, p := range []string{"$.adapter_policy_ref", "$.created_by", "$.issuance_policy_ref", "$.governance_policy_ref", "$.statistics.plan_ref", "$.sla_targets[0].measurement_method_ref", "$.tool_scope[0].definition_ref", "$.evidence_rules[0].method_refs[0]", "$.evidence_rules[3].qualification_policy_ref"} {
		if !paths[p] {
			t.Fatalf("unreported dependency %s", p)
		}
	}
}
func TestDomainAuthorityIntegration(t *testing.T) {
	t.Skip("BLOCKED: C2 identity source, governance roots, signatures, delegations and authority epoch service are not implemented; offline tests cannot prove publication eligibility")
}
func TestDomainCandidateClassificationIntegration(t *testing.T) {
	t.Skip("NOT RUN: no runtime classifier or actual qualified attribute evidence in C1-B1; no claim about candidate risk correctness")
}
func TestDomainDistributedRevocationIntegration(t *testing.T) {
	t.Skip("BLOCKED: C2/C3 ordered revocation and final-send protocol unavailable; no mock substitutes for distributed evidence")
}
func rolloutFixture(t *testing.T) DomainProfile {
	t.Helper()
	var d DomainProfile
	if e := json.Unmarshal(read(t, "testdata/rollout_profile.json"), &d); e != nil {
		t.Fatal(e)
	}
	return d
}
func TestDomainRolloutLocalInstanceConstraints(t *testing.T) {
	validate(t, rolloutFixture(t))
	cases := []struct {
		name string
		edit func(*DomainProfile)
	}{
		{"missing-thresholds", func(d *DomainProfile) { d.InstanceParameters = []InstanceParameter{} }},
		{"retention", func(d *DomainProfile) { d.Retention.CacheMs = 9007199254740991 }},
		{"sla", func(d *DomainProfile) { d.SLATargets[0].Bound.Value = 9007199254740991 }},
		{"telemetry-ttl", func(d *DomainProfile) {
			for i := range d.EvidenceRules {
				if d.EvidenceRules[i].RuleID == "fact" {
					d.EvidenceRules[i].TTLMs = 9007199254740991
				}
			}
		}},
		{"unsafe-write-adapter", func(d *DomainProfile) {
			d.ToolScope[0].Mode = "WRITE"
			d.ToolScope[0].AdapterMode = "IDEMPOTENT_ONLY"
			d.ToolScope[0].RiskLevels = []string{"R1"}
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			d := rolloutFixture(t)
			tc.edit(&d)
			if _, e := ValidateDomainProfile(encode(t, d), "", ValidationOptions{}); e == nil {
				t.Fatal("invalid ROLLOUT-LAB local instance accepted")
			}
		})
	}
}
func TestDomainAcceptsConservativeRiskEscalation(t *testing.T) {
	d := fixture(t)
	for j := range d.RiskPolicy.Rules[3].AllOf {
		if d.RiskPolicy.Rules[3].AllOf[j].AttributeID == "within_quota" {
			d.RiskPolicy.Rules[3].AllOf[j].Expected = json.RawMessage(`true`)
		}
	}
	validate(t, d)
}
func TestDomainFrozenAuthorityMutations(t *testing.T) {
	var golden struct {
		Mutations []struct {
			Path     string          `json:"path"`
			Value    json.RawMessage `json:"value"`
			Expected string          `json:"expected"`
			ID       string          `json:"artifact_id"`
		} `json:"mutations"`
	}
	if e := json.Unmarshal(read(t, "testdata/golden.json"), &golden); e != nil {
		t.Fatal(e)
	}
	for _, m := range golden.Mutations {
		t.Run(m.Path, func(t *testing.T) {
			var body map[string]any
			dec := json.NewDecoder(bytes.NewReader(read(t, "testdata/domain_profile.json")))
			dec.UseNumber()
			if e := dec.Decode(&body); e != nil {
				t.Fatal(e)
			}
			parts := strings.Split(m.Path, ".")
			target := body
			for _, key := range parts[:len(parts)-1] {
				target = target[key].(map[string]any)
			}
			var value any
			dec = json.NewDecoder(bytes.NewReader(m.Value))
			dec.UseNumber()
			if e := dec.Decode(&value); e != nil {
				t.Fatal(e)
			}
			target[parts[len(parts)-1]] = value
			v, e := ValidateDomainProfile(encode(t, body), "", ValidationOptions{})
			if m.Expected == "SCHEMA_REJECTED" {
				var ve *Error
				if !errors.As(e, &ve) || ve.Kind != "SCHEMA" {
					t.Fatalf("expected schema rejection, got %v", e)
				}
				return
			}
			if e != nil || v.ArtifactID() != m.ID {
				t.Fatalf("frozen mutation ID %s, want %s; error %v", v.ArtifactID(), m.ID, e)
			}
		})
	}
}
func TestDomainRejectsMalformedLocalVocabulary(t *testing.T) {
	d := fixture(t)
	vocab := DefaultVocabulary()
	vocab.Attributes["z-extra"] = ValueSpec{Kind: "anything"}
	d.RiskPolicy.RequiredAttributes = append(d.RiskPolicy.RequiredAttributes, "z-extra")
	d.RiskPolicy.Rules[2].AllOf = []RiskPredicate{{AttributeID: "z-extra", Operator: "KNOWN"}}
	if _, e := ValidateDomainProfile(encode(t, d), "", ValidationOptions{Vocabulary: &vocab}); e == nil {
		t.Fatal("unsupported type accepted through KNOWN predicate")
	}
}
func TestDomainRejectsConflictingExternalContent(t *testing.T) {
	reject(t, func(d *DomainProfile) { d.EvidenceRules[1].MethodRefs[0].ContentDigest = strings.Repeat("f", 64) }, "REFERENCE", "content_digest")
}
