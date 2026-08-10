package dataset

import (
	"context"
	"errors"
	"fmt"
	"log"
	"time"

	"github.com/google/uuid"

	"github.com/jamespud/magi/backend/application/auth"
	"github.com/jamespud/magi/backend/domain/entity"
	"github.com/jamespud/magi/backend/domain/port"
)

// ErrRunActive is returned when a dataset already has a queued/running run.
var ErrRunActive = errors.New("benchmark run already active")

// Orchestrator executes a decision case to resolution.
type Orchestrator interface {
	Orchestrate(ctx context.Context, case_ *entity.DecisionCase) (*entity.Resolution, error)
}

// Service manages ground-truth datasets and benchmark runs. Datasets are
// owner-scoped: every mutating and reading method verifies the authenticated
// principal may access the dataset. Runs are asynchronous: StartRun enqueues
// a worker that executes each item as a real DecisionCase through the
// orchestrator, persists per-item results, and finalizes accuracy metrics.
type Service struct {
	datasets            port.DatasetRepository
	cases               port.CaseRepository
	orch                Orchestrator
	maxDebateRounds     int
	workerSlots         chan struct{}
	runsPerItem         int
	regressionThreshold float64
	workerID            string
	leaseDuration       time.Duration
}

// Option configures a dataset.Service.
type Option func(*Service)

// WithRunsPerItem sets the default number of counterfactual repeats per item.
func WithRunsPerItem(n int) Option {
	return func(s *Service) {
		if n < 1 {
			n = 1
		}
		s.runsPerItem = n
	}
}

// WithRegressionThreshold sets the accuracy gate for benchmark runs (0 disables).
func WithRegressionThreshold(t float64) Option {
	return func(s *Service) { s.regressionThreshold = t }
}

func NewService(datasets port.DatasetRepository, cases port.CaseRepository, orch Orchestrator, maxDebateRounds int, opts ...Option) *Service {
	s := &Service{
		datasets: datasets, cases: cases, orch: orch, maxDebateRounds: maxDebateRounds, runsPerItem: 1,
		workerSlots:   make(chan struct{}, 2),
		workerID:      "bench-worker-" + uuid.NewString(),
		leaseDuration: 10 * time.Minute,
	}
	for _, o := range opts {
		o(s)
	}
	return s
}

// RunOptions controls one benchmark run.
type RunOptions struct {
	RunsPerItem         int
	RegressionThreshold float64
}

// NewItem is a ground-truth case to add to a dataset.
type NewItem struct {
	Question         string
	Background       string
	Constraints      []entity.Constraint
	ExpectedDecision entity.VoteDecision
	Weight           float64
	Tags             []string
}

func canAccess(ctx context.Context, ownerID int64) bool {
	return auth.CanAccess(ctx, ownerID)
}

func (s *Service) requireDataset(ctx context.Context, ownerID int64, id string) (*entity.BenchmarkDataset, error) {
	ds, err := s.datasets.GetDataset(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("dataset: get: %w", err)
	}
	if !canAccess(ctx, ds.OwnerID) {
		return nil, fmt.Errorf("dataset: forbidden")
	}
	return ds, nil
}

func (s *Service) Create(ctx context.Context, ownerID int64, name, description string) (*entity.BenchmarkDataset, error) {
	if name == "" {
		return nil, fmt.Errorf("dataset: name is required")
	}
	d := &entity.BenchmarkDataset{ID: "dataset-" + uuid.NewString(), OwnerID: ownerID, Name: name, Description: description}
	if err := s.datasets.CreateDataset(ctx, d); err != nil {
		return nil, fmt.Errorf("dataset: create: %w", err)
	}
	return d, nil
}

func (s *Service) List(ctx context.Context, ownerID int64) ([]*entity.BenchmarkDataset, error) {
	all, err := s.datasets.ListDatasets(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]*entity.BenchmarkDataset, 0, len(all))
	for _, d := range all {
		if canAccess(ctx, d.OwnerID) {
			out = append(out, d)
		}
	}
	return out, nil
}

func (s *Service) Get(ctx context.Context, ownerID int64, id string) (*entity.BenchmarkDataset, error) {
	return s.requireDataset(ctx, ownerID, id)
}

func (s *Service) AddItems(ctx context.Context, ownerID int64, datasetID string, items []NewItem) (int, error) {
	if len(items) == 0 {
		return 0, fmt.Errorf("dataset: at least one item is required")
	}
	ds, err := s.requireDataset(ctx, ownerID, datasetID)
	if err != nil {
		return 0, err
	}
	now := time.Now()
	entities := make([]*entity.BenchmarkItem, 0, len(items))
	for _, it := range items {
		if it.Question == "" {
			return 0, fmt.Errorf("dataset: item question is required")
		}
		if it.ExpectedDecision == "" {
			return 0, fmt.Errorf("dataset: item expected_decision is required")
		}
		weight := it.Weight
		if weight <= 0 {
			weight = 1
		}
		entities = append(entities, &entity.BenchmarkItem{
			ID: "ditem-" + uuid.NewString(), DatasetID: datasetID,
			Question: it.Question, Context: it.Background, Constraints: it.Constraints,
			ExpectedDecision: it.ExpectedDecision, Weight: weight, Tags: it.Tags, CreatedAt: now,
		})
	}
	if err := s.datasets.CreateItems(ctx, entities); err != nil {
		return 0, fmt.Errorf("dataset: create items: %w", err)
	}
	ds.ItemCount += len(entities)
	if err := s.datasets.UpdateDataset(ctx, ds); err != nil {
		return 0, fmt.Errorf("dataset: update count: %w", err)
	}
	return len(entities), nil
}

func (s *Service) ListItems(ctx context.Context, ownerID int64, datasetID string) ([]*entity.BenchmarkItem, error) {
	if _, err := s.requireDataset(ctx, ownerID, datasetID); err != nil {
		return nil, err
	}
	return s.datasets.ListItems(ctx, datasetID)
}

func (s *Service) UpdateItem(ctx context.Context, ownerID int64, datasetID, itemID string, it NewItem) error {
	if _, err := s.requireDataset(ctx, ownerID, datasetID); err != nil {
		return err
	}
	if it.Question == "" || it.ExpectedDecision == "" {
		return fmt.Errorf("dataset: question and expected_decision are required")
	}
	item, err := s.datasets.GetItem(ctx, itemID)
	if err != nil || item == nil || item.DatasetID != datasetID {
		return fmt.Errorf("dataset: item not found")
	}
	item.Question = it.Question
	item.Context = it.Background
	item.Constraints = it.Constraints
	item.ExpectedDecision = it.ExpectedDecision
	if it.Weight > 0 {
		item.Weight = it.Weight
	}
	item.Tags = it.Tags
	return s.datasets.UpdateItem(ctx, item)
}

func (s *Service) DeleteItem(ctx context.Context, ownerID int64, datasetID, itemID string) error {
	ds, err := s.requireDataset(ctx, ownerID, datasetID)
	if err != nil {
		return err
	}
	item, err := s.datasets.GetItem(ctx, itemID)
	if err != nil || item == nil || item.DatasetID != datasetID {
		return fmt.Errorf("dataset: item not found")
	}
	if err := s.datasets.DeleteItem(ctx, itemID); err != nil {
		return err
	}
	ds.ItemCount--
	if ds.ItemCount < 0 {
		ds.ItemCount = 0
	}
	return s.datasets.UpdateDataset(ctx, ds)
}

func (s *Service) ExportItems(ctx context.Context, ownerID int64, datasetID string) ([]*entity.BenchmarkItem, error) {
	if _, err := s.requireDataset(ctx, ownerID, datasetID); err != nil {
		return nil, err
	}
	return s.datasets.ListItems(ctx, datasetID)
}

// StartRun enqueues an asynchronous benchmark run and returns immediately.
// The worker executes each dataset item, persists results, and finalizes the
// run; poll run detail for progress.
func (s *Service) StartRun(ctx context.Context, ownerID int64, datasetID string) (*entity.BenchmarkRun, error) {
	return s.StartRunWithOptions(ctx, ownerID, datasetID, RunOptions{})
}

func (s *Service) StartRunWithOptions(ctx context.Context, ownerID int64, datasetID string, opts RunOptions) (*entity.BenchmarkRun, error) {
	ds, err := s.requireDataset(ctx, ownerID, datasetID)
	if err != nil {
		return nil, err
	}
	items, err := s.datasets.ListItems(ctx, datasetID)
	if err != nil {
		return nil, fmt.Errorf("dataset: list items: %w", err)
	}
	if len(items) == 0 {
		return nil, fmt.Errorf("dataset: no items to run")
	}
	runs, err := s.datasets.ListRuns(ctx, datasetID)
	if err != nil {
		return nil, fmt.Errorf("dataset: list runs: %w", err)
	}
	for _, r := range runs {
		if r.Status == entity.BenchmarkRunQueued || r.Status == entity.BenchmarkRunRunning {
			return nil, ErrRunActive
		}
	}
	run := &entity.BenchmarkRun{ID: "bench-" + uuid.NewString(), DatasetID: datasetID, Status: entity.BenchmarkRunQueued, Total: len(items), StartedAt: time.Now(), CreatedAt: time.Now()}
	if opts.RunsPerItem > 0 {
		run.RunsPerItem = opts.RunsPerItem
	} else {
		run.RunsPerItem = s.runsPerItem
	}
	thresholdOpt := opts.RegressionThreshold
	if thresholdOpt == 0 {
		thresholdOpt = s.regressionThreshold
	}
	run.RegressionThreshold = thresholdOpt
	if err := s.datasets.CreateRun(ctx, run); err != nil {
		return nil, fmt.Errorf("dataset: create run: %w", err)
	}
	go s.processRun(run.ID, items, ds.OwnerID, opts)
	return run, nil
}

func (s *Service) processRun(runID string, items []*entity.BenchmarkItem, ownerID int64, opts RunOptions) {
	s.workerSlots <- struct{}{}
	defer func() { <-s.workerSlots }()
	ctx := context.Background()
	// Atomic multi-instance claim: only the replica that wins the lease
	// executes this run. A crashed owner's lease expires and the run is
	// requeued by RecoverOrphanRuns for the next replica.
	leaseUntil := time.Now().Add(s.leaseDuration)
	claimed, err := s.datasets.ClaimRun(ctx, runID, s.workerID, &leaseUntil)
	if err != nil {
		log.Printf("dataset: claim run %s: %v", runID, err)
		return
	}
	if !claimed {
		return // another replica owns this run
	}
	run, err := s.datasets.GetRun(ctx, runID)
	if err != nil || run == nil {
		return
	}
	run.Status = entity.BenchmarkRunRunning
	matched := 0
	totalWeight := 0.0
	matchedWeight := 0.0
	sumConsistency := 0.0
	completedItems := 0
	hasError := false
	runs := s.runsPerItem
	if opts.RunsPerItem > 0 {
		runs = opts.RunsPerItem
	}
	if run.RunsPerItem > 0 {
		runs = run.RunsPerItem
	}
	threshold := opts.RegressionThreshold
	if threshold == 0 {
		threshold = s.regressionThreshold
	}
	if run.RegressionThreshold > 0 {
		threshold = run.RegressionThreshold
	}
	weights := make(map[string]float64, len(items))
	for _, it := range items {
		weights[it.ID] = it.Weight
	}
	done := map[string]bool{}
	existing, _ := s.datasets.ListItemResults(ctx, runID)
	for _, r := range existing {
		if r == nil || r.DatasetItemID == "" {
			continue
		}
		done[r.DatasetItemID] = true
		completedItems++
		sumConsistency += r.Consistency
		if r.Matched {
			matched++
			w := weights[r.DatasetItemID]
			if w <= 0 {
				w = 1
			}
			matchedWeight += w
			totalWeight += w
		}
	}
	for _, item := range items {
		if done[item.ID] {
			continue
		}
		res := &entity.BenchmarkItemResult{RunID: run.ID, DatasetItemID: item.ID, ExpectedDecision: item.ExpectedDecision}
		decisions := make([]entity.VoteDecision, 0, runs)
		var firstCaseID string
		for i := 0; i < runs; i++ {
			case_ := &entity.DecisionCase{
				ID: "case-" + uuid.NewString(), UserID: ownerID, Question: item.Question, Context: item.Context,
				Constraints: item.Constraints, MaxDebateRounds: s.maxDebateRounds,
				Status: entity.CaseStatusDraft, CreatedAt: time.Now(),
			}
			if s.cases != nil {
				if err := s.cases.Create(ctx, case_); err != nil {
					res.Error = fmt.Sprintf("create case: %v", err)
					break
				}
			}
			resolution, runErr := s.orch.Orchestrate(ctx, case_)
			if runErr != nil {
				res.Error = runErr.Error()
				break
			}
			if firstCaseID == "" {
				firstCaseID = case_.ID
			}
			decisions = append(decisions, resolution.FinalDecision)
		}
		if res.Error != "" {
			hasError = true
		} else {
			res.CaseID = firstCaseID
			res.Decisions = decisions
			res.Runs = len(decisions)
			res.ActualDecision, res.Consistency = majorityDecision(decisions)
			res.Matched = res.ActualDecision == item.ExpectedDecision
			completedItems++
			sumConsistency += res.Consistency
		}
		totalWeight += item.Weight
		if res.Matched {
			matched++
			matchedWeight += item.Weight
			res.Score = item.Weight
		}
		if err := s.datasets.CreateItemResult(ctx, res); err != nil {
			hasError = true
		}
	}
	// Build the final state on a copy: `run` was already handed to the
	// repository by the earlier UpdateRun, so mutating it in place would race
	// with concurrent readers of the stored entity.
	final := *run
	now := time.Now()
	final.Status = entity.BenchmarkRunSucceeded
	if hasError {
		final.Status = entity.BenchmarkRunFailed
	}
	final.RunsPerItem = runs
	if completedItems > 0 {
		final.Stability = sumConsistency / float64(completedItems)
	}
	final.Matched = matched
	if final.Total > 0 {
		final.Accuracy = float64(matched) / float64(final.Total)
	}
	if totalWeight > 0 {
		final.WeightedAccuracy = matchedWeight / totalWeight
	}
	if threshold > 0 && final.Accuracy < threshold {
		final.RegressionFailed = true
		final.FailureReason = fmt.Sprintf("accuracy %.2f below regression threshold %.2f", final.Accuracy, threshold)
		final.Status = entity.BenchmarkRunFailed
	}
	final.CompletedAt = &now
	_ = s.datasets.UpdateRun(ctx, &final)
}

// majorityDecision returns the most frequent decision and its agreement rate.
func majorityDecision(decisions []entity.VoteDecision) (entity.VoteDecision, float64) {
	if len(decisions) == 0 {
		return "", 0
	}
	counts := map[entity.VoteDecision]int{}
	for _, d := range decisions {
		counts[d]++
	}
	best := decisions[0]
	bestCount := 0
	for d, c := range counts {
		if c > bestCount {
			best, bestCount = d, c
		}
	}
	return best, float64(bestCount) / float64(len(decisions))
}

func (s *Service) ListRuns(ctx context.Context, ownerID int64, datasetID string) ([]*entity.BenchmarkRun, error) {
	if _, err := s.requireDataset(ctx, ownerID, datasetID); err != nil {
		return nil, err
	}
	return s.datasets.ListRuns(ctx, datasetID)
}

// AddFeedback records user feedback on a benchmark item result, verified
// against the run's dataset owner.
func (s *Service) AddFeedback(ctx context.Context, ownerID int64, runID, resultID, feedback string) error {
	if feedback == "" {
		return fmt.Errorf("dataset: feedback is required")
	}
	run, err := s.datasets.GetRun(ctx, runID)
	if err != nil {
		return err
	}
	if _, err := s.requireDataset(ctx, ownerID, run.DatasetID); err != nil {
		return err
	}
	return s.datasets.UpdateFeedback(ctx, resultID, feedback, time.Now())
}

// RunDetail returns a benchmark run with its per-case results, verifying the
// run belongs to an accessible dataset.
func (s *Service) RunDetail(ctx context.Context, ownerID int64, runID string) (*entity.BenchmarkRun, []*entity.BenchmarkItemResult, error) {
	run, err := s.datasets.GetRun(ctx, runID)
	if err != nil {
		return nil, nil, err
	}
	if _, err := s.requireDataset(ctx, ownerID, run.DatasetID); err != nil {
		return nil, nil, err
	}
	results, err := s.datasets.ListItemResults(ctx, runID)
	if err != nil {
		return nil, nil, err
	}
	return run, results, nil
}

// RecoverOrphanRuns marks queued/running runs as failed; called at startup to
// clean up runs interrupted by a process restart.
func (s *Service) RecoverOrphanRuns(ctx context.Context) error {
	if err := s.datasets.ExpireRunLeases(ctx, time.Now()); err != nil {
		return fmt.Errorf("dataset: expire run leases: %w", err)
	}
	runs, err := s.datasets.ListAllRuns(ctx)
	if err != nil {
		return err
	}
	for _, r := range runs {
		if r.Status != entity.BenchmarkRunQueued && r.Status != entity.BenchmarkRunRunning {
			continue
		}
		ds, err := s.datasets.GetDataset(ctx, r.DatasetID)
		if err != nil {
			continue
		}
		items, err := s.datasets.ListItems(ctx, r.DatasetID)
		if err != nil || len(items) == 0 {
			continue
		}
		go s.processRun(r.ID, items, ds.OwnerID, RunOptions{RunsPerItem: r.RunsPerItem, RegressionThreshold: r.RegressionThreshold})
	}
	return nil
}
