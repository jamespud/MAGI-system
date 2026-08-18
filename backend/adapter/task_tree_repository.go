package magi

import (
	"context"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/jamespud/magi/backend/domain/entity"
	"github.com/jamespud/magi/backend/domain/port"
)

// TaskNodeModel persists one node of the case task state tree.
type TaskNodeModel struct {
	ID          string `gorm:"primaryKey"`
	CaseID      string `gorm:"index"`
	ParentID    string
	RunID       string
	Kind        string `gorm:"index"`
	Title       string
	Status      string
	Detail      string `gorm:"type:text"`
	CreatedAt   time.Time
	CompletedAt *time.Time
}

func (TaskNodeModel) TableName() string { return "task_node" }

type taskTreeRepo struct{ db *gorm.DB }

func NewTaskTreeRepository(db *gorm.DB) port.TaskTreeRepository {
	return &taskTreeRepo{db: db}
}

func (r *taskTreeRepo) RecordAgent(ctx context.Context, caseID, runID, agentCode, status string) error {
	now := time.Now()
	node := TaskNodeModel{
		ID: "task-" + uuid.NewString(), CaseID: caseID, RunID: runID,
		Kind: entity.TaskNodeKindAgent, Title: agentCode, Status: status,
		CreatedAt: now, CompletedAt: &now,
	}
	return r.db.WithContext(ctx).Create(&node).Error
}

func (r *taskTreeRepo) ListByCase(ctx context.Context, caseID string) ([]*entity.TaskNode, error) {
	var models []TaskNodeModel
	if err := r.db.WithContext(ctx).Where("case_id = ?", caseID).Order("created_at ASC").Find(&models).Error; err != nil {
		return nil, err
	}
	out := make([]*entity.TaskNode, 0, len(models))
	for i := range models {
		m := &models[i]
		out = append(out, &entity.TaskNode{
			ID: m.ID, CaseID: m.CaseID, ParentID: m.ParentID, RunID: m.RunID,
			Kind: m.Kind, Title: m.Title, Status: m.Status, Detail: m.Detail,
			CreatedAt: m.CreatedAt, CompletedAt: m.CompletedAt,
		})
	}
	return out, nil
}

var _ port.TaskTreeRepository = (*taskTreeRepo)(nil)
var _ port.TaskTreeRecorder = (*taskTreeRepo)(nil)
