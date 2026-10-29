package magi

// SetAuthorityCommitFaultHookForTest injects deterministic transaction fault
// points into the test binary only. Production callers cannot set this hook.
func SetAuthorityCommitFaultHookForTest(repo *AuthorityCommitRepository, hook func(string) error) {
	repo.faultHook = hook
}

// SetExposureGateAttestationVerifierForTest injects the local test verifier.
// This symbol exists only in the test build; it is not part of the adapter API.
func SetExposureGateAttestationVerifierForTest(repo *AuthorityCommitRepository, verifier ExposureGateAttestationVerifier) {
	repo.witnessVerifier = verifier
	repo.gateAttestationVerifier = verifier
}
