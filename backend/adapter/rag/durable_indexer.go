package rag

import (
	"context"
	"log"

	"github.com/jamespud/magi/backend/domain/entity"
	"github.com/jamespud/magi/backend/domain/port"
)

// DurableIndexer implements KnowledgePort + MemoryIndexer + DocumentIndexer by
// enqueueing every RAG mutation into the durable rag_index_job queue instead
// of executing it in-process. Retrieve stays synchronous (read path unchanged).
// The inner adapter is only used by the poller to execute queued jobs.
type DurableIndexer struct {
	inner       *HybridKnowledgeAdapter
	repo        port.RagIndexJobRepository
	maxAttempts int
	logger      *log.Logger
}

func NewDurableIndexer(inner *HybridKnowledgeAdapter, repo port.RagIndexJobRepository) *DurableIndexer {
	return &DurableIndexer{inner: inner, repo: repo, maxAttempts: 3, logger: log.Default()}
}

func (d *DurableIndexer) Store(ctx context.Context, proj *entity.CaseMemoryProjection) (port.StoreStats, error) {
	if _, err := d.repo.Enqueue(ctx, entity.RagIndexJobKindIndex, port.SourceCaseMemory, proj.CaseID, d.maxAttempts); err != nil {
		d.logger.Printf("durable index: enqueue case_memory index failed case=%s: %v", proj.CaseID, err)
		return port.StoreStats{}, err
	}
	return port.StoreStats{}, nil
}

func (d *DurableIndexer) StoreDocument(ctx context.Context, doc *entity.KnowledgeDoc) (port.StoreStats, error) {
	if _, err := d.repo.Enqueue(ctx, entity.RagIndexJobKindIndex, port.SourceKnowledgeDoc, doc.ID, d.maxAttempts); err != nil {
		d.logger.Printf("durable index: enqueue knowledge_doc index failed doc=%s: %v", doc.ID, err)
		return port.StoreStats{}, err
	}
	return port.StoreStats{}, nil
}

func (d *DurableIndexer) DeleteSource(ctx context.Context, source, sourceRef string) error {
	if _, err := d.repo.Enqueue(ctx, entity.RagIndexJobKindDelete, source, sourceRef, d.maxAttempts); err != nil {
		d.logger.Printf("durable index: enqueue delete failed source=%s ref=%s: %v", source, sourceRef, err)
		return err
	}
	return nil
}

func (d *DurableIndexer) Retrieve(ctx context.Context, req port.RetrieveRequest) (port.RetrieveResult, error) {
	if d.inner == nil {
		return port.RetrieveResult{}, nil
	}
	return d.inner.Retrieve(ctx, req)
}

// Inner exposes the underlying synchronous adapter so the poller can execute
// queued jobs through it (async mode wraps inner in DurableIndexer).
func (d *DurableIndexer) Inner() *HybridKnowledgeAdapter { return d.inner }

var _ port.KnowledgePort = (*DurableIndexer)(nil)
var _ port.MemoryIndexer = (*DurableIndexer)(nil)
var _ port.DocumentIndexer = (*DurableIndexer)(nil)
