package harnesstest

import (
	"context"
	"fmt"

	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"
	"github.com/jamespud/magi/backend/domain/entity"
	"github.com/jamespud/magi/backend/domain/port"
)

// ScriptedModel returns a fixed sequence of messages, one per call, and then
// fails once exhausted.
type ScriptedModel struct {
	Responses []*schema.Message
	Calls     int
}

func (s *ScriptedModel) Generate(ctx context.Context, _ []*schema.Message, _ ...model.Option) (*schema.Message, error) {
	if s.Calls >= len(s.Responses) {
		return nil, fmt.Errorf("scripted model: no more responses")
	}
	msg := s.Responses[s.Calls]
	s.Calls++
	return msg, nil
}

func (s *ScriptedModel) Stream(ctx context.Context, _ []*schema.Message, _ ...model.Option) (*schema.StreamReader[*schema.Message], error) {
	return nil, fmt.Errorf("scripted model: streaming not implemented")
}

func (s *ScriptedModel) WithTools(_ []*schema.ToolInfo) (model.ToolCallingChatModel, error) {
	return s, nil
}

// CountingModel counts calls and returns a single fixed message.
type CountingModel struct {
	Calls int
	Msg   *schema.Message
}

func (c *CountingModel) Generate(ctx context.Context, _ []*schema.Message, _ ...model.Option) (*schema.Message, error) {
	c.Calls++
	return c.Msg, nil
}

func (c *CountingModel) Stream(ctx context.Context, _ []*schema.Message, _ ...model.Option) (*schema.StreamReader[*schema.Message], error) {
	return nil, fmt.Errorf("counting model: streaming not implemented")
}

func (c *CountingModel) WithTools(_ []*schema.ToolInfo) (model.ToolCallingChatModel, error) {
	return c, nil
}

// ModelPort adapts a model.ToolCallingChatModel into port.ModelPort so toolkits
// can drive AgentLoop without a real provider.
type ModelPort struct {
	M model.ToolCallingChatModel
}

func (p *ModelPort) Build(_ context.Context, _ entity.ModelRef) (model.ToolCallingChatModel, error) {
	return p.M, nil
}

var _ port.ModelPort = (*ModelPort)(nil)
