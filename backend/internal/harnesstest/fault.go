package harnesstest

import "errors"

// CrashPoint identifies a recoverable boundary where a crash-recovery test may
// force a process crash.
type CrashPoint string

const (
	CrashBeforeModel             CrashPoint = "before_model"
	CrashAfterModelBeforePersist CrashPoint = "after_model_before_persist"
	CrashBeforeTool              CrashPoint = "before_tool"
	CrashAfterToolBeforePersist  CrashPoint = "after_tool_before_persist"
	CrashAfterInvocationPersist  CrashPoint = "after_invocation_persist"
	CrashBeforeCheckpoint        CrashPoint = "before_checkpoint"
	CrashAfterCheckpoint         CrashPoint = "after_checkpoint"
)

// CrashError is the sentinel panic value used to signal an injected crash. Tests
// recover it to assert the recovery boundary rather than treat it as a real
// failure.
var CrashError = errors.New("harnesstest: injected crash")

// CrashInjector forces a crash at a configured CrashPoint. A nil injector or a
// mismatched point is a no-op, so the same primitive can be safely shared.
type CrashInjector struct {
	Point CrashPoint
}

// Trigger panics with CrashError when the current point matches the injected
// point.
func (c *CrashInjector) Trigger(point CrashPoint) {
	if c != nil && c.Point == point {
		panic(CrashError)
	}
}
