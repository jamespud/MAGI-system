package orchestration

import (
	"context"
	"errors"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/jamespud/magi/backend/domain/entity"
	"github.com/jamespud/magi/backend/domain/memory"
	"github.com/jamespud/magi/backend/domain/runtime"
	"github.com/jamespud/magi/backend/domain/service"
)

// State carries the shared working memory across FSM steps. It replaces the
// local variables that used to live inside the Orchestrate loop, so each step
// (and later the table-driven action dispatch) operates on one explicit state.
type State struct {
	MaxDebate  int
	Task       *entity.DecisionTask
	Results    []*runtime.LoopResult
	Votes      []*entity.Vote
	ConsResult entity.ConsensusResult
	Resolution *entity.Resolution
	AllRemaps  []*ArtifactRemap
	Round      int
}

// errCaseEnded marks an unrecognized terminal status (behavior-preserving
// sentinel for the legacy "default" branch).
var errCaseEnded = errors.New("case ended")

// ActionHandler is the deterministic business step executed for an FSM action.
// It mirrors the old switch-case branch: returns the next status, whether the
// FSM terminates after this step, and an error (surfaced as a case failure).
type ActionHandler func(ctx context.Context, case_ *entity.DecisionCase, st *State) (entity.CaseStatus, bool, error)

// buildActionRegistry returns the action -> handler table. Every action name
// returned by entity.KnownFSMActions is registered here, so a blueprint that
// passes save-time validation always resolves to a real handler.
func (o *Orchestrator) buildActionRegistry() map[string]ActionHandler {
	return map[string]ActionHandler{
		"start":           o.stepStart,
		"normalize":       o.stepNormalize,
		"build_context":   o.stepBuildContext,
		"retrieve_memory": o.stepRetrieveMemory,
		"investigate":     o.stepInvestigate,
		"gate_evidence":   o.stepGateEvidence,
		"collect_votes":   o.stepCollectVotes,
		"check_consensus": o.stepCheckConsensus,
		"debate":          o.stepDebate,
		"reflect":         o.stepReflect,
		"revote":          o.stepRevote,
		"resolve":         o.stepResolve,
		"generate_report": o.stepGenerateReport,
		"save_memory":     o.stepSaveMemory,
		"evaluate":        o.stepEvaluate,
		"complete":        o.stepComplete,
		"deadlock":        o.stepDeadlock,
	}
}

// dispatch resolves the action for the transition into status and executes its
// registered handler (table-driven NLAH dispatch). The action comes from the
// FSM blueprint when one declares it for this transition, otherwise it falls
// back to the canonical action bound to the target status. An unregistered
// action fails fast; an unrecognized status preserves the legacy default.
func (o *Orchestrator) dispatch(ctx context.Context, case_ *entity.DecisionCase, prevStatus, status entity.CaseStatus, st *State) (entity.CaseStatus, bool, error) {
	action := entity.ActionForStatus(string(status))
	if action == "" {
		// Unrecognized status: preserve the legacy "default" branch behavior.
		o.publish(ctx, case_, entity.EventCaseFailed, map[string]any{"status": string(status)})
		case_.Status = status
		return status, false, fmt.Errorf("%w: %s", errCaseEnded, status)
	}
	if o.blueprint != nil && prevStatus != "" {
		if declared := o.blueprint.ActionFor(string(prevStatus), string(status)); declared != "" {
			action = declared
		}
	}
	handler := o.actions[action]
	if handler == nil {
		return "", false, fmt.Errorf("fsm blueprint action %q is not a registered handler for status %s", action, status)
	}
	return handler(ctx, case_, st)
}

func (o *Orchestrator) stepStart(ctx context.Context, case_ *entity.DecisionCase, st *State) (entity.CaseStatus, bool, error) {
	return entity.CaseStatusNormalizing, false, nil
}

func (o *Orchestrator) stepNormalize(ctx context.Context, case_ *entity.DecisionCase, st *State) (entity.CaseStatus, bool, error) {
	t, err := o.commander.Normalize(ctx, case_)
	if err != nil {
		return "", false, fmt.Errorf("normalize: %w", err)
	}
	st.Task = t
	if o.caseRepo != nil {
		_ = o.caseRepo.UpdateTask(ctx, case_.ID, t)
	}
	return entity.CaseStatusContextBuilding, false, nil
}

func (o *Orchestrator) stepBuildContext(ctx context.Context, case_ *entity.DecisionCase, st *State) (entity.CaseStatus, bool, error) {
	o.publish(ctx, case_, entity.EventTaskNormalized, map[string]any{"canonical_question": st.Task.CanonicalQuestion})
	return entity.CaseStatusRetrievingMemory, false, nil
}

func (o *Orchestrator) stepRetrieveMemory(ctx context.Context, case_ *entity.DecisionCase, st *State) (entity.CaseStatus, bool, error) {
	o.publish(ctx, case_, entity.EventMemoryRetrieved, nil)
	return entity.CaseStatusInvestigating, false, nil
}

func (o *Orchestrator) stepInvestigate(ctx context.Context, case_ *entity.DecisionCase, st *State) (entity.CaseStatus, bool, error) {
	o.publish(ctx, case_, entity.EventAgentStarted, map[string]any{"round": st.Round})
	st.Results = o.dispatcher.Dispatch(ctx, case_, st.Task, o.configs, st.Round)
	st.Results = o.retryFailedAgents(ctx, case_, st.Task, st.Results, st.Round, "investigate")
	return entity.CaseStatusEvidenceGating, false, nil
}

func (o *Orchestrator) stepGateEvidence(ctx context.Context, case_ *entity.DecisionCase, st *State) (entity.CaseStatus, bool, error) {
	o.publish(ctx, case_, entity.EventEvidenceGatePassed, nil)
	return entity.CaseStatusCollectingVotes, false, nil
}

func (o *Orchestrator) stepCollectVotes(ctx context.Context, case_ *entity.DecisionCase, st *State) (entity.CaseStatus, bool, error) {
	st.Votes = o.extractVotes(st.Results)
	if o.failPolicy.Mode == "fail_case" && o.lastAgentErr != nil {
		return "", false, errors.New(o.lastAgentErr.Error())
	}
	remap := o.persistArtifacts(ctx, case_, st.Results, st.Votes, st.Round, "investigate")
	st.AllRemaps = append(st.AllRemaps, remap)
	o.publish(ctx, case_, entity.EventVoteSubmitted, map[string]any{"round": st.Round, "votes": len(st.Votes)})
	return entity.CaseStatusConsensusCheck, false, nil
}

func (o *Orchestrator) stepCheckConsensus(ctx context.Context, case_ *entity.DecisionCase, st *State) (entity.CaseStatus, bool, error) {
	st.ConsResult = o.consensus.Evaluate(derefVotes(st.Votes), st.Round, o.policy)
	o.publish(ctx, case_, entity.EventConsensusEvaluated, map[string]any{"outcome": string(st.ConsResult.Outcome), "round": st.Round})
	switch st.ConsResult.Outcome {
	case entity.ConsensusStrongApproval, entity.ConsensusStrongRejection, entity.ConsensusConditional:
		return entity.CaseStatusResolving, false, nil
	case entity.ConsensusMajorityApprovalDissent, entity.ConsensusMajorityRejectionDissent:
		if o.shouldDebate(st.Round, st.MaxDebate) {
			return entity.CaseStatusDebating, false, nil
		}
		return entity.CaseStatusResolving, false, nil
	case entity.ConsensusDeadlock, entity.ConsensusInsufficientQuorum:
		// A tie caused by agent FAILURES is not a genuine deadlock: it means
		// the decision could not be reached because an agent malfunctioned.
		// Surface that as a case failure instead of a misleading DEADLOCKED.
		if reasons := failedAgentReasons(st.Votes); len(reasons) > 0 {
			return "", false, fmt.Errorf("deadlock caused by agent failures: %s", strings.Join(reasons, "; "))
		}
		return entity.CaseStatusDeadlocked, false, nil
	default:
		return entity.CaseStatusResolving, false, nil
	}
}

func (o *Orchestrator) stepDebate(ctx context.Context, case_ *entity.DecisionCase, st *State) (entity.CaseStatus, bool, error) {
	o.publish(ctx, case_, entity.EventDebateStarted, map[string]any{"round": st.Round})
	allClaims := o.collectClaims(st.Results)
	allEvidence := o.collectEvidence(st.Results)
	packet := o.debate.BuildPacket(derefVotes(st.Votes), allClaims, st.Round, allEvidence)
	if o.repo != nil {
		_ = o.repo.DebateRepo().Create(ctx, &entity.DebateRound{
			ID: fmt.Sprintf("deb-%s-r%d", case_.ID, st.Round), CaseID: case_.ID, Round: st.Round,
			Packet: packet, StartedAt: time.Now(),
		})
	}
	st.Results = o.dispatcher.DispatchReconsider(ctx, case_, st.Task, packet, st.Results, o.configs, st.Round)
	st.Results = o.retryFailedAgents(ctx, case_, st.Task, st.Results, st.Round, "reconsider")
	return entity.CaseStatusReflecting, false, nil
}

func (o *Orchestrator) stepReflect(ctx context.Context, case_ *entity.DecisionCase, st *State) (entity.CaseStatus, bool, error) {
	o.publish(ctx, case_, entity.EventReflectionSubmitted, nil)
	return entity.CaseStatusRevoting, false, nil
}

func (o *Orchestrator) stepRevote(ctx context.Context, case_ *entity.DecisionCase, st *State) (entity.CaseStatus, bool, error) {
	o.publish(ctx, case_, entity.EventRevoteSubmitted, map[string]any{"round": st.Round})
	newVotes := o.extractVotes(st.Results)
	remap := o.persistArtifacts(ctx, case_, st.Results, newVotes, st.Round, "reconsider")
	st.AllRemaps = append(st.AllRemaps, remap)
	reflections := EnforceReflectionRule(st.Votes, newVotes, st.Results, o.configs, st.Round)
	if o.repo != nil {
		for idx, rf := range reflections {
			// IDs must be unique per case/attempt/round/agent: inferred
			// reflections are re-created on every retry attempt.
			rf.ID = fmt.Sprintf("refl-%s-a%d-r%d-%d", case_.ID, case_.ExecutionAttempt, rf.Round, idx)
			if rf.AgentRunID == "" && idx < len(newVotes) && newVotes[idx] != nil {
				rf.AgentRunID = newVotes[idx].AgentRunID
			}
			remapReflection(rf, remap)
			_ = o.repo.ReflectionRepo().Create(ctx, rf)
		}
	}
	st.Votes = newVotes
	st.Round++
	return entity.CaseStatusConsensusCheck, false, nil
}

func (o *Orchestrator) stepResolve(ctx context.Context, case_ *entity.DecisionCase, st *State) (entity.CaseStatus, bool, error) {
	st.ConsResult.Round = st.Round
	// Remap the resolution's cited IDs so the persisted record points at the
	// same namespaced artifacts the vote/evidence tables hold.
	remap := mergeRemaps(st.AllRemaps)
	res := &entity.Resolution{
		Consensus:      st.ConsResult,
		FinalDecision:  finalDecision(st.ConsResult),
		KeyEvidenceIDs: remap.RemapList(o.collectEvidenceIDs(st.Results)),
		KeyClaimIDs:    remap.RemapList(o.collectClaimIDs(st.Results)),
		VoteIDs:        voteIDs(st.Votes),
	}
	res.ID = fmt.Sprintf("res-%s", case_.ID)
	res.CaseID = case_.ID
	res.CreatedAt = time.Now()
	st.Resolution = res
	return entity.CaseStatusGeneratingReport, false, nil
}

func (o *Orchestrator) stepGenerateReport(ctx context.Context, case_ *entity.DecisionCase, st *State) (entity.CaseStatus, bool, error) {
	report, err := o.commander.GenerateReport(ctx, case_, st.Resolution, st.Votes,
		st.Resolution.KeyEvidenceIDs, st.Resolution.KeyClaimIDs)
	if err != nil {
		return "", false, fmt.Errorf("report generation: %w", err)
	}
	// The report body cites IDs as free text; rewrite them to the persisted
	// namespaced forms so citations resolve in the DB.
	st.Resolution.FinalReport = mergeRemaps(st.AllRemaps).RemapText(report)
	o.publish(ctx, case_, entity.EventResolutionCreated, map[string]any{"final_decision": string(st.Resolution.FinalDecision)})
	return entity.CaseStatusSavingMemory, false, nil
}

func (o *Orchestrator) stepSaveMemory(ctx context.Context, case_ *entity.DecisionCase, st *State) (entity.CaseStatus, bool, error) {
	ledger := o.mergeLedgers(st.Results)
	proj := memory.BuildProjection(case_, st.Resolution, ledger, st.Votes, mergeRemaps(st.AllRemaps))
	if o.knowledge != nil {
		// The knowledge adapter owns the MEMORY_INDEXED event: it is published
		// after indexing actually completes (sync) or by the async worker.
		_, _ = o.knowledge.Store(ctx, proj)
	}
	if o.memRepo != nil {
		// Persist the projection row itself. The Memory page search reads
		// case_memory_projection (LIKE fallback); RAG chunks derive from it.
		if err := o.memRepo.Save(ctx, proj); err != nil {
			log.Printf("orchestrator: save memory projection for case %s failed: %v", case_.ID, err)
		}
	}
	return entity.CaseStatusEvaluating, false, nil
}

func (o *Orchestrator) stepEvaluate(ctx context.Context, case_ *entity.DecisionCase, st *State) (entity.CaseStatus, bool, error) {
	st.Resolution.Evaluation = service.Evaluate(st.Results, st.Round, st.ConsResult.Outcome)
	return entity.CaseStatusResolved, false, nil
}

func (o *Orchestrator) stepComplete(ctx context.Context, case_ *entity.DecisionCase, st *State) (entity.CaseStatus, bool, error) {
	// Persist the resolution here, after FinalReport (GeneratingReport) and
	// Evaluation (Evaluating) are set. Persisting earlier would snapshot an
	// empty FinalReport.
	if o.repo != nil && st.Resolution != nil {
		if err := o.repo.ResolutionRepo().Create(ctx, st.Resolution); err != nil {
			return "", false, fmt.Errorf("persist resolution: %w", err)
		}
	}
	o.publish(ctx, case_, entity.EventCaseCompleted, map[string]any{"status": string(entity.CaseStatusResolved)})
	case_.Status = entity.CaseStatusResolved
	return entity.CaseStatusResolved, true, nil
}

func (o *Orchestrator) stepDeadlock(ctx context.Context, case_ *entity.DecisionCase, st *State) (entity.CaseStatus, bool, error) {
	// Deadlock is a legitimate terminal outcome, not a retryable failure:
	// finish the run without an error so the durable worker marks it done.
	o.publish(ctx, case_, entity.EventCaseCompleted, map[string]any{"status": string(entity.CaseStatusDeadlocked), "outcome": "deadlocked", "round": st.Round})
	case_.Status = entity.CaseStatusDeadlocked
	return entity.CaseStatusDeadlocked, true, nil
}
