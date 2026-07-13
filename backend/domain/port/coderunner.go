package port

import "context"

// CodeRunnerPort wraps the Coze sandbox code runner.
type CodeRunnerPort interface {
	Run(ctx context.Context, lang, code string) (string, error)
}
