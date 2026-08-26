package magi

import (
	"context"
	"errors"
	"fmt"
	"math/rand/v2"
	"strings"
	"time"

	a2aapp "github.com/jamespud/magi/backend/application/a2a"
	"github.com/jamespud/magi/backend/application/assistant"
	"github.com/jamespud/magi/backend/domain/entity"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// a2aSubmissionRepo is intentionally separate from the aggregate and
// conversation repositories: Prepare must never let any of its writes escape
// the binding transaction.
type a2aSubmissionRepo struct{ db *gorm.DB }

var errConcurrentConversationInsert = errors.New("concurrent conversation insert")

const (
	contextContentionMaxAttempts    = 6
	contextContentionInitialBackoff = time.Millisecond
	contextContentionMaxBackoff     = 32 * time.Millisecond
)

func NewA2ASubmissionRepository(db *gorm.DB) a2aapp.SubmissionRepository {
	return &a2aSubmissionRepo{db: db}
}

func (r *a2aSubmissionRepo) Prepare(ctx context.Context, cmd a2aapp.PrepareCommand) (*a2aapp.PreparedSubmission, bool, error) {
	for attempt := 0; attempt < contextContentionMaxAttempts; attempt++ {
		prepared, created, err := r.prepareOnce(ctx, cmd)
		if !errors.Is(err, errConcurrentConversationInsert) {
			return prepared, created, err
		}
		if attempt == contextContentionMaxAttempts-1 {
			return nil, false, fmt.Errorf("%w after %d attempts: %v", a2aapp.ErrContextContention, contextContentionMaxAttempts, err)
		}

		// A competing transaction is creating this ContextID. The binding
		// transaction rolled back, so retrying will lock the committed
		// conversation instead. Back off so contention cannot spin the DB.
		timer := time.NewTimer(contextContentionBackoff(attempt))
		select {
		case <-ctx.Done():
			if !timer.Stop() {
				<-timer.C
			}
			return nil, false, ctx.Err()
		case <-timer.C:
		}
	}
	return nil, false, a2aapp.ErrContextContention
}

func contextContentionBackoff(attempt int) time.Duration {
	delay := contextContentionInitialBackoff
	for i := 0; i < attempt && delay < contextContentionMaxBackoff; i++ {
		delay *= 2
	}
	if delay > contextContentionMaxBackoff {
		delay = contextContentionMaxBackoff
	}
	// Decorrelate competing submitters that discover the ContextID at once.
	return delay + time.Duration(rand.Int64N(int64(delay/4)+1))
}

func (r *a2aSubmissionRepo) prepareOnce(ctx context.Context, cmd a2aapp.PrepareCommand) (*a2aapp.PreparedSubmission, bool, error) {
	var prepared *a2aapp.PreparedSubmission
	created := false
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var existing A2ASubmissionModel
		err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("user_id = ? AND message_id = ?", cmd.UserID, cmd.MessageID).First(&existing).Error
		if err == nil {
			if existing.RequestHash != cmd.RequestHash {
				return a2aapp.ErrIdempotencyConflict
			}
			var loadErr error
			prepared, loadErr = loadPreparedSubmission(tx, existing)
			return loadErr
		}
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}

		now := time.Now().UTC()
		binding := A2ASubmissionModel{
			ID: cmd.SubmissionID, UserID: cmd.UserID, MessageID: cmd.MessageID,
			RequestHash: cmd.RequestHash, TaskID: cmd.TaskID, ContextID: cmd.ContextID,
			InputMessageID: cmd.InputMessageID, CaseMessageID: cmd.CaseMessageID,
			State: string(a2aapp.SubmissionPrepared), CreatedAt: now, UpdatedAt: now,
		}
		if err := tx.Create(&binding).Error; err != nil {
			return err
		}

		conv, history, err := prepareConversation(tx, cmd)
		if err != nil {
			return err
		}
		background := cmd.Background
		if conv != nil {
			resolutions, err := loadConversationResolutions(tx, history)
			if err != nil {
				return err
			}
			background = assistant.BuildFollowUpBackground(history, resolutions, cmd.Background)
		}

		caseEntity := &entity.DecisionCase{
			ID: cmd.TaskID, UserID: cmd.UserID, Question: cmd.Question, Context: background,
			Constraints: cmd.Constraints, Status: entity.CaseStatusDraft,
			MaxDebateRounds: cmd.MaxDebateRounds, CreatedAt: now, UpdatedAt: now,
		}
		caseModel := caseToModel(caseEntity)
		if err := tx.Create(&caseModel).Error; err != nil {
			return err
		}

		result := &a2aapp.PreparedSubmission{Binding: submissionFromModel(binding), Case: caseEntity, Conversation: conv}
		if conv != nil {
			input := &entity.ConversationMessage{
				ID: cmd.InputMessageID, ConversationID: conv.ID, UserID: cmd.UserID,
				Role: entity.ConversationRoleUser, Content: cmd.Question, CreatedAt: now,
			}
			caseMessage := &entity.ConversationMessage{
				ID: cmd.CaseMessageID, ConversationID: conv.ID, UserID: cmd.UserID,
				Role:    entity.ConversationRoleAssistant,
				Content: fmt.Sprintf("Decision case %s created and started.", caseEntity.ID),
				CaseID:  caseEntity.ID, CreatedAt: now,
			}
			if err := tx.Create(conversationMessageToModel(input)).Error; err != nil {
				return err
			}
			if err := tx.Create(conversationMessageToModel(caseMessage)).Error; err != nil {
				return err
			}
			if err := tx.Model(&ConversationModel{}).Where("id = ?", conv.ID).Update("updated_at", now).Error; err != nil {
				return err
			}
			conv.UpdatedAt = now
			result.InputMessage, result.CaseMessage = input, caseMessage
		}
		prepared, created = result, true
		return nil
	})
	if err == nil || errors.Is(err, a2aapp.ErrIdempotencyConflict) {
		return prepared, created, err
	}
	if errors.Is(err, errConcurrentConversationInsert) {
		return nil, false, err
	}
	if !isUniqueViolation(err) {
		return nil, false, err
	}

	// A competing insert won. Reload outside the failed transaction, where the
	// unique key is now the serialization point for equal-message replays.
	var existing A2ASubmissionModel
	if getErr := r.db.WithContext(ctx).Where("user_id = ? AND message_id = ?", cmd.UserID, cmd.MessageID).First(&existing).Error; getErr != nil {
		return nil, false, err
	}
	if existing.RequestHash != cmd.RequestHash {
		return nil, false, a2aapp.ErrIdempotencyConflict
	}
	prepared, err = loadPreparedSubmission(r.db.WithContext(ctx), existing)
	return prepared, false, err
}

func prepareConversation(tx *gorm.DB, cmd a2aapp.PrepareCommand) (*entity.Conversation, []*entity.ConversationMessage, error) {
	if cmd.ContextID == "" {
		return nil, nil, nil
	}
	var model ConversationModel
	err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ?", cmd.ContextID).First(&model).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		now := time.Now().UTC()
		conv := &entity.Conversation{ID: cmd.ContextID, UserID: cmd.UserID, Title: a2aConversationTitle(cmd.Question), CreatedAt: now, UpdatedAt: now}
		if err := tx.Create(conversationToModel(conv)).Error; err != nil {
			if isUniqueViolation(err) {
				return nil, nil, fmt.Errorf("%w: %v", errConcurrentConversationInsert, err)
			}
			return nil, nil, err
		}
		return conv, nil, nil
	}
	if err != nil {
		return nil, nil, err
	}
	if model.UserID != 0 && model.UserID != cmd.UserID {
		return nil, nil, a2aapp.ErrForbidden
	}
	var models []ConversationMessageModel
	if err := tx.Where("conversation_id = ?", model.ID).Order("created_at DESC, id DESC").Limit(20).Find(&models).Error; err != nil {
		return nil, nil, err
	}
	history := make([]*entity.ConversationMessage, len(models))
	for i := range models {
		history[i] = conversationMessageFromModel(&models[i])
	}
	return conversationFromModel(&model), history, nil
}

func a2aConversationTitle(question string) string {
	title := strings.TrimSpace(question)
	if runes := []rune(title); len(runes) > 80 {
		title = string(runes[:80])
	}
	if title == "" {
		return "New conversation"
	}
	return title
}

func loadConversationResolutions(tx *gorm.DB, messages []*entity.ConversationMessage) (map[string]*entity.Resolution, error) {
	caseIDs := make([]string, 0, len(messages))
	seen := make(map[string]struct{})
	for _, message := range messages {
		if message.CaseID == "" {
			continue
		}
		if _, ok := seen[message.CaseID]; !ok {
			seen[message.CaseID] = struct{}{}
			caseIDs = append(caseIDs, message.CaseID)
		}
	}
	if len(caseIDs) == 0 {
		return map[string]*entity.Resolution{}, nil
	}
	var models []ResolutionModel
	if err := tx.Where("case_id IN ?", caseIDs).Find(&models).Error; err != nil {
		return nil, err
	}
	out := make(map[string]*entity.Resolution, len(models))
	for i := range models {
		model := &models[i]
		out[model.CaseID] = &entity.Resolution{
			ID: model.ID, CaseID: model.CaseID, Consensus: fromJSON[entity.ConsensusResult](model.ConsensusJSON),
			FinalDecision: entity.VoteDecision(model.FinalDecision), FinalReport: model.FinalReport,
			KeyEvidenceIDs: fromJSON[[]string](model.KeyEvidenceIDsJSON), KeyClaimIDs: fromJSON[[]string](model.KeyClaimIDsJSON),
			VoteIDs: fromJSON[[]string](model.VoteIDsJSON), Evaluation: fromJSON[*entity.Evaluation](model.EvaluationJSON), CreatedAt: model.CreatedAt,
		}
	}
	return out, nil
}

func (r *a2aSubmissionRepo) MarkStarted(ctx context.Context, id string) error {
	return r.db.WithContext(ctx).Model(&A2ASubmissionModel{}).Where("id = ?", id).
		Update("state", string(a2aapp.SubmissionStarted)).Error
}

func (r *a2aSubmissionRepo) MarkRejected(ctx context.Context, id, code string) error {
	return r.db.WithContext(ctx).Model(&A2ASubmissionModel{}).Where("id = ?", id).
		Updates(map[string]any{"state": string(a2aapp.SubmissionRejected), "error_code": code}).Error
}

func (r *a2aSubmissionRepo) ListPrepared(ctx context.Context, limit int) ([]*a2aapp.PreparedSubmission, error) {
	if limit <= 0 || limit > 100 {
		limit = 100
	}
	var models []A2ASubmissionModel
	if err := r.db.WithContext(ctx).Where("state = ?", string(a2aapp.SubmissionPrepared)).Order("created_at ASC, id ASC").Limit(limit).Find(&models).Error; err != nil {
		return nil, err
	}
	prepared := make([]*a2aapp.PreparedSubmission, 0, len(models))
	for _, model := range models {
		item, err := loadPreparedSubmission(r.db.WithContext(ctx), model)
		if err != nil {
			return nil, err
		}
		prepared = append(prepared, item)
	}
	return prepared, nil
}

func (r *a2aSubmissionRepo) GetByTask(ctx context.Context, userID int64, taskID string) (*a2aapp.Submission, error) {
	var model A2ASubmissionModel
	if err := r.db.WithContext(ctx).Where("user_id = ? AND task_id = ?", userID, taskID).First(&model).Error; err != nil {
		return nil, err
	}
	result := submissionFromModel(model)
	return &result, nil
}

func (r *a2aSubmissionRepo) GetTaskRecord(ctx context.Context, userID int64, taskID string) (*a2aapp.TaskRecord, error) {
	// The owner-scoped binding, Case, Job, artifacts, and MaxEventSeq must be
	// read inside one transaction so a concurrent terminal commit can never
	// yield a pre-terminal Case paired with a terminal event watermark.
	var record *a2aapp.TaskRecord
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var model A2ASubmissionModel
		if err := tx.Where("user_id = ? AND task_id = ?", userID, taskID).First(&model).Error; err != nil {
			return err
		}
		var loadErr error
		record, loadErr = loadTaskRecord(tx, model, true)
		return loadErr
	})
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, a2aapp.ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return record, nil
}

func (r *a2aSubmissionRepo) ListTasks(ctx context.Context, filter a2aapp.TaskListFilter) (*a2aapp.TaskPage, error) {
	limit := filter.Limit
	if limit <= 0 || limit > 100 {
		limit = 50
	}
	q := r.db.WithContext(ctx).Model(&A2ASubmissionModel{}).Where("a2a_submission.user_id = ?", filter.UserID)
	needsJoins := filter.Status != "" || filter.StatusTimestampAfter != nil
	if needsJoins {
		q = q.Joins("JOIN decision_case ON decision_case.id = a2a_submission.task_id").
			Joins("LEFT JOIN decision_job ON decision_job.case_id = a2a_submission.task_id")
	}
	if filter.ContextID != "" {
		q = q.Where("a2a_submission.context_id = ?", filter.ContextID)
	}
	if filter.Status != "" {
		statusSQL, args := statusPredicateSQL(filter.Status)
		q = q.Where(statusSQL, args...)
	}
	if filter.StatusTimestampAfter != nil {
		// Portable GREATEST over the three authoritative update times. The
		// plan's literal GREATEST() is avoided because LEFT JOIN rows make
		// decision_job.updated_at NULL (NULL poisons GREATEST in both MySQL
		// and SQLite) and SQLite lacks GREATEST for the test dialect.
		q = q.Where(`
			CASE
				WHEN decision_case.updated_at >= COALESCE(decision_job.updated_at, decision_case.updated_at)
				 AND decision_case.updated_at >= a2a_submission.updated_at THEN decision_case.updated_at
				WHEN COALESCE(decision_job.updated_at, decision_case.updated_at) >= a2a_submission.updated_at
				 THEN COALESCE(decision_job.updated_at, decision_case.updated_at)
				ELSE a2a_submission.updated_at
			END >= ?`, *filter.StatusTimestampAfter)
	}
	var total int64
	if err := q.Count(&total).Error; err != nil {
		return nil, err
	}
	if filter.After != nil {
		q = q.Where("a2a_submission.created_at < ? OR (a2a_submission.created_at = ? AND a2a_submission.task_id < ?)", filter.After.CreatedAt, filter.After.CreatedAt, filter.After.ID)
	}
	var models []A2ASubmissionModel
	if err := q.Order("a2a_submission.created_at DESC, a2a_submission.task_id DESC").Limit(limit + 1).Find(&models).Error; err != nil {
		return nil, err
	}
	page := &a2aapp.TaskPage{Total: int(total)}
	if len(models) > limit {
		models = models[:limit]
		last := models[len(models)-1]
		page.Next = &a2aapp.TaskCursor{CreatedAt: last.CreatedAt, ID: last.TaskID}
	}
	for _, model := range models {
		record, err := r.loadRecordTx(ctx, model, filter.IncludeArtifacts)
		if err != nil {
			return nil, err
		}
		page.Records = append(page.Records, *record)
	}
	return page, nil
}

// loadRecordTx loads one TaskRecord inside a consistent read transaction.
func (r *a2aSubmissionRepo) loadRecordTx(ctx context.Context, model A2ASubmissionModel, includeArtifacts bool) (*a2aapp.TaskRecord, error) {
	var record *a2aapp.TaskRecord
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var loadErr error
		record, loadErr = loadTaskRecord(tx, model, includeArtifacts)
		return loadErr
	})
	return record, err
}

func statusPredicateSQL(state string) (string, []any) {
	groups := a2aapp.StatusPredicate(state)
	clauses := make([]string, 0, len(groups))
	var args []any
	for _, group := range groups {
		var conds []string
		if len(group.Bindings) > 0 {
			conds = append(conds, "a2a_submission.state IN ("+inPlaceholders(len(group.Bindings))+")")
			for _, b := range group.Bindings {
				args = append(args, string(b))
			}
		}
		if len(group.Cases) > 0 {
			conds = append(conds, "decision_case.status IN ("+inPlaceholders(len(group.Cases))+")")
			for _, c := range group.Cases {
				args = append(args, string(c))
			}
		}
		for _, excluded := range group.Exclude {
			conds = append(conds, "decision_case.status <> ?")
			args = append(args, string(excluded))
		}
		if group.Jobs != nil {
			hasNone := false
			var statuses []string
			for _, job := range group.Jobs {
				if job == a2aapp.JobNone {
					hasNone = true
				} else {
					statuses = append(statuses, job)
				}
			}
			switch {
			case hasNone && len(statuses) == 0:
				conds = append(conds, "decision_job.id IS NULL")
			case hasNone:
				conds = append(conds, "(decision_job.id IS NULL OR decision_job.status IN ("+inPlaceholders(len(statuses))+"))")
				for _, s := range statuses {
					args = append(args, s)
				}
			default:
				conds = append(conds, "decision_job.status IN ("+inPlaceholders(len(statuses))+")")
				for _, s := range statuses {
					args = append(args, s)
				}
			}
		}
		clauses = append(clauses, "("+strings.Join(conds, " AND ")+")")
	}
	return strings.Join(clauses, " OR "), args
}

func inPlaceholders(n int) string {
	return strings.TrimRight(strings.Repeat("?,", n), ",")
}

func (r *a2aSubmissionRepo) CancelTask(ctx context.Context, userID int64, taskID string) (*a2aapp.CancelResult, error) {
	var result *a2aapp.CancelResult
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var binding A2ASubmissionModel
		err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("user_id = ? AND task_id = ?", userID, taskID).First(&binding).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			result = &a2aapp.CancelResult{Outcome: a2aapp.CancelNotFound}
			return nil
		}
		if err != nil {
			return err
		}
		var caseModel CaseModel
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ?", binding.TaskID).First(&caseModel).Error; err != nil {
			return err
		}
		caseEntity := caseFromModel(&caseModel)
		// A REJECTED binding is already terminal from the client's perspective
		// (the Case stays DRAFT for other MAGI queries), so it must never
		// become cancelable. Check it before the DRAFT Case status.
		if binding.State == string(a2aapp.SubmissionRejected) {
			result = &a2aapp.CancelResult{Outcome: a2aapp.CancelNotCancelable}
			return nil
		}
		if caseEntity.Status == entity.CaseStatusCancelled {
			record, loadErr := loadTaskRecord(tx, binding, true)
			if loadErr != nil {
				return loadErr
			}
			result = &a2aapp.CancelResult{Record: record, Outcome: a2aapp.CancelAlreadyCanceled}
			return nil
		}
		if isTerminalCaseStatus(caseEntity.Status) {
			result = &a2aapp.CancelResult{Outcome: a2aapp.CancelNotCancelable}
			return nil
		}
		if err := tx.Model(&CaseModel{}).Where("id = ?", binding.TaskID).Updates(map[string]any{"status": string(entity.CaseStatusCancelled), "updated_at": time.Now().UTC()}).Error; err != nil {
			return err
		}
		if err := tx.Model(&DecisionJobModel{}).Where("case_id = ?", binding.TaskID).Update("status", string(entity.DecisionJobCancelled)).Error; err != nil {
			return err
		}
		// Every applied cancellation emits exactly one ordered durable event so
		// remote replicas can observe the transition; the live broker only
		// fans this committed event out after the transaction commits.
		event := entity.NewEvent(binding.TaskID, "", nil, entity.EventCaseStatusChanged, map[string]any{"status": string(entity.CaseStatusCancelled)})
		if err := createEventInTx(tx, &event); err != nil {
			return err
		}
		record, loadErr := loadTaskRecord(tx, binding, true)
		if loadErr != nil {
			return loadErr
		}
		result = &a2aapp.CancelResult{Record: record, Outcome: a2aapp.CancelApplied, Event: &event}
		return nil
	})
	return result, err
}

func loadPreparedSubmission(db *gorm.DB, binding A2ASubmissionModel) (*a2aapp.PreparedSubmission, error) {
	result := &a2aapp.PreparedSubmission{Binding: submissionFromModel(binding)}
	var caseModel CaseModel
	if err := db.Where("id = ?", binding.TaskID).First(&caseModel).Error; err != nil {
		return nil, err
	}
	result.Case = caseFromModel(&caseModel)
	if binding.ContextID == "" {
		return result, nil
	}
	var conversation ConversationModel
	if err := db.Where("id = ?", binding.ContextID).First(&conversation).Error; err != nil {
		return nil, err
	}
	result.Conversation = conversationFromModel(&conversation)
	if binding.InputMessageID != "" {
		var message ConversationMessageModel
		if err := db.Where("id = ?", binding.InputMessageID).First(&message).Error; err != nil {
			return nil, err
		}
		result.InputMessage = conversationMessageFromModel(&message)
	}
	if binding.CaseMessageID != "" {
		var message ConversationMessageModel
		if err := db.Where("id = ?", binding.CaseMessageID).First(&message).Error; err != nil {
			return nil, err
		}
		result.CaseMessage = conversationMessageFromModel(&message)
	}
	return result, nil
}

func loadTaskRecord(db *gorm.DB, binding A2ASubmissionModel, includeArtifacts bool) (*a2aapp.TaskRecord, error) {
	prepared, err := loadPreparedSubmission(db, binding)
	if err != nil {
		return nil, err
	}
	record := &a2aapp.TaskRecord{Submission: prepared.Binding, Case: prepared.Case, InputMessage: prepared.InputMessage}
	var job DecisionJobModel
	if err := db.Where("case_id = ?", binding.TaskID).First(&job).Error; err == nil {
		record.Job = jobFromModel(&job)
	} else if !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, err
	}
	if includeArtifacts {
		var resolution ResolutionModel
		if err := db.Where("case_id = ?", binding.TaskID).First(&resolution).Error; err == nil {
			record.Resolution = resolutionFromModel(&resolution)
		} else if !errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, err
		}
		var evidenceModels []EvidenceModel
		if err := db.Where("case_id = ?", binding.TaskID).Order("created_at ASC, id ASC").Find(&evidenceModels).Error; err != nil {
			return nil, err
		}
		record.Evidence = make([]*entity.EvidenceRecord, len(evidenceModels))
		for i := range evidenceModels {
			record.Evidence[i] = evidenceFromModel(&evidenceModels[i])
		}
		var claimModels []ClaimModel
		if err := db.Where("case_id = ?", binding.TaskID).Order("created_at ASC, id ASC").Find(&claimModels).Error; err != nil {
			return nil, err
		}
		record.Claims = make([]*entity.Claim, len(claimModels))
		for i := range claimModels {
			record.Claims[i] = claimFromModel(&claimModels[i])
		}
		var voteModels []VoteModel
		if err := db.Where("case_id = ?", binding.TaskID).Order("round ASC, created_at ASC, id ASC").Find(&voteModels).Error; err != nil {
			return nil, err
		}
		record.Votes = make([]*entity.Vote, len(voteModels))
		for i := range voteModels {
			record.Votes[i] = voteFromModel(&voteModels[i])
		}
	}
	var maxSeq uint64
	if err := db.Model(&EventModel{}).Where("case_id = ?", binding.TaskID).Select("COALESCE(MAX(seq), 0)").Scan(&maxSeq).Error; err != nil {
		return nil, err
	}
	record.MaxEventSeq = maxSeq
	return record, nil
}

func resolutionFromModel(m *ResolutionModel) *entity.Resolution {
	return &entity.Resolution{
		ID: m.ID, CaseID: m.CaseID, Consensus: fromJSON[entity.ConsensusResult](m.ConsensusJSON),
		FinalDecision: entity.VoteDecision(m.FinalDecision), FinalReport: m.FinalReport,
		KeyEvidenceIDs: fromJSON[[]string](m.KeyEvidenceIDsJSON), KeyClaimIDs: fromJSON[[]string](m.KeyClaimIDsJSON),
		VoteIDs: fromJSON[[]string](m.VoteIDsJSON), Evaluation: fromJSON[*entity.Evaluation](m.EvaluationJSON), CreatedAt: m.CreatedAt,
	}
}

func voteFromModel(m *VoteModel) *entity.Vote {
	return &entity.Vote{
		ID: m.ID, CaseID: m.CaseID, AgentRunID: m.AgentRunID, Round: m.Round, Decision: entity.VoteDecision(m.Decision),
		Confidence: m.Confidence, UtilityScores: fromJSON[[]entity.UtilityDimensionScore](m.UtilityScoresJSON),
		KeyClaimIDs: fromJSON[[]string](m.KeyClaimIDsJSON), EvidenceIDs: fromJSON[[]string](m.EvidenceIDsJSON),
		ReasoningSummary: m.ReasoningSummary, Conditions: fromJSON[[]entity.DecisionCondition](m.ConditionsJSON),
		CreatedAt: m.CreatedAt,
	}
}

func submissionFromModel(m A2ASubmissionModel) a2aapp.Submission {
	return a2aapp.Submission{ID: m.ID, MessageID: m.MessageID, RequestHash: m.RequestHash, TaskID: m.TaskID,
		ContextID: m.ContextID, InputMessageID: m.InputMessageID, UserID: m.UserID,
		State: a2aapp.SubmissionState(m.State), ErrorCode: m.ErrorCode, CreatedAt: m.CreatedAt, UpdatedAt: m.UpdatedAt}
}

func isUniqueViolation(err error) bool {
	text := strings.ToLower(err.Error())
	return strings.Contains(text, "unique constraint") || strings.Contains(text, "duplicate entry")
}

func isTerminalCaseStatus(status entity.CaseStatus) bool {
	switch status {
	case entity.CaseStatusResolved, entity.CaseStatusMemoryIndexed, entity.CaseStatusFailed,
		entity.CaseStatusTimedOut, entity.CaseStatusInsufficientEv, entity.CaseStatusDeadlocked:
		return true
	default:
		return false
	}
}

var _ a2aapp.SubmissionRepository = (*a2aSubmissionRepo)(nil)
