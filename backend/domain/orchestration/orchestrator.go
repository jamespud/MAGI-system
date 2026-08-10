package orchestration

import (
	"context"
	"fmt"
	"log"
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
}

func NewOrchestrator(d OrchestratorDeps) *Orchestrator {
	fp := d.FailPolicy
	if fp.Mode == "" {
		fp = DefaultFailurePolicy()
	}
	return &Orchestrator{
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
	}
}

func (o *Orchestrator) Orchestrate(ctx context.Context, case_ *entity.DecisionCase) (*entity.Resolution, error) {
	if case_ == nil {
		return nil, fmt.Errorf("nil case")
	}
	maxDebate := case_.MaxDebateRounds
	if maxDebate == 0 {
		maxDebate = 1
	}

	var task *entity.DecisionTask
	var results []*runtime.LoopResult
	var votes []*entity.Vote
	var consResult entity.ConsensusResult
	var resolution *entity.Resolution
	var artifactRemap *ArtifactRemap // old in-memory ID -> persisted ID, latest round
	var allRemaps []*ArtifactRemap   // every round's remap, for report/reflection rewriting
	round := 1

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

	for {
		switch status {

		case entity.CaseStatusDraft:
			status = entity.CaseStatusNormalizing

		case entity.CaseStatusNormalizing:
			t, err := o.commander.Normalize(ctx, case_)
			if err != nil {
				return o.fail(ctx, case_, fmt.Sprintf("normalize: %v", err))
			}
			task = t
			if o.caseRepo != nil {
				_ = o.caseRepo.UpdateTask(ctx, case_.ID, task)
			}
			status = entity.CaseStatusContextBuilding

		case entity.CaseStatusContextBuilding:
			o.publish(ctx, case_, entity.EventTaskNormalized, map[string]any{"canonical_question": task.CanonicalQuestion})
			status = entity.CaseStatusRetrievingMemory

		case entity.CaseStatusRetrievingMemory:
			o.publish(ctx, case_, entity.EventMemoryRetrieved, nil)
			status = entity.CaseStatusInvestigating

		case entity.CaseStatusInvestigating:
			o.publish(ctx, case_, entity.EventAgentStarted, map[string]any{"round": round})
			results = o.dispatcher.Dispatch(ctx, case_, task, o.configs, round)
			status = entity.CaseStatusEvidenceGating

		case entity.CaseStatusEvidenceGating:
			o.publish(ctx, case_, entity.EventEvidenceGatePassed, nil)
			status = entity.CaseStatusCollectingVotes

		case entity.CaseStatusCollectingVotes:
			votes = o.extractVotes(results)
			if o.failPolicy.Mode == "fail_case" && o.lastAgentErr != nil {
				return o.fail(ctx, case_, o.lastAgentErr.Error())
			}
			artifactRemap = o.persistArtifacts(ctx, case_, results, votes, round, "investigate")
			allRemaps = append(allRemaps, artifactRemap)
			o.publish(ctx, case_, entity.EventVoteSubmitted, map[string]any{"round": round, "votes": len(votes)})
			status = entity.CaseStatusConsensusCheck

		case entity.CaseStatusConsensusCheck:
			consResult = o.consensus.Evaluate(derefVotes(votes), round, o.policy)
			o.publish(ctx, case_, entity.EventConsensusEvaluated, map[string]any{"outcome": string(consResult.Outcome), "round": round})
			switch consResult.Outcome {
			case entity.ConsensusStrongApproval, entity.ConsensusStrongRejection, entity.ConsensusConditional:
				status = entity.CaseStatusResolving
			case entity.ConsensusMajorityApprovalDissent, entity.ConsensusMajorityRejectionDissent:
				if o.shouldDebate(round, maxDebate) {
					status = entity.CaseStatusDebating
				} else {
					status = entity.CaseStatusResolving
				}
			case entity.ConsensusDeadlock, entity.ConsensusInsufficientQuorum:
				status = entity.CaseStatusDeadlocked
			default:
				status = entity.CaseStatusResolving
			}

		case entity.CaseStatusDebating:
			o.publish(ctx, case_, entity.EventDebateStarted, map[string]any{"round": round})
			allClaims := o.collectClaims(results)
			allEvidence := o.collectEvidence(results)
			packet := o.debate.BuildPacket(derefVotes(votes), allClaims, round, allEvidence)
			if o.repo != nil {
				_ = o.repo.DebateRepo().Create(ctx, &entity.DebateRound{
					ID: fmt.Sprintf("deb-%s-r%d", case_.ID, round), CaseID: case_.ID, Round: round,
					Packet: packet, StartedAt: time.Now(),
				})
			}
			results = o.dispatcher.DispatchReconsider(ctx, case_, task, packet, results, o.configs, round)
			status = entity.CaseStatusReflecting

		case entity.CaseStatusReflecting:
			o.publish(ctx, case_, entity.EventReflectionSubmitted, nil)
			status = entity.CaseStatusRevoting

		case entity.CaseStatusRevoting:
			o.publish(ctx, case_, entity.EventRevoteSubmitted, map[string]any{"round": round})
			newVotes := o.extractVotes(results)
			artifactRemap = o.persistArtifacts(ctx, case_, results, newVotes, round, "reconsider")
			allRemaps = append(allRemaps, artifactRemap)
			reflections := EnforceReflectionRule(votes, newVotes, results, o.configs, round)
			if o.repo != nil {
				for idx, rf := range reflections {
					// IDs must be unique per case/attempt/round/agent: inferred
					// reflections are re-created on every retry attempt.
					rf.ID = fmt.Sprintf("refl-%s-a%d-r%d-%d", case_.ID, case_.ExecutionAttempt, rf.Round, idx)
					if rf.AgentRunID == "" && idx < len(newVotes) && newVotes[idx] != nil {
						rf.AgentRunID = newVotes[idx].AgentRunID
					}
					remapReflection(rf, artifactRemap)
					_ = o.repo.ReflectionRepo().Create(ctx, rf)
				}
			}
			votes = newVotes
			round++
			status = entity.CaseStatusConsensusCheck

		case entity.CaseStatusResolving:
			consResult.Round = round
			// Remap the resolution's cited IDs so the persisted record points
			// at the same namespaced artifacts the vote/evidence tables hold.
			remap := mergeRemaps(allRemaps)
			resolution = &entity.Resolution{
				Consensus:      consResult,
				FinalDecision:  finalDecision(consResult),
				KeyEvidenceIDs: remap.RemapList(o.collectEvidenceIDs(results)),
				KeyClaimIDs:    remap.RemapList(o.collectClaimIDs(results)),
				VoteIDs:        voteIDs(votes),
			}
			resolution.ID = fmt.Sprintf("res-%s", case_.ID)
			resolution.CaseID = case_.ID
			resolution.CreatedAt = time.Now()
			status = entity.CaseStatusGeneratingReport

		case entity.CaseStatusGeneratingReport:
			report, err := o.commander.GenerateReport(ctx, case_, resolution, votes,
				resolution.KeyEvidenceIDs, resolution.KeyClaimIDs)
			if err != nil {
				return o.fail(ctx, case_, fmt.Sprintf("report generation: %v", err))
			}
			// The report body cites IDs as free text; rewrite them to the
			// persisted namespaced forms so citations resolve in the DB.
			resolution.FinalReport = mergeRemaps(allRemaps).RemapText(report)
			o.publish(ctx, case_, entity.EventResolutionCreated, map[string]any{"final_decision": string(resolution.FinalDecision)})
			status = entity.CaseStatusSavingMemory

		case entity.CaseStatusSavingMemory:
			ledger := o.mergeLedgers(results)
			proj := memory.BuildProjection(case_, resolution, ledger, votes, mergeRemaps(allRemaps))
			if o.knowledge != nil {
				// The knowledge adapter owns the MEMORY_INDEXED event: it is
				// published after indexing actually completes (sync) or by the
				// async worker, with chunk counts in the payload.
				_, _ = o.knowledge.Store(ctx, proj)
			}
			if o.memRepo != nil {
				// Persist the projection row itself. The Memory page search
				// reads case_memory_projection (LIKE fallback); RAG chunks are
				// derived from the same projection.
				if err := o.memRepo.Save(ctx, proj); err != nil {
					log.Printf("orchestrator: save memory projection for case %s failed: %v", case_.ID, err)
				}
			}
			status = entity.CaseStatusEvaluating

		case entity.CaseStatusEvaluating:
			resolution.Evaluation = service.Evaluate(results, round, consResult.Outcome)
			status = entity.CaseStatusResolved

		case entity.CaseStatusResolved:
			// Persist the resolution here, after FinalReport (GeneratingReport) and
			// Evaluation (Evaluating) are set. Persisting earlier would snapshot
			// an empty FinalReport.
			if o.repo != nil && resolution != nil {
				if err := o.repo.ResolutionRepo().Create(ctx, resolution); err != nil {
					return o.fail(ctx, case_, fmt.Sprintf("persist resolution: %v", err))
				}
			}
			o.publish(ctx, case_, entity.EventCaseCompleted, map[string]any{"status": string(status)})
			case_.Status = status
			return resolution, nil

		case entity.CaseStatusDeadlocked:
			// Deadlock is a legitimate terminal outcome, not a retryable failure:
			// finish the run without an error so the durable worker marks it done.
			o.publish(ctx, case_, entity.EventCaseCompleted, map[string]any{"status": string(status), "outcome": "deadlocked", "round": round})
			case_.Status = status
			return nil, nil

		default:
			o.publish(ctx, case_, entity.EventCaseFailed, map[string]any{"status": string(status)})
			case_.Status = status
			return resolution, fmt.Errorf("case ended: %s", status)
		}
		case_.Status = status
		if o.caseRepo != nil {
			_ = o.caseRepo.UpdateStatus(ctx, case_.ID, status)
		}
		o.publish(ctx, case_, entity.EventCaseStatusChanged, map[string]any{"status": string(status), "round": round})
	}
}

func (o *Orchestrator) extractVotes(results []*runtime.LoopResult) []*entity.Vote {
	votes := make([]*entity.Vote, len(results))
	for i, r := range results {
		var cfg *entity.MagiConfig
		if i < len(o.configs) {
			cfg = o.configs[i]
		}
		if o.failPolicy.Mode == "fail_case" && (r == nil || r.Err != nil || r.Vote == nil) {
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
