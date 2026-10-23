package inputartifact

import (
	"bytes"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/jamespud/magi/backend/domain/canonical"
)

func personaFixture(t *testing.T, role string) map[string]any {
	t.Helper()
	var p map[string]any
	if err := json.Unmarshal(read(t, "testdata/persona_"+role+".json"), &p); err != nil {
		t.Fatal(err)
	}
	return p
}
func personaOptions(t *testing.T) PersonaValidationOptions {
	t.Helper()
	d := validate(t, fixture(t))
	return PersonaValidationOptions{DomainProfile: &d, DomainProfileID: d.ArtifactID()}
}
func personaValid(t *testing.T, role string) ValidatedPersonaSpec {
	t.Helper()
	v, err := ValidatePersonaSpec(read(t, "testdata/persona_"+role+".json"), "", personaOptions(t))
	if err != nil {
		t.Fatal(err)
	}
	return v
}
func personaReject(t *testing.T, role string, edit func(map[string]any)) {
	t.Helper()
	p := personaFixture(t, role)
	edit(p)
	v, err := ValidatePersonaSpec(encode(t, p), "", personaOptions(t))
	var ve *Error
	if !errors.As(err, &ve) {
		t.Fatalf("expected typed rejection; got %v", err)
	}
	if v.ArtifactID() != "" {
		t.Fatal("rejection returned validated identity")
	}
}
func TestPersonaGolden(t *testing.T) {
	for _, role := range []string{"melchior", "balthasar", "casper"} {
		t.Run(role, func(t *testing.T) {
			v := personaValid(t, role)
			var g struct {
				Digest string                  `json:"digest"`
				ID     string                  `json:"artifact_id"`
				Status PersonaValidationStatus `json:"status"`
				Refs   []struct {
					Path      string      `json:"path"`
					Reference ExternalRef `json:"reference"`
				} `json:"pending_references"`
				Mutations []struct {
					Field      string `json:"field"`
					Digest     string `json:"digest"`
					ID         string `json:"artifact_id"`
					Validation string `json:"validation"`
				} `json:"mutations"`
			}
			if err := json.Unmarshal(read(t, "testdata/persona_"+role+".golden.json"), &g); err != nil {
				t.Fatal(err)
			}
			if v.ArtifactID() != g.ID || v.Digest() != g.Digest || !bytes.Equal(v.CanonicalBytes(), read(t, "testdata/persona_"+role+".canonical.json")) {
				t.Fatal("independent Python golden mismatch")
			}
			if v.Status() != g.Status {
				t.Fatalf("status: %+v", v.Status())
			}
			for _, r := range g.Refs {
				found := false
				for _, dep := range v.PendingDependencies() {
					if dep.Path == r.Path && dep.Reference == r.Reference && dep.Kind == "UNVERIFIABLE_REFERENCE" {
						found = true
					}
				}
				if !found {
					t.Fatalf("missing pending reference: %s", r.Path)
				}
			}
			for _, m := range g.Mutations {
				t.Run(m.Field, func(t *testing.T) {
					p := personaFixture(t, role)
					a := p[m.Field].([]any)
					switch m.Field {
					case "minimum_investigation":
						a[0].(map[string]any)["requirement_id"] = "UNRELATED"
					case "call_allocation":
						a[0].(map[string]any)["bucket_id"] = "UNRELATED"
					default:
						a[0] = "UNRELATED"
					}
					c, err := canonical.Canonicalize(encode(t, p))
					if err != nil {
						t.Fatal(err)
					}
					id, _ := c.ArtifactID(canonical.TypePersonaSpec)
					digest, _ := c.Digest(canonical.TypePersonaSpec)
					if id != m.ID || digest != m.Digest || id == v.ArtifactID() || m.Validation != "REJECTED" {
						t.Fatal("mutation identity mismatch")
					}
					if _, err = ValidatePersonaSpec(c.Bytes(), "", personaOptions(t)); err == nil {
						t.Fatal("core mutation accepted")
					}
				})
			}
		})
	}
}

// Independent role cases derive their expected obligations from the checked-in
// C1-A fixtures, never from the production baseline registry. Removing any
// required code check must make a replacement/deletion below start succeeding.
func TestMelchiorBaseline(t *testing.T)  { testPersonaBaseline(t, "melchior") }
func TestBalthasarBaseline(t *testing.T) { testPersonaBaseline(t, "balthasar") }
func TestCasperBaseline(t *testing.T)    { testPersonaBaseline(t, "casper") }
func testPersonaBaseline(t *testing.T, role string) {
	personaValid(t, role)
	for _, field := range []string{"core_priority", "mandatory_reviews", "minimum_investigation", "evidence_priority", "call_allocation", "support_conditions", "reject_conditions", "abstain_conditions", "correction_triggers"} {
		original := personaFixture(t, role)[field].([]any)
		for i := range original {
			for _, op := range []string{"delete", "replace", "duplicate"} {
				t.Run(field+"/"+op+"/"+strings.ReplaceAll(string(encode(t, original[i])), "/", "_"), func(t *testing.T) {
					personaReject(t, role, func(p map[string]any) {
						a := p[field].([]any)
						switch op {
						case "delete":
							p[field] = append(a[:i], a[i+1:]...)
						case "duplicate":
							p[field] = append(a, a[i])
						case "replace":
							switch field {
							case "minimum_investigation":
								a[i].(map[string]any)["requirement_id"] = "UNRELATED"
							case "call_allocation":
								a[i].(map[string]any)["bucket_id"] = "UNRELATED"
							default:
								a[i] = "UNRELATED"
							}
						}
					})
				})
			}
		}
		t.Run(field+"/reorder", func(t *testing.T) {
			personaReject(t, role, func(p map[string]any) { a := p[field].([]any); a[0], a[1] = a[1], a[0] })
		})
	}
	for _, field := range []string{"schema_version", "spec_version", "baseline_id", "tool_call_limit", "output_token_limit", "transfer_policy", "required_output_fields", "investigation_output_fields"} {
		t.Run(field, func(t *testing.T) {
			personaReject(t, role, func(p map[string]any) {
				switch field {
				case "tool_call_limit":
					p[field] = 12
				case "output_token_limit":
					p[field] = 12000
				case "required_output_fields", "investigation_output_fields":
					a := p[field].([]any)
					a[0], a[1] = a[1], a[0]
				default:
					p[field] = "2.0"
				}
			})
		})
	}
	for i := 0; i < 4; i++ {
		t.Run("allocation_count", func(t *testing.T) {
			personaReject(t, role, func(p map[string]any) { p["call_allocation"].([]any)[i].(map[string]any)["calls"] = 0 })
		})
	}
}
func TestPersonaBaselineRoleBinding(t *testing.T) {
	personaReject(t, "casper", func(p map[string]any) { p["baseline_id"] = "MELCHIOR-1.0" })
	for _, role := range []string{"melchior", "balthasar", "casper"} {
		personaReject(t, role, func(p map[string]any) { p["version"] = "1.0.1"; p["core_priority"].([]any)[0] = "INNOVATION" })
	}
}
func TestPersonaNoSelfQualification(t *testing.T) {
	for _, field := range []string{"artifact_id", "approved", "approval_proof", "qualification", "PER-01"} {
		t.Run(field, func(t *testing.T) { personaReject(t, "casper", func(p map[string]any) { p[field] = "self-signed" }) })
	}
	for _, field := range []string{"maintenance_policy_ref", "issuance_policy_ref", "created_by"} {
		t.Run("missing/"+field, func(t *testing.T) { personaReject(t, "casper", func(p map[string]any) { delete(p, field) }) })
	}
	p := personaFixture(t, "casper")
	p["compatibility"].(map[string]any)["behavior_acceptance_ref"].(map[string]any)["object_id"] = "self-signed-PER-01-passed"
	v, err := ValidatePersonaSpec(encode(t, p), "", personaOptions(t))
	if err != nil {
		t.Fatal(err)
	}
	if v.Status().Qualification != "UNVERIFIABLE_REFERENCE" || v.Status().Behavior != "NOT_RUN" {
		t.Fatal("unverified body certified authority or behavior")
	}
	opts := personaOptions(t)
	opts.RequireCompleteQualification = true
	if _, err = ValidatePersonaSpec(encode(t, p), "", opts); err == nil {
		t.Fatal("complete qualification failed open")
	}
}
func TestPersonaReferenceVersionsAndTime(t *testing.T) {
	for _, alias := range []string{"latest", "current", "release/latest", "v1.x"} {
		personaReject(t, "casper", func(p map[string]any) { p["maintenance_policy_ref"].(map[string]any)["version"] = alias })
	}
	personaReject(t, "casper", func(p map[string]any) { p["created_by"].(map[string]any)["identity_source_version"] = "latest" })
	personaReject(t, "casper", func(p map[string]any) { p["frozen_at"] = "2026-02-30T00:00:00.000Z" })
	personaReject(t, "casper", func(p map[string]any) { p["validity"].(map[string]any)["valid_until"] = "2026-10-21T00:00:00.000Z" })
	personaReject(t, "casper", func(p map[string]any) {
		p["compatibility"].(map[string]any)["continuity_policy_ref"] = p["maintenance_policy_ref"]
		p["compatibility"].(map[string]any)["continuity_policy_ref"] = map[string]any{"namespace": "fixture", "object_id": "persona-maintenance", "version": "1", "digest_alg": "sha256", "content_digest": strings.Repeat("f", 64)}
	})
}
func TestPersonaBehaviorIntegration(t *testing.T) {
	t.Skip("NOT RUN: PER-01..03 and Rev4 17.5 require real observable investigations and independent intervention experiments; no mock votes certify persona fidelity")
}
func TestPersonaQualificationIntegration(t *testing.T) {
	t.Skip("BLOCKED: C2 governance roots, external identity/approval/continuity records and revocation services are absent")
}

func TestPersonaBodyPreservesCompleteSchema(t *testing.T) {
	for _, role := range []string{"melchior", "balthasar", "casper"} {
		v := personaValid(t, role)
		c, err := canonical.Canonicalize(encode(t, v.Body()))
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(c.Bytes(), v.CanonicalBytes()) {
			t.Fatal("typed Body lost signed Schema fields")
		}
	}
}
