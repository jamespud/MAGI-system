package magi

import (
	"context"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/jamespud/magi/backend/domain/entity"
	"github.com/jamespud/magi/backend/domain/port"
)

// TaskNodeModel persists one node of the case task state tree.
type TaskNodeModel struct {
	ID          string `gorm:"primaryKey"`
	CaseID      string `gorm:"uniqueIndex:idx_task_node_case_run_agent"`
	ParentID    string
	RunID       string `gorm:"uniqueIndex:idx_task_node_case_run_agent"`
	Kind        string `gorm:"index"`
	Title       string
	Status      string
	Detail      string `gorm:"type:text"`
	CreatedAt   time.Time
	CompletedAt *time.Time
	AgentCode   string `gorm:"uniqueIndex:idx_task_node_case_run_agent"`
}

func (TaskNodeModel) TableName() string { return "task_node" }

type taskTreeRepo struct{ db *gorm.DB }

func NewTaskTreeRepository(db *gorm.DB) port.TaskTreeRepository {
	return &taskTreeRepo{db: db}
}

// RecordAgent upserts one task node per (case_id, run_id, agent_code): a
// retried or re-run agent (stable checkpointRunID) updates the existing node
// instead of appending duplicates, so the task tree cannot grow unboundedly
// across retries/re-runs (task-tree growth fix).
func (r *taskTreeRepo) RecordAgent(ctx context.Context, caseID, runID, agentCode, status string) error {
	now := time.Now()
	node := TaskNodeModel{
		ID: "task-" + uuid.NewString(), CaseID: caseID, RunID: runID,
		Kind: entity.TaskNodeKindAgent, Title: agentCode, Status: status,
		CreatedAt: now, CompletedAt: &now, AgentCode: agentCode,
	}
	return r.db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "case_id"}, {Name: "run_id"}, {Name: "agent_code"}},
		DoUpdates: clause.AssignmentColumns([]string{"status", "completed_at"}),
	}).Create(&node).Error
}

// DeleteByCase removes a case's task nodes (fresh tree on re-run and cascade
// on case delete).
func (r *taskTreeRepo) DeleteByCase(ctx context.Context, caseID string) error {
	return r.db.WithContext(ctx).Where("case_id = ?", caseID).Delete(&TaskNodeModel{}).Error
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
var _ port.TaskTreeCleaner = (*taskTreeRepo)(nil)
