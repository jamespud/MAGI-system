package magi

// SetAuthorityCommitFaultHookForTest injects deterministic transaction fault
// points into the test binary only. Production callers cannot set this hook.
func SetAuthorityCommitFaultHookForTest(repo *AuthorityCommitRepository, hook func(string) error) {
	repo.faultHook = hook
}

// SetExposureGateAttestationVerifierForTest keeps the operational verifier
// unreachable from production wiring.
func SetExposureGateAttestationVerifierForTest(repo *AuthorityCommitRepository, verifier ExposureGateAttestationVerifier) {
	repo.SetExposureGateAttestationVerifierForTest(verifier)
}
