package magi

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/jamespud/magi/backend/domain/entity"
	"github.com/jamespud/magi/backend/domain/port"
)

type datasetRepo struct{ db *gorm.DB }

func NewDatasetRepository(db *gorm.DB) port.DatasetRepository {
	return &datasetRepo{db: db}
}

func (r *datasetRepo) CreateDataset(ctx context.Context, d *entity.BenchmarkDataset) error {
	if d.ID == "" {
		d.ID = "dataset-" + uuid.NewString()
	}
	now := time.Now()
	if d.CreatedAt.IsZero() {
		d.CreatedAt = now
	}
	d.UpdatedAt = now
	m := DatasetModel{ID: d.ID, OwnerID: d.OwnerID, Name: d.Name, Description: d.Description, ItemCount: d.ItemCount, CreatedAt: d.CreatedAt, UpdatedAt: d.UpdatedAt}
	return r.db.WithContext(ctx).Create(&m).Error
}

func (r *datasetRepo) GetDataset(ctx context.Context, id string) (*entity.BenchmarkDataset, error) {
	var m DatasetModel
	if err := r.db.WithContext(ctx).First(&m, "id = ?", id).Error; err != nil {
		return nil, err
	}
	return datasetFromModel(&m), nil
}

func (r *datasetRepo) ListDatasets(ctx context.Context) ([]*entity.BenchmarkDataset, error) {
	var models []DatasetModel
	if err := r.db.WithContext(ctx).Order("created_at DESC").Find(&models).Error; err != nil {
		return nil, err
	}
	out := make([]*entity.BenchmarkDataset, len(models))
	for i := range models {
		out[i] = datasetFromModel(&models[i])
	}
	return out, nil
}

func (r *datasetRepo) UpdateDataset(ctx context.Context, d *entity.BenchmarkDataset) error {
	d.UpdatedAt = time.Now()
	return r.db.WithContext(ctx).Model(&DatasetModel{}).Where("id = ?", d.ID).Updates(map[string]any{
		"name": d.Name, "description": d.Description, "item_count": d.ItemCount, "owner_id": d.OwnerID, "updated_at": d.UpdatedAt,
	}).Error
}

func (r *datasetRepo) CreateItems(ctx context.Context, items []*entity.BenchmarkItem) error {
	if len(items) == 0 {
		return nil
	}
	now := time.Now()
	models := make([]DatasetItemModel, 0, len(items))
	for _, it := range items {
		if it.ID == "" {
			it.ID = "ditem-" + uuid.NewString()
		}
		if it.CreatedAt.IsZero() {
			it.CreatedAt = now
		}
		models = append(models, DatasetItemModel{
			ID: it.ID, DatasetID: it.DatasetID, Question: it.Question, Context: it.Context,
			ConstraintsJSON: toJSON(it.Constraints), ExpectedDecision: string(it.ExpectedDecision),
			Weight: it.Weight, TagsJSON: toJSON(it.Tags), CreatedAt: it.CreatedAt,
		})
	}
	return r.db.WithContext(ctx).Create(&models).Error
}

func (r *datasetRepo) ListItems(ctx context.Context, datasetID string) ([]*entity.BenchmarkItem, error) {
	var models []DatasetItemModel
	if err := r.db.WithContext(ctx).Where("dataset_id = ?", datasetID).Order("created_at ASC").Find(&models).Error; err != nil {
		return nil, err
	}
	out := make([]*entity.BenchmarkItem, len(models))
	for i := range models {
		m := &models[i]
		out[i] = &entity.BenchmarkItem{
			ID: m.ID, DatasetID: m.DatasetID, Question: m.Question, Context: m.Context,
			Constraints:      fromJSON[[]entity.Constraint](m.ConstraintsJSON),
			ExpectedDecision: entity.VoteDecision(m.ExpectedDecision), Weight: m.Weight,
			Tags: fromJSON[[]string](m.TagsJSON), CreatedAt: m.CreatedAt,
		}
	}
	return out, nil
}

func (r *datasetRepo) GetItem(ctx context.Context, id string) (*entity.BenchmarkItem, error) {
	var m DatasetItemModel
	if err := r.db.WithContext(ctx).First(&m, "id = ?", id).Error; err != nil {
		return nil, err
	}
	return &entity.BenchmarkItem{
		ID: m.ID, DatasetID: m.DatasetID, Question: m.Question, Context: m.Context,
		Constraints: fromJSON[[]entity.Constraint](m.ConstraintsJSON), ExpectedDecision: entity.VoteDecision(m.ExpectedDecision),
		Weight: m.Weight, Tags: fromJSON[[]string](m.TagsJSON), CreatedAt: m.CreatedAt,
	}, nil
}

func (r *datasetRepo) UpdateItem(ctx context.Context, it *entity.BenchmarkItem) error {
	return r.db.WithContext(ctx).Model(&DatasetItemModel{}).Where("id = ?", it.ID).Updates(map[string]any{
		"question": it.Question, "context": it.Context, "constraints_json": toJSON(it.Constraints),
		"expected_decision": string(it.ExpectedDecision), "weight": it.Weight, "tags_json": toJSON(it.Tags),
	}).Error
}

func (r *datasetRepo) DeleteItem(ctx context.Context, id string) error {
	return r.db.WithContext(ctx).Where("id = ?", id).Delete(&DatasetItemModel{}).Error
}

func (r *datasetRepo) CreateRun(ctx context.Context, run *entity.BenchmarkRun) error {
	if run.ID == "" {
		run.ID = "bench-" + uuid.NewString()
	}
	if run.StartedAt.IsZero() {
		run.StartedAt = time.Now()
	}
	if run.CreatedAt.IsZero() {
		run.CreatedAt = run.StartedAt
	}
	m := BenchmarkRunModel{ID: run.ID, DatasetID: run.DatasetID, Status: string(run.Status), Total: run.Total, Matched: run.Matched, Accuracy: run.Accuracy, WeightedAccuracy: run.WeightedAccuracy, RunsPerItem: run.RunsPerItem, Stability: run.Stability, RegressionThreshold: run.RegressionThreshold, RegressionFailed: run.RegressionFailed, FailureReason: run.FailureReason, StartedAt: run.StartedAt, CompletedAt: run.CompletedAt, CreatedAt: run.CreatedAt}
	return r.db.WithContext(ctx).Create(&m).Error
}

func (r *datasetRepo) UpdateRun(ctx context.Context, run *entity.BenchmarkRun) error {
	return r.db.WithContext(ctx).Model(&BenchmarkRunModel{}).Where("id = ?", run.ID).Updates(map[string]any{
		"status": string(run.Status), "total": run.Total, "matched": run.Matched,
		"accuracy": run.Accuracy, "weighted_accuracy": run.WeightedAccuracy, "runs_per_item": run.RunsPerItem,
		"stability": run.Stability, "regression_threshold": run.RegressionThreshold,
		"regression_failed": run.RegressionFailed, "failure_reason": run.FailureReason, "completed_at": run.CompletedAt,
	}).Error
}

func (r *datasetRepo) GetRun(ctx context.Context, id string) (*entity.BenchmarkRun, error) {
	var m BenchmarkRunModel
	if err := r.db.WithContext(ctx).First(&m, "id = ?", id).Error; err != nil {
		return nil, err
	}
	return runFromModel(&m), nil
}

// ClaimRun claims a run for one worker: only a run that is queued/running with
// no unexpired lease can be claimed, and the status flip happens in the same
// UPDATE, so two replicas cannot both win.
func (r *datasetRepo) ClaimRun(ctx context.Context, runID, owner string, leaseUntil *time.Time) (bool, error) {
	if owner == "" || leaseUntil == nil {
		return false, fmt.Errorf("claim run: owner and lease are required")
	}
	res := r.db.WithContext(ctx).Model(&BenchmarkRunModel{}).
		Where("id = ? AND status IN ? AND (lease_until IS NULL OR lease_until < ?)",
			runID, []string{"queued", "running"}, time.Now()).
		Updates(map[string]any{
			"status":      "running",
			"lease_owner": owner,
			"lease_until": *leaseUntil,
		})
	if res.Error != nil {
		return false, res.Error
	}
	return res.RowsAffected > 0, nil
}

// ExpireRunLeases releases expired leases back to "queued" so recovery can
// resume interrupted runs.
func (r *datasetRepo) ExpireRunLeases(ctx context.Context, now time.Time) error {
	return r.db.WithContext(ctx).Model(&BenchmarkRunModel{}).
		Where("status = ? AND lease_until IS NOT NULL AND lease_until < ?", "running", now).
		Updates(map[string]any{"lease_owner": "", "lease_until": nil, "status": "queued"}).Error
}

func (r *datasetRepo) ListRuns(ctx context.Context, datasetID string) ([]*entity.BenchmarkRun, error) {
	var models []BenchmarkRunModel
	if err := r.db.WithContext(ctx).Where("dataset_id = ?", datasetID).Order("created_at DESC").Find(&models).Error; err != nil {
		return nil, err
	}
	out := make([]*entity.BenchmarkRun, len(models))
	for i := range models {
		out[i] = runFromModel(&models[i])
	}
	return out, nil
}

func (r *datasetRepo) ListAllRuns(ctx context.Context) ([]*entity.BenchmarkRun, error) {
	var models []BenchmarkRunModel
	if err := r.db.WithContext(ctx).Order("created_at DESC").Find(&models).Error; err != nil {
		return nil, err
	}
	out := make([]*entity.BenchmarkRun, len(models))
	for i := range models {
		out[i] = runFromModel(&models[i])
	}
	return out, nil
}

func (r *datasetRepo) CreateItemResult(ctx context.Context, res *entity.BenchmarkItemResult) error {
	if res.ID == "" {
		res.ID = "bresult-" + uuid.NewString()
	}
	if res.CreatedAt.IsZero() {
		res.CreatedAt = time.Now()
	}
	m := BenchmarkItemResultModel{ID: res.ID, RunID: res.RunID, DatasetItemID: res.DatasetItemID, CaseID: res.CaseID, ExpectedDecision: string(res.ExpectedDecision), ActualDecision: string(res.ActualDecision), Matched: res.Matched, Score: res.Score, Runs: res.Runs, Consistency: res.Consistency, DecisionsJSON: toJSON(res.Decisions), Error: res.Error, Feedback: res.Feedback, FeedbackAt: res.FeedbackAt, CreatedAt: res.CreatedAt}
	return r.db.WithContext(ctx).Create(&m).Error
}

func (r *datasetRepo) UpdateFeedback(ctx context.Context, resultID, feedback string, at time.Time) error {
	return r.db.WithContext(ctx).Model(&BenchmarkItemResultModel{}).Where("id = ?", resultID).Updates(map[string]any{
		"feedback": feedback, "feedback_at": at,
	}).Error
}

func (r *datasetRepo) ListItemResults(ctx context.Context, runID string) ([]*entity.BenchmarkItemResult, error) {
	var models []BenchmarkItemResultModel
	if err := r.db.WithContext(ctx).Where("run_id = ?", runID).Order("created_at ASC").Find(&models).Error; err != nil {
		return nil, err
	}
	out := make([]*entity.BenchmarkItemResult, len(models))
	for i := range models {
		m := &models[i]
		out[i] = &entity.BenchmarkItemResult{ID: m.ID, RunID: m.RunID, DatasetItemID: m.DatasetItemID, CaseID: m.CaseID, ExpectedDecision: entity.VoteDecision(m.ExpectedDecision), ActualDecision: entity.VoteDecision(m.ActualDecision), Matched: m.Matched, Score: m.Score, Runs: m.Runs, Consistency: m.Consistency, Decisions: fromJSON[[]entity.VoteDecision](m.DecisionsJSON), Error: m.Error, Feedback: m.Feedback, FeedbackAt: m.FeedbackAt, CreatedAt: m.CreatedAt}
	}
	return out, nil
}

func datasetFromModel(m *DatasetModel) *entity.BenchmarkDataset {
	return &entity.BenchmarkDataset{ID: m.ID, OwnerID: m.OwnerID, Name: m.Name, Description: m.Description, ItemCount: m.ItemCount, CreatedAt: m.CreatedAt, UpdatedAt: m.UpdatedAt}
}

func runFromModel(m *BenchmarkRunModel) *entity.BenchmarkRun {
	return &entity.BenchmarkRun{ID: m.ID, DatasetID: m.DatasetID, Status: entity.BenchmarkRunStatus(m.Status), LeaseOwner: m.LeaseOwner, LeaseUntil: m.LeaseUntil, Total: m.Total, Matched: m.Matched, Accuracy: m.Accuracy, WeightedAccuracy: m.WeightedAccuracy, RunsPerItem: m.RunsPerItem, Stability: m.Stability, RegressionThreshold: m.RegressionThreshold, RegressionFailed: m.RegressionFailed, FailureReason: m.FailureReason, StartedAt: m.StartedAt, CompletedAt: m.CompletedAt, CreatedAt: m.CreatedAt}
}

var _ port.DatasetRepository = (*datasetRepo)(nil)
