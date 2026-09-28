package magi

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/jamespud/magi/backend/domain/entity"
	"github.com/jamespud/magi/backend/domain/port"
)

type approvalRepo struct {
	db *gorm.DB
}

// NewApprovalRepository returns the GORM-backed ApprovalRepository.
func NewApprovalRepository(db *gorm.DB) port.ApprovalRepository {
	return &approvalRepo{db: db}
}

func (r *approvalRepo) Create(ctx context.Context, a *entity.ApprovalRequest) error {
	if a == nil {
		return fmt.Errorf("approval repo: nil request")
	}
	if a.ID == "" {
		a.ID = "appr-" + uuid.NewString()
	}
	if a.CreatedAt.IsZero() {
		a.CreatedAt = time.Now()
	}
	m := ApprovalModel{
		ID: a.ID, CaseID: a.CaseID, RunID: a.RunID, AgentCode: string(a.AgentCode),
		ToolName: a.ToolName, Arguments: a.Arguments, IntentDigest: a.IntentDigest,
		InvocationID: optionalInvocationID(a.InvocationID),
		Status:       string(a.Status), Reason: a.Reason, DecidedBy: a.DecidedBy,
		RequestedAt: a.RequestedAt, DecidedAt: a.DecidedAt, CreatedAt: a.CreatedAt,
	}
	if err := r.db.WithContext(ctx).Create(&m).Error; err != nil {
		if isUniqueViolation(err) {
			return fmt.Errorf("%w: invocation %s", port.ErrApprovalConflict, a.InvocationID)
		}
		return err
	}
	return nil
}

func (r *approvalRepo) Get(ctx context.Context, id string) (*entity.ApprovalRequest, error) {
	var m ApprovalModel
	if err := r.db.WithContext(ctx).First(&m, "id = ?", id).Error; err != nil {
		return nil, err
	}
	return approvalFromModel(&m), nil
}

func (r *approvalRepo) FindByInvocation(ctx context.Context, caseID, invocationID string) (*entity.ApprovalRequest, error) {
	// Fail closed on an empty invocation: rows persisted before invocation
	// binding existed carry NULL there, and resolving them would restore the
	// reuse-across-calls bug this key exists to prevent.
	if invocationID == "" {
		return nil, nil
	}
	var m ApprovalModel
	err := r.db.WithContext(ctx).
		Where("case_id = ? AND invocation_id = ?", caseID, invocationID).
		First(&m).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return approvalFromModel(&m), nil
}

func (r *approvalRepo) List(ctx context.Context, caseID string) ([]*entity.ApprovalRequest, error) {
	var models []ApprovalModel
	if err := r.db.WithContext(ctx).Where("case_id = ?", caseID).Order("created_at DESC, id DESC").Find(&models).Error; err != nil {
		return nil, err
	}
	out := make([]*entity.ApprovalRequest, len(models))
	for i := range models {
		out[i] = approvalFromModel(&models[i])
	}
	return out, nil
}

func (r *approvalRepo) ListAll(ctx context.Context) ([]*entity.ApprovalRequest, error) {
	var models []ApprovalModel
	if err := r.db.WithContext(ctx).Order("created_at DESC, id DESC").Find(&models).Error; err != nil {
		return nil, err
	}
	out := make([]*entity.ApprovalRequest, len(models))
	for i := range models {
		out[i] = approvalFromModel(&models[i])
	}
	return out, nil
}

func (r *approvalRepo) Approve(ctx context.Context, id, decidedBy, reason string) error {
	return r.decide(ctx, id, entity.ApprovalApproved, decidedBy, reason)
}

func (r *approvalRepo) Reject(ctx context.Context, id, decidedBy, reason string) error {
	return r.decide(ctx, id, entity.ApprovalRejected, decidedBy, reason)
}

func (r *approvalRepo) decide(ctx context.Context, id string, status entity.ApprovalStatus, decidedBy, reason string) error {
	now := time.Now()
	result := r.db.WithContext(ctx).Model(&ApprovalModel{}).
		Where("id = ? AND status = ?", id, string(entity.ApprovalPending)).
		Updates(map[string]any{
			"status": string(status), "decided_by": decidedBy, "reason": reason, "decided_at": now,
		})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return fmt.Errorf("approval %q is not pending or not found", id)
	}
	return nil
}

func (r *approvalRepo) MarkExpired(ctx context.Context, id string) error {
	now := time.Now()
	result := r.db.WithContext(ctx).Model(&ApprovalModel{}).
		Where("id = ? AND status = ?", id, string(entity.ApprovalPending)).
		Updates(map[string]any{"status": string(entity.ApprovalExpired), "decided_at": now})
	if result.Error != nil {
		return result.Error
	}
	return nil
}

func approvalFromModel(m *ApprovalModel) *entity.ApprovalRequest {
	invocationID := ""
	if m.InvocationID != nil {
		invocationID = *m.InvocationID
	}
	return &entity.ApprovalRequest{
		ID: m.ID, CaseID: m.CaseID, RunID: m.RunID, AgentCode: entity.MagiCode(m.AgentCode),
		ToolName: m.ToolName, Arguments: m.Arguments, IntentDigest: m.IntentDigest,
		InvocationID: invocationID,
		Status:       entity.ApprovalStatus(m.Status), Reason: m.Reason, DecidedBy: m.DecidedBy,
		RequestedAt: m.RequestedAt, DecidedAt: m.DecidedAt, CreatedAt: m.CreatedAt,
	}
}

// optionalInvocationID stores an empty invocation as SQL NULL so pre-existing
// rows and new rows never share a key, and so the unique index tolerates the
// repeated NULLs of a legacy table.
func optionalInvocationID(invocationID string) *string {
	if invocationID == "" {
		return nil
	}
	return &invocationID
}

var _ port.ApprovalRepository = (*approvalRepo)(nil)
