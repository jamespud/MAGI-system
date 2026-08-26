package a2aapp

import (
	"fmt"
	"math"
	"sort"
	"time"

	a2a "github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/jamespud/magi/backend/application/redact"
	"github.com/jamespud/magi/backend/domain/entity"
	"github.com/jamespud/magi/backend/domain/service"
)

// JobNone marks "no durable job row" inside a StatusGroup. The adapter turns it
// into a `decision_job.id IS NULL` condition.
const JobNone = "\x00__no_job__"

// terminalCaseStatuses are Case statuses whose projected A2A state always wins
// regardless of the durable Job or binding state.
var terminalCaseStatuses = []entity.CaseStatus{
	entity.CaseStatusResolved, entity.CaseStatusMemoryIndexed,
	entity.CaseStatusFailed, entity.CaseStatusCancelled,
	entity.CaseStatusTimedOut, entity.CaseStatusInsufficientEv,
	entity.CaseStatusDeadlocked,
}

var completedCaseStatuses = []entity.CaseStatus{
	entity.CaseStatusResolved, entity.CaseStatusMemoryIndexed,
	entity.CaseStatusInsufficientEv, entity.CaseStatusDeadlocked,
}

var failedCaseStatuses = []entity.CaseStatus{
	entity.CaseStatusFailed, entity.CaseStatusTimedOut,
}

// IsSupportedListState reports whether a public A2A TaskState is filterable by
// ListTasks. Unknown or extension states are rejected as invalid params.
func IsSupportedListState(state a2a.TaskState) bool {
	switch state {
	case a2a.TaskStateSubmitted, a2a.TaskStateWorking, a2a.TaskStateCompleted,
		a2a.TaskStateFailed, a2a.TaskStateCanceled, a2a.TaskStateRejected:
		return true
	default:
		return false
	}
}

// processingCaseStatuses are the non-terminal, non-draft FSM positions.
func processingCaseStatuses() []entity.CaseStatus {
	out := make([]entity.CaseStatus, 0, 15)
	for _, cs := range []entity.CaseStatus{
		entity.CaseStatusNormalizing, entity.CaseStatusContextBuilding,
		entity.CaseStatusRetrievingMemory, entity.CaseStatusInvestigating,
		entity.CaseStatusEvidenceGating, entity.CaseStatusCollectingVotes,
		entity.CaseStatusConsensusCheck, entity.CaseStatusResolving,
		entity.CaseStatusGeneratingReport, entity.CaseStatusSavingMemory,
		entity.CaseStatusEvaluating, entity.CaseStatusDebating,
		entity.CaseStatusReflecting, entity.CaseStatusRevoting,
		entity.CaseStatusPaused,
	} {
		out = append(out, cs)
	}
	return out
}

// StatusGroup is one OR-alternative in a status filter. Nil slices match any
// value; Exclude removes Case statuses so precedence rules (Case terminal wins)
// hold. Jobs holds DecisionJobStatus values and/or JobNone.
type StatusGroup struct {
	Bindings []SubmissionState
	Cases    []entity.CaseStatus
	Exclude  []entity.CaseStatus
	Jobs     []string
}

// StatusPredicate returns the deterministic SQL groups whose rows project to
// the given public A2A state. The adapter translates these groups into WHERE
// clauses; the parity test proves they agree with ClassifyState.
func StatusPredicate(state string) []StatusGroup {
	preparedOrStarting := []SubmissionState{SubmissionPrepared, SubmissionStarting}
	startedOrPrepared := []SubmissionState{SubmissionPrepared, SubmissionStarting, SubmissionStarted}
	switch a2a.TaskState(state) {
	case a2a.TaskStateSubmitted:
		return []StatusGroup{
			{Bindings: preparedOrStarting, Cases: []entity.CaseStatus{entity.CaseStatusDraft}, Jobs: []string{JobNone, string(entity.DecisionJobQueued)}},
			{Bindings: []SubmissionState{SubmissionStarted}, Cases: []entity.CaseStatus{entity.CaseStatusDraft}, Jobs: []string{JobNone}},
		}
	case a2a.TaskStateWorking:
		return []StatusGroup{
			{Bindings: startedOrPrepared, Cases: processingCaseStatuses(), Jobs: []string{JobNone, string(entity.DecisionJobQueued), string(entity.DecisionJobRunning), string(entity.DecisionJobPaused)}},
			{Bindings: preparedOrStarting, Cases: []entity.CaseStatus{entity.CaseStatusDraft}, Jobs: []string{string(entity.DecisionJobRunning), string(entity.DecisionJobPaused)}},
			{Bindings: []SubmissionState{SubmissionStarted}, Cases: []entity.CaseStatus{entity.CaseStatusDraft}, Jobs: []string{string(entity.DecisionJobQueued), string(entity.DecisionJobRunning), string(entity.DecisionJobPaused)}},
		}
	case a2a.TaskStateCompleted:
		return []StatusGroup{
			{Cases: completedCaseStatuses},
			{Bindings: startedOrPrepared, Exclude: terminalCaseStatuses, Jobs: []string{string(entity.DecisionJobSucceeded)}},
		}
	case a2a.TaskStateFailed:
		return []StatusGroup{
			{Cases: failedCaseStatuses},
			{Bindings: startedOrPrepared, Exclude: terminalCaseStatuses, Jobs: []string{string(entity.DecisionJobFailed)}},
		}
	case a2a.TaskStateCanceled:
		return []StatusGroup{
			{Cases: []entity.CaseStatus{entity.CaseStatusCancelled}},
			{Bindings: startedOrPrepared, Exclude: terminalCaseStatuses, Jobs: []string{string(entity.DecisionJobCancelled)}},
		}
	case a2a.TaskStateRejected:
		return []StatusGroup{
			{Bindings: []SubmissionState{SubmissionRejected}, Exclude: terminalCaseStatuses},
		}
	default:
		return nil
	}
}

// ClassifyState maps one internal state combination to its public A2A state.
// Precedence: terminal Case wins, then REJECTED binding, then durable Job, then
// the Case FSM. paused reports the PAUSED flag shown in MAGI metadata.
func ClassifyState(binding SubmissionState, cs entity.CaseStatus, job *entity.DecisionJob) (a2a.TaskState, bool) {
	jobStatus := ""
	if job != nil {
		jobStatus = string(job.Status)
	}
	pausedCase := cs == entity.CaseStatusPaused
	pausedJob := job != nil && job.Status == entity.DecisionJobPaused
	switch cs {
	case entity.CaseStatusCancelled:
		return a2a.TaskStateCanceled, false
	case entity.CaseStatusFailed, entity.CaseStatusTimedOut:
		return a2a.TaskStateFailed, false
	case entity.CaseStatusResolved, entity.CaseStatusMemoryIndexed,
		entity.CaseStatusInsufficientEv, entity.CaseStatusDeadlocked:
		return a2a.TaskStateCompleted, false
	}
	if binding == SubmissionRejected {
		return a2a.TaskStateRejected, false
	}
	if (binding == SubmissionPrepared || binding == SubmissionStarting) && cs == entity.CaseStatusDraft &&
		(jobStatus == "" || jobStatus == string(entity.DecisionJobQueued)) {
		return a2a.TaskStateSubmitted, false
	}
	if job != nil {
		switch job.Status {
		case entity.DecisionJobCancelled:
			return a2a.TaskStateCanceled, false
		case entity.DecisionJobFailed:
			return a2a.TaskStateFailed, false
		case entity.DecisionJobSucceeded:
			return a2a.TaskStateCompleted, false
		case entity.DecisionJobPaused:
			return a2a.TaskStateWorking, true
		case entity.DecisionJobQueued, entity.DecisionJobRunning:
			return a2a.TaskStateWorking, pausedCase
		}
	}
	switch cs {
	case entity.CaseStatusDraft:
		return a2a.TaskStateSubmitted, false
	case entity.CaseStatusPaused:
		return a2a.TaskStateWorking, true
	default:
		return a2a.TaskStateWorking, pausedCase || pausedJob
	}
}

// GroupMatches reports whether a status combination belongs to one SQL group.
// It mirrors the WHERE semantics the adapter generates from the group.
func GroupMatches(g StatusGroup, binding SubmissionState, cs entity.CaseStatus, job *entity.DecisionJob) bool {
	if len(g.Bindings) > 0 && !containsSubmissionState(g.Bindings, binding) {
		return false
	}
	if len(g.Cases) > 0 && !containsCaseStatus(g.Cases, cs) {
		return false
	}
	for _, excluded := range g.Exclude {
		if cs == excluded {
			return false
		}
	}
	if g.Jobs == nil {
		return true
	}
	if job == nil {
		return containsString(g.Jobs, JobNone)
	}
	return containsString(g.Jobs, string(job.Status))
}

func containsSubmissionState(list []SubmissionState, s SubmissionState) bool {
	for _, item := range list {
		if item == s {
			return true
		}
	}
	return false
}

func containsCaseStatus(list []entity.CaseStatus, s entity.CaseStatus) bool {
	for _, item := range list {
		if item == s {
			return true
		}
	}
	return false
}

func containsString(list []string, s string) bool {
	for _, item := range list {
		if item == s {
			return true
		}
	}
	return false
}

// TaskProjector projects durable MAGI records into deterministic A2A tasks.
// It is a pure projection: identical records always produce identical output.
type TaskProjector struct {
	redactor *redact.Redactor
}

func NewTaskProjector(redactor *redact.Redactor) *TaskProjector {
	return &TaskProjector{redactor: redactor}
}

// Project renders one A2A Task snapshot. history controls whether the task's
// single input message is attached: 0 omits it, any positive value includes
// it. includeArtifacts controls whether the completed task's resolution
// artifacts are attached; without a Resolution there is nothing structured to
// expose, so no fallback artifacts are fabricated.
func (p *TaskProjector) Project(rec *TaskRecord, history int, includeArtifacts bool) *a2a.Task {
	state, paused := ClassifyState(rec.Submission.State, rec.Case.Status, rec.Job)
	task := &a2a.Task{
		ID:        a2a.TaskID(rec.Case.ID),
		ContextID: rec.Submission.ContextID,
		Status: a2a.TaskStatus{
			State:     state,
			Timestamp: p.statusTimestamp(rec, state),
		},
		Metadata: p.magiMetadata(rec, paused),
	}
	if state == a2a.TaskStateFailed {
		task.Status.Message = p.failedMessage(rec)
	}
	if history > 0 && rec.InputMessage != nil {
		task.History = []*a2a.Message{{
			ID:        rec.Submission.MessageID,
			ContextID: rec.Submission.ContextID,
			Role:      a2a.MessageRoleUser,
			Parts:     a2a.ContentParts{a2a.NewTextPart(p.redactor.String(rec.InputMessage.Content))},
		}}
	}
	if includeArtifacts && state == a2a.TaskStateCompleted && rec.Resolution != nil {
		task.Artifacts = p.buildArtifacts(rec)
	}
	return task
}

func (p *TaskProjector) statusTimestamp(rec *TaskRecord, state a2a.TaskState) *time.Time {
	var t time.Time
	switch rec.Case.Status {
	case entity.CaseStatusCancelled, entity.CaseStatusFailed, entity.CaseStatusTimedOut,
		entity.CaseStatusResolved, entity.CaseStatusMemoryIndexed,
		entity.CaseStatusInsufficientEv, entity.CaseStatusDeadlocked:
		t = rec.Case.UpdatedAt
	}
	if t.IsZero() && rec.Submission.State == SubmissionRejected {
		t = rec.Submission.UpdatedAt
	}
	if t.IsZero() && state == a2a.TaskStateSubmitted {
		t = rec.Submission.UpdatedAt
	}
	if t.IsZero() && rec.Job != nil {
		t = rec.Job.UpdatedAt
	}
	if t.IsZero() {
		t = rec.Case.UpdatedAt
	}
	return &t
}

func (p *TaskProjector) magiMetadata(rec *TaskRecord, paused bool) map[string]any {
	round := 0
	jobStatus := ""
	if rec.Resolution != nil {
		round = rec.Resolution.Consensus.Round
	}
	if rec.Job != nil {
		jobStatus = string(rec.Job.Status)
	}
	return map[string]any{"magi": map[string]any{
		"schemaVersion": "1",
		"caseStatus":    string(rec.Case.Status),
		"jobStatus":     jobStatus,
		"round":         round,
		"paused":        paused,
		"eventSeq":      rec.MaxEventSeq,
	}}
}

func (p *TaskProjector) failedMessage(rec *TaskRecord) *a2a.Message {
	code := "task_failed"
	if rec.Submission.ErrorCode != "" {
		code = rec.Submission.ErrorCode
	}
	// A stable public sentence; SQL, paths, tool responses, and LastError stay
	// internal so they can never leak over the protocol.
	summary := "The task failed during execution."
	return &a2a.Message{
		ID:        fmt.Sprintf("%s-status", rec.Case.ID),
		ContextID: rec.Submission.ContextID,
		Role:      a2a.MessageRoleAgent,
		Parts:     a2a.ContentParts{a2a.NewTextPart(code + ": " + summary)},
	}
}

func (p *TaskProjector) buildArtifacts(rec *TaskRecord) []*a2a.Artifact {
	markdown, result := p.artifactParts(rec)
	accepted := acceptedModes(rec.Submission.AcceptedOutputModes)
	var out []*a2a.Artifact
	if accepted["text/markdown"] {
		out = append(out, &a2a.Artifact{
			ID:    a2a.ArtifactID(rec.Case.ID + "-decision-report"),
			Name:  "decision-report.md",
			Parts: a2a.ContentParts{markdown},
		})
	}
	if accepted["application/json"] {
		out = append(out, &a2a.Artifact{
			ID:    a2a.ArtifactID(rec.Case.ID + "-decision-result"),
			Name:  "decision-result.json",
			Parts: a2a.ContentParts{result},
		})
	}
	return out
}

func acceptedModes(modes []string) map[string]bool {
	if len(modes) == 0 {
		return map[string]bool{"text/markdown": true, "application/json": true}
	}
	out := map[string]bool{}
	for _, mode := range modes {
		out[mode] = true
	}
	return out
}

func (p *TaskProjector) artifactParts(rec *TaskRecord) (*a2a.Part, *a2a.Part) {
	if rec.Resolution == nil {
		outcome := string(entity.CaseStatus(rec.Case.Status))
		if rec.Case.Status == entity.CaseStatusInsufficientEv {
			outcome = "insufficient_evidence"
		} else if rec.Case.Status == entity.CaseStatusDeadlocked {
			outcome = "deadlocked"
		}
		summary := fmt.Sprintf("# Decision summary\n\nThe task ended in **%s**: no structured resolution was produced.\n\nEvidence records: %d\n", outcome, len(rec.Evidence))
		md := &a2a.Part{Content: a2a.Text(summary), Filename: "decision-report.md", MediaType: "text/markdown"}
		result := &a2a.Part{Content: a2a.Data{Value: decisionResultJSON{
			SchemaVersion: "1", CaseID: rec.Case.ID, ContextID: rec.Submission.ContextID,
			Outcome: outcome, Dissent: []dissentJSON{}, EvidenceRefs: []string{}, ClaimRefs: []string{},
		}}, Filename: "decision-result.json", MediaType: "application/json"}
		return md, result
	}

	report := p.redactor.String(rec.Resolution.FinalReport)
	md := &a2a.Part{Content: a2a.Text(report), Filename: "decision-report.md", MediaType: "text/markdown"}
	result := &a2a.Part{Content: a2a.Data{Value: p.decisionResult(rec)}, Filename: "decision-result.json", MediaType: "application/json"}
	return md, result
}

func (p *TaskProjector) decisionResult(rec *TaskRecord) decisionResultJSON {
	res := rec.Resolution
	decision := string(res.FinalDecision)
	confidence := decisionConfidence(res)
	consensus := &consensusJSON{Outcome: string(res.Consensus.Outcome), Round: res.Consensus.Round}
	dissent := make([]dissentJSON, 0)
	for _, d := range service.ExtractDissent(res.FinalDecision, resolutionVotes(res), nil) {
		dissent = append(dissent, dissentJSON{
			AgentCode: string(d.AgentCode), Decision: string(d.Decision),
			Reasoning: p.redactor.String(d.Reasoning), EvidenceRefs: d.EvidenceIDs, ClaimRefs: d.ClaimIDs,
		})
	}
	return decisionResultJSON{
		SchemaVersion: "1", CaseID: rec.Case.ID, ContextID: rec.Submission.ContextID,
		Outcome: "resolved", Decision: &decision, Consensus: consensus, Confidence: &confidence,
		Dissent: dissent, EvidenceRefs: sortedCopy(res.KeyEvidenceIDs), ClaimRefs: sortedCopy(res.KeyClaimIDs),
	}
}

func resolutionVotes(res *entity.Resolution) []*entity.Vote {
	votes := make([]*entity.Vote, 0, len(res.Consensus.Votes))
	for i := range res.Consensus.Votes {
		v := res.Consensus.Votes[i]
		votes = append(votes, &v)
	}
	return votes
}

// decisionConfidence is the deterministic mean of the normalized confidence of
// votes matching the final decision (the winning coalition). It is a pure
// projection of the persisted snapshot.
func decisionConfidence(res *entity.Resolution) float64 {
	if res == nil || res.FinalDecision == "" {
		return 0
	}
	var sum float64
	var n int
	for _, v := range resolutionVotes(res) {
		if v.Decision == res.FinalDecision {
			sum += entity.NormalizeConfidence(v.Confidence)
			n++
		}
	}
	if n == 0 {
		return 0
	}
	return math.Round(sum/float64(n)*10) / 10
}

func sortedCopy(in []string) []string {
	if len(in) == 0 {
		return []string{}
	}
	out := make([]string, len(in))
	copy(out, in)
	sort.Strings(out)
	return out
}

type decisionResultJSON struct {
	SchemaVersion string         `json:"schemaVersion"`
	CaseID        string         `json:"caseId"`
	ContextID     string         `json:"contextId"`
	Outcome       string         `json:"outcome"`
	Decision      *string        `json:"decision"`
	Consensus     *consensusJSON `json:"consensus,omitempty"`
	Confidence    *float64       `json:"confidence"`
	Dissent       []dissentJSON  `json:"dissent"`
	EvidenceRefs  []string       `json:"evidenceRefs"`
	ClaimRefs     []string       `json:"claimRefs"`
}

type consensusJSON struct {
	Outcome string `json:"outcome"`
	Round   int    `json:"round"`
}

type dissentJSON struct {
	AgentCode    string   `json:"agentCode,omitempty"`
	Decision     string   `json:"decision"`
	Reasoning    string   `json:"reasoning,omitempty"`
	EvidenceRefs []string `json:"evidenceRefs,omitempty"`
	ClaimRefs    []string `json:"claimRefs,omitempty"`
}
