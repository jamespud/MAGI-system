package authority_test

import (
	"encoding/base64"
	"testing"

	"github.com/jamespud/magi/backend/domain/authority"
)

func ownerCredentialB() authority.Credential {
	return credentialFor(7, "cred-owner-b", "owner-b", "identity-source", "identity-v1", "credential-scope")
}

func reSignFixture(t *testing.T, proof *authority.SignedProof, req *authority.VerificationRequest, seed byte) {
	t.Helper()
	raw, id := signProof(t, proof.Proof, seed)
	proof.ProofID = id
	req.ProofID = id
	parsed := mustParseProof(t, raw, id)
	*proof = parsed
}

func TestOwnerConsentThresholdSemantics(t *testing.T) {
	t.Run("THRESHOLD satisfied by distinct owners", func(t *testing.T) {
		state, req, raw, id := proofFixture(t)
		proof := mustParseProof(t, raw, id)
		ownerA := state.Credentials[1]
		ownerB := ownerCredentialB()
		state.Credentials = append(state.Credentials, ownerB)
		state.Policies[0].Mode = authority.OwnerThreshold
		state.Policies[0].Threshold = 2
		state.Policies[0].Owners = []authority.Subject{ownerA.Subject, ownerB.Subject}
		proof.Proof.Consent = signConsent(t, proof.Proof.Consent.Body,
			struct {
				owner  authority.Subject
				signer authority.CredentialRef
				seed   byte
			}{ownerA.Subject, ownerA.Ref, 2},
			struct {
				owner  authority.Subject
				signer authority.CredentialRef
				seed   byte
			}{ownerB.Subject, ownerB.Ref, 7},
		)
		reSignFixture(t, &proof, &req, 1)
		result := authority.VerifySignedProof(proof, req, state)
		if result.Outcome != authority.OutcomeValid {
			t.Fatalf("outcome=%s code=%s detail=%s", result.Outcome, result.Code, result.Detail)
		}
	})

	t.Run("THRESHOLD not met", func(t *testing.T) {
		state, req, raw, id := proofFixture(t)
		proof := mustParseProof(t, raw, id)
		ownerA := state.Credentials[1]
		state.Credentials = append(state.Credentials, ownerCredentialB())
		state.Policies[0].Mode = authority.OwnerThreshold
		state.Policies[0].Threshold = 2
		state.Policies[0].Owners = []authority.Subject{ownerA.Subject, ownerCredentialB().Subject}
		result := authority.VerifySignedProof(proof, req, state)
		if result.Outcome != authority.OutcomeDenied || result.Code != "OWNER_CONSENT_THRESHOLD_NOT_MET" {
			t.Fatalf("outcome=%s code=%s detail=%s", result.Outcome, result.Code, result.Detail)
		}
	})

	t.Run("duplicate owner does not increase threshold", func(t *testing.T) {
		state, req, raw, id := proofFixture(t)
		proof := mustParseProof(t, raw, id)
		ownerA := state.Credentials[1]
		ownerB := ownerCredentialB()
		state.Credentials = append(state.Credentials, ownerB)
		state.Policies[0].Mode = authority.OwnerThreshold
		state.Policies[0].Threshold = 2
		state.Policies[0].Owners = []authority.Subject{ownerA.Subject, ownerB.Subject}
		proof.Proof.Consent = signConsent(t, proof.Proof.Consent.Body,
			struct {
				owner  authority.Subject
				signer authority.CredentialRef
				seed   byte
			}{ownerA.Subject, ownerA.Ref, 2},
			struct {
				owner  authority.Subject
				signer authority.CredentialRef
				seed   byte
			}{ownerA.Subject, ownerA.Ref, 2},
		)
		reSignFixture(t, &proof, &req, 1)
		result := authority.VerifySignedProof(proof, req, state)
		if result.Outcome != authority.OutcomeDenied || result.Code != "OWNER_CONSENT_DUPLICATE" {
			t.Fatalf("outcome=%s code=%s detail=%s", result.Outcome, result.Code, result.Detail)
		}
	})

	t.Run("wrong owner", func(t *testing.T) {
		state, req, raw, id := proofFixture(t)
		proof := mustParseProof(t, raw, id)
		rogue := credentialFor(8, "cred-rogue", "rogue-owner", "identity-source", "identity-v1", "credential-scope")
		state.Credentials = append(state.Credentials, rogue)
		proof.Proof.Consent = signConsent(t, proof.Proof.Consent.Body,
			struct {
				owner  authority.Subject
				signer authority.CredentialRef
				seed   byte
			}{rogue.Subject, rogue.Ref, 8},
		)
		reSignFixture(t, &proof, &req, 1)
		result := authority.VerifySignedProof(proof, req, state)
		if result.Outcome != authority.OutcomeDenied || result.Code != "OWNER_CONSENT_WRONG_OWNER" {
			t.Fatalf("outcome=%s code=%s detail=%s", result.Outcome, result.Code, result.Detail)
		}
	})

	t.Run("bad consent signature", func(t *testing.T) {
		state, req, raw, id := proofFixture(t)
		proof := mustParseProof(t, raw, id)
		sig, err := base64.StdEncoding.DecodeString(proof.Proof.Consent.Approvals[0].Signature)
		if err != nil {
			t.Fatal(err)
		}
		sig[0] ^= 1
		proof.Proof.Consent.Approvals[0].Signature = base64.StdEncoding.EncodeToString(sig)
		reSignFixture(t, &proof, &req, 1)
		result := authority.VerifySignedProof(proof, req, state)
		if result.Outcome != authority.OutcomeDenied || result.Code != "OWNER_CONSENT_SIGNATURE_INVALID" {
			t.Fatalf("outcome=%s code=%s detail=%s", result.Outcome, result.Code, result.Detail)
		}
	})
}
