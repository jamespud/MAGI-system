package magi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math/rand/v2"
	"strings"
	"time"

	"github.com/go-sql-driver/mysql"

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
		retryable := errors.Is(err, errConcurrentConversationInsert) || isMySQLDeadlock(err)
		if !retryable {
			return prepared, created, err
		}
		if attempt == contextContentionMaxAttempts-1 {
			if isMySQLDeadlock(err) {
				return nil, false, fmt.Errorf("a2a prepare deadlock after %d attempts: %v", contextContentionMaxAttempts, err)
			}
			return nil, false, fmt.Errorf("%w after %d attempts: %v", a2aapp.ErrContextContention, contextContentionMaxAttempts, err)
		}

		// A competing transaction is creating this ContextID or a deadlock was
		// detected under concurrent same-message submission. The binding
		// transaction rolled back, so retrying will lock the committed row
		// instead. Back off so contention cannot spin the DB.
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

// isMySQLDeadlock reports whether err is MySQL error 1213 (lock deadlock),
// which is retryable under concurrent same-message submission.
func isMySQLDeadlock(err error) bool {
	var mysqlErr *mysql.MySQLError
	return errors.As(err, &mysqlErr) && mysqlErr.Number == 1213
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
		now := time.Now().UTC()
		binding := A2ASubmissionModel{
			ID: cmd.SubmissionID, UserID: cmd.UserID, MessageID: cmd.MessageID,
			RequestHash: cmd.RequestHash, TaskID: cmd.TaskID, ContextID: cmd.ContextID,
			InputMessageID: cmd.InputMessageID, CaseMessageID: cmd.CaseMessageID,
			State: string(a2aapp.SubmissionPrepared), CreatedAt: now, UpdatedAt: now,
			AcceptedOutputModesJSON: marshalOutputModes(cmd.AcceptedOutputModes),
		}
		// Atomic upsert on the (user_id, message_id) unique key: a single
		// INSERT ... ON DUPLICATE KEY UPDATE avoids the SELECT-then-INSERT
		// gap-lock deadlock that concurrent same-message submission triggers on
		// MySQL. RowsAffected==1 means we created it; 0 means a competing
		// transaction won and we replay its committed row.
		res := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&binding)
		if res.Error != nil {
			return res.Error
		}
		if res.RowsAffected == 0 {
			var existing A2ASubmissionModel
			if err := tx.Where("user_id = ? AND message_id = ?", cmd.UserID, cmd.MessageID).First(&existing).Error; err != nil {
				return err
			}
			if existing.RequestHash != cmd.RequestHash {
				return a2aapp.ErrIdempotencyConflict
			}
			var loadErr error
			prepared, loadErr = loadPreparedSubmission(tx, existing)
			return loadErr
		}
		created = true

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
	now := time.Now().UTC()
	conv := &entity.Conversation{ID: cmd.ContextID, UserID: cmd.UserID, Title: a2aConversationTitle(cmd.Question), CreatedAt: now, UpdatedAt: now}
	// Atomic upsert: a single INSERT ... ON DUPLICATE KEY UPDATE avoids the
	// SELECT-then-INSERT gap-lock deadlock when concurrent submitters share one
	// new ContextID. RowsAffected==1 means we created it.
	res := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(conversationToModel(conv))
	if res.Error != nil {
		if isUniqueViolation(res.Error) {
			return nil, nil, fmt.Errorf("%w: %v", errConcurrentConversationInsert, res.Error)
		}
		return nil, nil, res.Error
	}
	if res.RowsAffected == 1 {
		return conv, nil, nil
	}

	// A competing transaction created this ContextID first; lock and hydrate it.
	var model ConversationModel
	if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ?", cmd.ContextID).First(&model).Error; err != nil {
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

// ClaimStart atomically claims a PREPARED or expired-STARTING binding. The row
// lock serializes concurrent replicas, so exactly one claimant wins; a live
// claim held by another replica is not stealable until it expires.
func (r *a2aSubmissionRepo) ClaimStart(ctx context.Context, id, token string, leaseUntil time.Time) (*a2aapp.PreparedSubmission, bool, error) {
	var claimed *a2aapp.PreparedSubmission
	ok := false
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var binding A2ASubmissionModel
		err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ?", id).First(&binding).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil
		}
		if err != nil {
			return err
		}
		now := time.Now().UTC()
		switch a2aapp.SubmissionState(binding.State) {
		case a2aapp.SubmissionPrepared:
			// Always claimable.
		case a2aapp.SubmissionStarting:
			if binding.StartClaimUntil != nil && binding.StartClaimUntil.After(now) {
				return nil // another replica holds a live claim
			}
		default:
			return nil // STARTED / REJECTED are already settled
		}
		if err := tx.Model(&A2ASubmissionModel{}).Where("id = ?", id).Updates(map[string]any{
			"state":             string(a2aapp.SubmissionStarting),
			"start_claim_token": token, "start_claim_until": leaseUntil,
			"updated_at": now,
		}).Error; err != nil {
			return err
		}
		prepared, err := loadPreparedSubmission(tx, binding)
		if err != nil {
			return err
		}
		claimed, ok = prepared, true
		return nil
	})
	return claimed, ok, err
}

// SettleStarted finalizes a claimed STARTING binding to STARTED. It is a
// conditional write: only the current claim owner may settle, so a loser
// replica can never overwrite the winner's settlement.
func (r *a2aSubmissionRepo) SettleStarted(ctx context.Context, id, token string) error {
	res := r.db.WithContext(ctx).Model(&A2ASubmissionModel{}).
		Where("id = ? AND state = ? AND start_claim_token = ?", id, string(a2aapp.SubmissionStarting), token).
		Updates(map[string]any{
			"state":             string(a2aapp.SubmissionStarted),
			"start_claim_token": "", "start_claim_until": nil,
			"updated_at": time.Now().UTC(),
		})
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected != 1 {
		return a2aapp.ErrClaimLost
	}
	return nil
}

// SettleRejected finalizes a claimed STARTING binding to REJECTED with a
// stable code. Only the current claim owner may settle.
func (r *a2aSubmissionRepo) SettleRejected(ctx context.Context, id, token, code string) error {
	res := r.db.WithContext(ctx).Model(&A2ASubmissionModel{}).
		Where("id = ? AND state = ? AND start_claim_token = ?", id, string(a2aapp.SubmissionStarting), token).
		Updates(map[string]any{
			"state": string(a2aapp.SubmissionRejected), "error_code": code,
			"start_claim_token": "", "start_claim_until": nil,
			"updated_at": time.Now().UTC(),
		})
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected != 1 {
		return a2aapp.ErrClaimLost
	}
	return nil
}

// ListClaimable pages over PREPARED and STARTING bindings in stable id order
// so a recovery sweep can advance past a first page that only makes transient
// progress and never starve later rows. Live-lease expiry is decided by
// ClaimStart in Go (time comparisons are not portable across SQL dialects for
// DATETIME string formats), so STARTING rows that still hold a live claim are
// simply skipped there.
func (r *a2aSubmissionRepo) ListClaimable(ctx context.Context, limit int, afterID string) ([]*a2aapp.PreparedSubmission, error) {
	if limit <= 0 || limit > 100 {
		limit = 100
	}
	q := r.db.WithContext(ctx).
		Where("state = ? OR state = ?",
			string(a2aapp.SubmissionPrepared), string(a2aapp.SubmissionStarting))
	if afterID != "" {
		q = q.Where("id > ?", afterID)
	}
	var models []A2ASubmissionModel
	if err := q.Order("id ASC").Limit(limit).Find(&models).Error; err != nil {
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
		State: a2aapp.SubmissionState(m.State), ErrorCode: m.ErrorCode,
		AcceptedOutputModes: parseOutputModes(m.AcceptedOutputModesJSON),
		CreatedAt:           m.CreatedAt, UpdatedAt: m.UpdatedAt}
}

func marshalOutputModes(modes []string) string {
	if len(modes) == 0 {
		return `["text/markdown","application/json"]`
	}
	ordered := make([]string, 0, 2)
	seen := map[string]bool{}
	for _, mode := range []string{"text/markdown", "application/json"} {
		for _, in := range modes {
			if in == mode && !seen[mode] {
				ordered = append(ordered, mode)
				seen[mode] = true
				break
			}
		}
	}
	raw, err := json.Marshal(ordered)
	if err != nil {
		return `["text/markdown","application/json"]`
	}
	return string(raw)
}

func parseOutputModes(raw string) []string {
	if raw == "" {
		return []string{"text/markdown", "application/json"}
	}
	var modes []string
	if err := json.Unmarshal([]byte(raw), &modes); err != nil {
		// Fail closed: malformed stored JSON must not broaden output.
		return []string{"text/markdown", "application/json"}
	}
	if len(modes) == 0 {
		return []string{"text/markdown", "application/json"}
	}
	return modes
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
