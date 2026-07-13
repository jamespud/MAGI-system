package magi

import (
	"context"
	"fmt"
	"sync"

	"github.com/coze-dev/coze-studio/backend/infra/coderunner"
	"github.com/jamespud/magi/backend/domain/port"
)

// CodeRunnerAdapter implements port.CodeRunnerPort via Coze infra/coderunner.
// Progressive activation: Coze API availability is probed on first Run call.
type CodeRunnerAdapter struct {
	activated    bool
	activateOnce sync.Once
	activateErr  error
}

func NewCodeRunnerAdapter() *CodeRunnerAdapter { return &CodeRunnerAdapter{} }

func (a *CodeRunnerAdapter) activate(ctx context.Context) error {
	a.activateOnce.Do(func() {
		r := coderunner.GetCodeRunner()
		if r == nil {
			a.activateErr = fmt.Errorf("coderunner adapter: Coze code runner not initialized")
			return
		}
		a.activated = true
	})
	return a.activateErr
}

func (a *CodeRunnerAdapter) Run(ctx context.Context, lang, code string) (string, error) {
	if err := a.activate(ctx); err != nil {
		return "", fmt.Errorf("coderunner adapter: Coze code runner unavailable: %w", err)
	}
	return "", fmt.Errorf("coderunner: full wiring pending; Coze API confirmed available")
}

var _ port.CodeRunnerPort = (*CodeRunnerAdapter)(nil)
