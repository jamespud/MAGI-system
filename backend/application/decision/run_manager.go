package decision

import (
	"context"
	"errors"
	"sync"

	"github.com/jamespud/magi/backend/domain/entity"
)

// ErrAlreadyRunning is returned when Start is called for a case that already
// has an active (non-completed) run.
var ErrAlreadyRunning = errors.New("case already running")

type runHandle struct {
	cancel context.CancelFunc
	done   chan struct{}
}

// RunManager owns the async lifecycle of case runs. It detaches each run from
// the originating request context so a run survives the HTTP handler return,
// supports cancellation, and prevents duplicate concurrent runs per case.
type RunManager struct {
	orch Orchestrator
	mu   sync.Mutex
	runs map[string]*runHandle
}

func NewRunManager(orch Orchestrator) *RunManager {
	return &RunManager{orch: orch, runs: make(map[string]*runHandle)}
}

// Start launches Orchestrate in a background goroutine. Returns
// ErrAlreadyRunning if the case already has an active run. The run uses a
// context detached from ctx (Background-derived) so it survives ctx cancel.
func (m *RunManager) Start(ctx context.Context, c *entity.DecisionCase) error {
	m.mu.Lock()
	if _, ok := m.runs[c.ID]; ok {
		m.mu.Unlock()
		return ErrAlreadyRunning
	}
	runCtx, cancel := context.WithCancel(context.Background())
	h := &runHandle{cancel: cancel, done: make(chan struct{})}
	m.runs[c.ID] = h
	m.mu.Unlock()

	go func() {
		defer close(h.done)
		defer func() {
			m.mu.Lock()
			delete(m.runs, c.ID)
			m.mu.Unlock()
		}()
		_, _ = m.orch.Orchestrate(runCtx, c)
	}()
	return nil
}

// Cancel cancels the active run for caseID. Returns true if a run was active.
func (m *RunManager) Cancel(caseID string) bool {
	m.mu.Lock()
	h, ok := m.runs[caseID]
	m.mu.Unlock()
	if !ok {
		return false
	}
	h.cancel()
	return true
}

// IsRunning reports whether caseID has an active run.
func (m *RunManager) IsRunning(caseID string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	_, ok := m.runs[caseID]
	return ok
}
