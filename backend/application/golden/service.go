package golden

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/jamespud/magi/backend/application/dataset"
	"github.com/jamespud/magi/backend/domain/entity"
	"github.com/jamespud/magi/backend/domain/port"
)

// Service manages online-golden decision cases and keeps the built-in
// regression suite in sync with real production decisions.
type Service struct {
	repo     port.GoldenRepository
	cases    port.CaseRepository
	res      port.ResolutionRepository
	datasets datasetWriter
}

// datasetWriter is the minimal dataset surface used to sync golden cases into
// the built-in regression suite.
type datasetWriter interface {
	CreateDataset(ctx context.Context, d *entity.BenchmarkDataset) error
	ListDatasets(ctx context.Context) ([]*entity.BenchmarkDataset, error)
	CreateItems(ctx context.Context, items []*entity.BenchmarkItem) error
	UpdateDataset(ctx context.Context, d *entity.BenchmarkDataset) error
}

func NewService(repo port.GoldenRepository, cases port.CaseRepository, res port.ResolutionRepository, datasets datasetWriter) *Service {
	return &Service{repo: repo, cases: cases, res: res, datasets: datasets}
}

// Add promotes a completed production case to the golden set.
func (s *Service) Add(ctx context.Context, caseID string) (*entity.GoldenCase, error) {
	if s.repo == nil {
		return nil, fmt.Errorf("golden: repository not configured")
	}
	c, err := s.cases.Get(ctx, caseID)
	if err != nil || c == nil {
		return nil, fmt.Errorf("golden: case not found")
	}
	res, err := s.res.Get(ctx, caseID)
	if err != nil || res == nil || res.FinalDecision == "" {
		return nil, fmt.Errorf("golden: case has no resolution")
	}
	g := &entity.GoldenCase{
		ID: "golden-" + uuid.NewString(), CaseID: caseID,
		Question: c.Question, Context: c.Context,
		ExpectedDecision: res.FinalDecision, CreatedAt: time.Now(),
	}
	if err := s.repo.Create(ctx, g); err != nil {
		return nil, fmt.Errorf("golden: create: %w", err)
	}
	return g, nil
}

// List returns all golden cases.
func (s *Service) List(ctx context.Context) ([]*entity.GoldenCase, error) {
	if s.repo == nil {
		return nil, fmt.Errorf("golden: repository not configured")
	}
	return s.repo.List(ctx)
}

// Delete removes a golden case.
func (s *Service) Delete(ctx context.Context, id string) error {
	if s.repo == nil {
		return fmt.Errorf("golden: repository not configured")
	}
	return s.repo.Delete(ctx, id)
}

// SyncToDataset appends all golden cases to the built-in regression suite so
// the automated regression gate covers real production decisions.
func (s *Service) SyncToDataset(ctx context.Context) (int, error) {
	if s.datasets == nil {
		return 0, fmt.Errorf("golden: dataset repository not configured")
	}
	golden, err := s.repo.List(ctx)
	if err != nil {
		return 0, err
	}
	if len(golden) == 0 {
		return 0, nil
	}
	items := make([]*entity.BenchmarkItem, 0, len(golden))
	now := time.Now()
	for _, g := range golden {
		items = append(items, &entity.BenchmarkItem{
			ID: "ditem-golden-" + g.ID, DatasetID: "", Question: g.Question,
			Context: g.Context, ExpectedDecision: g.ExpectedDecision, Weight: 1,
			Tags: []string{"golden"}, CreatedAt: now,
		})
	}
	suite, err := s.findOrCreateBuiltin(ctx)
	if err != nil {
		return 0, err
	}
	for _, item := range items {
		item.DatasetID = suite.ID
	}
	if err := s.datasets.CreateItems(ctx, items); err != nil {
		return 0, fmt.Errorf("golden: sync items: %w", err)
	}
	suite.ItemCount += len(items)
	return len(items), s.datasets.UpdateDataset(ctx, suite)
}

func (s *Service) findOrCreateBuiltin(ctx context.Context) (*entity.BenchmarkDataset, error) {
	all, err := s.datasets.ListDatasets(ctx)
	if err != nil {
		return nil, err
	}
	for _, d := range all {
		if d != nil && d.Name == dataset.BuiltinBenchmarkName {
			return d, nil
		}
	}
	created := &entity.BenchmarkDataset{
		ID: "dataset-" + uuid.NewString(), OwnerID: 0,
		Name:        dataset.BuiltinBenchmarkName,
		Description: "Reusable decision sanity suite: grounded approve/reject/conditional cases across database, procurement, SRE, security, and strategy.",
		CreatedAt:   time.Now(), UpdatedAt: time.Now(),
	}
	if err := s.datasets.CreateDataset(ctx, created); err != nil {
		return nil, err
	}
	return created, nil
}
