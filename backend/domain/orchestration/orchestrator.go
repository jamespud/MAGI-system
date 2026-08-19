package orchestration

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jamespud/magi/backend/domain/consensus"
	"github.com/jamespud/magi/backend/domain/debate"
	"github.com/jamespud/magi/backend/domain/entity"
	"github.com/jamespud/magi/backend/domain/evidence"
	"github.com/jamespud/magi/backend/domain/memory"
	"github.com/jamespud/magi/backend/domain/port"
	"github.com/jamespud/magi/backend/domain/runtime"
	"github.com/jamespud/magi/backend/domain/service"
)

type Orchestrator struct {
	dispatcher   *Dispatcher
	consensus    *consensus.ConsensusEngine
	debate       *debate.DebateEngine
	commander    *service.Commander
	eventPub     port.EventPublisher
	caseRepo     port.CaseRepository
	repo         port.Repository
	knowledge    port.KnowledgePort
	memRepo      port.MemoryRepository
	policy       consensus.ConsensusPolicy
	failPolicy   FailurePolicy
	lastAgentErr error
	configs      []*entity.MagiConfig
	blueprint    *entity.FSMBlueprint
	actions      map[string]ActionHandler
}

type OrchestratorDeps struct {
	AgentLoop            runtime.MagiRuntime
	Consensus            *consensus.ConsensusEngine
	Debate               *debate.DebateEngine
	Commander            *service.Commander
	EventPub             port.EventPublisher
	CaseRepo             port.CaseRepository
	Repo                 port.Repository
	ContextBuilder       *memory.ContextBuilder
	Knowledge            port.KnowledgePort
	MemoryRepo           port.MemoryRepository
	ToolBindingsProvider ToolBindingsProvider
	Configs              []*entity.MagiConfig
	Policy               consensus.ConsensusPolicy
	FailPolicy           FailurePolicy
	Blueprint            *entity.FSMBlueprint
}

func NewOrchestrator(d OrchestratorDeps) *Orchestrator {
	fp := d.FailPolicy
	if fp.Mode == "" {
		fp = DefaultFailurePolicy()
	}
	o := &Orchestrator{
		dispatcher:   NewDispatcher(d.AgentLoop, d.ContextBuilder, WithToolBindingsProvider(d.ToolBindingsProvider)),
		consensus:    d.Consensus,
		debate:       d.Debate,
		commander:    d.Commander,
		eventPub:     d.EventPub,
		caseRepo:     d.CaseRepo,
		repo:         d.Repo,
		knowledge:    d.Knowledge,
		memRepo:      d.MemoryRepo,
		policy:       d.Policy,
		failPolicy:   fp,
		lastAgentErr: nil,
		configs:      d.Configs,
		blueprint:    d.Blueprint,
	}
	o.actions = o.buildActionRegistry()
	return o
}

func (o *Orchestrator) Orchestrate(ctx context.Context, case_ *entity.DecisionCase) (*entity.Resolution, error) {
	if case_ == nil {
		return nil, fmt.Errorf("nil case")
	}
	st := &State{MaxDebate: case_.MaxDebateRounds, Round: 1}
	if st.MaxDebate == 0 {
		st.MaxDebate = 1
	}

	status := case_.Status
	if status == "" {
		status = entity.CaseStatusDraft
	}
	if status == entity.CaseStatusResolved && o.repo != nil {
		if existing, err := o.repo.ResolutionRepo().Get(ctx, case_.ID); err == nil && existing != nil {
			case_.Status = entity.CaseStatusResolved
			return existing, nil
		}
	}
	if status == entity.CaseStatusFailed || status == entity.CaseStatusCancelled || status == entity.CaseStatusTimedOut {
		status = entity.CaseStatusDraft
		case_.Status = status
		if o.caseRepo != nil {
			_ = o.caseRepo.UpdateStatus(ctx, case_.ID, status)
		}
	}
	var prevStatus entity.CaseStatus

	for {
		if o.blueprint != nil && prevStatus != "" && prevStatus != status {
			if violations := o.blueprint.ValidatePath([]string{string(prevStatus), string(status)}); len(violations) > 0 {
				return o.fail(ctx, case_, fmt.Sprintf("fsm blueprint violation: %s", violations[0]))
			}
		}
		next, done, err := o.dispatch(ctx, case_, prevStatus, status, st)
		if errors.Is(err, errCaseEnded) {
			// Legacy "default" branch: unrecognized terminal status surfaces as
			// an error (dispatch already set case_.Status and published).
			return st.Resolution, err
		}
		if err != nil {
			return o.fail(ctx, case_, err.Error())
		}
		prevStatus = status
		if done {
			return st.Resolution, nil
		}
		status = next
		case_.Status = status
		if o.caseRepo != nil {
			_ = o.caseRepo.UpdateStatus(ctx, case_.ID, status)
		}
		o.publish(ctx, case_, entity.EventCaseStatusChanged, map[string]any{"status": string(status), "round": st.Round})
	}
}

func (o *Orchestrator) extractVotes(results []*runtime.LoopResult) []*entity.Vote {
	votes := make([]*entity.Vote, len(results))
	for i, r := range results {
		var cfg *entity.MagiConfig
		if i < len(o.configs) {
			cfg = o.configs[i]
		}
		if o.failPolicy.Mode == "fail_case" && (r == nil || r.Err != nil || r.Vote == nil || r.Status != runtime.LoopStatusCompleted) {
			votes[i] = &entity.Vote{Decision: entity.VoteDecisionAbstain, ReasoningSummary: "agent failed under fail_case policy"}
			// The loop below reports the failure so the durable worker retries
			// or fails the case per its own retry policy.
			o.lastAgentErr = ErrAgentFailed
			continue
		}
		votes[i] = o.failPolicy.HandleFailure(r, cfg)
	}
	return votes
}

// retryFailedAgents re-dispatches agents that did not complete successfully,
// up to the configured RetryLimit. Transient failures (one bad model call,
// a flaky tool) then no longer fabricate ABSTAIN ties; a persistent failure
// still falls through to the failure policy.
func (o *Orchestrator) retryFailedAgents(
	ctx context.Context,
	case_ *entity.DecisionCase,
	task *entity.DecisionTask,
	results []*runtime.LoopResult,
	round int,
	phase string,
) []*runtime.LoopResult {
	limit := o.failPolicy.RetryLimit
	if limit <= 0 || len(results) == 0 {
		return results
	}
	for i, r := range results {
		if isCompleted(r) {
			continue
		}
		if i >= len(o.configs) || o.configs[i] == nil {
			continue
		}
		for attempt := 1; attempt <= limit; attempt++ {
			rr := o.dispatcher.RetryAgent(ctx, case_, task, o.configs[i], round, attempt, phase)
			results[i] = rr
			if isCompleted(rr) {
				break
			}
		}
	}
	return results
}

// persistArtifacts writes agent runs, evidence, claims, and votes for one
// dispatch round. Nil-safe: no-op when Repo is unset (back-compat for tests).
//
// phase is "investigate" or "reconsider"; both occur within the same round
// (round only increments after revote), so the phase is part of the persisted
// ID prefix to keep investigate vs reconsider artifacts distinct.
//
// Evidence/claim IDs are namespaced per agent+round+phase ("<code>-r<round>-<phase>-<id>")
// before persistence: each agent's ledger independently generates EV-001/CL-001,
// which would collide on the shared table's primary key. Supports/contradicts/
// evidence_ids references are rewritten to the namespaced IDs so intra-agent
// links stay consistent. The in-memory ledger is not mutated (copies persisted).
func (o *Orchestrator) persistArtifacts(ctx context.Context, case_ *entity.DecisionCase, results []*runtime.LoopResult, votes []*entity.Vote, round int, phase string) *ArtifactRemap {
	remap := newArtifactRemap()
	if o.repo == nil {
		return remap
	}
	now := time.Now()
	for i, r := range results {
		cfg := o.configAt(i)
		code := codeOf(cfg)
		run := &entity.AgentRun{
			ID:          executionRunID(case_.ID, code, case_.ExecutionAttempt, round, phase),
			CaseID:      case_.ID,
			MagiCode:    code,
			Round:       round,
			Status:      agentRunStatus(r),
			StartedAt:   now,
			CompletedAt: &now,
			Usage:       r.Usage,
			Err:         errStr(r.Err),
		}
		run.Environment = &entity.RunEnvironment{
			ModelName:      cfg.Model.ModelName,
			ModelBaseURL:   cfg.Model.BaseURL,
			KnowledgeIndex: o.knowledge != nil,
			ConfigVersion:  cfg.Version,
		}
		for _, tb := range cfg.Tools {
			run.Environment.Tools = append(run.Environment.Tools, string(tb.Source)+":"+tb.ToolName)
		}
		_ = o.repo.AgentRunRepo().Create(ctx, run)

		// Build the ID remap (old in-memory ID -> namespaced persisted ID) and
		// persist copies so the ledger is left untouched for any later use.
		prefix := executionRunID(case_.ID, code, case_.ExecutionAttempt, round, phase)
		if r.Ledger != nil {
			evidence := r.Ledger.List()
			for _, ev := range evidence {
				newID := prefix + "-" + ev.ID
				remap.AddEvidence(ev.ID, newID)
				cp := *ev
				cp.ID = newID
				cp.CaseID = case_.ID
				cp.AgentRunID = run.ID
				_ = o.repo.EvidenceRepo().Create(ctx, &cp)
			}
			claims := r.Ledger.ListClaims()
			for _, cl := range claims {
				newID := prefix + "-" + cl.ID
				remap.AddClaim(cl.ID, newID)
			}
			for _, cl := range claims {
				cp := *cl
				cp.ID = remap.ClaimMap()[cl.ID]
				cp.CaseID = case_.ID
				cp.AgentRunID = run.ID
				cp.Supports = remapRefs(cl.Supports, remap.EvidenceMap())
				cp.Contradicts = remapRefs(cl.Contradicts, remap.ClaimMap())
				_ = o.repo.ClaimRepo().Create(ctx, &cp)
			}
		}
		// Persist tool-call records from the run trace. The PK is a namespaced
		// counter (the LLM's ToolCallID is stored separately and may collide
		// across agents); evidence_id is remapped to the persisted EV-ID.
		if r.Trace != nil {
			toolIdx := 0
			for _, st := range r.Trace.Steps {
				for _, tc := range st.ToolCalls {
					toolIdx++
					evID := tc.EvidenceID
					if remapped, ok := remap.EvidenceMap()[evID]; ok {
						evID = remapped
					}
					toolCall := &entity.ToolCall{
						ID:         fmt.Sprintf("%s-tc%d", prefix, toolIdx),
						CaseID:     case_.ID,
						AgentRunID: run.ID,
						ToolCallID: tc.ToolCallID,
						ToolName:   tc.ToolName,
						Arguments:  tc.Arguments,
						Valid:      tc.Valid,
						Result:     tc.Result,
						Err:        tc.Err,
						ApprovedBy: tc.ApprovedBy,
						EvidenceID: evID,
						DurationMs: tc.Duration.Milliseconds(),
						CreatedAt:  now,
					}
					_ = o.repo.ToolCallRepo().Create(ctx, toolCall)
				}
			}
		}
		// Remap this agent's vote evidence/claim references too, and link the
		// vote to its agent run so /agents can join votes to agents.
		if i < len(votes) && votes[i] != nil {
			votes[i].EvidenceIDs = remap.RemapList(votes[i].EvidenceIDs)
			votes[i].KeyClaimIDs = remap.RemapList(votes[i].KeyClaimIDs)
			votes[i].AgentRunID = run.ID
		}
	}
	for i, v := range votes {
		if v == nil {
			continue
		}
		cfg := o.configAt(i)
		// Regenerate the vote ID unconditionally: a reconsider result may reuse
		// the investigate Vote pointer, which would otherwise re-insert the old
		// ID and violate the primary key (observed on real runs).
		v.ID = "vote-" + executionRunID(case_.ID, codeOf(cfg), case_.ExecutionAttempt, round, phase)
		v.CaseID = case_.ID
		v.Round = round
		_ = o.repo.VoteRepo().Create(ctx, v)
	}
	return remap
}

func (o *Orchestrator) configAt(i int) *entity.MagiConfig {
	if i < len(o.configs) {
		return o.configs[i]
	}
	return nil
}

func codeOf(c *entity.MagiConfig) entity.MagiCode {
	if c == nil {
		return ""
	}
	return entity.MagiCode(c.Code)
}

func agentRunStatus(r *runtime.LoopResult) entity.AgentRunStatus {
	switch r.Status {
	case runtime.LoopStatusCompleted:
		return entity.AgentRunStatusCompleted
	case runtime.LoopStatusMaxSteps:
		return entity.AgentRunStatusMaxSteps
	case runtime.LoopStatusCancelled:
		return entity.AgentRunStatusCancelled
	case runtime.LoopStatusError,
		runtime.LoopStatusValidationFailed,
		runtime.LoopStatusTokenBudget,
		runtime.LoopStatusToolFailures,
		runtime.LoopStatusGateFailed:
		return entity.AgentRunStatusFailed
	default:
		return entity.AgentRunStatusFailed
	}
}

func errStr(e error) string {
	if e == nil {
		return ""
	}
	return e.Error()
}

// remapRefs rewrites a slice of ID references through a remap table. Unknown
// refs (cross-agent or non-EV-ID strings) are left unchanged.
func remapRefs(refs []string, remap map[string]string) []string {
	if len(refs) == 0 {
		return refs
	}
	out := make([]string, len(refs))
	for i, ref := range refs {
		if newID, ok := remap[ref]; ok {
			out[i] = newID
		} else {
			out[i] = ref
		}
	}
	return out
}

func (o *Orchestrator) collectClaims(results []*runtime.LoopResult) []*entity.Claim {
	var claims []*entity.Claim
	for _, r := range results {
		if r != nil && r.Ledger != nil {
			claims = append(claims, r.Ledger.ListClaims()...)
		}
	}
	return claims
}

func (o *Orchestrator) collectEvidence(results []*runtime.LoopResult) []*entity.EvidenceRecord {
	var evs []*entity.EvidenceRecord
	for _, r := range results {
		if r != nil && r.Ledger != nil {
			evs = append(evs, r.Ledger.List()...)
		}
	}
	return evs
}

func (o *Orchestrator) collectEvidenceIDs(results []*runtime.LoopResult) []string {
	var ids []string
	for _, ev := range o.collectEvidence(results) {
		ids = append(ids, ev.ID)
	}
	return ids
}

func (o *Orchestrator) collectClaimIDs(results []*runtime.LoopResult) []string {
	var ids []string
	for _, cl := range o.collectClaims(results) {
		ids = append(ids, cl.ID)
	}
	return ids
}

func (o *Orchestrator) mergeLedgers(results []*runtime.LoopResult) *evidence.EvidenceLedger {
	merged := evidence.NewEvidenceLedger("", "", "merged")
	for _, r := range results {
		if r == nil || r.Ledger == nil {
			continue
		}
		for _, ev := range r.Ledger.List() {
			merged.Record(ev.ToolCallID, ev.ToolName, string(ev.SourceType), "", ev.Observation, ev.Reliability)
		}
		for _, cl := range r.Ledger.ListClaims() {
			merged.RecordClaim(cl.Statement, cl.Supports, cl.Contradicts)
		}
	}
	return merged
}

// shouldDebate determines whether a split outcome should enter the debate phase.
// First-round splits always go to debate when the policy says FirstSplitGoesToDebate;
// subsequent splits respect the maxDebate round limit.
func (o *Orchestrator) shouldDebate(round int, maxDebate int) bool {
	if round == 1 && o.policy.FirstSplitGoesToDebate {
		return true
	}
	return round < maxDebate
}

// EnforceReflectionRule applies the design §17 four-of-one rule to vote changes
// between rounds. For each agent whose config requires justification and whose
// vote decision changed, it infers a Reflection from the vote diff and validates
// it; an unjustified change is reverted to the previous vote.
func EnforceReflectionRule(prevVotes, newVotes []*entity.Vote, results []*runtime.LoopResult, configs []*entity.MagiConfig, round int) []*entity.Reflection {
	var reflections []*entity.Reflection
	for i := 0; i < len(newVotes) && i < len(prevVotes); i++ {
		pv, nv := prevVotes[i], newVotes[i]
		if pv == nil || nv == nil || pv.Decision == nv.Decision {
			continue
		}
		if i >= len(configs) || !configs[i].ReflectionPolicy.RequireJustification {
			continue
		}
		var r *entity.Reflection
		if i < len(results) && results[i] != nil && results[i].Reflection != nil {
			r = results[i].Reflection
		} else {
			r = debate.InferReflection(pv, nv, round)
		}
		if r == nil {
			continue
		}
		if r.Round == 0 {
			r.Round = round
		}
		if r.CreatedAt.IsZero() {
			r.CreatedAt = time.Now()
		}
		reflections = append(reflections, r)
		var ledger *evidence.EvidenceLedger
		claimIDs := map[string]bool{}
		if i < len(results) && results[i] != nil && results[i].Ledger != nil {
			ledger = results[i].Ledger
			for _, c := range ledger.ListClaims() {
				claimIDs[c.ID] = true
			}
		} else {
			ledger = evidence.NewEvidenceLedger("", "", "")
		}
		requireNew := configs[i].ReflectionPolicy.RequireNewEvidence
		if err := debate.ValidateReflection(r, pv, ledger, claimIDs, requireNew); err != nil {
			newVotes[i] = pv // revert unjustified change
		}
	}
	return reflections
}

func (o *Orchestrator) publish(ctx context.Context, case_ *entity.DecisionCase, et entity.EventType, payload any) {
	if o.eventPub == nil {
		return
	}
	_ = o.eventPub.Publish(ctx, entity.NewEvent(case_.ID, "", nil, et, payload))
}

func (o *Orchestrator) fail(ctx context.Context, case_ *entity.DecisionCase, msg string) (*entity.Resolution, error) {
	o.publish(ctx, case_, entity.EventCaseFailed, map[string]any{"error": msg})
	case_.Status = entity.CaseStatusFailed
	if o.caseRepo != nil {
		_ = o.caseRepo.UpdateStatus(ctx, case_.ID, entity.CaseStatusFailed)
	}
	return nil, fmt.Errorf("%s", msg)
}

// failedAgentReasons collects the failure reasons carried by ABSTAIN votes
// produced through the failure policy (their reasoning starts with
// "agent failed"). Genuine model abstentions do not match and are ignored.
func failedAgentReasons(votes []*entity.Vote) []string {
	var out []string
	for _, v := range votes {
		if v != nil && v.Decision == entity.VoteDecisionAbstain && strings.HasPrefix(v.ReasoningSummary, "agent failed") {
			out = append(out, v.ReasoningSummary)
		}
	}
	return out
}

func finalDecision(c entity.ConsensusResult) entity.VoteDecision {
	switch c.Outcome {
	case entity.ConsensusStrongApproval, entity.ConsensusMajorityApprovalDissent:
		return entity.VoteDecisionApprove
	case entity.ConsensusStrongRejection, entity.ConsensusMajorityRejectionDissent:
		return entity.VoteDecisionReject
	case entity.ConsensusConditional:
		return entity.VoteDecisionConditionalApprove
	default:
		return entity.VoteDecisionAbstain
	}
}

func voteIDs(votes []*entity.Vote) []string {
	ids := make([]string, 0, len(votes))
	for _, v := range votes {
		ids = append(ids, v.ID)
	}
	return ids
}

func derefVotes(vs []*entity.Vote) []entity.Vote {
	out := make([]entity.Vote, len(vs))
	for i, v := range vs {
		if v != nil {
			out[i] = *v
		}
	}
	return out
}
