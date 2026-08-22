package ragindex

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/jamespud/magi/backend/adapter/rag"
	"github.com/jamespud/magi/backend/domain/entity"
	"github.com/jamespud/magi/backend/domain/port"
)

// PollerConfig tunes the RagIndexPoller loop.
type PollerConfig struct {
	Interval  time.Duration
	Lease     time.Duration
	RetryBase time.Duration
	WorkerID  string
}

// RagIndexPoller drains the durable rag_index_job queue. It mirrors the
// RunManager claim/lease/heartbeat/retry loop but executes RAG mutations
// through the inner adapter. Multi-instance safe via lease claims.
type RagIndexPoller struct {
	repo     port.RagIndexJobRepository
	memRepo  port.MemoryRepository
	knowRepo port.KnowledgeRepository
	inner    ragIndexProcessor
	cfg      PollerConfig
	logger   *log.Logger
}

// ragIndexProcessor is the subset of the adapter the poller executes.
type ragIndexProcessor interface {
	Store(ctx context.Context, proj *entity.CaseMemoryProjection) (port.StoreStats, error)
	StoreDocument(ctx context.Context, doc *entity.KnowledgeDoc) (port.StoreStats, error)
	DeleteSource(ctx context.Context, source, sourceRef string) error
}

// Ensure HybridKnowledgeAdapter satisfies ragIndexProcessor.
var _ ragIndexProcessor = (*rag.HybridKnowledgeAdapter)(nil)

func NewRagIndexPoller(repo port.RagIndexJobRepository, memRepo port.MemoryRepository, knowRepo port.KnowledgeRepository, inner ragIndexProcessor, cfg PollerConfig) *RagIndexPoller {
	if cfg.Interval <= 0 {
		cfg.Interval = time.Second
	}
	if cfg.Lease <= 0 {
		cfg.Lease = 60 * time.Second
	}
	if cfg.RetryBase <= 0 {
		cfg.RetryBase = time.Second
	}
	if cfg.WorkerID == "" {
		cfg.WorkerID = "rag-index-default"
	}
	return &RagIndexPoller{repo: repo, memRepo: memRepo, knowRepo: knowRepo, inner: inner, cfg: cfg, logger: log.Default()}
}

// Run blocks until ctx is cancelled. Call in a goroutine.
func (p *RagIndexPoller) Run(ctx context.Context) {
	ticker := time.NewTicker(p.cfg.Interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
		now := time.Now()
		if err := p.repo.RequeueExpired(ctx, now); err != nil {
			p.logger.Printf("rag index poller: requeue expired: %v", err)
		}
		jobs, err := p.repo.ListRunnable(ctx, now)
		if err != nil {
			p.logger.Printf("rag index poller: list runnable: %v", err)
			continue
		}
		for _, job := range jobs {
			p.processJob(ctx, job)
		}
	}
}

func (p *RagIndexPoller) processJob(ctx context.Context, job *entity.RagIndexJob) {
	leaseUntil := time.Now().Add(p.cfg.Lease)
	claimed, ok, err := p.repo.Claim(ctx, job.ID, p.cfg.WorkerID, leaseUntil)
	if err != nil || !ok {
		return
	}
	stopHeartbeat := p.startHeartbeat(ctx, claimed.ID)
	runErr := p.execute(ctx, claimed)
	stopHeartbeat()

	if runErr == nil {
		_ = p.repo.MarkSucceeded(ctx, claimed.ID, p.cfg.WorkerID)
		return
	}
	if claimed.Attempt < claimed.MaxAttempts {
		retryAt := time.Now().Add(p.retryDelay(claimed.Attempt))
		_ = p.repo.MarkFailed(ctx, claimed.ID, p.cfg.WorkerID, runErr.Error(), &retryAt)
		return
	}
	_ = p.repo.MarkFailed(ctx, claimed.ID, p.cfg.WorkerID, runErr.Error(), nil)
	p.recordFinalFailure(ctx, claimed, runErr)
}

func (p *RagIndexPoller) execute(ctx context.Context, job *entity.RagIndexJob) error {
	switch job.Kind {
	case entity.RagIndexJobKindDelete:
		return p.inner.DeleteSource(ctx, job.Source, job.SourceRef)
	case entity.RagIndexJobKindIndex:
		switch job.Source {
		case port.SourceCaseMemory:
			proj, err := p.memRepo.Get(ctx, job.SourceRef)
			if err != nil {
				return fmt.Errorf("rag index: load projection %s: %w", job.SourceRef, err)
			}
			if proj == nil {
				return nil // projection gone; nothing to index
			}
			_, err = p.inner.Store(ctx, proj)
			return err
		case port.SourceKnowledgeDoc:
			doc, err := p.knowRepo.Get(ctx, job.SourceRef)
			if err != nil {
				return fmt.Errorf("rag index: load doc %s: %w", job.SourceRef, err)
			}
			if doc == nil {
				return nil
			}
			stats, err := p.inner.StoreDocument(ctx, doc)
			if err == nil {
				doc.Status = entity.KnowledgeStatusIndexed
				doc.Chunks = stats.Chunks300
				doc.Error = ""
				_ = p.knowRepo.Update(ctx, doc)
			}
			return err
		}
	}
	return fmt.Errorf("rag index: unknown job kind/source %s/%s", job.Kind, job.Source)
}

// recordFinalFailure writes the failed status back onto a knowledge doc when
// an index job exhausts its retries.
func (p *RagIndexPoller) recordFinalFailure(ctx context.Context, job *entity.RagIndexJob, cause error) {
	if job.Kind != entity.RagIndexJobKindIndex || job.Source != port.SourceKnowledgeDoc {
		return
	}
	doc, err := p.knowRepo.Get(ctx, job.SourceRef)
	if err != nil || doc == nil {
		return
	}
	doc.Status = entity.KnowledgeStatusFailed
	doc.Error = cause.Error()
	_ = p.knowRepo.Update(ctx, doc)
}

func (p *RagIndexPoller) startHeartbeat(ctx context.Context, jobID string) func() {
	done := make(chan struct{})
	go func() {
		t := time.NewTicker(p.cfg.Lease / 2)
		defer t.Stop()
		for {
			select {
			case <-done:
				return
			case <-t.C:
				if err := p.repo.Heartbeat(ctx, jobID, p.cfg.WorkerID, time.Now().Add(p.cfg.Lease)); err != nil {
					return
				}
			}
		}
	}()
	return func() { close(done) }
}

func (p *RagIndexPoller) retryDelay(attempt int) time.Duration {
	delay := p.cfg.RetryBase
	for i := 1; i < attempt; i++ {
		if delay >= time.Minute/2 {
			return time.Minute
		}
		delay *= 2
	}
	return delay
}
