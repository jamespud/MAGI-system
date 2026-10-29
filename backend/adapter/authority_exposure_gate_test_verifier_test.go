package magi_test

import (
	"time"

	magi "github.com/jamespud/magi/backend/adapter"
	"github.com/jamespud/magi/backend/domain/authority"
	"github.com/jamespud/magi/backend/domain/authoritycommit"
)

// localGateAttestationVerifier proves only that test fixtures can be bound to
// the transaction. It is not a real provider or independent witness verifier.
type localGateAttestationVerifier struct{}

func (localGateAttestationVerifier) VerifyGateAttestation(
	request authoritycommit.ExposureGateRequest,
	capability authoritycommit.ProviderCapability,
	fence magi.AuthorityExposureGateFenceEvidence,
	now time.Time,
) authority.Result {
	if capability.EnforcesFence && capability.SupportsLease && capability.EnforcesRevocationWatermark &&
		request.Witness.AdmissionCommitID == request.Gate.AdmissionCommitID &&
		request.Witness.EvidenceDigest != "" {
		return authority.Result{Outcome: authority.OutcomeValid, Code: "TEST_ATTESTATION_VALID"}
	}
	return authority.Result{Outcome: authority.OutcomeDenied, Code: "TEST_ATTESTATION_DENIED"}
}
