package authority_test

import (
	"strings"
	"testing"

	"github.com/jamespud/magi/backend/domain/authority"
	"github.com/jamespud/magi/backend/domain/canonical"
)

func TestScopedEpochsAreIndependentAndMonotonic(t *testing.T) {
	t.Run("stale authority epoch", func(t *testing.T) {
		state, req, raw, id := proofFixture(t)
		proof := mustParseProof(t, raw, id)
		state.Epochs[0].Epoch = 4
		result := authority.VerifySignedProof(proof, req, state)
		if result.Outcome != authority.OutcomeStale || result.Code != "SCOPED_EPOCH_STALE" {
			t.Fatalf("outcome=%s code=%s detail=%s", result.Outcome, result.Code, result.Detail)
		}
	})

	t.Run("revoked context epoch", func(t *testing.T) {
		state, req, raw, id := proofFixture(t)
		proof := mustParseProof(t, raw, id)
		state.Epochs[1].Revoked = true
		state.Epochs[1].Epoch = 5
		result := authority.VerifySignedProof(proof, req, state)
		if result.Outcome != authority.OutcomeRevoked || result.Code != "SCOPE_REVOKED" {
			t.Fatalf("outcome=%s code=%s detail=%s", result.Outcome, result.Code, result.Detail)
		}
	})

	t.Run("unknown credential epoch", func(t *testing.T) {
		state, req, raw, id := proofFixture(t)
		proof := mustParseProof(t, raw, id)
		state.Epochs = state.Epochs[:2]
		result := authority.VerifySignedProof(proof, req, state)
		if result.Outcome != authority.OutcomeUnknown || result.Code != "SCOPED_EPOCH_UNKNOWN" {
			t.Fatalf("outcome=%s code=%s detail=%s", result.Outcome, result.Code, result.Detail)
		}
	})

	t.Run("execution generation is not an epoch", func(t *testing.T) {
		state, req, raw, id := proofFixture(t)
		proof := mustParseProof(t, raw, id)
		proof.Proof.Scopes[1].Epoch = proof.Proof.ExecutionGeneration
		proof.Proof.Consent.Body.Scopes[1].Epoch = proof.Proof.ExecutionGeneration
		owner := state.Credentials[1]
		proof.Proof.Consent = signConsent(t, proof.Proof.Consent.Body, struct {
			owner  authority.Subject
			signer authority.CredentialRef
			seed   byte
		}{owner.Subject, owner.Ref, 2})
		reSignFixture(t, &proof, &req, 1)
		req.Scopes[1].Epoch = proof.Proof.ExecutionGeneration
		result := authority.VerifySignedProof(proof, req, state)
		if result.Outcome != authority.OutcomeStale || result.Code != "SCOPED_EPOCH_STALE" {
			t.Fatalf("outcome=%s code=%s detail=%s", result.Outcome, result.Code, result.Detail)
		}
	})
}

func TestProofObservableBindingFailures(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(*authority.VerificationRequest)
	}{
		{"subject", func(r *authority.VerificationRequest) {
			r.Subject.SubjectID = "someone-else"
		}},
		{"resource", func(r *authority.VerificationRequest) { r.Resource = "case/99" }},
		{"action", func(r *authority.VerificationRequest) { r.Action = "DELETE" }},
		{"tenant", func(r *authority.VerificationRequest) { r.Tenant = "tenant-b" }},
		{"operation", func(r *authority.VerificationRequest) { r.Operation = "SEND_COMMIT" }},
		{"case", func(r *authority.VerificationRequest) { r.CaseID = "case-99" }},
		{"round", func(r *authority.VerificationRequest) { r.Round = 3 }},
		{"execution-generation", func(r *authority.VerificationRequest) { r.ExecutionGeneration = 8 }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			state, req, raw, id := proofFixture(t)
			proof := mustParseProof(t, raw, id)
			tc.mutate(&req)
			result := authority.VerifySignedProof(proof, req, state)
			if result.Outcome != authority.OutcomeStale || result.Code != "PROOF_REQUEST_BINDING_MISMATCH" {
				t.Fatalf("outcome=%s code=%s detail=%s", result.Outcome, result.Code, result.Detail)
			}
		})
	}
}

func TestProofRejectsExpiredAndOldVersions(t *testing.T) {
	t.Run("expired proof", func(t *testing.T) {
		state, req, raw, id := proofFixture(t)
		proof := mustParseProof(t, raw, id)
		state.TrustedNow = "2026-10-24T12:01:00.000Z"
		result := authority.VerifySignedProof(proof, req, state)
		if result.Outcome != authority.OutcomeExpired || result.Code != "PROOF_EXPIRED" {
			t.Fatalf("outcome=%s code=%s detail=%s", result.Outcome, result.Code, result.Detail)
		}
	})

	t.Run("old proof version", func(t *testing.T) {
		state, req, raw, id := proofFixture(t)
		proof := mustParseProof(t, raw, id)
		proof.Proof.Version = "magi-authority-proof/0"
		raw, id = signProof(t, proof.Proof, 1)
		_, result := authority.ParseSignedProof(raw, id)
		if result.Outcome != authority.OutcomeDenied || result.Code != "PROOF_VERSION_UNSUPPORTED" {
			t.Fatalf("outcome=%s code=%s detail=%s", result.Outcome, result.Code, result.Detail)
		}
		_ = state
		_ = req
	})

	t.Run("non-canonical readback", func(t *testing.T) {
		_, _, raw, id := proofFixture(t)
		_, result := authority.ParseSignedProof([]byte(" "+string(raw)), id)
		if result.Outcome != authority.OutcomeDenied || result.Code != "PROOF_BYTES_NON_CANONICAL" {
			t.Fatalf("outcome=%s code=%s detail=%s", result.Outcome, result.Code, result.Detail)
		}
	})

	t.Run("old credential version", func(t *testing.T) {
		state, req, raw, id := proofFixture(t)
		proof := mustParseProof(t, raw, id)
		proof.Proof.Signer.CredentialVersion = 2
		raw, id = signProof(t, proof.Proof, 1)
		proof = mustParseProof(t, raw, id)
		req.ProofID = id
		result := authority.VerifySignedProof(proof, req, state)
		if result.Outcome != authority.OutcomeUnknown || result.Code != "CREDENTIAL_UNKNOWN" {
			t.Fatalf("outcome=%s code=%s detail=%s", result.Outcome, result.Code, result.Detail)
		}
	})

	t.Run("unknown field rejected", func(t *testing.T) {
		_, _, raw, id := proofFixture(t)
		tampered := strings.Replace(string(raw), `"signature":`, `"unexpected":true,"signature":`, 1)
		canonicalTampered, err := canonical.Canonicalize([]byte(tampered))
		if err != nil {
			t.Fatal(err)
		}
		_, result := authority.ParseSignedProof(canonicalTampered.Bytes(), id)
		if result.Outcome != authority.OutcomeDenied || result.Code != "PROOF_SHAPE_INVALID" {
			t.Fatalf("outcome=%s code=%s detail=%s", result.Outcome, result.Code, result.Detail)
		}
	})
}
