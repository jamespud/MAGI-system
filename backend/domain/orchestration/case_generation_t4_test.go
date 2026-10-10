package orchestration

import (
	"context"
	"errors"
	"testing"

	"github.com/jamespud/magi/backend/domain/entity"
	"github.com/jamespud/magi/backend/domain/port"
)

func TestOwnedCaseWritesNeverFallBackToLegacy(t *testing.T) {
	owner := &entity.ExecutionContext{CaseID: "case", JobID: "job", WorkerID: "worker", ExecutionGeneration: 3}
	// Embedded nil methods panic if a legacy repository API is reached.
	o := &Orchestrator{repo: legacyOnlyRepo{}}
	for _, execution := range []*entity.ExecutionContext{nil, owner} {
		c := &entity.DecisionCase{ID: "case", Status: entity.CaseStatusDraft, ExecutionGeneration: 3}
		if err := o.advanceStatus(context.Background(), c, []entity.CaseStatus{entity.CaseStatusDraft}, entity.CaseStatusNormalizing, 1, execution); !errors.Is(err, port.ErrLeaseLost) {
			t.Fatalf("status error=%v", err)
		}
		event := entity.NewEvent(c.ID, "", nil, entity.EventCaseCompleted, nil)
		if err := o.commitTerminal(context.Background(), c, entity.CaseStatusDraft, entity.CaseStatusResolved, nil, event, execution); !errors.Is(err, port.ErrLeaseLost) {
			t.Fatalf("terminal error=%v", err)
		}
		if c.Status != entity.CaseStatusDraft {
			t.Fatalf("rejected write exposed status %s", c.Status)
		}
	}
}

type t4UnknownCaseRepo struct{ port.Repository }

func (r t4UnknownCaseRepo) CommitStatusTransitionOwned(context.Context, *entity.ExecutionContext, []entity.CaseStatus, entity.CaseStatus, *entity.MagiEvent) (bool, error) {
	return false, port.ErrCommitOutcomeUnknown
}
func (r t4UnknownCaseRepo) CommitTerminalOwned(context.Context, *entity.ExecutionContext, entity.CaseStatus, entity.CaseStatus, *entity.Resolution, *entity.MagiEvent) (bool, error) {
	return false, port.ErrCommitOutcomeUnknown
}
func (r t4UnknownCaseRepo) ResetCaseForRetryOwned(context.Context, *entity.ExecutionContext, []entity.CaseStatus) (bool, error) {
	return false, port.ErrCommitOutcomeUnknown
}

type t4LiveEvents struct{ calls int }

func (p *t4LiveEvents) Publish(context.Context, entity.MagiEvent) error     { p.calls++; return nil }
func (p *t4LiveEvents) PublishLive(context.Context, entity.MagiEvent) error { p.calls++; return nil }

func TestUnknownCaseCommitDoesNotPublishOrExposeSuccess(t *testing.T) {
	publisher := &t4LiveEvents{}
	o := &Orchestrator{repo: t4UnknownCaseRepo{}, eventPub: publisher}
	owner := &entity.ExecutionContext{CaseID: "case", JobID: "job", WorkerID: "worker", ExecutionGeneration: 3}
	c := &entity.DecisionCase{ID: "case", Status: entity.CaseStatusDraft, ExecutionGeneration: 3}
	if err := o.advanceStatus(context.Background(), c, []entity.CaseStatus{entity.CaseStatusDraft}, entity.CaseStatusNormalizing, 1, owner); !errors.Is(err, port.ErrCommitOutcomeUnknown) {
		t.Fatalf("status error=%v", err)
	}
	event := entity.NewEvent(c.ID, "", nil, entity.EventCaseCompleted, nil)
	if err := o.commitTerminal(context.Background(), c, entity.CaseStatusDraft, entity.CaseStatusResolved, nil, event, owner); !errors.Is(err, port.ErrCommitOutcomeUnknown) {
		t.Fatalf("terminal error=%v", err)
	}
	if publisher.calls != 0 || c.Status != entity.CaseStatusDraft {
		t.Fatalf("unknown commit published=%d status=%s", publisher.calls, c.Status)
	}
}
