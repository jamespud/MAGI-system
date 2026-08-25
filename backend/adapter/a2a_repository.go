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

		binding := A2ASubmissionModel{
			ID: cmd.SubmissionID, UserID: cmd.UserID, MessageID: cmd.MessageID,
			RequestHash: cmd.RequestHash, TaskID: cmd.TaskID, ContextID: cmd.ContextID,
			InputMessageID: cmd.InputMessageID, CaseMessageID: cmd.CaseMessageID,
			State: string(a2aapp.SubmissionPrepared),
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

		now := time.Now().UTC()
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

func (r *a2aSubmissionRepo) ListTasks(ctx context.Context, filter a2aapp.TaskListFilter) (*a2aapp.TaskPage, error) {
	limit := filter.Limit
	if limit <= 0 || limit > 100 {
		limit = 50
	}
	q := r.db.WithContext(ctx).Model(&A2ASubmissionModel{}).Where("user_id = ?", filter.UserID)
	if filter.ContextID != "" {
		q = q.Where("context_id = ?", filter.ContextID)
	}
	var total int64
	if err := q.Count(&total).Error; err != nil {
		return nil, err
	}
	if filter.After != nil {
		q = q.Where("created_at < ? OR (created_at = ? AND task_id < ?)", filter.After.CreatedAt, filter.After.CreatedAt, filter.After.ID)
	}
	var models []A2ASubmissionModel
	if err := q.Order("created_at DESC, task_id DESC").Limit(limit + 1).Find(&models).Error; err != nil {
		return nil, err
	}
	page := &a2aapp.TaskPage{Total: int(total)}
	if len(models) > limit {
		models = models[:limit]
		last := models[len(models)-1]
		page.Next = &a2aapp.TaskCursor{CreatedAt: last.CreatedAt, ID: last.TaskID}
	}
	for _, model := range models {
		record, err := loadTaskRecord(r.db.WithContext(ctx), model)
		if err != nil {
			return nil, err
		}
		page.Records = append(page.Records, *record)
	}
	return page, nil
}

func (r *a2aSubmissionRepo) CancelTask(ctx context.Context, userID int64, taskID string) (*a2aapp.TaskRecord, a2aapp.CancelOutcome, error) {
	var record *a2aapp.TaskRecord
	outcome := a2aapp.CancelNotFound
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var binding A2ASubmissionModel
		err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("user_id = ? AND task_id = ?", userID, taskID).First(&binding).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
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
		if caseEntity.Status == entity.CaseStatusCancelled {
			outcome = a2aapp.CancelAlreadyCanceled
		} else if isTerminalCaseStatus(caseEntity.Status) {
			outcome = a2aapp.CancelNotCancelable
		} else {
			if err := tx.Model(&CaseModel{}).Where("id = ?", binding.TaskID).Updates(map[string]any{"status": string(entity.CaseStatusCancelled), "updated_at": time.Now().UTC()}).Error; err != nil {
				return err
			}
			if err := tx.Model(&DecisionJobModel{}).Where("case_id = ?", binding.TaskID).Update("status", string(entity.DecisionJobCancelled)).Error; err != nil {
				return err
			}
			outcome = a2aapp.CancelApplied
		}
		var loadErr error
		record, loadErr = loadTaskRecord(tx, binding)
		return loadErr
	})
	return record, outcome, err
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

func loadTaskRecord(db *gorm.DB, binding A2ASubmissionModel) (*a2aapp.TaskRecord, error) {
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
	var resolution ResolutionModel
	if err := db.Where("case_id = ?", binding.TaskID).First(&resolution).Error; err == nil {
		resolutions, loadErr := loadConversationResolutions(db, []*entity.ConversationMessage{{CaseID: binding.TaskID}})
		if loadErr != nil {
			return nil, loadErr
		}
		record.Resolution = resolutions[binding.TaskID]
	} else if !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, err
	}
	var maxSeq uint64
	if err := db.Model(&EventModel{}).Where("case_id = ?", binding.TaskID).Select("COALESCE(MAX(seq), 0)").Scan(&maxSeq).Error; err != nil {
		return nil, err
	}
	record.MaxEventSeq = maxSeq
	return record, nil
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
