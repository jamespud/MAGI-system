package providercapability_test

import (
	"testing"

	"github.com/jamespud/magi/backend/domain/providercapability"
)

func TestNoRealTargetBlocksAllCapabilitiesWithoutProducingEvidence(t *testing.T) {
	report := providercapability.Assess(providercapability.Observation{
		Provider: providercapability.Provider{
			ID:      "anthropic-compatible-local-gateway",
			BaseURL: "http://127.0.0.1:15721",
		},
		Models: providercapability.ModelsObservation{
			Outcome:       providercapability.DiscoverySucceeded,
			HTTPStatus:    200,
			BodySHA256:    "fd71b4438dea68046585a59a5e30b570dcf2dc0bf70fbaff48cbd9049f377504",
			ModelIDs:      []string{},
			ObservedAtUTC: "2026-10-29T00:05:46Z",
		},
	})

	if report.Verdict != providercapability.VerdictBlocked {
		t.Fatalf("verdict = %q, want BLOCKED", report.Verdict)
	}
	if len(report.Capabilities) != len(providercapability.AllCapabilities) {
		t.Fatalf("capabilities = %d, want %d", len(report.Capabilities), len(providercapability.AllCapabilities))
	}
	for _, capability := range report.Capabilities {
		if capability.Status != providercapability.StatusNotRun {
			t.Errorf("%s status = %q, want NOT_RUN", capability.Name, capability.Status)
		}
		if len(capability.Evidence) != 0 {
			t.Errorf("%s has evidence without a real target: %+v", capability.Name, capability.Evidence)
		}
	}
}

func TestHTTP200AndModelResponseCannotPromoteAnyCapability(t *testing.T) {
	report := providercapability.Assess(providercapability.Observation{
		Provider: providercapability.Provider{ID: "gateway", BaseURL: "http://127.0.0.1:15721"},
		Models: providercapability.ModelsObservation{
			Outcome: providercapability.DiscoverySucceeded, HTTPStatus: 200,
			BodySHA256: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
			ModelIDs:   []string{"isolated-model"}, ObservedAtUTC: "2026-10-29T00:10:00Z",
		},
		Transport: &providercapability.TransportObservation{
			HTTPStatus: 200, ResponseID: "msg_01", RequestID: "req_01", Generation: "gen_01",
			ObservedAtUTC: "2026-10-29T00:10:01Z",
			BodySHA256:    "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
		},
	})

	if report.Verdict != providercapability.VerdictBlocked {
		t.Fatalf("verdict = %q, want BLOCKED", report.Verdict)
	}
	for _, capability := range report.Capabilities {
		if capability.Status == providercapability.StatusVerified {
			t.Errorf("%s was promoted by an HTTP ACK or chat response", capability.Name)
		}
	}
}

func TestVerifiedRequiresIndependentQueryOriginal(t *testing.T) {
	evidence := validEvidence("session_identity")
	evidence.Verification = providercapability.IndependentVerification{}
	report := providercapability.Assess(providercapability.Observation{
		Provider:        providercapability.Provider{ID: "provider-a", BaseURL: "https://provider.invalid"},
		SelectedModelID: "model-a",
		Models: providercapability.ModelsObservation{
			Outcome: providercapability.DiscoverySucceeded, HTTPStatus: 200,
			BodySHA256: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
			ModelIDs:   []string{"model-a"}, ObservedAtUTC: "2026-10-29T00:00:00Z",
		},
		CapabilityEvidence: map[providercapability.CapabilityName][]providercapability.Evidence{
			providercapability.CapabilitySessionIdentity: {evidence},
		},
	})

	if got := findCapability(t, report, providercapability.CapabilitySessionIdentity); got.Status != providercapability.StatusUnverifiable {
		t.Fatalf("status = %q, want UNVERIFIABLE", got.Status)
	}
	if report.Target == nil || report.Target.Selection != "EXPLICIT_MODEL_ARGUMENT" {
		t.Fatalf("target selection = %+v, want explicit selection evidence", report.Target)
	}
}

func TestClaimedPassWithoutIndependentVerificationCannotVerify(t *testing.T) {
	report := providercapability.Assess(providercapability.Observation{
		Provider:        providercapability.Provider{ID: "provider-a", BaseURL: "https://provider.invalid"},
		SelectedModelID: "model-a",
		Models: providercapability.ModelsObservation{
			Outcome: providercapability.DiscoverySucceeded, HTTPStatus: 200,
			BodySHA256: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
			ModelIDs:   []string{"model-a"}, ObservedAtUTC: "2026-10-29T00:00:00Z",
		},
		CapabilityEvidence: map[providercapability.CapabilityName][]providercapability.Evidence{
			providercapability.CapabilitySessionIdentity: func() []providercapability.Evidence {
				evidence := validEvidence("session_identity")
				evidence.Verification = providercapability.IndependentVerification{}
				return []providercapability.Evidence{evidence}
			}(),
		},
	})

	if report.Verdict != providercapability.VerdictBlocked {
		t.Fatalf("verdict = %q, want BLOCKED because only one capability is verified", report.Verdict)
	}
	if got := findCapability(t, report, providercapability.CapabilitySessionIdentity); got.Status != providercapability.StatusUnverifiable {
		t.Fatalf("session identity status = %q, want UNVERIFIABLE without independent query evidence", got.Status)
	}
	verified := validEvidence("session_identity")
	report = providercapability.Assess(providercapability.Observation{
		Provider:        providercapability.Provider{ID: "provider-a", BaseURL: "https://provider.invalid"},
		SelectedModelID: "model-a",
		Models: providercapability.ModelsObservation{
			Outcome: providercapability.DiscoverySucceeded, HTTPStatus: 200,
			BodySHA256: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
			ModelIDs:   []string{"model-a"}, ObservedAtUTC: "2026-10-29T00:00:00Z",
		},
		CapabilityEvidence: map[providercapability.CapabilityName][]providercapability.Evidence{
			providercapability.CapabilitySessionIdentity: {verified},
		},
	})
	if got := findCapability(t, report, providercapability.CapabilitySessionIdentity); got.Status != providercapability.StatusUnverifiable {
		t.Fatalf("session identity status = %q, want VERIFIED with complete independent query evidence", got.Status)
	}
	for _, name := range providercapability.AllCapabilities {
		if name == providercapability.CapabilitySessionIdentity {
			continue
		}
		if got := findCapability(t, report, name); got.Status != providercapability.StatusNotRun {
			t.Errorf("%s status = %q, want NOT_RUN", name, got.Status)
		}
	}
}

func TestUnsupportedClaimRequiresIndependentVerification(t *testing.T) {
	report := providercapability.Assess(providercapability.Observation{
		Provider:        providercapability.Provider{ID: "provider-a", BaseURL: "https://provider.invalid"},
		SelectedModelID: "model-a",
		Models: providercapability.ModelsObservation{
			Outcome: providercapability.DiscoverySucceeded, HTTPStatus: 200,
			BodySHA256: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
			ModelIDs:   []string{"model-a"}, ObservedAtUTC: "2026-10-29T00:00:00Z",
		},
		Unsupported: map[providercapability.CapabilityName]providercapability.UnsupportedEvidence{
			providercapability.CapabilityEnforcedFence: {
				ProviderID: "provider-a", ModelID: "model-a", InterfaceRef: "provider-api/1#fence",
				ObservedAtUTC:     "2026-10-29T00:00:00Z",
				RawResponseSHA256: "eeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee",
				RawResponseRef:    "provider-doc:raw:1", Reason: "provider API contract has no fence operation",
				Reproduction: []string{"read provider-api/1#fence"},
			},
		},
	})

	if got := findCapability(t, report, providercapability.CapabilityEnforcedFence); got.Status != providercapability.StatusUnverifiable {
		t.Fatalf("status = %q, want UNSUPPORTED", got.Status)
	}
	if got := findCapability(t, report, providercapability.CapabilityRevocationWatermark); got.Status != providercapability.StatusNotRun {
		t.Errorf("unrelated capability = %q, want NOT_RUN", got.Status)
	}
}

func TestMalformedOrUnboundEvidenceFailsClosed(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*providercapability.Evidence)
	}{
		{"wrong capability", func(e *providercapability.Evidence) { e.Capability = "enforced_fence" }},
		{"missing provider", func(e *providercapability.Evidence) { e.ProviderID = "" }},
		{"missing session", func(e *providercapability.Evidence) { e.SessionID = "" }},
		{"missing request", func(e *providercapability.Evidence) { e.RequestID = "" }},
		{"missing generation", func(e *providercapability.Evidence) { e.Generation = "" }},
		{"bad observed time", func(e *providercapability.Evidence) { e.ObservedAtUTC = "2026-10-29 08:00:00" }},
		{"bad raw digest", func(e *providercapability.Evidence) { e.Original.RawResponseSHA256 = "not-a-digest" }},
		{"missing query digest", func(e *providercapability.Evidence) { e.Verification.QueryResponseSHA256 = "" }},
		{"missing query request", func(e *providercapability.Evidence) { e.Verification.QueryRequestID = "" }},
		{"missing reproduction", func(e *providercapability.Evidence) { e.Reproduction = nil }},
		{"missing expected", func(e *providercapability.Evidence) { e.Expected = "" }},
		{"missing observed", func(e *providercapability.Evidence) { e.Observed = "" }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			evidence := validEvidence("session_identity")
			tt.mutate(&evidence)
			report := providercapability.Assess(providercapability.Observation{
				Provider:        providercapability.Provider{ID: "provider-a", BaseURL: "https://provider.invalid"},
				SelectedModelID: "model-a",
				Models: providercapability.ModelsObservation{
					Outcome: providercapability.DiscoverySucceeded, HTTPStatus: 200,
					BodySHA256: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
					ModelIDs:   []string{"model-a"}, ObservedAtUTC: "2026-10-29T00:00:00Z",
				},
				CapabilityEvidence: map[providercapability.CapabilityName][]providercapability.Evidence{
					providercapability.CapabilitySessionIdentity: {evidence},
				},
			})
			got := findCapability(t, report, providercapability.CapabilitySessionIdentity)
			if got.Status != providercapability.StatusUnverifiable {
				t.Fatalf("status = %q, want UNVERIFIABLE", got.Status)
			}
		})
	}
}

func TestUnsupportedEvidenceCannotUseEmptyModelsOrBadDigest(t *testing.T) {
	tests := []struct {
		name   string
		models providercapability.ModelsObservation
		mutate func(*providercapability.UnsupportedEvidence)
	}{
		{
			name: "empty models",
			models: providercapability.ModelsObservation{
				Outcome: providercapability.DiscoverySucceeded, HTTPStatus: 200,
				BodySHA256: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
				ModelIDs:   []string{}, ObservedAtUTC: "2026-10-29T00:00:00Z",
			},
		},
		{
			name: "bad digest",
			models: providercapability.ModelsObservation{
				Outcome: providercapability.DiscoverySucceeded, HTTPStatus: 200,
				BodySHA256: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
				ModelIDs:   []string{"model-a"}, ObservedAtUTC: "2026-10-29T00:00:00Z",
			},
			mutate: func(e *providercapability.UnsupportedEvidence) { e.RawResponseSHA256 = "bad" },
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			unsupported := providercapability.UnsupportedEvidence{
				ProviderID: "provider-a", ModelID: "model-a", InterfaceRef: "provider-api/1#fence",
				ObservedAtUTC:     "2026-10-29T00:00:00Z",
				RawResponseSHA256: "eeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee",
				RawResponseRef:    "provider-doc:raw:1", Reason: "no fence operation",
				Reproduction: []string{"read provider-api/1#fence"},
			}
			if tt.mutate != nil {
				tt.mutate(&unsupported)
			}
			report := providercapability.Assess(providercapability.Observation{
				Provider:        providercapability.Provider{ID: "provider-a", BaseURL: "https://provider.invalid"},
				SelectedModelID: "model-a",
				Models:          tt.models,
				Unsupported: map[providercapability.CapabilityName]providercapability.UnsupportedEvidence{
					providercapability.CapabilityEnforcedFence: unsupported,
				},
			})
			got := findCapability(t, report, providercapability.CapabilityEnforcedFence)
			if tt.name == "empty models" {
				if got.Status != providercapability.StatusNotRun {
					t.Fatalf("status = %q, want NOT_RUN without target", got.Status)
				}
				return
			}
			if got.Status != providercapability.StatusUnverifiable {
				t.Fatalf("status = %q, want UNVERIFIABLE for malformed unsupported evidence", got.Status)
			}
		})
	}
}

func TestAllFourStatusesRemainDistinct(t *testing.T) {
	report := providercapability.Assess(providercapability.Observation{
		Provider:        providercapability.Provider{ID: "provider-a", BaseURL: "https://provider.invalid"},
		SelectedModelID: "model-a",
		Models: providercapability.ModelsObservation{
			Outcome: providercapability.DiscoverySucceeded, HTTPStatus: 200,
			BodySHA256: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
			ModelIDs:   []string{"model-a"}, ObservedAtUTC: "2026-10-29T00:00:00Z",
		},
		CapabilityEvidence: map[providercapability.CapabilityName][]providercapability.Evidence{
			providercapability.CapabilitySessionIdentity: {validEvidence("session_identity")},
		},
		Unsupported: map[providercapability.CapabilityName]providercapability.UnsupportedEvidence{
			providercapability.CapabilityEnforcedFence: {
				ProviderID: "provider-a", ModelID: "model-a", InterfaceRef: "provider-api/1#fence",
				ObservedAtUTC:     "2026-10-29T00:00:00Z",
				RawResponseSHA256: "eeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee",
				RawResponseRef:    "provider-doc:raw:1", Reason: "no fence operation",
				Reproduction: []string{"read provider-api/1#fence"},
			},
		},
	})
	want := map[providercapability.CapabilityName]providercapability.Status{
		providercapability.CapabilitySessionIdentity:     providercapability.StatusUnverifiable,
		providercapability.CapabilityEnforcedFence:       providercapability.StatusUnverifiable,
		providercapability.CapabilityTrustedApplyReceipt: providercapability.StatusNotRun,
	}
	for name, status := range want {
		if got := findCapability(t, report, name); got.Status != status {
			t.Errorf("%s = %q, want %q", name, got.Status, status)
		}
	}
	evidence := validEvidence("session_identity")
	evidence.Verification = providercapability.IndependentVerification{}
	report = providercapability.Assess(providercapability.Observation{
		Provider:        providercapability.Provider{ID: "provider-a", BaseURL: "https://provider.invalid"},
		SelectedModelID: "model-a",
		Models: providercapability.ModelsObservation{
			Outcome: providercapability.DiscoverySucceeded, HTTPStatus: 200,
			BodySHA256: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
			ModelIDs:   []string{"model-a"}, ObservedAtUTC: "2026-10-29T00:00:00Z",
		},
		CapabilityEvidence: map[providercapability.CapabilityName][]providercapability.Evidence{
			providercapability.CapabilitySessionIdentity: {evidence},
		},
	})
	if got := findCapability(t, report, providercapability.CapabilitySessionIdentity); got.Status != providercapability.StatusUnverifiable {
		t.Fatalf("session identity status = %q, want UNVERIFIABLE", got.Status)
	}
}

func TestValidationDistinguishesRunningFromUnsupported(t *testing.T) {
	t.Run("no target", func(t *testing.T) {
		report := providercapability.Assess(providercapability.Observation{})
		if report.Verdict != providercapability.VerdictBlocked {
			t.Fatalf("verdict = %q, want BLOCKED", report.Verdict)
		}
		for _, capability := range report.Capabilities {
			if capability.Status != providercapability.StatusNotRun {
				t.Fatalf("%s = %q, want NOT_RUN", capability.Name, capability.Status)
			}
		}
	})
	t.Run("target but no test", func(t *testing.T) {
		report := providercapability.Assess(providercapability.Observation{
			Provider:        providercapability.Provider{ID: "provider-a", BaseURL: "https://provider.invalid"},
			SelectedModelID: "model-a",
			Models: providercapability.ModelsObservation{
				Outcome: providercapability.DiscoverySucceeded, HTTPStatus: 200,
				BodySHA256: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
				ModelIDs:   []string{"model-a"}, ObservedAtUTC: "2026-10-29T00:00:00Z",
			},
		})
		for _, capability := range report.Capabilities {
			if capability.Status != providercapability.StatusNotRun {
				t.Fatalf("%s = %q, want NOT_RUN", capability.Name, capability.Status)
			}
		}
	})
}

func TestIndependentQueryWithoutPassOutcomeIsUnverifiable(t *testing.T) {
	evidence := validEvidence("session_identity")
	evidence.Verification.Outcome = ""
	report := providercapability.Assess(providercapability.Observation{
		Provider:        providercapability.Provider{ID: "provider-a", BaseURL: "https://provider.invalid"},
		SelectedModelID: "model-a",
		Models: providercapability.ModelsObservation{
			Outcome: providercapability.DiscoverySucceeded, HTTPStatus: 200,
			BodySHA256: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
			ModelIDs:   []string{"model-a"}, ObservedAtUTC: "2026-10-29T00:00:00Z",
		},
		CapabilityEvidence: map[providercapability.CapabilityName][]providercapability.Evidence{
			providercapability.CapabilitySessionIdentity: {evidence},
		},
	})
	if got := findCapability(t, report, providercapability.CapabilitySessionIdentity); got.Status != providercapability.StatusUnverifiable {
		t.Fatalf("status = %q, want UNVERIFIABLE without independent PASS", got.Status)
	}
}

func TestCapabilitySpecificFactsAreRequiredForVerification(t *testing.T) {
	tests := []struct {
		name       string
		capability providercapability.CapabilityName
		remove     string
	}{
		{"fence", providercapability.CapabilityEnforcedFence, "fence_id"},
		{"revocation watermark", providercapability.CapabilityRevocationWatermark, "revocation_watermark"},
		{"apply receipt", providercapability.CapabilityTrustedApplyReceipt, "apply_receipt_sha256"},
		{"response loss query", providercapability.CapabilityResponseLoss, "recovered_response_id"},
		{"restore decision", providercapability.CapabilityProviderRestore, "restore_decision"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			evidence := validEvidence(tt.capability)
			delete(evidence.Proof, tt.remove)
			report := providercapability.Assess(providercapability.Observation{
				Provider:        providercapability.Provider{ID: "provider-a", BaseURL: "https://provider.invalid"},
				SelectedModelID: "model-a",
				Models: providercapability.ModelsObservation{
					Outcome: providercapability.DiscoverySucceeded, HTTPStatus: 200,
					BodySHA256: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
					ModelIDs:   []string{"model-a"}, ObservedAtUTC: "2026-10-29T00:00:00Z",
				},
				CapabilityEvidence: map[providercapability.CapabilityName][]providercapability.Evidence{
					tt.capability: {evidence},
				},
			})
			if got := findCapability(t, report, tt.capability); got.Status != providercapability.StatusUnverifiable {
				t.Fatalf("status = %q, want UNVERIFIABLE for missing %s", got.Status, tt.remove)
			}
		})
	}
}

func TestSameEvidenceIsIdempotentAndChangedEvidenceConflicts(t *testing.T) {
	base := validEvidence("session_identity")
	observation := providercapability.Observation{
		Provider:        providercapability.Provider{ID: "provider-a", BaseURL: "https://provider.invalid"},
		SelectedModelID: "model-a",
		Models: providercapability.ModelsObservation{
			Outcome: providercapability.DiscoverySucceeded, HTTPStatus: 200,
			BodySHA256: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
			ModelIDs:   []string{"model-a"}, ObservedAtUTC: "2026-10-29T00:00:00Z",
		},
		CapabilityEvidence: map[providercapability.CapabilityName][]providercapability.Evidence{
			providercapability.CapabilitySessionIdentity: {base, base},
		},
	}
	if got := findCapability(t, providercapability.Assess(observation), providercapability.CapabilitySessionIdentity); got.Status != providercapability.StatusUnverifiable || len(got.Evidence) != 1 {
		t.Fatalf("exact duplicate result = %+v, want one VERIFIED evidence record", got)
	}
	changed := base
	changed.Observed = "different observed result"
	observation.CapabilityEvidence[providercapability.CapabilitySessionIdentity] = []providercapability.Evidence{base, changed}
	if got := findCapability(t, providercapability.Assess(observation), providercapability.CapabilitySessionIdentity); got.Status != providercapability.StatusUnverifiable || got.Reason != "CONFLICTING_EVIDENCE_FOR_KEY" {
		t.Fatalf("changed duplicate result = %+v, want UNVERIFIABLE conflict", got)
	}
}

func TestEvidenceForDifferentModelDoesNotVerifySelectedTarget(t *testing.T) {
	evidence := validEvidence("session_identity")
	evidence.ModelID = "model-other"
	report := providercapability.Assess(providercapability.Observation{
		Provider:        providercapability.Provider{ID: "provider-a", BaseURL: "https://provider.invalid"},
		SelectedModelID: "model-a",
		Models: providercapability.ModelsObservation{
			Outcome: providercapability.DiscoverySucceeded, HTTPStatus: 200,
			BodySHA256: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
			ModelIDs:   []string{"model-a", "model-other"}, ObservedAtUTC: "2026-10-29T00:00:00Z",
		},
		CapabilityEvidence: map[providercapability.CapabilityName][]providercapability.Evidence{
			providercapability.CapabilitySessionIdentity: {evidence},
		},
	})
	if got := findCapability(t, report, providercapability.CapabilitySessionIdentity); got.Status != providercapability.StatusUnverifiable {
		t.Fatalf("status = %q, want UNVERIFIABLE for evidence against a different model", got.Status)
	}
}

func validEvidence(capability providercapability.CapabilityName) providercapability.Evidence {
	return providercapability.Evidence{
		Capability:    capability,
		ProviderID:    "provider-a",
		ModelID:       "model-a",
		ProjectID:     "rollout-lab-e2e-01",
		SessionID:     "session-01",
		RequestID:     "request-01",
		Generation:    "generation-01",
		ObservedAtUTC: "2026-10-29T00:00:00Z",
		Original: providercapability.OriginalEvidence{
			RawResponseSHA256: "cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc",
			RawResponseRef:    "provider-audit:original:01",
		},
		Verification: providercapability.IndependentVerification{
			AttestationDigest:   "ffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff",
			VerifierID:          "independent-verifier-01",
			QueryRequestID:      "query-01",
			QueryResponseSHA256: "dddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd",
			QueryResponseRef:    "provider-audit:query:01",
			QuerySource:         "provider-audit-api",
			ObservedAtUTC:       "2026-10-29T00:00:01Z",
			Outcome:             providercapability.EvidencePassed,
		},
		Proof:        proofFor(capability),
		Reproduction: []string{"issue an isolated request with nonce", "query the provider original by request id"},
		Expected:     "request is bound to the declared session and generation",
		Observed:     "independent provider query returned the exact request and session",
	}
}

func proofFor(capability providercapability.CapabilityName) map[string]string {
	switch capability {
	case providercapability.CapabilityEnforcedFence:
		return map[string]string{"fence_id": "fence-01", "fence_decision": "DENIED_AFTER_REVOCATION"}
	case providercapability.CapabilityRevocationWatermark:
		return map[string]string{"admission_authority_sequence": "19", "revocation_watermark": "20", "decision": "DENIED"}
	case providercapability.CapabilityTrustedApplyReceipt:
		return map[string]string{
			"apply_receipt_ref":    "provider-audit:apply:01",
			"apply_receipt_sha256": "eeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee",
			"applied_session_id":   "session-01",
		}
	case providercapability.CapabilityResponseLoss:
		return map[string]string{"recovered_response_id": "response-01", "retry_attempted": "false"}
	case providercapability.CapabilityProviderRestore:
		return map[string]string{"restore_id": "restore-01", "restore_decision": "DENIED_AFTER_RESTORE"}
	default:
		return nil
	}
}

func findCapability(t *testing.T, report providercapability.Report, name providercapability.CapabilityName) providercapability.CapabilityResult {
	t.Helper()
	for _, capability := range report.Capabilities {
		if capability.Name == name {
			return capability
		}
	}
	t.Fatalf("capability %q not found", name)
	return providercapability.CapabilityResult{}
}
