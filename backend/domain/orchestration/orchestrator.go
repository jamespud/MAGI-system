package orchestration

import (
	"context"
	"errors"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/jamespud/magi/backend/application/metrics"
	"github.com/jamespud/magi/backend/domain/consensus"
	"github.com/jamespud/magi/backend/domain/debate"
	"github.com/jamespud/magi/backend/domain/entity"
	"github.com/jamespud/magi/backend/domain/evidence"
	"github.com/jamespud/magi/backend/domain/memory"
	"github.com/jamespud/magi/backend/domain/port"
	"github.com/jamespud/magi/backend/domain/runtime"
	"github.com/jamespud/magi/backend/domain/service"
)

// ConsensusEvaluator is the deterministic ballot evaluator the orchestrator
// consults. It is an interface so a test can inject an outcome the orchestrator
// does not understand and prove the case fails closed instead of resolving on an
// uninterpretable verdict.
type ConsensusEvaluator interface {
	EvaluateBallots(ballots consensus.BallotSet, round int, policy consensus.ConsensusPolicy) entity.ConsensusResult
}

type Orchestrator struct {
	dispatcher *Dispatcher
	consensus  ConsensusEvaluator
	debate     *debate.DebateEngine
	commander  *service.Commander
	eventPub   port.EventPublisher
	caseRepo   port.CaseRepository
	repo       port.Repository
	knowledge  port.KnowledgePort
	memRepo    port.MemoryRepository
	policy     consensus.ConsensusPolicy
	failPolicy FailurePolicy
	configs    []*entity.MagiConfig
	blueprint  *entity.FSMBlueprint
	actions    map[string]ActionHandler
	metrics    *metrics.Registry
}

type OrchestratorDeps struct {
	AgentLoop            runtime.MagiRuntime
	Consensus            ConsensusEvaluator
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
	Metrics              *metrics.Registry
}

func NewOrchestrator(d OrchestratorDeps) *Orchestrator {
	fp := d.FailPolicy
	if fp.Mode == "" {
		fp = DefaultFailurePolicy()
	}
	o := &Orchestrator{
		dispatcher: NewDispatcher(d.AgentLoop, d.ContextBuilder, WithToolBindingsProvider(d.ToolBindingsProvider)),
		consensus:  d.Consensus,
		debate:     d.Debate,
		commander:  d.Commander,
		eventPub:   d.EventPub,
		caseRepo:   d.CaseRepo,
		repo:       d.Repo,
		knowledge:  d.Knowledge,
		memRepo:    d.MemoryRepo,
		policy:     d.Policy,
		failPolicy: fp,
		configs:    d.Configs,
		blueprint:  d.Blueprint,
		metrics:    d.Metrics,
	}
	o.actions = o.buildActionRegistry()
	return o
}

func (o *Orchestrator) Orchestrate(ctx context.Context, case_ *entity.DecisionCase) (*entity.Resolution, error) {
	return o.orchestrate(ctx, case_, nil)
}

// OrchestrateForExecution carries the exact durable Claim owner through every
// authoritative artifact/checkpoint write. A positive generation without this
// identity is not sufficient to authorize persistence.
func (o *Orchestrator) OrchestrateForExecution(ctx context.Context, case_ *entity.DecisionCase, execution *entity.ExecutionContext) (*entity.Resolution, error) {
	if case_ == nil || execution == nil || !execution.IsDurable() ||
		execution.CaseID != case_.ID || execution.ExecutionGeneration != case_.ExecutionGeneration {
		return nil, port.ErrLeaseLost
	}
	return o.orchestrate(ctx, case_, execution)
}

func (o *Orchestrator) orchestrate(ctx context.Context, case_ *entity.DecisionCase, execution *entity.ExecutionContext) (*entity.Resolution, error) {
	if case_ == nil {
		return nil, fmt.Errorf("nil case")
	}
	st := &State{MaxDebate: case_.MaxDebateRounds, Round: 1, Execution: execution}
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
	if status == entity.CaseStatusDeadlocked {
		// DEADLOCKED is a terminal success: replaying an already-deadlocked
		// case must not re-run the terminal commit or duplicate its event.
		return nil, nil
	}
	if status == entity.CaseStatusFailed || status == entity.CaseStatusCancelled || status == entity.CaseStatusTimedOut {
		return nil, port.ErrLeaseLost
	}
	var prevStatus entity.CaseStatus

	for {
		if o.blueprint != nil && prevStatus != "" && prevStatus != status {
			if violations := o.blueprint.ValidatePath([]string{string(prevStatus), string(status)}); len(violations) > 0 {
				return o.fail(ctx, case_, fmt.Sprintf("fsm blueprint violation: %s", violations[0]))
			}
		}
		next, done, err := o.dispatch(ctx, case_, prevStatus, status, st)
		if err != nil {
			if errors.Is(err, port.ErrLeaseLost) {
				return nil, err
			}
			return o.fail(ctx, case_, err.Error())
		}
		prevStatus = status
		if isNormalTerminal(next) {
			// Normal terminal outcomes are committed atomically: the status
			// transition, the terminal artifacts, and the one ordered
			// completion event land in a single transaction. No
			// CASE_STATUS_CHANGED is published first, so an A2A subscriber can
			// never observe a completed Task before its Resolution exists.
			if err := o.commitTerminal(ctx, case_, status, next, st.Resolution, terminalCompletionEvent(case_, next, st)); err != nil {
				return nil, err
			}
			case_.Status = next
			return st.Resolution, nil
		}
		if done {
			if next != status {
				if err := o.advanceStatus(ctx, case_, []entity.CaseStatus{status}, next, st.Round); err != nil {
					return nil, err
				}
			}
			return st.Resolution, nil
		}
		if err := o.advanceStatus(ctx, case_, []entity.CaseStatus{status}, next, st.Round); err != nil {
			return nil, err
		}
		status = next
	}
}

// confirmCurrentStatus persists an expected->target case transition through
// the conditional writer when available, degrading to a plain update for
// in-memory fakes.
func (o *Orchestrator) confirmCurrentStatus(ctx context.Context, case_ *entity.DecisionCase, expected, target entity.CaseStatus) error {
	if o.caseRepo == nil {
		return nil
	}
	writer, ok := o.caseRepo.(port.ConditionalCaseStatusWriter)
	if ok {
		updated, err := writer.UpdateStatusIfCurrent(ctx, case_.ID, []entity.CaseStatus{expected}, target)
		if err != nil {
			return err
		}
		if !updated {
			return port.ErrLeaseLost
		}
		return nil
	}
	return o.caseRepo.UpdateStatus(ctx, case_.ID, target)
}

// commitTerminal writes the terminal artifacts behind one production
// transaction fence. Test/in-memory repositories retain the historical
// conditional-status fallback so their narrow fakes need no DB transaction.
func (o *Orchestrator) commitTerminal(ctx context.Context, case_ *entity.DecisionCase, expected, target entity.CaseStatus, resolution *entity.Resolution, event entity.MagiEvent) error {
	if committer, ok := o.repo.(port.TerminalCommitter); ok {
		committed, err := committer.CommitTerminal(ctx, case_.ID, expected, target, resolution, &event)
		if err != nil {
			return err
		}
		if !committed {
			return port.ErrLeaseLost
		}
		if live, ok := o.eventPub.(port.LiveEventPublisher); ok {
			if err := live.PublishLive(ctx, event); err != nil {
				log.Printf("orchestrator: live fan-out for terminal case %s degraded: %v", case_.ID, err)
			}
		} else {
			// Custom terminal committers may use an event publisher that lacks a
			// durable-free fanout capability. Preserve the historical callback
			// rather than silently dropping the completion notification.
			_ = o.publish(ctx, case_, event.Type, event.Payload)
		}
		return nil
	}
	// The repository cannot fence the status write, the Resolution row and the
	// completion event in one transaction, so a failure in any of the three
	// steps can leave a terminal case without a resolution, or with one but
	// without its completion event. Reordering cannot fix a path that has no
	// transaction, so a repository that persists decisions must fail closed
	// unless it explicitly opted into the in-memory/test semantics.
	o.metrics.IncCommitFenceFallback(metrics.CommitFenceTerminal)
	// Only a repository that explicitly declares itself non-durable may use this
	// path; anything else fails closed instead of degrading to three independent
	// writes that cannot be rolled back.
	if o.repo == nil {
		// With no aggregate repository nothing can commit a resolution or its
		// completion event, yet a case repository can still persist the terminal
		// status on its own. That is not "nothing durable", so it may only
		// proceed for a run with no persistence at all or for a case repository
		// that explicitly declares itself non-durable.
		if o.caseRepo == nil {
			return nil
		}
		marker, ok := o.caseRepo.(port.NonAtomicTerminalRepository)
		if !ok || !marker.AllowsNonAtomicTerminalCommit() {
			return fmt.Errorf("terminal commit for case %s has a case repository but no aggregate repository; refusing to advance the status without an atomic commit", case_.ID)
		}
		return o.confirmCurrentStatus(ctx, case_, expected, target)
	}
	marker, ok := o.repo.(port.NonAtomicTerminalRepository)
	if !ok || !marker.AllowsNonAtomicTerminalCommit() {
		return fmt.Errorf("terminal commit for case %s requires an atomic TerminalCommitter; refusing to commit the outcome non-atomically", case_.ID)
	}
	log.Printf("orchestrator: case %s: committing terminal outcome non-atomically (explicit in-memory/test repository)", case_.ID)
	// This path cannot roll back, so the reference check must run BEFORE the
	// status advances: otherwise a refused commit would leave the case in a
	// terminal state with no resolution.
	if resolution != nil && o.repo != nil {
		if err := o.verifyResolutionArtifacts(ctx, resolution); err != nil {
			return err
		}
	}
	if err := o.confirmCurrentStatus(ctx, case_, expected, target); err != nil {
		return err
	}
	if resolution != nil && o.repo != nil {
		if err := o.repo.ResolutionRepo().Create(ctx, resolution); err != nil {
			return fmt.Errorf("persist resolution: %w", err)
		}
	}
	return o.publish(ctx, case_, event.Type, event.Payload)
}

// verifyResolutionArtifacts refuses a terminal success whose resolution cites
// artifacts that are not durable. A resolution asserts "these ballots and this
// evidence decided the case"; citing a row that was never persisted would make
// that claim false, so the terminal commit must not happen.
//
// The cited ids embed the execution attempt, so checking them for existence is
// also what keeps a retry from validating against artifacts of an older
// attempt. This is the fallback path's guard; the database-backed
// TerminalCommitter runs the same check inside its terminal transaction.
func (o *Orchestrator) verifyResolutionArtifacts(ctx context.Context, res *entity.Resolution) error {
	if res == nil {
		return nil
	}
	if len(res.VoteIDs) > 0 {
		votes, err := o.repo.VoteRepo().ListByCase(ctx, res.CaseID)
		if err != nil {
			return fmt.Errorf("verify resolution votes: %w", err)
		}
		present := make(map[string]struct{}, len(votes))
		for _, v := range votes {
			if v.ExecutionGeneration == res.ExecutionGeneration {
				present[v.ID] = struct{}{}
			}
		}
		if err := requireAllPresent("vote", res.VoteIDs, present); err != nil {
			return err
		}
	}
	if len(res.KeyEvidenceIDs) > 0 {
		evidence, err := o.repo.EvidenceRepo().ListByCase(ctx, res.CaseID)
		if err != nil {
			return fmt.Errorf("verify resolution evidence: %w", err)
		}
		present := make(map[string]struct{}, len(evidence))
		for _, e := range evidence {
			if e.ExecutionGeneration == res.ExecutionGeneration {
				present[e.ID] = struct{}{}
			}
		}
		if err := requireAllPresent("evidence", res.KeyEvidenceIDs, present); err != nil {
			return err
		}
	}
	if len(res.KeyClaimIDs) > 0 {
		claims, err := o.repo.ClaimRepo().ListByCase(ctx, res.CaseID)
		if err != nil {
			return fmt.Errorf("verify resolution claims: %w", err)
		}
		present := make(map[string]struct{}, len(claims))
		for _, c := range claims {
			if c.ExecutionGeneration == res.ExecutionGeneration {
				present[c.ID] = struct{}{}
			}
		}
		if err := requireAllPresent("claim", res.KeyClaimIDs, present); err != nil {
			return err
		}
	}
	return nil
}

// requireAllPresent reports every cited id that has no persisted row.
func requireAllPresent(kind string, cited []string, present map[string]struct{}) error {
	var missing []string
	seen := make(map[string]struct{}, len(cited))
	for _, id := range cited {
		if id == "" {
			return fmt.Errorf("resolution cites an empty %s id", kind)
		}
		if _, dup := seen[id]; dup {
			continue
		}
		seen[id] = struct{}{}
		if _, ok := present[id]; !ok {
			missing = append(missing, id)
		}
	}
	if len(missing) > 0 {
		return fmt.Errorf("resolution cites %d unpersisted %s id(s): %s", len(missing), kind, strings.Join(missing, ", "))
	}
	return nil
}

// isNormalTerminal reports whether a status is one of the normal terminal
// outcomes committed atomically by commitTerminal. Cancelled, failed, and
// timed-out outcomes use their own (failure) paths.
func isNormalTerminal(status entity.CaseStatus) bool {
	return status == entity.CaseStatusResolved || status == entity.CaseStatusDeadlocked
}

// terminalCompletionEvent builds the single durable completion event published
// with a normal terminal transition.
func terminalCompletionEvent(case_ *entity.DecisionCase, target entity.CaseStatus, st *State) entity.MagiEvent {
	payload := map[string]any{"status": string(target)}
	if target == entity.CaseStatusDeadlocked {
		payload["outcome"] = "deadlocked"
		payload["round"] = st.Round
	}
	return entity.NewEvent(case_.ID, "", nil, entity.EventCaseCompleted, payload)
}

// advanceStatus persists the FSM transition before exposing it in memory or
// events. Database-backed repositories compare against the expected source
// state, so a remote cancellation or other owner transition fences late work.
func (o *Orchestrator) advanceStatus(ctx context.Context, case_ *entity.DecisionCase, from []entity.CaseStatus, to entity.CaseStatus, round int) error {
	allowed := make([]entity.CaseStatus, 0, len(from))
	for _, status := range from {
		switch status {
		case entity.CaseStatusCancelled, entity.CaseStatusFailed, entity.CaseStatusTimedOut:
			continue
		default:
			allowed = append(allowed, status)
			// Historical rows persist the zero-value status even though the
			// orchestrator interprets it as DRAFT. Keep that representation in
			// the compare-and-set set during the rollout.
			if status == entity.CaseStatusDraft {
				allowed = append(allowed, "")
			}
		}
	}
	if len(allowed) == 0 {
		return port.ErrLeaseLost
	}
	event := entity.NewEvent(case_.ID, "", nil, entity.EventCaseStatusChanged,
		map[string]any{"status": string(to), "round": round})
	if committer, ok := o.repo.(port.StatusTransitionCommitter); ok {
		committed, err := committer.CommitStatusTransition(ctx, case_.ID, allowed, to, &event)
		if err != nil {
			return err
		}
		if !committed {
			return port.ErrLeaseLost
		}
		// The transition is durable now: expose it in memory and fan out live.
		// A live fan-out failure cannot roll back the transaction; durable
		// polling replays the committed event, so log it as delivery
		// degradation rather than dropping it silently.
		case_.Status = to
		if live, ok := o.eventPub.(port.LiveEventPublisher); ok {
			if err := live.PublishLive(ctx, event); err != nil {
				log.Printf("orchestrator: live fan-out for case %s status %s degraded: %v", case_.ID, to, err)
			}
		}
		return nil
	}
	// Same signal as the terminal path: the transition and its event cannot be
	// fenced together. Counted rather than logged per transition because
	// standalone and test repositories use this path routinely.
	o.metrics.IncCommitFenceFallback(metrics.CommitFenceStatus)
	if o.caseRepo != nil {
		if writer, ok := o.caseRepo.(port.ConditionalCaseStatusWriter); ok {
			updated, err := writer.UpdateStatusIfCurrent(ctx, case_.ID, allowed, to)
			if err != nil {
				return err
			}
			if !updated {
				return port.ErrLeaseLost
			}
		} else if err := o.caseRepo.UpdateStatus(ctx, case_.ID, to); err != nil {
			return err
		}
	}
	case_.Status = to
	return o.publish(ctx, case_, entity.EventCaseStatusChanged, map[string]any{"status": string(to), "round": round})
}

// collectBallots separates authoritative ballots from non-votes. A failed,
// timed-out, cancelled, missing or invalid result becomes an AgentAbsence and is
// never injected into the ballot set as a synthetic ABSTAIN. The vote slice
// stays index-aligned with results/configs: a nil entry means that agent
// produced no ballot, and State.Absences carries the reason.
func (o *Orchestrator) collectBallots(results []*runtime.LoopResult) ([]*entity.Vote, []entity.AgentAbsence) {
	votes := make([]*entity.Vote, len(results))
	var absences []entity.AgentAbsence
	for i, r := range results {
		vote, absence := o.failPolicy.Classify(r)
		if absence != nil {
			absence.AgentCode = codeOf(o.configAt(i))
			absences = append(absences, *absence)
			continue
		}
		votes[i] = vote
	}
	return votes, absences
}

// absencesMsg renders the non-votes for a case-failure message.
func absencesMsg(absences []entity.AgentAbsence) string {
	parts := make([]string, 0, len(absences))
	for _, a := range absences {
		parts = append(parts, a.String())
	}
	return strings.Join(parts, "; ")
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
	execution *entity.ExecutionContext,
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
			rr := o.dispatcher.RetryAgentForExecution(ctx, case_, task, o.configs[i], round, attempt, phase, execution)
			results[i] = rr
			if isCompleted(rr) {
				break
			}
		}
	}
	return results
}

// persistArtifactValue routes positive-generation writes through the durable
// active-owner repository. Legacy generation-0 callers retain the old
// sub-repository path for standalone/tests only.
func (o *Orchestrator) persistArtifactValue(ctx context.Context, execution *entity.ExecutionContext, value any) error {
	if o.repo == nil {
		return nil
	}
	if execution != nil && execution.IsDurable() {
		owned, ok := o.repo.(port.OwnedArtifactRepository)
		if !ok {
			return port.ErrLeaseLost
		}
		switch v := value.(type) {
		case *entity.AgentRun:
			return owned.CreateAgentRunOwned(ctx, execution, v)
		case *entity.EvidenceRecord:
			return owned.CreateEvidenceOwned(ctx, execution, v)
		case *entity.Claim:
			return owned.CreateClaimOwned(ctx, execution, v)
		case *entity.Vote:
			return owned.CreateVoteOwned(ctx, execution, v)
		case *entity.DebateRound:
			return owned.CreateDebateRoundOwned(ctx, execution, v)
		case *entity.Reflection:
			return owned.CreateReflectionOwned(ctx, execution, v)
		case *entity.ToolCall:
			return owned.CreateToolCallOwned(ctx, execution, v)
		default:
			return fmt.Errorf("unsupported authoritative artifact type %T", value)
		}
	}
	// A positive generation without a complete durable owner would otherwise
	// bypass T2's worker/job/lease predicate through a legacy Create method.
	switch v := value.(type) {
	case *entity.AgentRun:
		if v.ExecutionGeneration > 0 { return port.ErrLeaseLost }
		return o.repo.AgentRunRepo().Create(ctx, v)
	case *entity.EvidenceRecord:
		if v.ExecutionGeneration > 0 { return port.ErrLeaseLost }
		return o.repo.EvidenceRepo().Create(ctx, v)
	case *entity.Claim:
		if v.ExecutionGeneration > 0 { return port.ErrLeaseLost }
		return o.repo.ClaimRepo().Create(ctx, v)
	case *entity.Vote:
		if v.ExecutionGeneration > 0 {
			return port.ErrLeaseLost
		}
		return o.repo.VoteRepo().Create(ctx, v)
	case *entity.DebateRound:
		if v.ExecutionGeneration > 0 { return port.ErrLeaseLost }
		return o.repo.DebateRepo().Create(ctx, v)
	case *entity.Reflection:
		if v.ExecutionGeneration > 0 { return port.ErrLeaseLost }
		return o.repo.ReflectionRepo().Create(ctx, v)
	case *entity.ToolCall:
		if v.ExecutionGeneration > 0 { return port.ErrLeaseLost }
		return o.repo.ToolCallRepo().Create(ctx, v)
	default:
		return fmt.Errorf("unsupported authoritative artifact type %T", value)
	}
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
func (o *Orchestrator) persistArtifacts(ctx context.Context, case_ *entity.DecisionCase, results []*runtime.LoopResult, votes []*entity.Vote, round int, phase string, execution *entity.ExecutionContext) (*ArtifactRemap, error) {
	remap := newArtifactRemap()
	if o.repo == nil {
		return remap, nil
	}
	if case_.ExecutionGeneration > 0 && (execution == nil || !execution.IsDurable() ||
		execution.CaseID != case_.ID || execution.ExecutionGeneration != case_.ExecutionGeneration) {
		return remap, port.ErrLeaseLost
	}
	now := time.Now()
	for i, r := range results {
		cfg := o.configAt(i)
		code := codeOf(cfg)
		run := &entity.AgentRun{
			ID:          executionRunID(case_.ID, code, case_.ExecutionGeneration, round, phase),
			CaseID:      case_.ID,
			ExecutionGeneration: case_.ExecutionGeneration,
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
		if err := o.persistArtifact(ctx, metrics.ArtifactAgentRun, case_.ID, func() error {
			return o.persistArtifactValue(ctx, execution, run)
		}); err != nil {
			return remap, err
		}

		// Build the ID remap (old in-memory ID -> namespaced persisted ID) and
		// persist copies so the ledger is left untouched for any later use.
		prefix := executionRunID(case_.ID, code, case_.ExecutionGeneration, round, phase)
		if r.Ledger != nil {
			evidence := r.Ledger.List()
			for _, ev := range evidence {
				newID := prefix + "-" + ev.ID
				remap.AddEvidence(ev.ID, newID)
				cp := *ev
				cp.ID = newID
				cp.CaseID = case_.ID
				cp.ExecutionGeneration = case_.ExecutionGeneration
				cp.AgentRunID = run.ID
				if err := o.persistArtifact(ctx, metrics.ArtifactEvidence, case_.ID, func() error {
					return o.persistArtifactValue(ctx, execution, &cp)
				}); err != nil {
					return remap, err
				}
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
				cp.ExecutionGeneration = case_.ExecutionGeneration
				cp.AgentRunID = run.ID
				cp.Supports = remapRefs(cl.Supports, remap.EvidenceMap())
				cp.Contradicts = remapRefs(cl.Contradicts, remap.ClaimMap())
				if err := o.persistArtifact(ctx, metrics.ArtifactClaim, case_.ID, func() error {
					return o.persistArtifactValue(ctx, execution, &cp)
				}); err != nil {
					return remap, err
				}
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
						ExecutionGeneration: case_.ExecutionGeneration,
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
					if err := o.persistArtifact(ctx, metrics.ArtifactToolCall, case_.ID, func() error {
						return o.persistArtifactValue(ctx, execution, toolCall)
					}); err != nil {
						return remap, err
					}
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
		v.ID = "vote-" + executionRunID(case_.ID, codeOf(cfg), case_.ExecutionGeneration, round, phase)
		v.CaseID = case_.ID
		v.ExecutionGeneration = case_.ExecutionGeneration
		v.Round = round
		if err := o.persistArtifact(ctx, metrics.ArtifactVote, case_.ID, func() error {
			return o.persistArtifactValue(ctx, execution, v)
		}); err != nil {
			return remap, err
		}
	}
	return remap, nil
}

// persistArtifact runs one durable artifact write. Failures are counted and
// logged rather than silently dropped: a missing vote/evidence/tool-call row is
// an audit hole, and the stock-mcp incident showed it can show up as a complete
// case with no trace of the tool call.
func (o *Orchestrator) persistArtifact(ctx context.Context, kind metrics.ArtifactKind, caseID string, write func() error) error {
	if write == nil {
		return nil
	}
	if err := write(); err != nil {
		log.Printf("orchestrator: persist %s for case %s failed: %v", kind, caseID, err)
		o.metrics.IncArtifactPersistFailure(kind)
		if errors.Is(err, port.ErrLeaseLost) {
			return port.ErrLeaseLost
		}
	}
	return nil
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

func (o *Orchestrator) publish(ctx context.Context, case_ *entity.DecisionCase, et entity.EventType, payload any) error {
	if o.eventPub == nil {
		return nil
	}
	return o.eventPub.Publish(ctx, entity.NewEvent(case_.ID, "", nil, et, payload))
}

func (o *Orchestrator) fail(ctx context.Context, case_ *entity.DecisionCase, msg string) (*entity.Resolution, error) {
	runErr := fmt.Errorf("%s", msg)
	if isPublicTerminalCaseStatus(case_.Status) {
		if case_.ExecutionAttempt > 0 {
			return nil, runErr
		}
		return nil, port.ErrLeaseLost
	}
	if case_.ExecutionAttempt > 0 {
		return nil, runErr
	}
	event := entity.NewEvent(case_.ID, "", nil, entity.EventCaseFailed,
		map[string]any{"status": string(entity.CaseStatusFailed)})
	if err := o.commitTerminal(ctx, case_, case_.Status, entity.CaseStatusFailed, nil, event); err != nil {
		return nil, err
	}
	case_.Status = entity.CaseStatusFailed
	return nil, runErr
}

func isPublicTerminalCaseStatus(status entity.CaseStatus) bool {
	switch status {
	case entity.CaseStatusResolved, entity.CaseStatusMemoryIndexed, entity.CaseStatusFailed,
		entity.CaseStatusCancelled, entity.CaseStatusTimedOut, entity.CaseStatusInsufficientEv,
		entity.CaseStatusDeadlocked:
		return true
	default:
		return false
	}
}

// failedAgentReasons collects the failure reasons carried by ABSTAIN votes
// produced through the failure policy (their reasoning starts with
// "agent failed"). Genuine model abstentions do not match and are ignored.
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
		if v != nil {
			ids = append(ids, v.ID)
		}
	}
	return ids
}

// ballotCount counts the authoritative ballots, ignoring the nil slots that
// mark a participant which produced none.
func ballotCount(votes []*entity.Vote) int {
	n := 0
	for _, v := range votes {
		if v != nil {
			n++
		}
	}
	return n
}

// derefVotes returns only the authoritative ballots. A nil entry means the
// agent produced no ballot (see State.Absences); a non-vote must never be
// materialised as a zero-value Vote, because an empty decision is not abstain.
func derefVotes(vs []*entity.Vote) []entity.Vote {
	out := make([]entity.Vote, 0, len(vs))
	for _, v := range vs {
		if v != nil {
			out = append(out, *v)
		}
	}
	return out
}
