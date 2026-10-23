package inputartifact

import (
	"strings"
	"testing"
)

func TestPersonaDomainSelection(t *testing.T) {
	raw := read(t, "testdata/persona_casper.json")
	for _, opts := range []PersonaValidationOptions{{}, {DomainProfileID: personaOptions(t).DomainProfileID}} {
		v, err := ValidatePersonaSpec(raw, "", opts)
		if err != nil {
			t.Fatal(err)
		}
		if v.Status().Baseline != "VALID_OFFLINE" || v.Status().DomainRelations != "UNVERIFIABLE_REFERENCE" {
			t.Fatal("missing domain marked verified")
		}
		if _, ok := v.EffectiveBudget(); ok {
			t.Fatal("invented domain budget")
		}
		opts.RequireDomain = true
		if _, err = ValidatePersonaSpec(raw, "", opts); err == nil {
			t.Fatal("required domain failed open")
		}
	}
	for _, edit := range []func(*PersonaValidationOptions){
		func(o *PersonaValidationOptions) { o.DomainProfileID = "" },
		func(o *PersonaValidationOptions) { o.DomainProfileID = "dprf_" + strings.Repeat("f", 64) },
		func(o *PersonaValidationOptions) { o.DomainProfileID = "latest"; o.DomainProfile = nil },
		func(o *PersonaValidationOptions) { d := ValidatedDomainProfile{}; o.DomainProfile = &d },
	} {
		opts := personaOptions(t)
		edit(&opts)
		if _, err := ValidatePersonaSpec(raw, "", opts); err == nil {
			t.Fatal("unbound or zero domain accepted")
		}
	}
}
func TestPersonaDomainReferences(t *testing.T) {
	for _, role := range []string{"melchior", "balthasar", "casper"} {
		wrongKind := "authority"
		if role == "casper" {
			wrongKind = "fact"
		}
		for _, rule := range []string{"missing", wrongKind} {
			personaReject(t, role, func(p map[string]any) {
				p["minimum_investigation"].([]any)[0].(map[string]any)["acceptable_evidence_rule_ids"] = []any{rule}
			})
		}
		personaReject(t, role, func(p map[string]any) {
			p["domain_extensions"].([]any)[0].(map[string]any)["domain_profile_id"] = "dprf_" + strings.Repeat("f", 64)
		})
	}
	// Every rule must match, not merely one of a mixed list.
	personaReject(t, "casper", func(p map[string]any) {
		p["minimum_investigation"].([]any)[0].(map[string]any)["acceptable_evidence_rule_ids"] = []any{"authority", "preference"}
	})
	personaReject(t, "balthasar", func(p map[string]any) {
		p["minimum_investigation"].([]any)[1].(map[string]any)["acceptable_evidence_rule_ids"] = []any{"prediction"}
	})
	personaReject(t, "casper", func(p map[string]any) {
		p["domain_extensions"].([]any)[0].(map[string]any)["minimum_investigation"].([]any)[0].(map[string]any)["acceptable_evidence_rule_ids"] = []any{"missing"}
	})
	personaReject(t, "melchior", func(p map[string]any) {
		p["minimum_investigation"].([]any)[0].(map[string]any)["acceptable_evidence_rule_ids"] = []any{"fact", "authority"}
	})
	// Same rule names in another valid domain cannot satisfy the selected ID.
	opts := personaOptions(t)
	d := fixture(t)
	d.Version = "1.0.1"
	other := validate(t, d)
	opts.DomainProfile = &other
	if _, err := ValidatePersonaSpec(read(t, "testdata/persona_melchior.json"), "", opts); err == nil {
		t.Fatal("wrong exact domain version accepted")
	}
	// Declared validity has no overlap. Real current validity remains external.
	d = fixture(t)
	d.Validity.ValidUntil = ptr("2026-10-23T00:00:00.000Z")
	expired := validate(t, d)
	p := personaFixture(t, "casper")
	p["validity"].(map[string]any)["not_before"] = "2026-10-24T00:00:00.000Z"
	p["domain_extensions"] = []any{}
	if _, err := ValidatePersonaSpec(encode(t, p), "", PersonaValidationOptions{DomainProfile: &expired, DomainProfileID: expired.ArtifactID()}); err == nil {
		t.Fatal("inapplicable declared validity accepted")
	}
}
func TestPersonaDomainPositiveTypeBindings(t *testing.T) {
	for _, tc := range []struct {
		role  string
		index int
		rule  string
	}{
		{"melchior", 0, "fact"}, {"melchior", 1, "fact"}, {"melchior", 1, "prediction"},
		{"balthasar", 0, "fact"}, {"balthasar", 0, "prediction"}, {"balthasar", 1, "fact"},
		{"casper", 0, "preference"}, {"casper", 1, "authority"}, {"casper", 1, "fact"},
	} {
		p := personaFixture(t, tc.role)
		p["minimum_investigation"].([]any)[tc.index].(map[string]any)["acceptable_evidence_rule_ids"] = []any{tc.rule}
		v, err := ValidatePersonaSpec(encode(t, p), "", personaOptions(t))
		if err != nil {
			t.Fatal(tc, err)
		}
		found := false
		for _, d := range v.PendingDependencies() {
			if d.Kind == "UNVERIFIABLE_APPLICABILITY" {
				found = true
			}
		}
		if !found {
			t.Fatal("declared rule type mistaken for actual environment/source/method applicability")
		}
	}
}
func TestPersonaExtensionsOnlyAdd(t *testing.T) {
	for _, role := range []string{"melchior", "balthasar", "casper"} {
		personaValid(t, role)
		personaReject(t, role, func(p map[string]any) { x := p["domain_extensions"].([]any); p["domain_extensions"] = append(x, x[0]) })
		personaReject(t, role, func(p map[string]any) {
			p["domain_extensions"].([]any)[0].(map[string]any)["review_item_ids"] = []any{p["mandatory_reviews"].([]any)[0]}
		})
		personaReject(t, role, func(p map[string]any) {
			p["domain_extensions"].([]any)[0].(map[string]any)["minimum_investigation"].([]any)[0].(map[string]any)["requirement_id"] = p["minimum_investigation"].([]any)[0].(map[string]any)["requirement_id"]
		})
		personaReject(t, role, func(p map[string]any) {
			p["domain_extensions"].([]any)[0].(map[string]any)["core_priority"] = []any{"OVERRIDE"}
		})
		personaReject(t, role, func(p map[string]any) {
			x := p["domain_extensions"].([]any)
			other := map[string]any{}
			for k, v := range x[0].(map[string]any) {
				other[k] = v
			}
			other["extension_id"] = "second"
			p["domain_extensions"] = append(x, other)
		})
		// No extension is legal, and releases/descriptions can change identity while
		// leaving the frozen formal core unchanged. Prose cannot override codes.
		p := personaFixture(t, role)
		p["domain_extensions"] = []any{}
		p["version"] = "1.0.1"
		p["minimum_investigation"].([]any)[0].(map[string]any)["description"] = "Explanation only: this does not prove real investigation."
		v, err := ValidatePersonaSpec(encode(t, p), "", personaOptions(t))
		if err != nil {
			t.Fatal(err)
		}
		if v.ArtifactID() == personaValid(t, role).ArtifactID() {
			t.Fatal("full body/version not hashed")
		}
	}
}
func TestPersonaBudgetIntersection(t *testing.T) {
	d := fixture(t)
	d.Budget.RoleToolCalls = 12
	d.Budget.RoleOutputTokens = 12000
	domain := validate(t, d)
	p := personaFixture(t, "melchior")
	p["domain_extensions"] = []any{}
	opts := PersonaValidationOptions{DomainProfile: &domain, DomainProfileID: domain.ArtifactID()}
	v, err := ValidatePersonaSpec(encode(t, p), "", opts)
	if err != nil {
		t.Fatal(err)
	}
	b, ok := v.EffectiveBudget()
	if !ok || b.RoleToolCalls != 6 || b.RoleOutputTokens != 6000 {
		t.Fatalf("persona caps bypassed: %+v", b)
	}
	p["tool_call_limit"] = 12
	if _, err = ValidatePersonaSpec(encode(t, p), "", opts); err == nil {
		t.Fatal("domain enlarged fixed persona ceiling")
	}
	p["tool_call_limit"] = 6
	charter := d.Budget
	charter.RoleToolCalls = 4
	charter.RoleOutputTokens = 3000
	engineering := d.Budget
	engineering.TaskToolCalls = 1
	engineering.TaskOutputTokens = 1000
	opts.AdditionalBudgets = []Budget{charter, engineering}
	v, err = ValidatePersonaSpec(encode(t, p), "", opts)
	if err != nil {
		t.Fatal(err)
	}
	b, ok = v.EffectiveBudget()
	if !ok || b.RoleToolCalls != 1 || b.RoleOutputTokens != 1000 {
		t.Fatalf("strictest applicable caps lost: %+v", b)
	}
	found := false
	for _, dep := range v.PendingDependencies() {
		if dep.Kind == "RUNTIME_BUDGET_REVIEW_REQUIRED" {
			found = true
		}
	}
	if !found || v.Status().Behavior != "NOT_RUN" {
		t.Fatal("budget shortage declared investigations complete")
	}
	opts.AdditionalBudgets[0].RoleToolCalls = 0
	if _, err = ValidatePersonaSpec(encode(t, p), "", opts); err == nil {
		t.Fatal("invalid additional budget accepted")
	}
}

func TestPersonaRejectsConflictingSharedReference(t *testing.T) {
	p := personaFixture(t, "casper")
	// DomainProfile's fixture method tuple is immutable. A Persona declaration
	// cannot attach contradictory bytes to the same external object/version.
	r := fixture(t).EvidenceRules[0].MethodRefs[0]
	p["maintenance_policy_ref"] = map[string]any{"namespace": r.Namespace, "object_id": r.ObjectID, "version": r.Version, "digest_alg": r.DigestAlg, "content_digest": strings.Repeat("f", 64)}
	if _, err := ValidatePersonaSpec(encode(t, p), "", personaOptions(t)); err == nil {
		t.Fatal("same external object/version accepted with conflicting cross-artifact digests")
	}
}

func TestPersonaAdditionalBudgetBillingRemainsUnverified(t *testing.T) {
	opts := personaOptions(t)
	b := opts.DomainProfile.Body().Budget
	b.CostLimit = &Money{MinorUnits: 100, Currency: "USD", MinorUnitExponent: 2}
	opts.AdditionalBudgets = []Budget{b}
	v, err := ValidatePersonaSpec(read(t, "testdata/persona_casper.json"), "", opts)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, dep := range v.PendingDependencies() {
		if dep.Kind == "UNVERIFIABLE_BILLING_MAPPING" {
			found = true
		}
	}
	if !found {
		t.Fatal("additional money ceiling lost its unverified billing dimension")
	}
}

func TestPersonaResultRetainsExactDomainContext(t *testing.T) {
	p := personaFixture(t, "melchior")
	p["domain_extensions"] = []any{}
	opts := personaOptions(t)
	selected := opts.DomainProfileID
	v, err := ValidatePersonaSpec(encode(t, p), "", opts)
	if err != nil {
		t.Fatal(err)
	}
	opts.DomainProfileID = "dprf_" + strings.Repeat("f", 64)
	if v.DomainProfileID() != selected {
		t.Fatal("validated relations lost the exact selected domain context")
	}
}
