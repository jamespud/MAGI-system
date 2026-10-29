package authoritycommit_test

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"math"
	"strings"
	"testing"

	"github.com/jamespud/magi/backend/domain/authority"
	"github.com/jamespud/magi/backend/domain/authoritycommit"
	"github.com/jamespud/magi/backend/domain/canonical"
)

// The plan uses the frozen v2 JSON field names, not production constructors.
// All references are synthetic immutable inputs; no C2 authentication is implied.
func admissionRequest(t *testing.T) authoritycommit.AdmissionRequest {
	t.Helper()
	const plan = `{
		"key":{"case_id":"case-r0","round":1,"context_manifest_id":"manifest-r0","persona_id":"melchior","delivery_id":"delivery-r0","provider_request_id":"request-r0"},
		"principal":{"subject_id":"recorder","identity_source_id":"identity-source","identity_source_version":"1"},
		"execution_generation":7,
		"scoped_epochs":[{"kind":"AUTHORITY","scope_id":"authority-r0","epoch":0},{"kind":"CONTEXT","scope_id":"context-r0","epoch":4},{"kind":"CREDENTIAL","scope_id":"credential-r0","epoch":5}],
		"mutation_set":["admission","dependency_edges","exposure_fence","journal_event","journal_high_watermark","lineage","outbox","pending_transition","provider_reservation","token_consumed","unique_binding"],
		"admission_input":{"id":"admission-r0","digest":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","object_ref":"object:admission-r0"},
		"journal_event_input":{"id":"event-r0","digest":"bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb","object_ref":"object:event-r0"},
		"dependency_edge_inputs":[{"id":"edge-r0","digest":"cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc","object_ref":"object:edge-r0"}],
		"high_watermark":{"journal_id":"journal-r0","expected":0,"next":1},
		"provider_reservation":{"request_id":"request-r0","session_scope_id":"session-r0","expected_status":"ABSENT","next_status":"RESERVED"},
		"pending_transition":{"journal_id":"journal-r0","expected":0,"next":1},
		"fence":{"mode":"PROVIDER_ENFORCED_LEASE","lease_id":"lease-r0","lease_until":"2026-10-26T01:00:30.000Z","authority_watermark":19,"execution_generation":7,"scoped_epochs":[{"kind":"AUTHORITY","scope_id":"authority-r0","epoch":0},{"kind":"CONTEXT","scope_id":"context-r0","epoch":4},{"kind":"CREDENTIAL","scope_id":"credential-r0","epoch":5}],"provider_capability_ref":{"id":"capability-r0","digest":"dddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd","object_ref":"object:capability-r0"}}
	}`
	var req authoritycommit.AdmissionRequest
	if err := json.Unmarshal([]byte(plan), &req.Admission); err != nil {
		t.Fatal(err)
	}
	req.ContractVersion = "magi-authority-journal-v2/1"
	req.Operation = "JOURNAL_ADMIT_V2"
	req.ExactTargetID = "admission-r0"
	req.CommitToken = "commit-token-r0"
	req.Verification = authority.VerificationRequest{
		TrustDomain: "trust-r0", ProofID: "aprf_" + strings.Repeat("e", 64),
		Subject: req.Admission.Principal, Resource: "admission-r0", Action: "READ", Tenant: "tenant-r0",
		Operation: "JOURNAL_ADMIT_V2", CaseID: "case-r0", Round: 1, ExecutionGeneration: 7,
		Scopes: append([]authority.ScopedEpoch(nil), req.Admission.ScopedEpochs...),
	}
	return req
}

func requireCode(t *testing.T, req authoritycommit.AdmissionRequest, code string) {
	t.Helper()
	result := authoritycommit.Validate(req)
	if result.Outcome != authority.OutcomeDenied || result.Code != code {
		t.Fatalf("Validate() = %+v; want DENIED/%s", result, code)
	}
	if result.Commit != nil || result.ExposurePermitted {
		t.Fatalf("rejected contract returned authority: %+v", result)
	}
}

func TestAdmissionRequestValidateAcceptsExactR0JournalAdmit(t *testing.T) {
	req := admissionRequest(t)
	result := authoritycommit.Validate(req)
	if result.Outcome != authority.OutcomeValid || result.Code != "ADMISSION_REQUEST_VALID" {
		t.Fatalf("Validate() = %+v; want VALID/ADMISSION_REQUEST_VALID", result)
	}
	if result.Commit != nil || result.ExposurePermitted {
		t.Fatalf("structural validation granted authority: %+v", result)
	}
	// Journal-local HWM need not equal the authority watermark or pending count.
	req.Admission.HighWatermark.Expected, req.Admission.HighWatermark.Next = 300, 301
	req.Admission.PendingTransition.Expected, req.Admission.PendingTransition.Next = 511, 512
	if result := authoritycommit.Validate(req); result.Outcome != authority.OutcomeValid {
		t.Fatalf("valid independent counters rejected: %+v", result)
	}
	req.Admission.Fence.Mode = "AUTHORITY_ORDERED_GATE"
	if result := authoritycommit.Validate(req); result.Outcome != authority.OutcomeValid || result.ExposurePermitted {
		t.Fatalf("gate declaration rejected or permitted exposure: %+v", result)
	}
}

func TestAdmissionRequestRejectsPrincipalTargetAndScopeDrift(t *testing.T) {
	tests := []struct {
		name string
		edit func(*authoritycommit.AdmissionRequest)
		code string
	}{
		{"subject", func(r *authoritycommit.AdmissionRequest) { r.Verification.Subject.SubjectID = "other" }, "PRINCIPAL_MISMATCH"},
		{"identity source", func(r *authoritycommit.AdmissionRequest) { r.Verification.Subject.IdentitySourceID = "other" }, "PRINCIPAL_MISMATCH"},
		{"identity version", func(r *authoritycommit.AdmissionRequest) { r.Verification.Subject.IdentitySourceVersion = "2" }, "PRINCIPAL_MISMATCH"},
		{"empty principal", func(r *authoritycommit.AdmissionRequest) {
			r.Admission.Principal = authority.Subject{}
			r.Verification.Subject = authority.Subject{}
		}, "PRINCIPAL_MISMATCH"},
		{"target", func(r *authoritycommit.AdmissionRequest) { r.ExactTargetID = "other" }, "TARGET_MISMATCH"},
		{"resource", func(r *authoritycommit.AdmissionRequest) { r.Verification.Resource = "other" }, "TARGET_MISMATCH"},
		{"case", func(r *authoritycommit.AdmissionRequest) { r.Verification.CaseID = "other" }, "CASE_MISMATCH"},
		{"round", func(r *authoritycommit.AdmissionRequest) { r.Verification.Round++ }, "ROUND_MISMATCH"},
		{"generation", func(r *authoritycommit.AdmissionRequest) { r.Verification.ExecutionGeneration++ }, "GENERATION_MISMATCH"},
		{"legacy generation", func(r *authoritycommit.AdmissionRequest) {
			r.Verification.ExecutionGeneration = 0
			r.Admission.ExecutionGeneration = 0
			r.Admission.Fence.ExecutionGeneration = 0
		}, "GENERATION_MISMATCH"},
		{"fence generation", func(r *authoritycommit.AdmissionRequest) { r.Admission.Fence.ExecutionGeneration++ }, "GENERATION_MISMATCH"},
		{"epoch", func(r *authoritycommit.AdmissionRequest) { r.Verification.Scopes[0].Epoch++ }, "SCOPE_DRIFT"},
		{"scope id", func(r *authoritycommit.AdmissionRequest) { r.Verification.Scopes[1].ScopeID = "other" }, "SCOPE_DRIFT"},
		{"fence epoch", func(r *authoritycommit.AdmissionRequest) { r.Admission.Fence.ScopedEpochs[2].Epoch++ }, "SCOPE_DRIFT"},
		{"missing scope", func(r *authoritycommit.AdmissionRequest) { r.Admission.ScopedEpochs = r.Admission.ScopedEpochs[:2] }, "SCOPE_DRIFT"},
		{"extra scope", func(r *authoritycommit.AdmissionRequest) {
			r.Admission.ScopedEpochs = append(r.Admission.ScopedEpochs, r.Admission.ScopedEpochs[2])
		}, "SCOPE_DRIFT"},
		{"duplicate scope", func(r *authoritycommit.AdmissionRequest) { r.Admission.ScopedEpochs[1] = r.Admission.ScopedEpochs[0] }, "SCOPE_DRIFT"},
		{"unsorted scopes", func(r *authoritycommit.AdmissionRequest) {
			r.Admission.ScopedEpochs[0], r.Admission.ScopedEpochs[1] = r.Admission.ScopedEpochs[1], r.Admission.ScopedEpochs[0]
		}, "SCOPE_DRIFT"},
		{"duplicate kind", func(r *authoritycommit.AdmissionRequest) {
			r.Admission.ScopedEpochs[1].Kind = authority.ScopeAuthority
			r.Admission.ScopedEpochs[1].ScopeID = "authority-z"
		}, "SCOPE_DRIFT"},
		{"unknown kind", func(r *authoritycommit.AdmissionRequest) { r.Admission.ScopedEpochs[2].Kind = "OTHER" }, "SCOPE_DRIFT"},
		{"empty scope id", func(r *authoritycommit.AdmissionRequest) { r.Admission.ScopedEpochs[0].ScopeID = "" }, "SCOPE_DRIFT"},
		{"all scopes reordered", func(r *authoritycommit.AdmissionRequest) {
			for _, scopes := range [][]authority.ScopedEpoch{r.Admission.ScopedEpochs, r.Verification.Scopes, r.Admission.Fence.ScopedEpochs} {
				scopes[0], scopes[1] = scopes[1], scopes[0]
			}
		}, "SCOPE_DRIFT"},
		{"all scopes missing kind", func(r *authoritycommit.AdmissionRequest) {
			for _, scopes := range [][]authority.ScopedEpoch{r.Admission.ScopedEpochs, r.Verification.Scopes, r.Admission.Fence.ScopedEpochs} {
				scopes[1].Kind = authority.ScopeAuthority
				scopes[1].ScopeID = "authority-z"
			}
		}, "SCOPE_DRIFT"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) { req := admissionRequest(t); tt.edit(&req); requireCode(t, req, tt.code) })
	}
}

func TestAdmissionRequestRejectsNonV2OperationAndForbiddenMutation(t *testing.T) {
	for _, operation := range []string{"", "JOURNAL_ADMIT", "JOURNAL_EXPOSURE_GATE_V2", "JOURNAL_SETTLE_V2", "SEND_COMMIT"} {
		t.Run(operation, func(t *testing.T) {
			req := admissionRequest(t)
			req.Operation = operation
			requireCode(t, req, "OPERATION_INVALID")
		})
	}
	t.Run("verification operation", func(t *testing.T) {
		req := admissionRequest(t)
		req.Verification.Operation = "JOURNAL_ADMIT"
		requireCode(t, req, "OPERATION_INVALID")
	})
	t.Run("version", func(t *testing.T) {
		req := admissionRequest(t)
		req.ContractVersion = "magi-authority/1"
		requireCode(t, req, "CONTRACT_VERSION_INVALID")
	})
	for _, proofID := range []string{"", "DISABLED", "disabled", "aprf_" + strings.Repeat("g", 64)} {
		t.Run("proof/"+proofID, func(t *testing.T) {
			req := admissionRequest(t)
			req.Verification.ProofID = proofID
			requireCode(t, req, "PROOF_ID_INVALID")
		})
	}
	t.Run("empty token", func(t *testing.T) {
		req := admissionRequest(t)
		req.CommitToken = ""
		requireCode(t, req, "COMMIT_TOKEN_REQUIRED")
	})
	for index := range admissionRequest(t).Admission.MutationSet {
		t.Run("missing/"+admissionRequest(t).Admission.MutationSet[index], func(t *testing.T) {
			req := admissionRequest(t)
			req.Admission.MutationSet = append(req.Admission.MutationSet[:index], req.Admission.MutationSet[index+1:]...)
			requireCode(t, req, "MUTATION_SET_INVALID")
		})
		t.Run("duplicate/"+admissionRequest(t).Admission.MutationSet[index], func(t *testing.T) {
			req := admissionRequest(t)
			req.Admission.MutationSet = append(req.Admission.MutationSet, req.Admission.MutationSet[index])
			requireCode(t, req, "MUTATION_SET_INVALID")
		})
	}
	for _, mutation := range []string{"provider_settlement", "APPLIED_VERIFIED", "read_completed", "other", "exposure_permit"} {
		t.Run(mutation, func(t *testing.T) {
			req := admissionRequest(t)
			req.Admission.MutationSet = append(req.Admission.MutationSet, mutation)
			code := "MUTATION_SET_INVALID"
			if mutation == "exposure_permit" {
				code = "EXPOSURE_PERMIT_FORBIDDEN"
			}
			requireCode(t, req, code)
		})
	}
}

func TestAdmissionRequestRejectsNonAtomicHighWatermarkAndPendingTransitions(t *testing.T) {
	tests := []struct {
		name string
		edit func(*authoritycommit.AdmissionRequest)
		code string
	}{
		{"HWM unchanged", func(r *authoritycommit.AdmissionRequest) { r.Admission.HighWatermark.Next = 0 }, "HWM_TRANSITION_INVALID"},
		{"HWM skipped", func(r *authoritycommit.AdmissionRequest) { r.Admission.HighWatermark.Next = 2 }, "HWM_TRANSITION_INVALID"},
		{"HWM overflow", func(r *authoritycommit.AdmissionRequest) {
			r.Admission.HighWatermark.Expected = math.MaxUint64
			r.Admission.HighWatermark.Next = 0
		}, "HWM_TRANSITION_INVALID"},
		{"HWM safe range", func(r *authoritycommit.AdmissionRequest) {
			r.Admission.HighWatermark.Expected = uint64(canonical.MaxSafeInteger)
			r.Admission.HighWatermark.Next = uint64(canonical.MaxSafeInteger) + 1
		}, "HWM_TRANSITION_INVALID"},
		{"empty journal", func(r *authoritycommit.AdmissionRequest) { r.Admission.HighWatermark.JournalID = "" }, "HWM_TRANSITION_INVALID"},
		{"pending unchanged", func(r *authoritycommit.AdmissionRequest) { r.Admission.PendingTransition.Next = 0 }, "PENDING_TRANSITION_INVALID"},
		{"pending skipped", func(r *authoritycommit.AdmissionRequest) { r.Admission.PendingTransition.Next = 2 }, "PENDING_TRANSITION_INVALID"},
		{"pending decrement", func(r *authoritycommit.AdmissionRequest) {
			r.Admission.PendingTransition.Expected = 1
			r.Admission.PendingTransition.Next = 0
		}, "PENDING_TRANSITION_INVALID"},
		{"pending ceiling", func(r *authoritycommit.AdmissionRequest) {
			r.Admission.PendingTransition.Expected = 512
			r.Admission.PendingTransition.Next = 513
		}, "PENDING_TRANSITION_INVALID"},
		{"pending overflow", func(r *authoritycommit.AdmissionRequest) {
			r.Admission.PendingTransition.Expected = math.MaxUint64
			r.Admission.PendingTransition.Next = 0
		}, "PENDING_TRANSITION_INVALID"},
		{"different journals", func(r *authoritycommit.AdmissionRequest) { r.Admission.PendingTransition.JournalID = "other" }, "PENDING_TRANSITION_INVALID"},
		{"reservation expected", func(r *authoritycommit.AdmissionRequest) { r.Admission.ProviderReservation.ExpectedStatus = "PENDING" }, "RESERVATION_TRANSITION_INVALID"},
		{"reservation next", func(r *authoritycommit.AdmissionRequest) {
			r.Admission.ProviderReservation.NextStatus = "APPLIED_VERIFIED"
		}, "RESERVATION_TRANSITION_INVALID"},
		{"reservation request", func(r *authoritycommit.AdmissionRequest) { r.Admission.ProviderReservation.RequestID = "other" }, "TARGET_MISMATCH"},
		{"reservation session", func(r *authoritycommit.AdmissionRequest) { r.Admission.ProviderReservation.SessionScopeID = "" }, "RESERVATION_TRANSITION_INVALID"},
		{"missing dependency", func(r *authoritycommit.AdmissionRequest) { r.Admission.DependencyEdgeInputs = nil }, "DEPENDENCY_INVALID"},
		{"empty dependency", func(r *authoritycommit.AdmissionRequest) {
			r.Admission.DependencyEdgeInputs[0] = authoritycommit.PreparedObject{}
		}, "DEPENDENCY_INVALID"},
		{"dependency reference", func(r *authoritycommit.AdmissionRequest) { r.Admission.DependencyEdgeInputs[0].ObjectRef = "" }, "DEPENDENCY_INVALID"},
		{"dependency digest", func(r *authoritycommit.AdmissionRequest) { r.Admission.DependencyEdgeInputs[0].Digest = "bad" }, "DIGEST_INVALID"},
		{"admission digest", func(r *authoritycommit.AdmissionRequest) { r.Admission.AdmissionInput.Digest = strings.Repeat("A", 64) }, "DIGEST_INVALID"},
		{"event digest", func(r *authoritycommit.AdmissionRequest) {
			r.Admission.JournalEventInput.Digest = strings.Repeat("a", 63)
		}, "DIGEST_INVALID"},
		{"capability digest", func(r *authoritycommit.AdmissionRequest) {
			r.Admission.Fence.ProviderCapabilityRef.Digest = strings.Repeat("z", 64)
		}, "DIGEST_INVALID"},
		{"missing event", func(r *authoritycommit.AdmissionRequest) {
			r.Admission.JournalEventInput = authoritycommit.PreparedObject{}
		}, "DEPENDENCY_INVALID"},
		{"query only fence", func(r *authoritycommit.AdmissionRequest) { r.Admission.Fence.Mode = "QUERY_ONLY" }, "FENCE_INVALID"},
		{"missing lease", func(r *authoritycommit.AdmissionRequest) { r.Admission.Fence.LeaseID = "" }, "FENCE_INVALID"},
		{"malformed lease", func(r *authoritycommit.AdmissionRequest) { r.Admission.Fence.LeaseUntil = "2026-02-30T01:00:30.000Z" }, "FENCE_INVALID"},
		{"lease precision", func(r *authoritycommit.AdmissionRequest) { r.Admission.Fence.LeaseUntil = "2026-10-26T01:00:30Z" }, "FENCE_INVALID"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) { req := admissionRequest(t); tt.edit(&req); requireCode(t, req, tt.code) })
	}
}

// Independently compute the specified two-layer digest using encoding/json and
// SHA-256. The ASCII fixture uses only strings, integers, arrays and objects.
func expectedDigest(t *testing.T, value any, prefix string) string {
	t.Helper()
	raw, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	var tree any
	if err := decoder.Decode(&tree); err != nil {
		t.Fatal(err)
	}
	sorted, err := json.Marshal(tree)
	if err != nil {
		t.Fatal(err)
	}
	inner := sha256.Sum256(append([]byte("magi:canonical:v1:authority_proof\n"), sorted...))
	outer := sha256.Sum256([]byte(prefix + hex.EncodeToString(inner[:])))
	return hex.EncodeToString(outer[:])
}

func TestAdmissionRequestDigestIsCanonicalAndBindsCommitToken(t *testing.T) {
	req := admissionRequest(t)
	digest, err := authoritycommit.RequestDigest(req)
	if err != nil {
		t.Fatal(err)
	}
	if want := expectedDigest(t, req, "magi:authority-journal-admit-v2:"); digest != want {
		t.Fatalf("request digest = %s, want %s", digest, want)
	}
	raw, err := json.MarshalIndent(req, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	var roundTrip authoritycommit.AdmissionRequest
	if err := json.Unmarshal(raw, &roundTrip); err != nil {
		t.Fatal(err)
	}
	if got, err := authoritycommit.RequestDigest(roundTrip); err != nil || got != digest {
		t.Fatalf("same request changed digest: %s, %v", got, err)
	}
	for name, edit := range map[string]func(*authoritycommit.AdmissionRequest){
		"commit token": func(r *authoritycommit.AdmissionRequest) { r.CommitToken = "another-token" },
		"target":       func(r *authoritycommit.AdmissionRequest) { r.ExactTargetID = "other-target" },
		"proof":        func(r *authoritycommit.AdmissionRequest) { r.Verification.ProofID = "aprf_" + strings.Repeat("f", 64) },
		"principal":    func(r *authoritycommit.AdmissionRequest) { r.Admission.Principal.SubjectID = "other" },
		"dependency": func(r *authoritycommit.AdmissionRequest) {
			r.Admission.DependencyEdgeInputs[0].Digest = strings.Repeat("f", 64)
		},
		"reservation": func(r *authoritycommit.AdmissionRequest) { r.Admission.ProviderReservation.SessionScopeID = "other" },
		"generation":  func(r *authoritycommit.AdmissionRequest) { r.Admission.ExecutionGeneration++ },
		"scope":       func(r *authoritycommit.AdmissionRequest) { r.Admission.ScopedEpochs[0].Epoch++ },
		"HWM":         func(r *authoritycommit.AdmissionRequest) { r.Admission.HighWatermark.Next++ },
		"pending":     func(r *authoritycommit.AdmissionRequest) { r.Admission.PendingTransition.Next++ },
		"fence":       func(r *authoritycommit.AdmissionRequest) { r.Admission.Fence.LeaseUntil = "2026-10-26T01:00:31.000Z" },
	} {
		t.Run(name, func(t *testing.T) {
			changed := admissionRequest(t)
			edit(&changed)
			got, err := authoritycommit.RequestDigest(changed)
			if err != nil || got == digest {
				t.Fatalf("changed %s did not get distinct digest: %s, %v", name, got, err)
			}
		})
	}
	t.Run("noncanonical request", func(t *testing.T) {
		bad := admissionRequest(t)
		bad.CommitToken = "e\u0301"
		if got, err := authoritycommit.RequestDigest(bad); err == nil || got != "" {
			t.Fatalf("non-NFC request hashed: %s, %v", got, err)
		}
	})
	commit := authoritycommit.AdmissionCommit{RequestDigest: digest, CommitID: "commit-r0", AuthoritySequence: 20, JournalHighWatermark: 1}
	lineage, err := authoritycommit.LineageDigest(commit)
	if err != nil {
		t.Fatal(err)
	}
	want := expectedDigest(t, map[string]any{"request_digest": digest, "commit_id": "commit-r0", "authority_sequence": 20, "journal_high_watermark": 1}, "magi:authority-journal-admit-v2:lineage:")
	if lineage != want || lineage == digest {
		t.Fatalf("lineage = %s, want %s", lineage, want)
	}
	for name, edit := range map[string]func(*authoritycommit.AdmissionCommit){
		"request":            func(c *authoritycommit.AdmissionCommit) { c.RequestDigest = strings.Repeat("f", 64) },
		"commit":             func(c *authoritycommit.AdmissionCommit) { c.CommitID = "commit-other" },
		"authority sequence": func(c *authoritycommit.AdmissionCommit) { c.AuthoritySequence++ },
		"journal HWM":        func(c *authoritycommit.AdmissionCommit) { c.JournalHighWatermark++ },
		"swapped sequence and HWM": func(c *authoritycommit.AdmissionCommit) {
			c.AuthoritySequence, c.JournalHighWatermark = c.JournalHighWatermark, c.AuthoritySequence
		},
	} {
		t.Run("lineage/"+name, func(t *testing.T) {
			changed := commit
			edit(&changed)
			got, err := authoritycommit.LineageDigest(changed)
			if err != nil || got == lineage {
				t.Fatalf("lineage did not bind %s: %s, %v", name, got, err)
			}
		})
	}
	commit.LineageDigest = lineage
	if got, err := authoritycommit.LineageDigest(commit); err != nil || got != lineage {
		t.Fatalf("lineage depends on itself: %s, %v", got, err)
	}
}

func TestAdmissionResultNeverPermitsExposure(t *testing.T) {
	var zero authoritycommit.Result
	if zero.ExposurePermitted || zero.Commit != nil || zero.Outcome == authority.OutcomeValid {
		t.Fatalf("zero result grants authority: %+v", zero)
	}
	valid := admissionRequest(t)
	invalid := admissionRequest(t)
	invalid.Admission.MutationSet = append(invalid.Admission.MutationSet, "exposure_permit")
	for _, req := range []authoritycommit.AdmissionRequest{{}, valid, invalid} {
		result := authoritycommit.Validate(req)
		if result.ExposurePermitted || result.Commit != nil {
			t.Fatalf("validation grants exposure/commit: %+v", result)
		}
		encoded, err := json.Marshal(result)
		if err != nil {
			t.Fatal(err)
		}
		var wire map[string]any
		if err := json.Unmarshal(encoded, &wire); err != nil {
			t.Fatal(err)
		}
		if wire["exposure_permitted"] != false {
			t.Fatalf("exposure denial missing on wire: %s", encoded)
		}
	}
}

func admissionUniqueKey(t *testing.T, req authoritycommit.AdmissionRequest) string {
	t.Helper()
	key, err := authoritycommit.UniqueKey(authoritycommit.ProjectAdmissionIdentity(req))
	if err != nil {
		t.Fatal(err)
	}
	if len(key) != 64 {
		t.Fatalf("unique key = %q; want a SHA-256 identity", key)
	}
	return key
}

func TestAdmissionUniqueKeyIsolatesTrustDomains(t *testing.T) {
	req := admissionRequest(t)
	key := admissionUniqueKey(t, req)
	req.Verification.TrustDomain = "other-trust-domain"
	if other := admissionUniqueKey(t, req); other == key {
		t.Fatalf("same admission in different trust domains shared key %s", key)
	}
}

func TestAdmissionUniqueKeyIgnoresRequestAndAllocationFields(t *testing.T) {
	req := admissionRequest(t)
	key := admissionUniqueKey(t, req)
	requestDigest, err := authoritycommit.RequestDigest(req)
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name string
		edit func(*authoritycommit.AdmissionRequest)
	}{
		{"provider request", func(r *authoritycommit.AdmissionRequest) {
			r.Admission.Key.ProviderRequestID = "other-request"
			r.Admission.ProviderReservation.RequestID = "other-request"
		}},
		{"commit token", func(r *authoritycommit.AdmissionRequest) { r.CommitToken = "other-token" }},
		{"admission input digest", func(r *authoritycommit.AdmissionRequest) { r.Admission.AdmissionInput.Digest = strings.Repeat("f", 64) }},
		{"event input digest", func(r *authoritycommit.AdmissionRequest) {
			r.Admission.JournalEventInput.Digest = strings.Repeat("f", 64)
		}},
		{"dependency digest", func(r *authoritycommit.AdmissionRequest) {
			r.Admission.DependencyEdgeInputs[0].Digest = strings.Repeat("f", 64)
		}},
		{"capability digest", func(r *authoritycommit.AdmissionRequest) {
			r.Admission.Fence.ProviderCapabilityRef.Digest = strings.Repeat("f", 64)
		}},
		{"authority watermark", func(r *authoritycommit.AdmissionRequest) { r.Admission.Fence.AuthorityWatermark++ }},
		{"journal HWM", func(r *authoritycommit.AdmissionRequest) {
			r.Admission.HighWatermark.Expected = 30
			r.Admission.HighWatermark.Next = 31
		}},
		{"pending allocation", func(r *authoritycommit.AdmissionRequest) {
			r.Admission.PendingTransition.Expected = 10
			r.Admission.PendingTransition.Next = 11
		}},
		{"execution generation", func(r *authoritycommit.AdmissionRequest) {
			r.Admission.ExecutionGeneration++
			r.Verification.ExecutionGeneration++
			r.Admission.Fence.ExecutionGeneration++
		}},
		{"exact target", func(r *authoritycommit.AdmissionRequest) {
			r.ExactTargetID = "other-admission"
			r.Verification.Resource = "other-admission"
			r.Admission.AdmissionInput.ID = "other-admission"
		}},
		{"proof", func(r *authoritycommit.AdmissionRequest) { r.Verification.ProofID = "aprf_" + strings.Repeat("f", 64) }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			changed := admissionRequest(t)
			tt.edit(&changed)
			if got := admissionUniqueKey(t, changed); got != key {
				t.Fatalf("non-key field %s changed durable key: %s != %s", tt.name, got, key)
			}
			// Exact request binding still distinguishes these proposals so that
			// later persistence can detect same-key/different-request conflicts.
			got, err := authoritycommit.RequestDigest(changed)
			if err != nil || got == requestDigest {
				t.Fatalf("request digest did not distinguish %s: %s, %v", tt.name, got, err)
			}
		})
	}
}

func TestAdmissionUniqueKeyBindsExactDurableIdentity(t *testing.T) {
	key := admissionUniqueKey(t, admissionRequest(t))
	tests := []struct {
		name string
		edit func(*authoritycommit.AdmissionRequest)
	}{
		{"case", func(r *authoritycommit.AdmissionRequest) { r.Admission.Key.CaseID = "other-case" }},
		{"round", func(r *authoritycommit.AdmissionRequest) { r.Admission.Key.Round++ }},
		{"context manifest", func(r *authoritycommit.AdmissionRequest) { r.Admission.Key.ContextManifestID = "other-manifest" }},
		{"persona", func(r *authoritycommit.AdmissionRequest) { r.Admission.Key.PersonaID = "casper" }},
		{"delivery", func(r *authoritycommit.AdmissionRequest) { r.Admission.Key.DeliveryID = "other-delivery" }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := admissionRequest(t)
			tt.edit(&req)
			if got := admissionUniqueKey(t, req); got == key {
				t.Fatalf("durable key did not bind %s", tt.name)
			}
		})
	}
	// Legal separator characters cannot make different tuples alias by
	// ambiguous concatenation of adjacent identity fields.
	a, b := admissionRequest(t), admissionRequest(t)
	a.Admission.Key.CaseID, a.Admission.Key.ContextManifestID = "case:r0", "manifest"
	b.Admission.Key.CaseID, b.Admission.Key.ContextManifestID = "case", "r0:manifest"
	if admissionUniqueKey(t, a) == admissionUniqueKey(t, b) {
		t.Fatal("distinct tuples collided at field boundaries")
	}
}

func TestAdmissionUniqueKeyProjectionIsImmutable(t *testing.T) {
	req := admissionRequest(t)
	identity := authoritycommit.ProjectAdmissionIdentity(req)
	key := admissionUniqueKey(t, req)
	req.Verification.TrustDomain = "other-trust"
	req.Admission.Key.CaseID = "other-case"
	req.Admission.Key.Round++
	req.Admission.Key.ContextManifestID = "other-manifest"
	req.Admission.Key.PersonaID = "casper"
	req.Admission.Key.DeliveryID = "other-delivery"
	got, err := authoritycommit.UniqueKey(identity)
	if err != nil || got != key {
		t.Fatalf("identity changed with its source request: %s, %v", got, err)
	}
	if admissionUniqueKey(t, req) == key {
		t.Fatal("fresh projection ignored changed identity")
	}
}

func TestAdmissionUniqueKeyIsDomainSeparatedAndDoesNotAuthorizeOrValidate(t *testing.T) {
	req := admissionRequest(t)
	want := expectedDigest(t, map[string]any{
		"trust_domain": "trust-r0", "case_id": "case-r0", "round": 1,
		"context_manifest_id": "manifest-r0", "persona_id": "melchior", "delivery_id": "delivery-r0",
	}, "magi:authority-journal-admit-v2:unique-key:")
	if got := admissionUniqueKey(t, req); got != want {
		t.Fatalf("unique key = %s, want domain-separated exact projection %s", got, want)
	}
	if requestDigest, err := authoritycommit.RequestDigest(req); err != nil || requestDigest == want {
		t.Fatalf("request identity reused as unique key: %s, %v", requestDigest, err)
	}

	// Projection and derivation work even when all authorization fields are
	// unusable. They must not validate, normalize or upgrade the input request.
	req.ContractVersion = "unsupported"
	req.Operation = "SEND_COMMIT"
	req.Verification.ProofID = ""
	req.CommitToken = ""
	req.Admission.MutationSet = []string{"exposure_permit"}
	if got := admissionUniqueKey(t, req); got != want {
		t.Fatalf("unvalidated request lost its stable identity: %s", got)
	}
	if result := authoritycommit.Validate(req); result.Outcome != authority.OutcomeDenied || result.Commit != nil || result.ExposurePermitted {
		t.Fatalf("deriving an identity authorized or repaired the request: %+v", result)
	}
	// Even the zero tuple has deterministic identity bytes, without acquiring
	// semantic validity; only C0 representation errors stop derivation.
	_ = admissionUniqueKey(t, authoritycommit.AdmissionRequest{})
	if result := authoritycommit.Validate(authoritycommit.AdmissionRequest{}); result.Outcome != authority.OutcomeDenied {
		t.Fatalf("empty identity granted validity: %+v", result)
	}
	req.Admission.Key.CaseID = "case-e\u0301"
	if key, err := authoritycommit.UniqueKey(authoritycommit.ProjectAdmissionIdentity(req)); err == nil || key != "" {
		t.Fatalf("noncanonical key material was silently repaired: %s, %v", key, err)
	}
}

func TestAdmissionCommitCarriesUniqueKey(t *testing.T) {
	key := admissionUniqueKey(t, admissionRequest(t))
	original := authoritycommit.Result{
		Outcome: authority.OutcomeValid,
		Commit: &authoritycommit.AdmissionCommit{
			UniqueKey: key, RequestDigest: strings.Repeat("f", 64),
			CommitID: "commit-r0", AuthoritySequence: 20, JournalHighWatermark: 1,
		},
	}
	raw, err := json.Marshal(original)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(raw, []byte(`"unique_key":"`+key+`"`)) {
		t.Fatalf("unique key missing from persisted result: %s", raw)
	}
	var replay authoritycommit.Result
	if err := json.Unmarshal(raw, &replay); err != nil {
		t.Fatal(err)
	}
	if replay.Commit == nil || replay.Commit.UniqueKey != key || replay.ExposurePermitted {
		t.Fatalf("replayed result lost identity or gained exposure permission: %+v", replay)
	}
}

func TestAdmissionRequestRejectsDuplicateDependencyIdentities(t *testing.T) {
	req := admissionRequest(t)
	duplicate := req.Admission.DependencyEdgeInputs[0]
	duplicate.Digest = strings.Repeat("f", 64)
	req.Admission.DependencyEdgeInputs = append(req.Admission.DependencyEdgeInputs, duplicate)
	if result := authoritycommit.Validate(req); result.Outcome != authority.OutcomeDenied || result.Code != "DEPENDENCY_INVALID" {
		t.Fatalf("duplicate dependency result=%+v", result)
	}
}

func TestValidateReplayRequiresIndependentAuditBinding(t *testing.T) {
	original := admissionRequest(t)
	audit := original.Verification
	audit.ProofID = "aprf_" + strings.Repeat("f", 64)
	audit.Operation = authoritycommit.JournalAuditRead
	request := authoritycommit.ReplayRequest{Original: original, AuditVerification: audit}
	result := authoritycommit.ValidateReplay(request)
	if result.Outcome != authority.OutcomeValid || result.Code != "ADMISSION_REPLAY_REQUEST_VALID" || result.Commit != nil || result.ExposurePermitted {
		t.Fatalf("valid replay request=%+v", result)
	}
	tests := []struct {
		name string
		edit func(*authoritycommit.ReplayRequest)
		code string
	}{
		{"missing proof", func(r *authoritycommit.ReplayRequest) { r.AuditVerification.ProofID = "" }, "AUDIT_AUTHORIZATION_REQUIRED"},
		{"operational operation", func(r *authoritycommit.ReplayRequest) { r.AuditVerification.Operation = authoritycommit.JournalAdmitV2 }, "AUDIT_BINDING_MISMATCH"},
		{"resource", func(r *authoritycommit.ReplayRequest) { r.AuditVerification.Resource = "other" }, "AUDIT_BINDING_MISMATCH"},
		{"trust domain", func(r *authoritycommit.ReplayRequest) { r.AuditVerification.TrustDomain = "other" }, "AUDIT_BINDING_MISMATCH"},
		{"tenant", func(r *authoritycommit.ReplayRequest) { r.AuditVerification.Tenant = "other" }, "AUDIT_BINDING_MISMATCH"},
		{"action", func(r *authoritycommit.ReplayRequest) { r.AuditVerification.Action = "other" }, "AUDIT_BINDING_MISMATCH"},
		{"case", func(r *authoritycommit.ReplayRequest) { r.AuditVerification.CaseID = "other" }, "AUDIT_BINDING_MISMATCH"},
		{"round", func(r *authoritycommit.ReplayRequest) { r.AuditVerification.Round++ }, "AUDIT_BINDING_MISMATCH"},
		{"generation", func(r *authoritycommit.ReplayRequest) { r.AuditVerification.ExecutionGeneration++ }, "AUDIT_BINDING_MISMATCH"},
		{"subject", func(r *authoritycommit.ReplayRequest) { r.AuditVerification.Subject.SubjectID = "" }, "AUDIT_BINDING_MISMATCH"},
		{"scopes", func(r *authoritycommit.ReplayRequest) { r.AuditVerification.Scopes = nil }, "AUDIT_BINDING_MISMATCH"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			changed := request
			tt.edit(&changed)
			if result := authoritycommit.ValidateReplay(changed); result.Outcome != authority.OutcomeDenied || result.Code != tt.code || result.Commit != nil || result.ExposurePermitted {
				t.Fatalf("ValidateReplay()=%+v want DENIED/%s", result, tt.code)
			}
		})
	}
}

func TestExposureGateAttemptBlockedIsTerminalAndNeverPermits(t *testing.T) {
	attempt := authoritycommit.ExposureGateAttempt{
		AttemptID: "attempt-r0", RequestDigest: strings.Repeat("a", 64),
		Outcome: "BLOCKED", Code: "PROVIDER_CAPABILITY_UNVERIFIED",
		GateID: "gate-r0", AdmissionCommitID: "ajc_" + strings.Repeat("b", 64),
		BlockedReason: "missing independently verified provider fence", ExposurePermitted: false,
	}
	if err := authoritycommit.ValidateBlockedAttempt(attempt); err != nil {
		t.Fatalf("blocked attempt must be a valid terminal record: %v", err)
	}
	attempt.ExposurePermitted = true
	if err := authoritycommit.ValidateBlockedAttempt(attempt); err == nil {
		t.Fatal("BLOCKED attempt must never permit exposure")
	}
	attempt.ExposurePermitted = false
	attempt.AuthoritySequence = 1
	if err := authoritycommit.ValidateBlockedAttempt(attempt); err == nil {
		t.Fatal("BLOCKED attempt must never consume authority sequence")
	}
}

func TestExposureGateRequestRejectsRemoteOrForgedCapability(t *testing.T) {
	req := admissionRequest(t)
	gate := authoritycommit.ExposureGateRequest{
		ContractVersion: authoritycommit.ContractVersion,
		Operation:       authoritycommit.JournalExposureGateV2,
		ExactTargetID:   "gate-r0",
		CommitToken:     "gate-token-r0",
		AttemptID:       "attempt-r0",
		Verification: authority.VerificationRequest{
			TrustDomain: "trust-r0", ProofID: "aprf_" + strings.Repeat("e", 64),
			Subject: req.Admission.Principal, Resource: "gate-r0", Action: "READ", Tenant: "tenant-r0",
			Operation: authoritycommit.JournalExposureGateV2, CaseID: "case-r0", Round: 1,
			ExecutionGeneration: 7, Scopes: req.Admission.ScopedEpochs,
		},
		Witness: authoritycommit.WitnessEvidence{
			WitnessID: "witness-r0", EvidenceID: "witness-evidence-r0",
			EvidenceDigest: strings.Repeat("f", 64), ObjectRef: "object:witness-evidence-r0",
			AdmissionCommitID: "ajc_" + strings.Repeat("b", 64),
			RequestDigest:     strings.Repeat("e", 64),
			ValidUntil:        "2026-10-26T01:00:30.000Z",
		},
		Gate: authoritycommit.ExposureGatePlan{
			GateID: "gate-r0", AdmissionCommitID: "ajc_" + strings.Repeat("b", 64),
			AdmissionRequestDigest: strings.Repeat("e", 64),
			FenceID:                "lease-r0", LinearizationSequence: 1, TrustedNow: "2026-10-26T01:00:31.000Z",
			ProviderCapability: authoritycommit.ProviderCapability{
				ProviderID: "provider-r0", CapabilityID: "cap-r0",
				CapabilityDigest: strings.Repeat("c", 64),
				VerificationRef:  authoritycommit.PreparedObject{ID: "cap-r0", Digest: strings.Repeat("d", 64), ObjectRef: "object:cap-r0"},
				EnforcesFence:    true, SupportsLease: true, EnforcesRevocationWatermark: true,
				MaxLeaseUntil: "2026-10-26T01:01:00.000Z",
			},
		},
	}
	if got := authoritycommit.ValidateExposureGate(gate); got.Outcome != authority.OutcomeValid || got.Code != "EXPOSURE_GATE_REQUEST_VALID" {
		t.Fatalf("valid gate request=%+v", got)
	}
	gate.Gate.LinearizationSequence = 0
	if got := authoritycommit.ValidateExposureGate(gate); got.Outcome != authority.OutcomeValid {
		t.Fatalf("validation must not allocate a sequence: %+v", got)
	}
	if got := authoritycommit.ValidateExposureGate(gate); got.Outcome != authority.OutcomeValid {
		t.Fatalf("validation must not forge provider attestation: %+v", got)
	}
	gate.Gate.ProviderCapability.EnforcesRevocationWatermark = false
	if got := authoritycommit.ValidateProviderCapability(gate.Gate.ProviderCapability); got.Outcome != authority.OutcomeDenied || got.Code != "PROVIDER_CAPABILITY_INSUFFICIENT" {
		t.Fatalf("insufficient capability=%+v", got)
	}
}

func TestExposureGateReplayRequiresExactAuditBinding(t *testing.T) {
	req := admissionRequest(t)
	gate := authoritycommit.ExposureGateRequest{
		ContractVersion: authoritycommit.ContractVersion,
		Operation:       authoritycommit.JournalExposureGateV2,
		ExactTargetID:   "gate-r0",
		CommitToken:     "gate-token-r0",
		AttemptID:       "attempt-r0",
		Verification: authority.VerificationRequest{
			TrustDomain: "trust-r0", ProofID: "aprf_" + strings.Repeat("e", 64),
			Subject: req.Admission.Principal, Resource: "gate-r0", Action: "READ", Tenant: "tenant-r0",
			Operation: authoritycommit.JournalExposureGateV2, CaseID: "case-r0", Round: 1,
			ExecutionGeneration: 7, Scopes: req.Admission.ScopedEpochs,
		},
		Witness: authoritycommit.WitnessEvidence{
			WitnessID: "witness-r0", EvidenceID: "witness-evidence-r0",
			EvidenceDigest: strings.Repeat("f", 64), ObjectRef: "object:witness-evidence-r0",
			AdmissionCommitID: "ajc_" + strings.Repeat("b", 64),
			RequestDigest:     strings.Repeat("a", 64),
			ValidUntil:        "2026-10-26T01:00:30.000Z",
		},
		Gate: authoritycommit.ExposureGatePlan{
			GateID: "gate-r0", AdmissionCommitID: "ajc_" + strings.Repeat("b", 64),
			AdmissionRequestDigest: strings.Repeat("a", 64),
			FenceID:                "lease-r0", LinearizationSequence: 1,
			TrustedNow: "2026-10-26T01:00:31.000Z",
			ProviderCapability: authoritycommit.ProviderCapability{
				ProviderID: "provider-r0", CapabilityID: "cap-r0",
				CapabilityDigest: strings.Repeat("c", 64),
				VerificationRef:  authoritycommit.PreparedObject{ID: "cap-r0", Digest: strings.Repeat("d", 64), ObjectRef: "object:cap-r0"},
				EnforcesFence:    true, SupportsLease: true, EnforcesRevocationWatermark: true,
				MaxLeaseUntil: "2026-10-26T01:01:00.000Z",
			},
		},
	}
	audit := gate.Verification
	audit.ProofID = "aprf_" + strings.Repeat("9", 64)
	audit.Operation = authoritycommit.JournalAuditRead
	replay := authoritycommit.ExposureGateReplayRequest{Original: gate, AuditVerification: audit}
	if got := authoritycommit.ValidateExposureGateReplay(replay); got.Outcome != authority.OutcomeValid || got.Code != "EXPOSURE_GATE_REPLAY_REQUEST_VALID" {
		t.Fatalf("valid replay=%+v", got)
	}
	replay.AuditVerification = gate.Verification
	if got := authoritycommit.ValidateExposureGateReplay(replay); got.Outcome != authority.OutcomeDenied || got.Code != "AUDIT_AUTHORIZATION_REQUIRED" {
		t.Fatalf("operational proof replay=%+v", got)
	}
}
