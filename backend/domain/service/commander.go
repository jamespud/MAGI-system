package service

import (
	"context"
	"fmt"
	"log"

	"github.com/cloudwego/eino/schema"

	"github.com/jamespud/magi/backend/domain/entity"
	"github.com/jamespud/magi/backend/domain/port"
	"github.com/jamespud/magi/backend/domain/validation"
)

type CommanderConfig struct {
	Model   entity.ModelRef
	Persona string
}

// Commander makes validated structured-output LLM calls (no loop, no tools).
type Commander struct {
	cfg     CommanderConfig
	model   port.ModelPort
	gen     validation.SchemaGenerator
	val     validation.Validator
	taskVal *validation.TypedValidator[entity.DecisionTask]
}

func NewCommander(cfg CommanderConfig, model port.ModelPort, gen validation.SchemaGenerator, val validation.Validator) (*Commander, error) {
	tv, err := validation.NewTypedValidator[entity.DecisionTask](gen, val)
	if err != nil {
		return nil, fmt.Errorf("commander: task validator: %w", err)
	}
	return &Commander{cfg: cfg, model: model, gen: gen, val: val, taskVal: tv}, nil
}

func (c *Commander) Normalize(ctx context.Context, case_ *entity.DecisionCase) (*entity.DecisionTask, error) {
	if case_ == nil {
		return nil, fmt.Errorf("nil case")
	}
	cm, err := c.model.Build(ctx, c.cfg.Model)
	if err != nil {
		return nil, fmt.Errorf("commander: build model: %w", err)
	}
	prompt := fmt.Sprintf(`%s

Normalize this decision question into a DecisionTask JSON. You MUST include ALL fields:
- canonical_question: the standardized, unambiguous form of the question
- decision_type: the type of decision (adopt/migrate/launch/strategic/generic)
- background: relevant context for understanding the decision
- dimensions: key decision dimensions, each with code + description (at least 2)
- information_needs: information gaps to fill, each with topic + rationale
- success_criteria: how to evaluate the decision, each with code + description
- unknowns: what is currently unknown or uncertain

Question: %s
Context: %s
Constraints: %v`,
		c.cfg.Persona, case_.Question, case_.Context, case_.Constraints)
	for attempt := 0; attempt < 3; attempt++ {
		resp, err := cm.Generate(ctx, []*schema.Message{
			schema.SystemMessage(prompt),
			schema.UserMessage("Output the DecisionTask JSON now."),
		})
		if err != nil {
			log.Printf("commander: normalize attempt %d: model generate error: %v", attempt+1, err)
			continue
		}
		task, vr := c.taskVal.ValidateAndUnmarshal([]byte(resp.Content))
		if vr != nil && vr.Valid {
			return task, nil
		}
		if vr != nil {
			log.Printf("commander: normalize attempt %d: validation failed with %d violations", attempt+1, len(vr.Violations))
			for i, v := range vr.Violations {
				log.Printf("  violation %d: [%s] %s field=%s", i+1, v.Code, v.Message, v.Field)
			}
		}
	}
	return nil, fmt.Errorf("commander: normalize failed after retries")
}

func (c *Commander) GenerateReport(ctx context.Context, case_ *entity.DecisionCase, resolution *entity.Resolution, votes []*entity.Vote) (string, error) {
	cm, err := c.model.Build(ctx, c.cfg.Model)
	if err != nil {
		return "", fmt.Errorf("commander: build model: %w", err)
	}
	prompt := fmt.Sprintf("%s\n\nGenerate a human-readable decision report. Question: %s. Consensus: %s. Votes: %d.",
		c.cfg.Persona, case_.Question, resolution.Consensus.Outcome, len(votes))
	resp, err := cm.Generate(ctx, []*schema.Message{
		schema.SystemMessage(prompt),
		schema.UserMessage("Output the report now."),
	})
	if err != nil {
		return "", err
	}
	return resp.Content, nil
}
