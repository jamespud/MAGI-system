package port

import (
	"context"
	"time"

	"github.com/jamespud/magi/backend/domain/entity"
)

// DatasetRepository persists ground-truth datasets, benchmark runs, and
// per-case results. It is separate from Repository so existing aggregate
// fakes remain valid.
type DatasetRepository interface {
	CreateDataset(ctx context.Context, d *entity.BenchmarkDataset) error
	GetDataset(ctx context.Context, id string) (*entity.BenchmarkDataset, error)
	ListDatasets(ctx context.Context) ([]*entity.BenchmarkDataset, error)
	UpdateDataset(ctx context.Context, d *entity.BenchmarkDataset) error
	// DeleteDataset removes a dataset and its items, runs, and run results
	// (P2 D16). Active runs are cancelled by the caller before deletion.
	DeleteDataset(ctx context.Context, id string) error
	CreateItems(ctx context.Context, items []*entity.BenchmarkItem) error
	ListItems(ctx context.Context, datasetID string) ([]*entity.BenchmarkItem, error)
	GetItem(ctx context.Context, id string) (*entity.BenchmarkItem, error)
	UpdateItem(ctx context.Context, item *entity.BenchmarkItem) error
	DeleteItem(ctx context.Context, id string) error
	CreateRun(ctx context.Context, r *entity.BenchmarkRun) error
	// ClaimRun atomically transitions a queued/running run into the caller's
	// lease, returning false when another replica already owns it.
	ClaimRun(ctx context.Context, runID, owner string, leaseUntil *time.Time) (bool, error)
	// ExpireRunLeases requeues runs whose lease expired (owner crashed), so a
	// replica can claim and resume them.
	ExpireRunLeases(ctx context.Context, now time.Time) error
	UpdateRun(ctx context.Context, r *entity.BenchmarkRun) error
	GetRun(ctx context.Context, id string) (*entity.BenchmarkRun, error)
	ListRuns(ctx context.Context, datasetID string) ([]*entity.BenchmarkRun, error)
	ListAllRuns(ctx context.Context) ([]*entity.BenchmarkRun, error)
	CreateItemResult(ctx context.Context, r *entity.BenchmarkItemResult) error
	ListItemResults(ctx context.Context, runID string) ([]*entity.BenchmarkItemResult, error)
	UpdateFeedback(ctx context.Context, resultID, feedback string, at time.Time) error
}
