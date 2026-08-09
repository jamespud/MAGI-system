package rag

import (
	"context"
	"log"

	"github.com/jamespud/magi/backend/domain/entity"
	"github.com/jamespud/magi/backend/domain/port"
)

type storeJob struct {
	proj *entity.CaseMemoryProjection
}

// AsyncIndexer wraps a KnowledgePort Store in a bounded worker pool. Store
// returns immediately; indexing happens in the background. Case resolution is
// never blocked. Errors are logged (and optionally published as events).
type AsyncIndexer struct {
	inner  port.KnowledgePort
	pub    port.EventPublisher
	jobs   chan storeJob
	logger *log.Logger
}

func NewAsyncIndexer(inner port.KnowledgePort, pub port.EventPublisher, workers int) *AsyncIndexer {
	if workers <= 0 {
		workers = 4
	}
	a := &AsyncIndexer{inner: inner, pub: pub, jobs: make(chan storeJob, workers*4), logger: log.Default()}
	for i := 0; i < workers; i++ {
		go a.worker()
	}
	return a
}

func (a *AsyncIndexer) worker() {
	for job := range a.jobs {
		ctx := context.Background()
		stats, err := a.inner.Store(ctx, job.proj)
		if err != nil {
			a.logger.Printf("rag async store failed: %v", err)
		} else if a.pub != nil && job.proj != nil {
			a.logger.Printf("rag async store: indexed source_ref=%s chunks(300=%d 900=%d 1800=%d)", job.proj.CaseID, stats.Chunks300, stats.Chunks900, stats.Chunks1800)
			_ = a.pub.Publish(ctx, entity.NewEvent(job.proj.CaseID, "", nil, entity.EventMemoryIndexed, map[string]any{
				"source_ref": job.proj.CaseID, "chunks_300": stats.Chunks300,
				"chunks_900": stats.Chunks900, "chunks_1800": stats.Chunks1800,
			}))
		}
	}
}

// Store submits the projection for async indexing and returns immediately.
func (a *AsyncIndexer) Store(ctx context.Context, proj *entity.CaseMemoryProjection) (port.StoreStats, error) {
	select {
	case a.jobs <- storeJob{proj: proj}:
	default:
		a.logger.Printf("rag async store: queue full, dropping job for %s", proj.CaseID)
	}
	return port.StoreStats{}, nil
}

// Retrieve delegates synchronously to the inner adapter.
func (a *AsyncIndexer) Retrieve(ctx context.Context, req port.RetrieveRequest) (port.RetrieveResult, error) {
	return a.inner.Retrieve(ctx, req)
}

var _ port.KnowledgePort = (*AsyncIndexer)(nil)
