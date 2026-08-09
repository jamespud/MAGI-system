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
	CreateItems(ctx context.Context, items []*entity.BenchmarkItem) error
	ListItems(ctx context.Context, datasetID string) ([]*entity.BenchmarkItem, error)
	GetItem(ctx context.Context, id string) (*entity.BenchmarkItem, error)
	UpdateItem(ctx context.Context, item *entity.BenchmarkItem) error
	DeleteItem(ctx context.Context, id string) error
	CreateRun(ctx context.Context, r *entity.BenchmarkRun) error
	UpdateRun(ctx context.Context, r *entity.BenchmarkRun) error
	GetRun(ctx context.Context, id string) (*entity.BenchmarkRun, error)
	ListRuns(ctx context.Context, datasetID string) ([]*entity.BenchmarkRun, error)
	ListAllRuns(ctx context.Context) ([]*entity.BenchmarkRun, error)
	CreateItemResult(ctx context.Context, r *entity.BenchmarkItemResult) error
	ListItemResults(ctx context.Context, runID string) ([]*entity.BenchmarkItemResult, error)
	UpdateFeedback(ctx context.Context, resultID, feedback string, at time.Time) error
}
