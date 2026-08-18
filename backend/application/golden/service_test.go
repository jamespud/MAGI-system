package golden_test

import (
	"context"
	"testing"

	"github.com/jamespud/magi/backend/application/dataset"
	"github.com/jamespud/magi/backend/application/golden"
	"github.com/jamespud/magi/backend/domain/entity"
)

type stubGoldenRepo struct {
	items map[string]*entity.GoldenCase
}

func (s *stubGoldenRepo) Create(ctx context.Context, g *entity.GoldenCase) error {
	s.items[g.ID] = g
	return nil
}
func (s *stubGoldenRepo) List(ctx context.Context) ([]*entity.GoldenCase, error) {
	out := make([]*entity.GoldenCase, 0, len(s.items))
	for _, g := range s.items {
		out = append(out, g)
	}
	return out, nil
}
func (s *stubGoldenRepo) Delete(ctx context.Context, id string) error {
	delete(s.items, id)
	return nil
}

type stubGoldenCaseRepo struct {
	c *entity.DecisionCase
}

func (s *stubGoldenCaseRepo) Create(ctx context.Context, c *entity.DecisionCase) error { return nil }
func (s *stubGoldenCaseRepo) Get(ctx context.Context, id string) (*entity.DecisionCase, error) {
	return s.c, nil
}
func (s *stubGoldenCaseRepo) List(ctx context.Context) ([]*entity.DecisionCase, error) {
	return nil, nil
}
func (s *stubGoldenCaseRepo) ListPaged(ctx context.Context, userID int64, page, pageSize int) ([]*entity.DecisionCase, int64, error) {
	return nil, 0, nil
}
func (s *stubGoldenCaseRepo) UpdateStatus(ctx context.Context, id string, status entity.CaseStatus) error {
	return nil
}
func (s *stubGoldenCaseRepo) UpdateTask(ctx context.Context, id string, task *entity.DecisionTask) error {
	return nil
}
func (s *stubGoldenCaseRepo) UpdateFlags(ctx context.Context, id string, pinned, archived *bool) error {
	return nil
}
func (s *stubGoldenCaseRepo) Delete(ctx context.Context, id string) error { return nil }

type stubGoldenResRepo struct {
	res *entity.Resolution
}

func (s *stubGoldenResRepo) Create(ctx context.Context, r *entity.Resolution) error { return nil }
func (s *stubGoldenResRepo) Get(ctx context.Context, caseID string) (*entity.Resolution, error) {
	return s.res, nil
}

type stubGoldenDatasetRepo struct {
	datasets map[string]*entity.BenchmarkDataset
	items    map[string][]*entity.BenchmarkItem
}

func (s *stubGoldenDatasetRepo) CreateDataset(ctx context.Context, d *entity.BenchmarkDataset) error {
	s.datasets[d.ID] = d
	return nil
}
func (s *stubGoldenDatasetRepo) ListDatasets(ctx context.Context) ([]*entity.BenchmarkDataset, error) {
	out := make([]*entity.BenchmarkDataset, 0, len(s.datasets))
	for _, d := range s.datasets {
		out = append(out, d)
	}
	return out, nil
}
func (s *stubGoldenDatasetRepo) CreateItems(ctx context.Context, items []*entity.BenchmarkItem) error {
	if len(items) == 0 {
		return nil
	}
	dsID := items[0].DatasetID
	s.items[dsID] = append(s.items[dsID], items...)
	return nil
}
func (s *stubGoldenDatasetRepo) UpdateDataset(ctx context.Context, d *entity.BenchmarkDataset) error {
	s.datasets[d.ID] = d
	return nil
}

func TestService_AddFromCompletedCase(t *testing.T) {
	repo := &stubGoldenRepo{items: map[string]*entity.GoldenCase{}}
	svc := golden.NewService(repo,
		&stubGoldenCaseRepo{c: &entity.DecisionCase{ID: "c1", Question: "ship?", Context: "team ready"}},
		&stubGoldenResRepo{res: &entity.Resolution{CaseID: "c1", FinalDecision: entity.VoteDecisionApprove}},
		&stubGoldenDatasetRepo{datasets: map[string]*entity.BenchmarkDataset{}, items: map[string][]*entity.BenchmarkItem{}},
	)
	g, err := svc.Add(context.Background(), "c1")
	if err != nil {
		t.Fatalf("add: %v", err)
	}
	if g.ExpectedDecision != entity.VoteDecisionApprove || g.Question != "ship?" {
		t.Fatalf("golden = %+v", g)
	}
}

func TestService_AddRequiresResolution(t *testing.T) {
	svc := golden.NewService(&stubGoldenRepo{items: map[string]*entity.GoldenCase{}},
		&stubGoldenCaseRepo{c: &entity.DecisionCase{ID: "c1"}},
		&stubGoldenResRepo{res: nil},
		&stubGoldenDatasetRepo{datasets: map[string]*entity.BenchmarkDataset{}, items: map[string][]*entity.BenchmarkItem{}},
	)
	if _, err := svc.Add(context.Background(), "c1"); err == nil {
		t.Fatal("case without resolution must be rejected")
	}
}

func TestService_SyncToDatasetCreatesBuiltinSuite(t *testing.T) {
	repo := &stubGoldenRepo{items: map[string]*entity.GoldenCase{
		"g1": {ID: "g1", CaseID: "c1", Question: "q1", ExpectedDecision: entity.VoteDecisionApprove},
		"g2": {ID: "g2", CaseID: "c2", Question: "q2", ExpectedDecision: entity.VoteDecisionReject},
	}}
	dsRepo := &stubGoldenDatasetRepo{datasets: map[string]*entity.BenchmarkDataset{}, items: map[string][]*entity.BenchmarkItem{}}
	svc := golden.NewService(repo, &stubGoldenCaseRepo{}, &stubGoldenResRepo{}, dsRepo)
	count, err := svc.SyncToDataset(context.Background())
	if err != nil {
		t.Fatalf("sync: %v", err)
	}
	if count != 2 {
		t.Fatalf("synced = %d", count)
	}
	var suite *entity.BenchmarkDataset
	for _, d := range dsRepo.datasets {
		if d.Name == dataset.BuiltinBenchmarkName {
			suite = d
		}
	}
	if suite == nil {
		t.Fatal("builtin suite must be created")
	}
	if len(dsRepo.items[suite.ID]) != 2 {
		t.Fatalf("items = %d", len(dsRepo.items[suite.ID]))
	}
}
