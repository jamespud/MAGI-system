package service

import (
	"context"
	"fmt"
	"log"

	"github.com/cloudwego/eino/schema"

	"github.com/jamespud/magi/backend/domain/entity"
	"github.com/jamespud/magi/backend/domain/port"
	"github.com/jamespud/magi/backend/domain/prompt"
	"github.com/jamespud/magi/backend/domain/validation"
)

type CommanderConfig struct {
	Model   entity.ModelRef
	Persona string
	// Prompts is the optional versioned prompt source (P2 D12). When nil the
	// commander falls back to the built-in default templates.
	Prompts port.PromptProvider
}

// Commander makes validated structured-output LLM calls (no loop, no tools).
type Commander struct {
	cfg       CommanderConfig
	model     port.ModelPort
	gen       validation.SchemaGenerator
	val       validation.Validator
	taskVal   *validation.TypedValidator[entity.DecisionTask]
	reportVal *validation.TypedValidator[entity.FinalReportData]
}

// render resolves a versioned prompt template (registry first, built-in
// fallback) and substitutes the given placeholders.
func (c *Commander) render(ctx context.Context, key string, vars map[string]string) string {
	tmpl := prompt.Default()[key]
	if c.cfg.Prompts != nil {
		if s, ok := c.cfg.Prompts.Load(ctx, key); ok && s != "" {
			tmpl = s
		}
	}
	return prompt.Render(tmpl, vars)
}

func NewCommander(cfg CommanderConfig, model port.ModelPort, gen validation.SchemaGenerator, val validation.Validator) (*Commander, error) {
	tv, err := validation.NewTypedValidator[entity.DecisionTask](gen, val)
	if err != nil {
		return nil, fmt.Errorf("commander: task validator: %w", err)
	}
	rv, err := validation.NewTypedValidator[entity.FinalReportData](gen, val)
	if err != nil {
		return nil, fmt.Errorf("commander: report validator: %w", err)
	}
	return &Commander{cfg: cfg, model: model, gen: gen, val: val, taskVal: tv, reportVal: rv}, nil
}

func (c *Commander) Normalize(ctx context.Context, case_ *entity.DecisionCase) (*entity.DecisionTask, error) {
	if case_ == nil {
		return nil, fmt.Errorf("nil case")
	}
	cm, err := c.model.Build(ctx, c.cfg.Model)
	if err != nil {
		return nil, fmt.Errorf("commander: build model: %w", err)
	}
	prompt := c.render(ctx, prompt.KeyCommanderNormalize, map[string]string{
		"PERSONA":     c.cfg.Persona,
		"QUESTION":    case_.Question,
		"CONTEXT":     case_.Context,
		"CONSTRAINTS": fmt.Sprintf("%v", case_.Constraints),
	})
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

func (c *Commander) GenerateReport(ctx context.Context, case_ *entity.DecisionCase, resolution *entity.Resolution, votes []*entity.Vote, evidenceIDs, claimIDs []string) (string, error) {
	cm, err := c.model.Build(ctx, c.cfg.Model)
	if err != nil {
		return "", fmt.Errorf("commander: build model: %w", err)
	}
	prompt := c.render(ctx, prompt.KeyCommanderReport, map[string]string{
		"PERSONA":      c.cfg.Persona,
		"QUESTION":     case_.Question,
		"CONSENSUS":    string(resolution.Consensus.Outcome),
		"VOTES":        fmt.Sprintf("%d", len(votes)),
		"EVIDENCE_IDS": fmt.Sprintf("%v", evidenceIDs),
		"CLAIM_IDS":    fmt.Sprintf("%v", claimIDs),
	})
	for attempt := 0; attempt < 3; attempt++ {
		msgs := []*schema.Message{schema.SystemMessage(prompt)}
		if attempt > 0 {
			msgs = append(msgs, schema.UserMessage(
				"Your previous response did not pass validation or did not cite any available evidence/claim ID. "+
					"Re-output the FinalReportData JSON now, citing at least one of the provided IDs in key_evidence_ids/key_claim_ids."))
		}
		msgs = append(msgs, schema.UserMessage("Output the FinalReportData JSON now."))
		resp, err := cm.Generate(ctx, msgs)
		if err != nil {
			log.Printf("commander: report attempt %d: model generate error: %v", attempt+1, err)
			continue
		}
		data, vr := c.reportVal.ValidateAndUnmarshal([]byte(resp.Content))
		if vr != nil && vr.Valid && reportCites(data, evidenceIDs, claimIDs) {
			return RenderReport(data), nil
		}
		if vr != nil {
			log.Printf("commander: report attempt %d: validation failed with %d violations", attempt+1, len(vr.Violations))
			for i, v := range vr.Violations {
				log.Printf("  violation %d: [%s] %s field=%s", i+1, v.Code, v.Message, v.Field)
			}
		} else if data != nil && !reportCites(data, evidenceIDs, claimIDs) {
			log.Printf("commander: report attempt %d: no evidence/claim citation", attempt+1)
		}
	}
	return "", fmt.Errorf("commander: report generation failed after retries")
}

func reportCites(data *entity.FinalReportData, evidenceIDs, claimIDs []string) bool {
	if data == nil {
		return false
	}
	if len(evidenceIDs)+len(claimIDs) == 0 {
		return true
	}
	return len(data.KeyEvidenceIDs)+len(data.KeyClaimIDs) > 0
}
