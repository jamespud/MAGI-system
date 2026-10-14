package service

import (
	"context"
	"fmt"
	"log"
	"sort"

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

// GenerateReport produces the validated final report body.
//
// Boundary (issue #13): this enforces the STRUCTURED report contract —
// the authoritative decision is bound from the resolution (never model-authored)
// and every cited ID must belong to the frozen, current-generation artifact set
// passed in by the caller (validateReportCitations). It does NOT semantically
// verify free-text claims about authorization, sending or verification inside
// Summary/KeyReasons/Risks/NextSteps: those authority and execution states must
// eventually be rendered from a deterministic template under the six-dimension
// state contract, which is owned by Epic #7, not this fix. Until then this
// function must not be described as a complete REPORT-01 implementation.
func (c *Commander) GenerateReport(ctx context.Context, case_ *entity.DecisionCase, resolution *entity.Resolution, votes []*entity.Vote, evidenceIDs, claimIDs []string) (string, error) {
	cm, err := c.model.Build(ctx, c.cfg.Model)
	if err != nil {
		return "", fmt.Errorf("commander: build model: %w", err)
	}
	// Count the authoritative ballots: a nil slot marks a participant that
	// produced none, which is not an abstention.
	ballots := 0
	for _, v := range votes {
		if v != nil {
			ballots++
		}
	}
	prompt := c.render(ctx, prompt.KeyCommanderReport, map[string]string{
		"PERSONA":      c.cfg.Persona,
		"QUESTION":     case_.Question,
		"CONSENSUS":    string(resolution.Consensus.Outcome),
		"VOTES":        fmt.Sprintf("%d", ballots),
		"EVIDENCE_IDS": fmt.Sprintf("%v", evidenceIDs),
		"CLAIM_IDS":    fmt.Sprintf("%v", claimIDs),
	})
	for attempt := 0; attempt < 3; attempt++ {
		msgs := []*schema.Message{schema.SystemMessage(prompt)}
		if attempt > 0 {
			msgs = append(msgs, schema.UserMessage(
				"Your previous response did not pass validation, cited no available ID, or cited an ID that is not in the provided set. "+
					"Re-output the FinalReportData JSON now, citing at least one of the provided IDs in key_evidence_ids/key_claim_ids and citing only IDs from that set."))
		}
		msgs = append(msgs, schema.UserMessage("Output the FinalReportData JSON now."))
		resp, err := cm.Generate(ctx, msgs)
		if err != nil {
			log.Printf("commander: report attempt %d: model generate error: %v", attempt+1, err)
			continue
		}
		data, vr := c.reportVal.ValidateAndUnmarshal([]byte(resp.Content))
		if vr != nil && vr.Valid {
			if err := validateReportCitations(data, evidenceIDs, claimIDs); err == nil {
				return RenderReport(resolution.FinalDecision, data), nil
			} else {
				log.Printf("commander: report attempt %d: citation rejected: %v", attempt+1, err)
			}
		} else if vr != nil {
			log.Printf("commander: report attempt %d: validation failed with %d violations", attempt+1, len(vr.Violations))
			for i, v := range vr.Violations {
				log.Printf("  violation %d: [%s] %s field=%s", i+1, v.Code, v.Message, v.Field)
			}
		}
	}
	return "", fmt.Errorf("commander: report generation failed after retries")
}

// validateReportCitations enforces that every ID the report cites belongs to
// the frozen authoritative set handed to the model, that duplicates are not
// laundered into a longer citation list, and that a report cannot silently omit
// citations when authoritative material exists. Empty/unknown/duplicate
// references are rejected so the caller retries instead of persisting a report
// that claims evidence MAGI cannot prove (invariants REPORT-01, EVID-02).
func validateReportCitations(data *entity.FinalReportData, evidenceIDs, claimIDs []string) error {
	if data == nil {
		return fmt.Errorf("report data is nil")
	}
	allowed := make(map[string]struct{}, len(evidenceIDs)+len(claimIDs))
	for _, id := range evidenceIDs {
		allowed[id] = struct{}{}
	}
	for _, id := range claimIDs {
		allowed[id] = struct{}{}
	}
	cited := append(append([]string{}, data.KeyEvidenceIDs...), data.KeyClaimIDs...)
	for _, id := range cited {
		if id == "" {
			return fmt.Errorf("report cites an empty artifact id")
		}
		if _, ok := allowed[id]; !ok {
			return fmt.Errorf("report cites %q which is not in the authoritative set %v", id, sortedKeys(allowed))
		}
	}
	for name, ids := range map[string][]string{"evidence": data.KeyEvidenceIDs, "claim": data.KeyClaimIDs} {
		seen := make(map[string]struct{}, len(ids))
		for _, id := range ids {
			if _, dup := seen[id]; dup {
				return fmt.Errorf("report cites duplicate %s id %q", name, id)
			}
			seen[id] = struct{}{}
		}
	}
	if len(allowed) > 0 && len(cited) == 0 {
		return fmt.Errorf("report cites no authoritative artifact while %d are available", len(allowed))
	}
	return nil
}

func sortedKeys(m map[string]struct{}) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
