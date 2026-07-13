package magi

import (
	"context"
	"fmt"

	"github.com/jamespud/magi/backend/domain/port"
)

// CodeRunnerAdapter implements port.CodeRunnerPort via Coze infra/coderunner.
// S1 skeleton; full wiring in S2.
type CodeRunnerAdapter struct{}

func NewCodeRunnerAdapter() *CodeRunnerAdapter { return &CodeRunnerAdapter{} }

func (a *CodeRunnerAdapter) Run(ctx context.Context, lang, code string) (string, error) {
	return "", fmt.Errorf("coderunner: not yet wired in S1 skeleton")
}

var _ port.CodeRunnerPort = (*CodeRunnerAdapter)(nil)
