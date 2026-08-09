package runtime

import (
	"context"
	"fmt"
	"strings"

	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"

	"github.com/jamespud/magi/backend/domain/entity"
)

const compactionMarker = "[compacted]"

// compactHistory summarizes the middle of the working-memory history so the
// loop can continue past a token-budget threshold instead of hard-stopping.
// The system message and the most recent messages are preserved verbatim;
// older messages are replaced by a model-generated summary that preserves
// evidence/claim IDs, tool names and decisions. When the model call fails a
// deterministic truncation fallback keeps the loop moving.
func compactHistory(ctx context.Context, modelPort model.ToolCallingChatModel, messages []*schema.Message) ([]*schema.Message, *entity.Usage, error) {
	if modelPort == nil || len(messages) <= 4 {
		return nil, nil, fmt.Errorf("compaction: nothing to compact")
	}
	for _, m := range messages {
		if strings.Contains(m.Content, compactionMarker) {
			return nil, nil, nil // already compacted
		}
	}
	keep := 4
	if len(messages) <= keep+1 {
		return nil, nil, fmt.Errorf("compaction: history too short")
	}
	head := messages[1 : len(messages)-keep]
	tail := messages[len(messages)-keep:]

	var b strings.Builder
	for _, m := range head {
		role := m.Role
		if role == "" {
			role = "message"
		}
		fmt.Fprintf(&b, "\n[%s]\n%s\n", role, m.Content)
	}
	prompt := fmt.Sprintf(
		"Summarize the following agent working-memory history so the run can continue. "+
			"Preserve evidence IDs (EV-*), claim IDs (CL-*), tool names, key findings, pending decisions and the current phase. "+
			"Output plain text only.\n\n%s", b.String())
	summary, err := modelPort.Generate(ctx, []*schema.Message{
		schema.SystemMessage("You are a lossy summarizer for an agent run. Preserve identifiers and decisions."),
		schema.UserMessage(prompt),
	})
	if err != nil {
		replacement := schema.UserMessage(fmt.Sprintf(
			"%s Previous %d message(s) omitted because summarization failed.", compactionMarker, len(head)))
		out := append([]*schema.Message{sysMessage(messages[0])}, replacement)
		out = append(out, tail...)
		return out, &entity.Usage{}, nil
	}
	text := strings.TrimSpace(summary.Content)
	if text == "" {
		text = "(empty summary)"
	}
	replacement := schema.UserMessage(fmt.Sprintf("%s Previous working memory (summary):\n%s", compactionMarker, text))
	out := append([]*schema.Message{sysMessage(messages[0])}, replacement)
	out = append(out, tail...)
	return out, extractUsage(summary), nil
}

func sysMessage(m *schema.Message) *schema.Message {
	if m == nil {
		return schema.SystemMessage("")
	}
	return &schema.Message{Role: m.Role, Content: m.Content}
}
