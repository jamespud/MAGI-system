package authority_test

import (
	"testing"

	"github.com/jamespud/magi/backend/domain/authority"
)

func mustParseProof(t *testing.T, raw []byte, id string) authority.SignedProof {
	t.Helper()
	proof, result := authority.ParseSignedProof(raw, id)
	if result.Outcome != authority.OutcomeValid {
		t.Fatalf("parse outcome=%s code=%s detail=%s", result.Outcome, result.Code, result.Detail)
	}
	return proof
}

func TestDelegationRejectsWideningCyclesAndIdentityConfusion(t *testing.T) {
	type testCase struct {
		name    string
		mutate  func(t *testing.T, state *authority.TrustState, proof *authority.SignedProof, req *authority.VerificationRequest)
		code    string
		outcome authority.Outcome
	}
	cases := []testCase{
		{
			name: "scope widening",
			mutate: func(t *testing.T, state *authority.TrustState, proof *authority.SignedProof, req *authority.VerificationRequest) {
				hop := proof.Proof.Delegations[0]
				hop.Body.Resources = []string{"case/99"}
				proof.Proof.Delegations[0] = signDelegation(t, hop.Body, 1)
			},
			code: "DELEGATION_SCOPE_WIDENED",
		},
		{
			name: "non-delegable parent",
			mutate: func(t *testing.T, state *authority.TrustState, proof *authority.SignedProof, req *authority.VerificationRequest) {
				delegateA := proof.Proof.Subject
				delegateB := credentialFor(4, "cred-delegate-b", "delegate-b", "identity-source", "identity-v1", "credential-scope")
				state.Credentials = append(state.Credentials, delegateB)
				first := proof.Proof.Delegations[0]
				first.Body.Delegable = false
				first = signDelegation(t, first.Body, 1)
				second := signDelegation(t, authority.DelegationBody{
					Version: authority.DelegationVersion, DelegationID: "hop-2",
					TrustDomain: "tenant-a", RootIncarnation: "root-1",
					Issuer: delegateA, IssuerCredential: credentialFor(3, "cred-delegate", "delegate-a", "identity-source", "identity-v1", "credential-scope").Ref,
					Delegate:  delegateB.Subject,
					Resources: []string{"case/42"}, Actions: []string{"PUBLISH"}, Tenants: []string{"tenant-a"},
					NotBefore: "2026-10-24T09:05:00.000Z", ExpiresAt: "2026-10-24T11:00:00.000Z", Delegable: false,
				}, 3)
				proof.Proof.Delegations = []authority.SignedDelegation{first, second}
				proof.Proof.Issuer = delegateB.Subject
				proof.Proof.Subject = delegateB.Subject
				proof.Proof.Signer = delegateB.Ref
				req.Subject = delegateB.Subject
			},
			code: "DELEGATION_NOT_DELEGABLE",
		},
		{
			name: "cycle",
			mutate: func(t *testing.T, state *authority.TrustState, proof *authority.SignedProof, req *authority.VerificationRequest) {
				delegateA := proof.Proof.Subject
				first := proof.Proof.Delegations[0]
				first.Body.Delegable = true
				first = signDelegation(t, first.Body, 1)
				second := signDelegation(t, authority.DelegationBody{
					Version: authority.DelegationVersion, DelegationID: "hop-2",
					TrustDomain: "tenant-a", RootIncarnation: "root-1",
					Issuer: delegateA, IssuerCredential: credentialFor(3, "cred-delegate", "delegate-a", "identity-source", "identity-v1", "credential-scope").Ref,
					Delegate:  state.GovernanceSubject,
					Resources: []string{"case/42"}, Actions: []string{"PUBLISH"}, Tenants: []string{"tenant-a"},
					NotBefore: "2026-10-24T09:05:00.000Z", ExpiresAt: "2026-10-24T11:00:00.000Z", Delegable: false,
				}, 3)
				proof.Proof.Delegations = []authority.SignedDelegation{first, second}
				proof.Proof.Issuer = state.GovernanceSubject
				proof.Proof.Subject = state.GovernanceSubject
				proof.Proof.Signer = credentialFor(1, "cred-governance", "governance", "identity-source", "identity-v1", "credential-scope").Ref
				req.Subject = state.GovernanceSubject
			},
			code: "DELEGATION_CYCLE", outcome: authority.OutcomeConflict,
		},
		{
			name: "wrong issuer",
			mutate: func(t *testing.T, state *authority.TrustState, proof *authority.SignedProof, req *authority.VerificationRequest) {
				delegateA := proof.Proof.Subject
				delegateB := credentialFor(4, "cred-delegate-b", "delegate-b", "identity-source", "identity-v1", "credential-scope")
				state.Credentials = append(state.Credentials, delegateB)
				first := proof.Proof.Delegations[0]
				first.Body.Delegable = true
				first = signDelegation(t, first.Body, 1)
				second := signDelegation(t, authority.DelegationBody{
					Version: authority.DelegationVersion, DelegationID: "hop-2",
					TrustDomain: "tenant-a", RootIncarnation: "root-1",
					Issuer: state.GovernanceSubject, IssuerCredential: credentialFor(1, "cred-governance", "governance", "identity-source", "identity-v1", "credential-scope").Ref,
					Delegate:  delegateB.Subject,
					Resources: []string{"case/42"}, Actions: []string{"PUBLISH"}, Tenants: []string{"tenant-a"},
					NotBefore: "2026-10-24T09:05:00.000Z", ExpiresAt: "2026-10-24T11:00:00.000Z", Delegable: false,
				}, 1)
				proof.Proof.Delegations = []authority.SignedDelegation{first, second}
				proof.Proof.Issuer = delegateB.Subject
				proof.Proof.Subject = delegateB.Subject
				proof.Proof.Signer = delegateB.Ref
				_ = delegateA
				req.Subject = delegateB.Subject
			},
			code: "DELEGATION_ISSUER_MISMATCH",
		},
		{
			name: "wrong subject",
			mutate: func(t *testing.T, state *authority.TrustState, proof *authority.SignedProof, req *authority.VerificationRequest) {
				attacker := credentialFor(5, "cred-attacker", "attacker", "identity-source", "identity-v1", "credential-scope")
				state.Credentials = append(state.Credentials, attacker)
				proof.Proof.Issuer = attacker.Subject
				proof.Proof.Subject = attacker.Subject
				proof.Proof.Signer = attacker.Ref
				req.Subject = attacker.Subject
			},
			code: "PROOF_ISSUER_MISMATCH",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			state, req, raw, id := delegationFixture(t)
			proof := mustParseProof(t, raw, id)
			seed := byte(3)
			tc.mutate(t, &state, &proof, &req)
			switch proof.Proof.Subject.SubjectID {
			case "governance":
				seed = 1
			case "delegate-b":
				seed = 4
			case "attacker":
				seed = 5
			}
			raw, id = signProof(t, proof.Proof, seed)
			proof = mustParseProof(t, raw, id)
			req.ProofID = id
			result := authority.VerifySignedProof(proof, req, state)
			wantOutcome := tc.outcome
			if wantOutcome == "" {
				wantOutcome = authority.OutcomeDenied
			}
			if result.Outcome != wantOutcome || result.Code != tc.code {
				t.Fatalf("outcome=%s code=%s detail=%s, want %s/%s", result.Outcome, result.Code, result.Detail, wantOutcome, tc.code)
			}
		})
	}
}
