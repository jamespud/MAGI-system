package dataset_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/jamespud/magi/backend/application/auth"
	"github.com/jamespud/magi/backend/application/dataset"
	"github.com/jamespud/magi/backend/domain/entity"
)

type stubDatasetRepo struct {
	mu      sync.Mutex
	ds      map[string]*entity.BenchmarkDataset
	items   map[string][]*entity.BenchmarkItem
	runs    map[string]*entity.BenchmarkRun
	results map[string][]*entity.BenchmarkItemResult
	byItem  map[string]string // runID -> itemID prefix for GetRun
	deletedDatasets []string
}

func newStubDatasetRepo() *stubDatasetRepo {
	return &stubDatasetRepo{
		ds:      make(map[string]*entity.BenchmarkDataset),
		items:   make(map[string][]*entity.BenchmarkItem),
		runs:    make(map[string]*entity.BenchmarkRun),
		results: make(map[string][]*entity.BenchmarkItemResult),
	}
}

func (s *stubDatasetRepo) CreateDataset(ctx context.Context, d *entity.BenchmarkDataset) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.ds[d.ID] = d
	return nil
}
func (s *stubDatasetRepo) GetDataset(ctx context.Context, id string) (*entity.BenchmarkDataset, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if d, ok := s.ds[id]; ok {
		return d, nil
	}
	return nil, errors.New("not found")
}
func (s *stubDatasetRepo) ListDatasets(ctx context.Context) ([]*entity.BenchmarkDataset, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]*entity.BenchmarkDataset, 0, len(s.ds))
	for _, d := range s.ds {
		out = append(out, d)
	}
	return out, nil
}
func (s *stubDatasetRepo) UpdateDataset(ctx context.Context, d *entity.BenchmarkDataset) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.ds[d.ID] = d
	return nil
}
func (s *stubDatasetRepo) GetItem(ctx context.Context, id string) (*entity.BenchmarkItem, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, list := range s.items {
		for _, it := range list {
			if it.ID == id {
				return it, nil
			}
		}
	}
	return nil, errors.New("not found")
}
func (s *stubDatasetRepo) UpdateItem(ctx context.Context, it *entity.BenchmarkItem) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, list := range s.items {
		for i, x := range list {
			if x.ID == it.ID {
				list[i] = it
				return nil
			}
		}
	}
	return errors.New("not found")
}
func (s *stubDatasetRepo) DeleteItem(ctx context.Context, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for key, list := range s.items {
		for i, it := range list {
			if it.ID == id {
				s.items[key] = append(list[:i], list[i+1:]...)
				return nil
			}
		}
	}
	return errors.New("not found")
}

func (s *stubDatasetRepo) DeleteDataset(ctx context.Context, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.ds, id)
	s.deletedDatasets = append(s.deletedDatasets, id)
	return nil
}

func (s *stubDatasetRepo) CreateItems(ctx context.Context, items []*entity.BenchmarkItem) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, it := range items {
		s.items[it.DatasetID] = append(s.items[it.DatasetID], it)
	}
	return nil
}
func (s *stubDatasetRepo) ListItems(ctx context.Context, datasetID string) ([]*entity.BenchmarkItem, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]*entity.BenchmarkItem(nil), s.items[datasetID]...), nil
}
func (s *stubDatasetRepo) CreateRun(ctx context.Context, r *entity.BenchmarkRun) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.runs[r.ID] = r
	return nil
}
func (s *stubDatasetRepo) ClaimRun(ctx context.Context, runID, owner string, leaseUntil *time.Time) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	r, ok := s.runs[runID]
	if !ok {
		return false, errors.New("not found")
	}
	if r.LeaseUntil != nil && r.LeaseUntil.After(time.Now()) {
		return false, nil // another replica holds the lease
	}
	r.LeaseOwner = owner
	r.LeaseUntil = leaseUntil
	r.Status = entity.BenchmarkRunRunning
	return true, nil
}
func (s *stubDatasetRepo) ExpireRunLeases(ctx context.Context, now time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, r := range s.runs {
		if r.LeaseUntil != nil && r.LeaseUntil.Before(now) {
			r.LeaseOwner = ""
			r.LeaseUntil = nil
			r.Status = entity.BenchmarkRunQueued
		}
	}
	return nil
}
func (s *stubDatasetRepo) UpdateRun(ctx context.Context, r *entity.BenchmarkRun) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.runs[r.ID] = r
	return nil
}
func (s *stubDatasetRepo) GetRun(ctx context.Context, id string) (*entity.BenchmarkRun, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if r, ok := s.runs[id]; ok {
		cp := *r
		return &cp, nil
	}
	return nil, errors.New("not found")
}
func (s *stubDatasetRepo) ListRuns(ctx context.Context, datasetID string) ([]*entity.BenchmarkRun, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []*entity.BenchmarkRun
	for _, r := range s.runs {
		if r.DatasetID == datasetID {
			cp := *r
			out = append(out, &cp)
		}
	}
	return out, nil
}
func (s *stubDatasetRepo) ListAllRuns(ctx context.Context) ([]*entity.BenchmarkRun, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]*entity.BenchmarkRun, 0, len(s.runs))
	for _, r := range s.runs {
		cp := *r
		out = append(out, &cp)
	}
	return out, nil
}
func (s *stubDatasetRepo) CreateItemResult(ctx context.Context, r *entity.BenchmarkItemResult) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.results[r.RunID] = append(s.results[r.RunID], r)
	return nil
}
func (s *stubDatasetRepo) ListItemResults(ctx context.Context, runID string) ([]*entity.BenchmarkItemResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]*entity.BenchmarkItemResult(nil), s.results[runID]...), nil
}
func (s *stubDatasetRepo) UpdateFeedback(ctx context.Context, resultID, feedback string, at time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, list := range s.results {
		for _, r := range list {
			if r.ID == resultID {
				r.Feedback = feedback
				r.FeedbackAt = &at
				return nil
			}
		}
	}
	return errors.New("not found")
}

type stubCaseRepo struct {
	mu      sync.Mutex
	created int
}

func (s *stubCaseRepo) Create(ctx context.Context, c *entity.DecisionCase) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.created++
	return nil
}

func (s *stubCaseRepo) Get(ctx context.Context, id string) (*entity.DecisionCase, error) {
	return nil, nil
}
func (s *stubCaseRepo) List(ctx context.Context) ([]*entity.DecisionCase, error) { return nil, nil }
func (s *stubCaseRepo) UpdateStatus(ctx context.Context, id string, st entity.CaseStatus) error {
	return nil
}
func (s *stubCaseRepo) UpdateTask(ctx context.Context, id string, task *entity.DecisionTask) error {
	return nil
}
func (s *stubCaseRepo) ListPaged(ctx context.Context, userID int64, page, pageSize int) ([]*entity.DecisionCase, int64, error) {
	return nil, 0, nil
}
func (s *stubCaseRepo) UpdateFlags(ctx context.Context, id string, pinned, archived *bool) error { return nil }
func (s *stubCaseRepo) Delete(ctx context.Context, id string) error { return nil }

type stubOrch struct {
	mu   sync.Mutex
	call int
	errs map[string]error
}

func (s *stubOrch) Orchestrate(ctx context.Context, c *entity.DecisionCase) (*entity.Resolution, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.call++
	if err := s.errs[c.Question]; err != nil {
		return nil, err
	}
	return &entity.Resolution{CaseID: c.ID, FinalDecision: entity.VoteDecisionApprove}, nil
}

func TestService_CreateAndAddItems(t *testing.T) {
	svc := dataset.NewService(newStubDatasetRepo(), &stubCaseRepo{}, &stubOrch{}, 1)
	ctx := context.Background()
	d, err := svc.Create(ctx, 0, "launch-eval", "")
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if _, err := svc.Create(ctx, 0, "", ""); err == nil {
		t.Fatal("expected error for empty name")
	}
	n, err := svc.AddItems(ctx, 0, d.ID, []dataset.NewItem{
		{Question: "ship?", ExpectedDecision: entity.VoteDecisionApprove},
		{Question: "ship2?", ExpectedDecision: entity.VoteDecisionReject, Weight: 2},
	})
	if err != nil || n != 2 {
		t.Fatalf("add items: %v n=%d", err, n)
	}
	if d.ItemCount != 2 {
		t.Fatalf("item count: %d", d.ItemCount)
	}
	if _, err := svc.AddItems(ctx, 0, d.ID, []dataset.NewItem{{Question: "", ExpectedDecision: entity.VoteDecisionApprove}}); err == nil {
		t.Fatal("expected error for empty question")
	}
}

func TestService_RunComputesAccuracy(t *testing.T) {
	repo := newStubDatasetRepo()
	svc := dataset.NewService(repo, &stubCaseRepo{}, &stubOrch{}, 1)
	ctx := context.Background()
	d, _ := svc.Create(ctx, 0, "eval", "")
	_, _ = svc.AddItems(ctx, 0, d.ID, []dataset.NewItem{
		{Question: "q1", ExpectedDecision: entity.VoteDecisionApprove, Weight: 1},
		{Question: "q2", ExpectedDecision: entity.VoteDecisionReject, Weight: 3},
	})
	run, err := svc.StartRun(ctx, 0, d.ID)
	if err != nil {
		t.Fatalf("start run: %v", err)
	}
	run = waitRun(t, repo, run.ID, entity.BenchmarkRunSucceeded)
	if run.Total != 2 || run.Matched != 1 {
		t.Fatalf("run totals: %+v", run)
	}
	if run.Accuracy != 0.5 || run.WeightedAccuracy != 0.25 {
		t.Fatalf("accuracy: acc=%v wacc=%v", run.Accuracy, run.WeightedAccuracy)
	}
	results, err := repo.ListItemResults(ctx, run.ID)
	if err != nil || len(results) != 2 {
		t.Fatalf("results: %v %d", err, len(results))
	}
	matchedCount := 0
	for _, r := range results {
		if r.Matched {
			matchedCount++
		}
	}
	if matchedCount != 1 {
		t.Fatalf("matched results: %d", matchedCount)
	}
}

func TestService_RunMarksFailedOnItemError(t *testing.T) {
	repo := newStubDatasetRepo()
	orch := &stubOrch{errs: map[string]error{"bad": errors.New("boom")}}
	svc := dataset.NewService(repo, &stubCaseRepo{}, orch, 1)
	ctx := context.Background()
	d, _ := svc.Create(ctx, 0, "eval", "")
	_, _ = svc.AddItems(ctx, 0, d.ID, []dataset.NewItem{
		{Question: "good", ExpectedDecision: entity.VoteDecisionApprove},
		{Question: "bad", ExpectedDecision: entity.VoteDecisionReject},
	})
	run, err := svc.StartRun(ctx, 0, d.ID)
	if err != nil {
		t.Fatalf("start run: %v", err)
	}
	run = waitRun(t, repo, run.ID, entity.BenchmarkRunFailed)
	results, _ := repo.ListItemResults(ctx, run.ID)
	if len(results) != 2 {
		t.Fatalf("results: %d", len(results))
	}
	errorCount := 0
	matchedCount := 0
	for _, r := range results {
		if r.Error != "" {
			errorCount++
		}
		if r.Matched {
			matchedCount++
		}
	}
	if errorCount != 1 || matchedCount != 1 {
		t.Fatalf("errorCount=%d matchedCount=%d", errorCount, matchedCount)
	}
}

func TestService_OwnershipEnforced(t *testing.T) {
	svc := dataset.NewService(newStubDatasetRepo(), &stubCaseRepo{}, &stubOrch{}, 1)
	ctx := context.Background()
	d, err := svc.Create(ctx, 1, "private", "")
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	other := auth.WithPrincipal(ctx, &auth.Principal{UserID: 2, Role: "user"})
	if _, err := svc.Get(other, 2, d.ID); err == nil {
		t.Fatal("expected forbidden for other user")
	}
	owner := auth.WithPrincipal(ctx, &auth.Principal{UserID: 1, Role: "admin"})
	got, err := svc.Get(owner, 1, d.ID)
	if err != nil || got == nil || got.ID != d.ID {
		t.Fatalf("owner get: %v %+v", err, got)
	}
}

func waitRun(t *testing.T, repo *stubDatasetRepo, runID string, want entity.BenchmarkRunStatus) *entity.BenchmarkRun {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		r, _ := repo.GetRun(context.Background(), runID)
		if r != nil && r.Status == want {
			return r
		}
		time.Sleep(5 * time.Millisecond)
	}
	r, _ := repo.GetRun(context.Background(), runID)
	t.Fatalf("run %s did not reach %s: %+v", runID, want, r)
	return nil
}

type gatedOrchestrator struct {
	once    sync.Once
	started chan struct{}
	release chan struct{}
}

func (o *gatedOrchestrator) Orchestrate(ctx context.Context, c *entity.DecisionCase) (*entity.Resolution, error) {
	o.once.Do(func() { close(o.started) })
	<-o.release
	return &entity.Resolution{CaseID: c.ID, FinalDecision: entity.VoteDecisionApprove}, nil
}

func TestService_RejectsSecondActiveRun(t *testing.T) {
	repo := newStubDatasetRepo()
	orch := &gatedOrchestrator{started: make(chan struct{}), release: make(chan struct{})}
	svc := dataset.NewService(repo, &stubCaseRepo{}, orch, 1)
	ctx := context.Background()
	d, _ := svc.Create(ctx, 0, "eval", "")
	_, _ = svc.AddItems(ctx, 0, d.ID, []dataset.NewItem{{Question: "q", ExpectedDecision: entity.VoteDecisionApprove}})
	run, err := svc.StartRun(ctx, 0, d.ID)
	if err != nil {
		t.Fatalf("first start: %v", err)
	}
	<-orch.started
	if _, err := svc.StartRun(ctx, 0, d.ID); err != dataset.ErrRunActive {
		t.Fatalf("expected ErrRunActive, got %v", err)
	}
	close(orch.release)
	waitRun(t, repo, run.ID, entity.BenchmarkRunSucceeded)
}

func TestService_RecoverOrphanRuns(t *testing.T) {
	repo := newStubDatasetRepo()
	orch := &stubOrch{}
	svc := dataset.NewService(repo, &stubCaseRepo{}, orch, 1)
	ctx := context.Background()
	d, _ := svc.Create(ctx, 0, "eval", "")
	_, _ = svc.AddItems(ctx, 0, d.ID, []dataset.NewItem{{Question: "q", ExpectedDecision: entity.VoteDecisionApprove}})
	run := &entity.BenchmarkRun{ID: "bench-orphan", DatasetID: d.ID, Status: entity.BenchmarkRunRunning, Total: 1, StartedAt: time.Now(), CreatedAt: time.Now()}
	_ = repo.CreateRun(ctx, run)
	if err := svc.RecoverOrphanRuns(ctx); err != nil {
		t.Fatalf("recover: %v", err)
	}
	run = waitRun(t, repo, run.ID, entity.BenchmarkRunSucceeded)
	if run.Accuracy != 1 || run.Stability != 1 {
		t.Fatalf("resumed run: %+v", run)
	}
}

func TestService_AddFeedbackOwnershipAndPersists(t *testing.T) {
	repo := newStubDatasetRepo()
	svc := dataset.NewService(repo, &stubCaseRepo{}, &stubOrch{}, 1)
	ctx := context.Background()
	d, _ := svc.Create(ctx, 1, "eval", "")
	_, _ = svc.AddItems(ctx, 1, d.ID, []dataset.NewItem{{Question: "q", ExpectedDecision: entity.VoteDecisionApprove}})
	run, err := svc.StartRun(ctx, 1, d.ID)
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	run = waitRun(t, repo, run.ID, entity.BenchmarkRunSucceeded)
	results, _ := repo.ListItemResults(ctx, run.ID)
	if len(results) == 0 {
		t.Fatal("no results")
	}
	other := auth.WithPrincipal(ctx, &auth.Principal{UserID: 2})
	if err := svc.AddFeedback(other, 2, run.ID, results[0].ID, "wrong user"); err == nil {
		t.Fatal("expected forbidden for other user")
	}
	owner := auth.WithPrincipal(ctx, &auth.Principal{UserID: 1})
	if err := svc.AddFeedback(owner, 1, run.ID, results[0].ID, "agree with the call"); err != nil {
		t.Fatalf("feedback: %v", err)
	}
	got, _ := repo.ListItemResults(ctx, run.ID)
	if got[0].Feedback != "agree with the call" || got[0].FeedbackAt == nil {
		t.Fatalf("feedback not persisted: %+v", got[0])
	}
}

// countingOrchForClaims counts orchestrations so a test can prove that only
// one replica executes a claimed benchmark run.
type countingOrchForClaims struct {
	mu    sync.Mutex
	calls int
}

func (c *countingOrchForClaims) Orchestrate(ctx context.Context, case_ *entity.DecisionCase) (*entity.Resolution, error) {
	c.mu.Lock()
	c.calls++
	c.mu.Unlock()
	return &entity.Resolution{CaseID: case_.ID, FinalDecision: entity.VoteDecisionApprove}, nil
}

func TestService_RecoverOrphanRunsRespectsUnexpiredLease(t *testing.T) {
	repo := newStubDatasetRepo()
	orch := &countingOrchForClaims{}
	svc := dataset.NewService(repo, &stubCaseRepo{}, orch, 1)
	ctx := context.Background()
	d, _ := svc.Create(ctx, 0, "recover", "")
	_, _ = svc.AddItems(ctx, 0, d.ID, []dataset.NewItem{
		{Question: "q1", ExpectedDecision: entity.VoteDecisionApprove},
	})
	run, err := svc.StartRun(ctx, 0, d.ID)
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	run = waitRun(t, repo, run.ID, entity.BenchmarkRunSucceeded)
	if run.Status != entity.BenchmarkRunSucceeded {
		t.Fatalf("run: %+v", run)
	}

	// Simulate a crashed owner with an unexpired lease: recovery must not
	// re-execute the run.
	repo.mu.Lock()
	stored := repo.runs[run.ID]
	future := time.Now().Add(time.Hour)
	stored.Status = entity.BenchmarkRunQueued
	stored.LeaseOwner = "dead-worker"
	stored.LeaseUntil = &future
	repo.mu.Unlock()
	if err := svc.RecoverOrphanRuns(ctx); err != nil {
		t.Fatalf("recover: %v", err)
	}
	if !waitCalls(t, orch, 1) {
		t.Fatalf("run must not be re-executed while lease is held")
	}

	// Once the lease expires, recovery resumes the run.
	repo.mu.Lock()
	past := time.Now().Add(-time.Minute)
	repo.runs[run.ID].LeaseUntil = &past
	repo.mu.Unlock()
	if err := svc.RecoverOrphanRuns(ctx); err != nil {
		t.Fatalf("recover after expiry: %v", err)
	}
	run = waitRun(t, repo, run.ID, entity.BenchmarkRunSucceeded)
	// Completed items are skipped on resume (the done map is rebuilt from
	// persisted results), so no additional orchestration is needed here.
	if !waitCalls(t, orch, 1) {
		t.Fatalf("resume must not re-run completed items")
	}
}

func waitCalls(t *testing.T, orch *countingOrchForClaims, want int) bool {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		orch.mu.Lock()
		calls := orch.calls
		orch.mu.Unlock()
		if calls == want {
			return true
		}
		time.Sleep(10 * time.Millisecond)
	}
	orch.mu.Lock()
	defer orch.mu.Unlock()
	return orch.calls == want
}

func TestService_DeleteCancelsRunsAndRemovesDataset(t *testing.T) {
	repo := newStubDatasetRepo()
	svc := dataset.NewService(repo, &stubCaseRepo{}, &stubOrch{}, 1)
	ctx := context.Background()

	ds, err := svc.Create(ctx, 7, "launch", "desc")
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	// An in-flight queued run.
	run := &entity.BenchmarkRun{ID: "bench-act", DatasetID: ds.ID, Status: entity.BenchmarkRunQueued}
	if err := repo.CreateRun(ctx, run); err != nil {
		t.Fatalf("create run: %v", err)
	}

	if err := svc.Delete(ctx, 7, ds.ID); err != nil {
		t.Fatalf("delete: %v", err)
	}

	// Dataset is gone.
	if _, err := svc.Get(ctx, 7, ds.ID); err == nil {
		t.Fatal("dataset should be deleted")
	}
	// The active run was marked failed (cancelled), not left queued.
	got, err := repo.GetRun(ctx, run.ID)
	if err != nil {
		t.Fatalf("get run: %v", err)
	}
	if got.Status != entity.BenchmarkRunFailed {
		t.Fatalf("expected in-flight run to be failed after dataset delete, got %s", got.Status)
	}

	// A different owner cannot delete (principal in context enforces access).
	otherCtx := auth.WithPrincipal(ctx, &auth.Principal{UserID: 9, Role: "user"})
	other, err := svc.Create(otherCtx, 9, "other", "")
	if err != nil {
		t.Fatalf("create other: %v", err)
	}
	ownerCtx := auth.WithPrincipal(ctx, &auth.Principal{UserID: 7, Role: "admin"})
	if err := svc.Delete(ownerCtx, 7, other.ID); err == nil {
		t.Fatal("non-owner delete must fail")
	}
}
