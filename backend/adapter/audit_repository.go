package magi

import (
	"context"
	"time"

	"gorm.io/gorm"

	"github.com/jamespud/magi/backend/domain/entity"
	"github.com/jamespud/magi/backend/domain/port"
)

// AuditLogModel persists one audit-trail row.
type AuditLogModel struct {
	ID        int64 `gorm:"primaryKey"`
	UserID    int64 `gorm:"index"`
	Username  string
	Role      string
	Action    string `gorm:"index"`
	Resource  string
	Detail    string `gorm:"type:text"`
	Status    int
	CreatedAt time.Time `gorm:"index"`
}

func (AuditLogModel) TableName() string { return "audit_log" }

type auditRepo struct{ db *gorm.DB }

func NewAuditRepository(db *gorm.DB) port.AuditRepository {
	return &auditRepo{db: db}
}

func (r *auditRepo) Record(ctx context.Context, e *entity.AuditEvent) error {
	if e.CreatedAt.IsZero() {
		e.CreatedAt = time.Now()
	}
	return r.db.WithContext(ctx).Create(&AuditLogModel{
		UserID: e.UserID, Username: e.Username, Role: e.Role,
		Action: e.Action, Resource: e.Resource, Detail: e.Detail,
		Status: e.Status, CreatedAt: e.CreatedAt,
	}).Error
}

func (r *auditRepo) List(ctx context.Context, limit, offset int) ([]*entity.AuditEvent, int64, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	if offset < 0 {
		offset = 0
	}
	var total int64
	if err := r.db.WithContext(ctx).Model(&AuditLogModel{}).Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var rows []AuditLogModel
	if err := r.db.WithContext(ctx).Order("created_at DESC, id DESC").Limit(limit).Offset(offset).Find(&rows).Error; err != nil {
		return nil, 0, err
	}
	out := make([]*entity.AuditEvent, 0, len(rows))
	for i := range rows {
		row := &rows[i]
		out = append(out, &entity.AuditEvent{
			ID: row.ID, UserID: row.UserID, Username: row.Username, Role: row.Role,
			Action: row.Action, Resource: row.Resource, Detail: row.Detail,
			Status: row.Status, CreatedAt: row.CreatedAt,
		})
	}
	return out, total, nil
}

var _ port.AuditRepository = (*auditRepo)(nil)
