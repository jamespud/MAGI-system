package orchestration

import (
	"context"
	"fmt"

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
	dispatcher *Dispatcher
	consensus  *consensus.ConsensusEngine
	debate     *debate.DebateEngine
	commander  *service.Commander
	eventPub   port.EventPublisher
	caseRepo   port.CaseRepository
	policy     consensus.ConsensusPolicy
	failPolicy FailurePolicy
	configs    []*entity.MagiConfig
}

type OrchestratorDeps struct {
	AgentLoop  runtime.MagiRuntime
	Consensus  *consensus.ConsensusEngine
	Debate     *debate.DebateEngine
	Commander  *service.Commander
	EventPub   port.EventPublisher
	CaseRepo   port.CaseRepository
	Configs    []*entity.MagiConfig
	Policy     consensus.ConsensusPolicy
	FailPolicy FailurePolicy
}

func NewOrchestrator(d OrchestratorDeps) *Orchestrator {
	fp := d.FailPolicy
	if fp.Mode == "" {
		fp = DefaultFailurePolicy()
	}
	return &Orchestrator{
		dispatcher: NewDispatcher(d.AgentLoop),
		consensus:  d.Consensus,
		debate:     d.Debate,
		commander:  d.Commander,
		eventPub:   d.EventPub,
		caseRepo:   d.CaseRepo,
		policy:     d.Policy,
		failPolicy: fp,
		configs:    d.Configs,
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
	round := 1

	status := case_.Status
	if status == "" {
		status = entity.CaseStatusDraft
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
			status = entity.CaseStatusContextBuilding

		case entity.CaseStatusContextBuilding:
			o.publish(ctx, case_, entity.EventTaskNormalized)
			status = entity.CaseStatusRetrievingMemory

		case entity.CaseStatusRetrievingMemory:
			o.publish(ctx, case_, entity.EventMemoryRetrieved)
			status = entity.CaseStatusInvestigating

		case entity.CaseStatusInvestigating:
			o.publish(ctx, case_, entity.EventAgentStarted)
			results = o.dispatcher.Dispatch(ctx, case_, task, o.configs)
			status = entity.CaseStatusEvidenceGating

		case entity.CaseStatusEvidenceGating:
			o.publish(ctx, case_, entity.EventEvidenceGatePassed)
			status = entity.CaseStatusCollectingVotes

		case entity.CaseStatusCollectingVotes:
			votes = o.extractVotes(results)
			o.publish(ctx, case_, entity.EventVoteSubmitted)
			status = entity.CaseStatusConsensusCheck

		case entity.CaseStatusConsensusCheck:
			consResult = o.consensus.Evaluate(derefVotes(votes), round, o.policy)
			o.publish(ctx, case_, entity.EventConsensusEvaluated)
			switch consResult.Outcome {
			case entity.ConsensusStrongApproval, entity.ConsensusStrongRejection:
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
			o.publish(ctx, case_, entity.EventDebateStarted)
			allClaims := o.collectClaims(results)
			allEvidence := o.collectEvidence(results)
			packet := o.debate.BuildPacket(derefVotes(votes), allClaims, round, allEvidence)
			results = o.dispatcher.DispatchReconsider(ctx, case_, task, packet, results, o.configs)
			status = entity.CaseStatusReflecting

		case entity.CaseStatusReflecting:
			o.publish(ctx, case_, entity.EventReflectionSubmitted)
			status = entity.CaseStatusRevoting

		case entity.CaseStatusRevoting:
			o.publish(ctx, case_, entity.EventRevoteSubmitted)
			votes = o.extractVotes(results)
			round++
			status = entity.CaseStatusConsensusCheck

		case entity.CaseStatusResolving:
			consResult.Round = round
			resolution = &entity.Resolution{
				Consensus:      consResult,
				FinalDecision:  finalDecision(consResult),
				KeyEvidenceIDs: o.collectEvidenceIDs(results),
				KeyClaimIDs:    o.collectClaimIDs(results),
				VoteIDs:        voteIDs(votes),
			}
			status = entity.CaseStatusGeneratingReport

		case entity.CaseStatusGeneratingReport:
			report, err := o.commander.GenerateReport(ctx, case_, &entity.Resolution{Consensus: consResult}, votes)
			if err != nil {
				report = "report generation failed"
			}
			resolution.FinalReport = report
			o.publish(ctx, case_, entity.EventResolutionCreated)
			status = entity.CaseStatusSavingMemory

		case entity.CaseStatusSavingMemory:
			ledger := o.mergeLedgers(results)
			memory.BuildProjection(case_, resolution, ledger, votes)
			o.publish(ctx, case_, entity.EventMemoryIndexed)
			status = entity.CaseStatusEvaluating

		case entity.CaseStatusEvaluating:
			service.Evaluate(results, round, consResult.Outcome)
			status = entity.CaseStatusResolved

		case entity.CaseStatusResolved:
			o.publish(ctx, case_, entity.EventCaseCompleted)
			case_.Status = status
			return resolution, nil

		default:
			o.publish(ctx, case_, entity.EventCaseFailed)
			case_.Status = status
			return resolution, fmt.Errorf("case ended: %s", status)
		}
		case_.Status = status
		if o.caseRepo != nil {
			_ = o.caseRepo.UpdateStatus(ctx, case_.ID, status)
		}
	}
}

func (o *Orchestrator) extractVotes(results []*runtime.LoopResult) []*entity.Vote {
	votes := make([]*entity.Vote, len(results))
	for i, r := range results {
		var cfg *entity.MagiConfig
		if i < len(o.configs) {
			cfg = o.configs[i]
		}
		votes[i] = o.failPolicy.HandleFailure(r, cfg)
	}
	return votes
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

func (o *Orchestrator) publish(ctx context.Context, case_ *entity.DecisionCase, et entity.EventType) {
	if o.eventPub == nil {
		return
	}
	_ = o.eventPub.Publish(ctx, entity.MagiEvent{CaseID: case_.ID, Type: et})
}

func (o *Orchestrator) fail(ctx context.Context, case_ *entity.DecisionCase, msg string) (*entity.Resolution, error) {
	o.publish(ctx, case_, entity.EventCaseFailed)
	case_.Status = entity.CaseStatusFailed
	return nil, fmt.Errorf("%s", msg)
}

func finalDecision(c entity.ConsensusResult) entity.VoteDecision {
	switch c.Outcome {
	case entity.ConsensusStrongApproval, entity.ConsensusMajorityApprovalDissent:
		return entity.VoteDecisionApprove
	case entity.ConsensusStrongRejection, entity.ConsensusMajorityRejectionDissent:
		return entity.VoteDecisionReject
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
