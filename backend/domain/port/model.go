// Package port defines the MAGI domain ports (ADR-007). domain/magi depends
// only on these interfaces + eino; Coze implementations live in application/magi.
package port

import (
	"context"

	"github.com/cloudwego/eino/components/model"

	"github.com/jamespud/magi/backend/domain/entity"
)

// ModelPort builds a Coze ToolCallingChatModel from a MagiConfig.ModelRef.
type ModelPort interface {
	Build(ctx context.Context, ref entity.ModelRef) (model.ToolCallingChatModel, error)
}
