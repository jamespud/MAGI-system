package runtime

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/cloudwego/eino/schema"

	"github.com/jamespud/magi/backend/domain/entity"
	"github.com/jamespud/magi/backend/domain/evidence"
	"github.com/jamespud/magi/backend/domain/port"
	"github.com/jamespud/magi/backend/domain/validation"
)

var ErrMaxSteps = errors.New("magi agent loop: max steps exceeded")

// RelaxEvidenceStandard drops count/type requirements when no tools are bound
// (the agent reasons from intrinsic knowledge), but preserves CustomRules so
// semantic claim rules (e.g. worst-case claim) remain enforced without tools.
func RelaxEvidenceStandard(std entity.EvidenceStandard, hasTools bool) entity.EvidenceStandard {
	if hasTools {
		return std
	}
	return entity.EvidenceStandard{CustomRules: std.CustomRules}
}

type AgentLoop struct {
	modelPort  port.ModelPort
	toolReg    port.ToolRegistryPort
	toolExec   port.ToolExecutorPort
	validator  validation.Validator
	gen        validation.SchemaGenerator
	adapter    *evidence.EvidenceAdapterRegistry
	gate       *evidence.EvidenceGate
	summaryVal   *validation.TypedValidator[entity.EvidenceSummary]
	voteVal      *validation.TypedValidator[entity.Vote]
	claimVal     *validation.TypedValidator[entity.ClaimSubmission]
	reflectionVal *validation.TypedValidator[entity.Reflection]
	eventPub     port.EventPublisher
}

type AgentLoopDeps struct {
	ModelPort port.ModelPort
	ToolReg   port.ToolRegistryPort
	ToolExec  port.ToolExecutorPort
	Validator validation.Validator
	Gen       validation.SchemaGenerator
	Adapter   *evidence.EvidenceAdapterRegistry
	Gate      *evidence.EvidenceGate
	EventPub  port.EventPublisher
}

func NewAgentLoop(d AgentLoopDeps) (*AgentLoop, error) {
	if d.ModelPort == nil || d.Validator == nil || d.Gen == nil {
		return nil, errors.New("agent loop: nil dependency")
	}
	sv, err := validation.NewTypedValidator[entity.EvidenceSummary](d.Gen, d.Validator)
	if err != nil {
		return nil, fmt.Errorf("agent loop: summary validator: %w", err)
	}
	vv, err := validation.NewTypedValidator[entity.Vote](d.Gen, d.Validator)
	if err != nil {
		return nil, fmt.Errorf("agent loop: vote validator: %w", err)
	}
	cv, err := validation.NewTypedValidator[entity.ClaimSubmission](d.Gen, d.Validator)
	if err != nil {
		return nil, fmt.Errorf("agent loop: claim validator: %w", err)
	}
	rv, err := validation.NewTypedValidator[entity.Reflection](d.Gen, d.Validator)
	if err != nil {
		return nil, fmt.Errorf("agent loop: reflection validator: %w", err)
	}
	gate := d.Gate
	if gate == nil {
		gate = evidence.NewEvidenceGate()
	}
	adapter := d.Adapter
	if adapter == nil {
		adapter = evidence.NewEvidenceAdapterRegistry(evidence.FullReliabilityResolver(),
			evidence.NewNativeAdapter(evidence.FullReliabilityResolver()),
			evidence.NewRawObservationAdapter(evidence.FullReliabilityResolver()))
	}
	return &AgentLoop{
		modelPort: d.ModelPort, toolReg: d.ToolReg, toolExec: d.ToolExec,
		validator: d.Validator, gen: d.Gen, adapter: adapter, gate: gate,
		summaryVal: sv, voteVal: vv, claimVal: cv,
		reflectionVal: rv,
		eventPub: d.EventPub,
	}, nil
}

func (l *AgentLoop) publish(ctx context.Context, caseID, runID string, agentCode entity.MagiCode, et entity.EventType) {
	if l.eventPub == nil {
		return
	}
	ac := agentCode
	_ = l.eventPub.Publish(ctx, entity.MagiEvent{
		CaseID:    caseID,
		RunID:     runID,
		AgentCode: &ac,
		Type:      et,
		Timestamp: time.Now(),
	})
}

func (l *AgentLoop) Run(ctx context.Context, cfg *entity.MagiConfig, actx *AgentContext) (*LoopResult, error) {
	if cfg == nil {
		return nil, errors.New("agent loop: nil config")
	}
	if actx == nil {
		actx = &AgentContext{}
	}
	maxSteps := cfg.LoopPolicy.MaxSteps
	if maxSteps <= 0 {
		maxSteps = 12
	}
	if cfg.LoopPolicy.Timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, cfg.LoopPolicy.Timeout)
		defer cancel()
	}

	// Build model
	cm, err := l.modelPort.Build(ctx, cfg.Model)
	if err != nil {
		return nil, fmt.Errorf("agent loop: build model: %w", err)
	}

	// Resolve tools
	var defs []port.ToolDefinition
	hasTools := false
	if l.toolReg != nil && len(cfg.Tools) > 0 {
		defs, err = l.toolReg.List(ctx, cfg.Tools)
		if err != nil {
			return nil, fmt.Errorf("agent loop: list tools: %w", err)
		}
		hasTools = len(defs) > 0
	}
	// In standalone/knowledge-only mode, relax evidence requirements so the
	// agent can reason from intrinsic knowledge and produce a valid vote.
	evidenceStd := RelaxEvidenceStandard(cfg.EvidenceStandard, hasTools)
	nameToDef := make(map[string]port.ToolDefinition)
	infos := make([]*schema.ToolInfo, 0, len(defs))
	for _, d := range defs {
		nameToDef[d.Name] = d
		infos = append(infos, &schema.ToolInfo{Name: d.Name, Desc: d.Desc})
	}
	bound := cm
	if len(infos) > 0 {
		b, err := cm.WithTools(infos)
		if err != nil {
			return nil, fmt.Errorf("agent loop: bind tools: %w", err)
		}
		bound = b
	}

	// Ledger + schemas
	ledger := evidence.NewEvidenceLedger(actx.CaseID, "", cfg.Code)
	summarySchema, _ := l.gen.FromStruct(entity.EvidenceSummary{})
	voteSchema, _ := l.gen.FromStruct(entity.Vote{})
	reflectionSchema, _ := l.gen.FromStruct(entity.Reflection{})

	// Messages
	question := actx.Task.CanonicalQuestion
	if question == "" {
		question = ""
	}
	messages := []*schema.Message{schema.SystemMessage(BuildAgentSystemPrompt(cfg, summarySchema, voteSchema, reflectionSchema, actx.DebateContext, hasTools))}
	messages = append(messages, schema.UserMessage(question))

	trace := &LoopTrace{StartedAt: time.Now()}
	result := &LoopResult{Trace: trace, Ledger: ledger}
	phase := "gather"
	if actx.DebateContext != nil {
		phase = "reconsider_gather"
	}
	ts := &terminationState{}
	agentCode := entity.MagiCode(cfg.Code)

	for step := 1; step <= maxSteps; step++ {
		if err := ctx.Err(); err != nil {
			result.Status = LoopStatusCancelled
			result.Err = err
			finalizeTrace(trace, result.Status)
			return result, err
		}
		stepStart := time.Now()
		resp, err := bound.Generate(ctx, messages)
		l.publish(ctx, actx.CaseID, actx.RunID, agentCode, entity.EventModelResponded)
		st := &Step{Index: step, StartedAt: stepStart, Duration: time.Since(stepStart)}
		if err != nil {
			result.Status = LoopStatusError
			result.Err = err
			trace.Steps = append(trace.Steps, st)
			finalizeTrace(trace, result.Status)
			return result, err
		}
		st.ModelOutput = resp
		st.ModelUsage = extractUsage(resp)
		result.Usage = addUsage(result.Usage, st.ModelUsage)
		ts.tokenUsed += st.ModelUsage.TotalTokens
		if checkTermination(ts, cfg.LoopPolicy, &result.Status, &result.Err) {
			trace.Steps = append(trace.Steps, st)
			finalizeTrace(trace, result.Status)
			return result, result.Err
		}

		pr := parseResponse(resp, phase, l.summaryVal, l.voteVal, l.claimVal, l.reflectionVal)

		switch pr.Type {
		case ResponseToolCall:
			messages = append(messages, resp)
			for _, tc := range resp.ToolCalls {
				tcr := ToolCallRecord{ToolCallID: tc.ID, ToolName: tc.Function.Name, Arguments: tc.Function.Arguments}
				l.publish(ctx, actx.CaseID, actx.RunID, agentCode, entity.EventToolCallRequested)
				// Permission Check: toolReg.List(cfg.Tools) already filtered tools to
				// cfg.ToolBindings. nameToDef only contains permitted tools. A tool call
				// to a non-permitted tool falls into the !ok branch below (tool not found).
				td, ok := nameToDef[tc.Function.Name]
				if !ok {
					tcr.Err = "tool not found: " + tc.Function.Name
					messages = append(messages, schema.ToolMessage(tcr.Err, tc.ID))
					ts.consecToolFail++
					st.ToolCalls = append(st.ToolCalls, tcr)
					continue
				}
				// Validate args
				vr := l.validator.Validate(td.ArgsSchema, []byte(tc.Function.Arguments))
				if vr != nil && !vr.Valid {
					tcr.Valid = false
					tcr.Violations = vr.Violations
					tcr.Err = vr.Error()
					ts.consecToolFail++
					messages = append(messages, schema.ToolMessage(fmt.Sprintf("tool %s args invalid: %s", tc.Function.Name, vr.Error()), tc.ID))
					st.ToolCalls = append(st.ToolCalls, tcr)
					continue
				}
				l.publish(ctx, actx.CaseID, actx.RunID, agentCode, entity.EventToolCallValidated)
				// Execute
				toolStart := time.Now()
				l.publish(ctx, actx.CaseID, actx.RunID, agentCode, entity.EventToolCallStarted)
				execRes, execErr := l.toolExec.Execute(ctx, port.ToolExecutionRequest{ToolName: tc.Function.Name, ArgumentsJSON: tc.Function.Arguments})
				tcr.Duration = time.Since(toolStart)
				if execErr != nil {
					tcr.Err = execErr.Error()
					ts.consecToolFail++
					l.publish(ctx, actx.CaseID, actx.RunID, agentCode, entity.EventToolCallFailed)
					messages = append(messages, schema.ToolMessage(fmt.Sprintf("tool %s failed: %s", tc.Function.Name, execErr.Error()), tc.ID))
					st.ToolCalls = append(st.ToolCalls, tcr)
					continue
				}
				l.publish(ctx, actx.CaseID, actx.RunID, agentCode, entity.EventToolCallCompleted)
				ts.consecToolFail = 0
				tcr.Valid = true
				tcr.Result = execRes.Output
				// Evidence
				candidates, _ := l.adapter.Extract(ctx, td, execRes)
				for _, c := range candidates {
					ev := ledger.Record(tc.ID, tc.Function.Name, string(td.Source), c.SourceURI, c.Observation, c.Reliability)
					if ev != nil {
						tcr.EvidenceID = ev.ID
						l.publish(ctx, actx.CaseID, actx.RunID, agentCode, entity.EventEvidenceCreated)
					}
				}
				messages = append(messages, schema.ToolMessage(execRes.Output, tc.ID))
				st.ToolCalls = append(st.ToolCalls, tcr)
			}
			trace.Steps = append(trace.Steps, st)
			if checkTermination(ts, cfg.LoopPolicy, &result.Status, &result.Err) {
				finalizeTrace(trace, result.Status)
				return result, result.Err
			}
			continue

		case ResponseClaimSubmission:
			// Incremental claim submission: validate EV-IDs, record claims, continue gather.
			messages = append(messages, resp)
			if pr.Claims != nil {
				for _, c := range pr.Claims.Claims {
					// Validate that cited EV-IDs exist in the ledger.
					validEVs := true
					for _, evID := range c.Supports {
						if !ledger.ExistsCollected(evID, "") {
							validEVs = false
							break
						}
					}
					if validEVs {
						ledger.RecordClaim(c.Statement, c.Supports, c.Contradicts)
						l.publish(ctx, actx.CaseID, actx.RunID, agentCode, entity.EventClaimCreated)
					}
				}
			}
			messages = append(messages, schema.UserMessage("Claims recorded. Continue investigating or output EvidenceSummary when ready."))
			trace.Steps = append(trace.Steps, st)
			continue

		case ResponseEvidenceSummary:
			gateRes := l.gate.Evaluate(pr.Summary, ledger, evidenceStd, cfg.Code)
			if !gateRes.Passed {
				l.publish(ctx, actx.CaseID, actx.RunID, agentCode, entity.EventEvidenceGateFailed)
				ts.gateFail++
				messages = append(messages, resp)
				messages = append(messages, schema.UserMessage("Evidence gate failed: "+gateViolationsMsg(gateRes)+"; gather more evidence or fix your EvidenceSummary."))
				trace.Steps = append(trace.Steps, st)
				if checkTermination(ts, cfg.LoopPolicy, &result.Status, &result.Err) {
					finalizeTrace(trace, result.Status)
					return result, result.Err
				}
				continue
			}
			// Gate passed
			l.publish(ctx, actx.CaseID, actx.RunID, agentCode, entity.EventEvidenceGatePassed)
			result.Summary = pr.Summary
			for _, c := range pr.Summary.Claims {
				ledger.RecordClaim(c.Statement, c.Supports, c.Contradicts)
				l.publish(ctx, actx.CaseID, actx.RunID, agentCode, entity.EventClaimCreated)
			}
			messages = append(messages, resp)
			if actx.DebateContext != nil {
				messages = append(messages, schema.UserMessage("Evidence gate passed. Now output the Reflection JSON."))
				phase = "reconsider_reflect"
			} else {
				messages = append(messages, schema.UserMessage("Evidence gate passed. Now output the Vote JSON."))
				phase = "vote"
			}
			trace.Steps = append(trace.Steps, st)
			continue

		case ResponseReflection:
			result.Reflection = pr.Reflection
			messages = append(messages, resp)
			messages = append(messages, schema.UserMessage("Reflection recorded. Now output the Vote JSON."))
			phase = "vote"
			trace.Steps = append(trace.Steps, st)
			continue

		case ResponseVote:
			if derr := ValidateVoteDimensions(pr.Vote, cfg.Objective); derr != nil {
				messages = append(messages, resp)
				messages = append(messages, schema.UserMessage("Vote dimension check failed: "+derr.Error()+"; fix and output a valid Vote JSON."))
				trace.Steps = append(trace.Steps, st)
				continue
			}
			result.Vote = pr.Vote
			l.publish(ctx, actx.CaseID, actx.RunID, agentCode, entity.EventVoteSubmitted)
			result.Status = LoopStatusCompleted
			st.IsFinal = true
			trace.Steps = append(trace.Steps, st)
			finalizeTrace(trace, result.Status)
			return result, nil

		default: // invalid
			ts.validationFail++
			messages = append(messages, resp)
			messages = append(messages, schema.UserMessage("Invalid response. In gather phase output EvidenceSummary JSON; in vote phase output Vote JSON."))
			trace.Steps = append(trace.Steps, st)
			continue
		}
	}

	result.Status = LoopStatusMaxSteps
	result.Err = ErrMaxSteps
	finalizeTrace(trace, result.Status)
	return result, ErrMaxSteps
}

func gateViolationsMsg(g *evidence.GateResult) string {
	if g == nil {
		return "unknown"
	}
	out := ""
	for i, v := range g.Violations {
		if i > 0 {
			out += "; "
		}
		out += fmt.Sprintf("[%s] %s", v.Code, v.Message)
	}
	return out
}
