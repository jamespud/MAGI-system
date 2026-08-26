package a2aapp

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
	"time"

	a2a "github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/jamespud/magi/backend/application/redact"
	"github.com/jamespud/magi/backend/domain/entity"
)

func jobWith(status entity.DecisionJobStatus) *entity.DecisionJob {
	return &entity.DecisionJob{ID: "job-1", CaseID: "case-1", Status: status}
}

func allCaseStatuses() []entity.CaseStatus {
	return []entity.CaseStatus{
		entity.CaseStatusDraft, entity.CaseStatusNormalizing, entity.CaseStatusContextBuilding,
		entity.CaseStatusRetrievingMemory, entity.CaseStatusInvestigating, entity.CaseStatusEvidenceGating,
		entity.CaseStatusCollectingVotes, entity.CaseStatusConsensusCheck, entity.CaseStatusResolving,
		entity.CaseStatusGeneratingReport, entity.CaseStatusSavingMemory, entity.CaseStatusEvaluating,
		entity.CaseStatusDebating, entity.CaseStatusReflecting, entity.CaseStatusRevoting,
		entity.CaseStatusResolved, entity.CaseStatusMemoryIndexed, entity.CaseStatusFailed,
		entity.CaseStatusCancelled, entity.CaseStatusTimedOut, entity.CaseStatusInsufficientEv,
		entity.CaseStatusDeadlocked, entity.CaseStatusPaused,
	}
}

func allJobStates() []*entity.DecisionJob {
	return []*entity.DecisionJob{
		nil,
		jobWith(entity.DecisionJobQueued),
		jobWith(entity.DecisionJobRunning),
		jobWith(entity.DecisionJobSucceeded),
		jobWith(entity.DecisionJobFailed),
		jobWith(entity.DecisionJobCancelled),
		jobWith(entity.DecisionJobPaused),
	}
}

func TestClassifyState_DesignTable(t *testing.T) {
	tests := []struct {
		name       string
		binding    SubmissionState
		cs         entity.CaseStatus
		job        *entity.DecisionJob
		want       a2a.TaskState
		wantPaused bool
	}{
		{"prepared draft queued submits", SubmissionPrepared, entity.CaseStatusDraft, jobWith(entity.DecisionJobQueued), a2a.TaskStateSubmitted, false},
		{"prepared draft no job submits", SubmissionPrepared, entity.CaseStatusDraft, nil, a2a.TaskStateSubmitted, false},
		{"prepared draft running works", SubmissionPrepared, entity.CaseStatusDraft, jobWith(entity.DecisionJobRunning), a2a.TaskStateWorking, false},
		{"started draft no job submits", SubmissionStarted, entity.CaseStatusDraft, nil, a2a.TaskStateSubmitted, false},
		{"started draft running works", SubmissionStarted, entity.CaseStatusDraft, jobWith(entity.DecisionJobRunning), a2a.TaskStateWorking, false},
		{"investigating running works", SubmissionStarted, entity.CaseStatusInvestigating, jobWith(entity.DecisionJobRunning), a2a.TaskStateWorking, false},
		{"paused case works paused", SubmissionStarted, entity.CaseStatusPaused, jobWith(entity.DecisionJobPaused), a2a.TaskStateWorking, true},
		{"paused job works paused", SubmissionStarted, entity.CaseStatusInvestigating, jobWith(entity.DecisionJobPaused), a2a.TaskStateWorking, true},
		{"resolved completes", SubmissionStarted, entity.CaseStatusResolved, jobWith(entity.DecisionJobRunning), a2a.TaskStateCompleted, false},
		{"memory indexed completes", SubmissionStarted, entity.CaseStatusMemoryIndexed, jobWith(entity.DecisionJobRunning), a2a.TaskStateCompleted, false},
		{"insufficient evidence completes", SubmissionStarted, entity.CaseStatusInsufficientEv, jobWith(entity.DecisionJobRunning), a2a.TaskStateCompleted, false},
		{"deadlocked completes", SubmissionStarted, entity.CaseStatusDeadlocked, jobWith(entity.DecisionJobRunning), a2a.TaskStateCompleted, false},
		{"cancelled case beats succeeded job", SubmissionStarted, entity.CaseStatusCancelled, jobWith(entity.DecisionJobSucceeded), a2a.TaskStateCanceled, false},
		{"failed case beats succeeded job", SubmissionStarted, entity.CaseStatusFailed, jobWith(entity.DecisionJobSucceeded), a2a.TaskStateFailed, false},
		{"timed out fails", SubmissionStarted, entity.CaseStatusTimedOut, jobWith(entity.DecisionJobSucceeded), a2a.TaskStateFailed, false},
		{"job succeeded completes processing", SubmissionStarted, entity.CaseStatusInvestigating, jobWith(entity.DecisionJobSucceeded), a2a.TaskStateCompleted, false},
		{"job failed fails processing", SubmissionStarted, entity.CaseStatusInvestigating, jobWith(entity.DecisionJobFailed), a2a.TaskStateFailed, false},
		{"job cancelled cancels processing", SubmissionStarted, entity.CaseStatusInvestigating, jobWith(entity.DecisionJobCancelled), a2a.TaskStateCanceled, false},
		{"rejected binding rejects", SubmissionRejected, entity.CaseStatusDraft, nil, a2a.TaskStateRejected, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, paused := ClassifyState(tt.binding, tt.cs, tt.job)
			if got != tt.want || paused != tt.wantPaused {
				t.Fatalf("ClassifyState(%s,%s,%v) = (%s,%v), want (%s,%v)", tt.binding, tt.cs, tt.job, got, paused, tt.want, tt.wantPaused)
			}
		})
	}
}

func TestClassifyState_EveryCaseStatus(t *testing.T) {
	processing := map[entity.CaseStatus]bool{}
	for _, cs := range processingCaseStatuses() {
		processing[cs] = true
	}
	for _, cs := range allCaseStatuses() {
		job := jobWith(entity.DecisionJobRunning)
		want, wantPaused := a2a.TaskStateWorking, false
		switch cs {
		case entity.CaseStatusResolved, entity.CaseStatusMemoryIndexed, entity.CaseStatusInsufficientEv, entity.CaseStatusDeadlocked:
			want = a2a.TaskStateCompleted
		case entity.CaseStatusFailed, entity.CaseStatusTimedOut:
			want = a2a.TaskStateFailed
		case entity.CaseStatusCancelled:
			want = a2a.TaskStateCanceled
		case entity.CaseStatusPaused:
			wantPaused = true
		case entity.CaseStatusDraft:
			want = a2a.TaskStateWorking // STARTED + DRAFT + running job
		}
		got, paused := ClassifyState(SubmissionStarted, cs, job)
		if got != want || paused != wantPaused {
			t.Fatalf("case %s: got (%s,%v), want (%s,%v)", cs, got, paused, want, wantPaused)
		}
	}
}

func TestClassifyState_EveryJobStatus(t *testing.T) {
	for _, job := range allJobStates() {
		want, wantPaused := a2a.TaskStateWorking, false
		if job != nil {
			switch job.Status {
			case entity.DecisionJobSucceeded:
				want = a2a.TaskStateCompleted
			case entity.DecisionJobFailed:
				want = a2a.TaskStateFailed
			case entity.DecisionJobCancelled:
				want = a2a.TaskStateCanceled
			case entity.DecisionJobPaused:
				wantPaused = true
			}
		}
		got, paused := ClassifyState(SubmissionStarted, entity.CaseStatusInvestigating, job)
		if got != want || paused != wantPaused {
			t.Fatalf("job %v: got (%s,%v), want (%s,%v)", job, got, paused, want, wantPaused)
		}
	}
}

func TestStatusPredicate_ParityWithClassifyState(t *testing.T) {
	states := []a2a.TaskState{
		a2a.TaskStateSubmitted, a2a.TaskStateWorking, a2a.TaskStateCompleted,
		a2a.TaskStateFailed, a2a.TaskStateCanceled, a2a.TaskStateRejected,
	}
	bindings := []SubmissionState{SubmissionPrepared, SubmissionStarting, SubmissionStarted, SubmissionRejected}
	for _, binding := range bindings {
		for _, cs := range allCaseStatuses() {
			for _, job := range allJobStates() {
				want, _ := ClassifyState(binding, cs, job)
				for _, s := range states {
					matches := false
					for _, g := range StatusPredicate(string(s)) {
						if GroupMatches(g, binding, cs, job) {
							matches = true
							break
						}
					}
					if matches != (s == want) {
						t.Fatalf("parity broken for (binding=%s, case=%s, job=%v): SQL predicate %s matches=%v but projector says %s",
							binding, cs, jobStatusLabel(job), s, matches, want)
					}
				}
			}
		}
	}
}

func jobStatusLabel(job *entity.DecisionJob) string {
	if job == nil {
		return "none"
	}
	return string(job.Status)
}

func resolvedRecord() *TaskRecord {
	return &TaskRecord{
		Submission:   Submission{TaskID: "case-1", MessageID: "msg-1", ContextID: "conv-1", State: SubmissionStarted},
		Case:         &entity.DecisionCase{ID: "case-1", Status: entity.CaseStatusResolved, UpdatedAt: time.Date(2026, 8, 25, 1, 2, 3, 0, time.UTC)},
		Job:          jobWith(entity.DecisionJobSucceeded),
		InputMessage: &entity.ConversationMessage{ID: "input-1", Content: "Should we ship?"},
		Resolution: &entity.Resolution{
			ID: "res-1", CaseID: "case-1",
			Consensus: entity.ConsensusResult{
				Outcome: entity.ConsensusMajorityApprovalDissent, Round: 2,
				Votes: []entity.Vote{
					{ID: "v1", Decision: entity.VoteDecisionApprove, Confidence: 0.9},
					{ID: "v2", Decision: entity.VoteDecisionApprove, Confidence: 80},
					{ID: "v3", Decision: entity.VoteDecisionReject, Confidence: 30},
				},
			},
			FinalDecision: entity.VoteDecisionApprove, FinalReport: "# Final report",
			KeyEvidenceIDs: []string{"EV-2", "EV-1"}, KeyClaimIDs: []string{"CL-2", "CL-1"},
		},
		Evidence: []*entity.EvidenceRecord{{ID: "EV-1"}, {ID: "EV-2"}},
		Claims:   []*entity.Claim{{ID: "CL-1"}, {ID: "CL-2"}},
	}
}

func TestTaskProjector_ResolvedArtifactsAreDeterministic(t *testing.T) {
	proj := NewTaskProjector(redact.New("sk-secret"))
	first := proj.Project(resolvedRecord(), 1, true)
	if first.ID != "case-1" || first.ContextID != "conv-1" {
		t.Fatalf("task ids = %s/%s", first.ID, first.ContextID)
	}
	if first.Status.State != a2a.TaskStateCompleted {
		t.Fatalf("state = %s", first.Status.State)
	}
	if len(first.Artifacts) != 2 {
		t.Fatalf("artifacts = %d, want 2", len(first.Artifacts))
	}
	md, jsonArt := first.Artifacts[0], first.Artifacts[1]
	if md.ID != "case-1-decision-report" || md.Name != "decision-report.md" || md.Parts[0].MediaType != "text/markdown" {
		t.Fatalf("markdown artifact = %+v", md)
	}
	if jsonArt.ID != "case-1-decision-result" || jsonArt.Name != "decision-result.json" || jsonArt.Parts[0].MediaType != "application/json" {
		t.Fatalf("json artifact = %+v", jsonArt)
	}
	raw, err := json.Marshal(first)
	if err != nil {
		t.Fatal(err)
	}
	again, err := json.Marshal(proj.Project(resolvedRecord(), 1, true))
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(raw, again) {
		t.Fatal("artifact projection is not byte-stable across identical snapshots")
	}

	jsonText, _ := json.Marshal(jsonArt.Parts[0].Data())
	var result decisionResultJSON
	if err := json.Unmarshal(jsonText, &result); err != nil {
		t.Fatal(err)
	}
	if result.SchemaVersion != "1" || result.Outcome != "resolved" || result.CaseID != "case-1" || result.ContextID != "conv-1" {
		t.Fatalf("result = %+v", result)
	}
	if result.Decision == nil || *result.Decision != "approve" {
		t.Fatalf("decision = %v", result.Decision)
	}
	if result.Confidence == nil || *result.Confidence != 85 {
		t.Fatalf("confidence = %v, want 85", result.Confidence)
	}
	if result.Consensus == nil || result.Consensus.Outcome != "majority_approval_with_dissent" || result.Consensus.Round != 2 {
		t.Fatalf("consensus = %+v", result.Consensus)
	}
	if !reflect.DeepEqual(result.EvidenceRefs, []string{"EV-1", "EV-2"}) {
		t.Fatalf("evidence refs = %v", result.EvidenceRefs)
	}
	if !reflect.DeepEqual(result.ClaimRefs, []string{"CL-1", "CL-2"}) {
		t.Fatalf("claim refs = %v", result.ClaimRefs)
	}
	if len(result.Dissent) != 1 || result.Dissent[0].Decision != "reject" {
		t.Fatalf("dissent = %+v", result.Dissent)
	}
}

func TestTaskProjector_ArtifactsRespectAcceptedOutputModes(t *testing.T) {
	proj := NewTaskProjector(redact.New("sk-secret"))
	cases := []struct {
		name          string
		modes         []string
		wantNames     []string
		wantArtifacts int
	}{
		{"default both", []string{"text/markdown", "application/json"}, []string{"decision-report.md", "decision-result.json"}, 2},
		{"markdown only", []string{"text/markdown"}, []string{"decision-report.md"}, 1},
		{"json only", []string{"application/json"}, []string{"decision-result.json"}, 1},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			rec := resolvedRecord()
			rec.Submission.AcceptedOutputModes = tt.modes
			task := proj.Project(rec, 1, true)
			if len(task.Artifacts) != tt.wantArtifacts {
				t.Fatalf("artifacts = %d, want %d (%v)", len(task.Artifacts), tt.wantArtifacts, task.Artifacts)
			}
			for i, name := range tt.wantNames {
				if i >= len(task.Artifacts) || task.Artifacts[i].Name != name {
					t.Fatalf("artifact[%d].Name = %v, want %s", i, task.Artifacts, name)
				}
			}
		})
	}
}

// TestTaskProjector_CompletedWithoutResolutionHasNoArtifacts guards the
// projection invariant: a completed Task without a structured Resolution must
// never fabricate fallback artifacts.
func TestTaskProjector_CompletedWithoutResolutionHasNoArtifacts(t *testing.T) {
	proj := NewTaskProjector(redact.New())
	for _, cs := range []entity.CaseStatus{entity.CaseStatusDeadlocked, entity.CaseStatusInsufficientEv} {
		rec := resolvedRecord()
		rec.Case.Status = cs
		rec.Resolution = nil
		task := proj.Project(rec, 0, true)
		if task.Status.State != a2a.TaskStateCompleted {
			t.Fatalf("state = %s", task.Status.State)
		}
		if len(task.Artifacts) != 0 {
			t.Fatalf("%s completed task fabricated %d artifacts without a Resolution", cs, len(task.Artifacts))
		}
	}
}

func TestTaskProjector_HistoryAndMetadata(t *testing.T) {
	proj := NewTaskProjector(redact.New("sk-secret"))
	rec := resolvedRecord()
	rec.Case.Status = entity.CaseStatusInvestigating
	rec.Job = jobWith(entity.DecisionJobRunning)
	rec.Resolution = nil
	withHistory := proj.Project(rec, 1, true)
	if len(withHistory.History) != 1 || withHistory.History[0].ID != "msg-1" {
		t.Fatalf("history = %+v", withHistory.History)
	}
	without := proj.Project(rec, 0, true)
	if len(without.History) != 0 {
		t.Fatalf("history not suppressed: %+v", without.History)
	}
	magi := withHistory.Metadata["magi"].(map[string]any)
	if magi["caseStatus"] != string(entity.CaseStatusInvestigating) || magi["jobStatus"] != string(entity.DecisionJobRunning) {
		t.Fatalf("magi metadata = %+v", magi)
	}
	if magi["paused"] != false || magi["schemaVersion"] != "1" {
		t.Fatalf("magi metadata = %+v", magi)
	}
}

func TestTaskProjector_CanceledNeverEmitsArtifacts(t *testing.T) {
	proj := NewTaskProjector(redact.New())
	rec := resolvedRecord()
	rec.Case.Status = entity.CaseStatusCancelled
	rec.Job = jobWith(entity.DecisionJobCancelled)
	rec.Resolution = nil
	task := proj.Project(rec, 0, true)
	if task.Status.State != a2a.TaskStateCanceled {
		t.Fatalf("state = %s", task.Status.State)
	}
	if len(task.Artifacts) != 0 {
		t.Fatalf("canceled task emitted artifacts: %+v", task.Artifacts)
	}
}

func TestTaskProjector_FailedStatusMessageIsStable(t *testing.T) {
	proj := NewTaskProjector(redact.New("sk-secret-1"))
	rec := resolvedRecord()
	rec.Case.Status = entity.CaseStatusFailed
	rec.Job = jobWith(entity.DecisionJobFailed)
	rec.Job.LastError = "provider failed with sk-secret-1"
	rec.Submission.ErrorCode = "provider_error"
	rec.Resolution = nil
	task := proj.Project(rec, 0, true)
	if task.Status.State != a2a.TaskStateFailed {
		t.Fatalf("state = %s", task.Status.State)
	}
	if task.Status.Message == nil || len(task.Status.Message.Parts) == 0 ||
		task.Status.Message.Parts[0].Text() != "provider_error: The task failed during execution." {
		t.Fatalf("status message = %+v", task.Status.Message)
	}
	if strings.Contains(task.Status.Message.Parts[0].Text(), "sk-secret-1") || strings.Contains(task.Status.Message.Parts[0].Text(), "provider failed") {
		t.Fatalf("internal failure text leaked: %+v", task.Status.Message.Parts[0].Text())
	}
}

func TestTaskProjector_StatusTimestampIsStable(t *testing.T) {
	proj := NewTaskProjector(redact.New())
	rec := resolvedRecord()
	rec.Case.UpdatedAt = time.Date(2026, 8, 25, 2, 0, 0, 0, time.UTC)
	rec.Job.UpdatedAt = time.Date(2026, 8, 24, 2, 0, 0, 0, time.UTC)
	task := proj.Project(rec, 0, true)
	if task.Status.Timestamp == nil || !task.Status.Timestamp.Equal(rec.Case.UpdatedAt) {
		t.Fatalf("timestamp = %v, want case updated at", task.Status.Timestamp)
	}
}
