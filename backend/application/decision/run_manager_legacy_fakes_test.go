package decision_test

// These fixtures test the Job envelope without an atomic Case/Resolution
// implementation. Their non-durable orchestration behavior is explicit.
func (o *attemptRecordingOrchestrator) AllowsNonAtomicTerminalCommit() bool          { return true }
func (o *blockingOrchestrator) AllowsNonAtomicTerminalCommit() bool                  { return true }
func (o *blockingUserOrchestrator) AllowsNonAtomicTerminalCommit() bool              { return true }
func (o *countingRecoveryOrchestrator) AllowsNonAtomicTerminalCommit() bool          { return true }
func (o *durableRetryOrchestrator) AllowsNonAtomicTerminalCommit() bool              { return true }
func (o *failThenSucceedCaseOrchestrator) AllowsNonAtomicTerminalCommit() bool       { return true }
func (o *lateSuccessAfterLeaseLossOrchestrator) AllowsNonAtomicTerminalCommit() bool { return true }
func (o *remoteCancelOrchestrator) AllowsNonAtomicTerminalCommit() bool              { return true }
func (o *retryResetOrchestrator) AllowsNonAtomicTerminalCommit() bool                { return true }
func (o *shutdownBlockingOrchestrator) AllowsNonAtomicTerminalCommit() bool          { return true }
func (o *successThenObserveCancellationOrchestrator) AllowsNonAtomicTerminalCommit() bool {
	return true
}
func (o terminalCaseErrorOrchestrator) AllowsNonAtomicTerminalCommit() bool { return true }
