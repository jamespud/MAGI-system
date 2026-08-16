package rag

import (
	"context"
	"fmt"
	"log"

	"github.com/jamespud/magi/backend/domain/entity"
	"github.com/jamespud/magi/backend/domain/memory"
	"github.com/jamespud/magi/backend/domain/port"
)

// HybridKnowledgeAdapter implements port.KnowledgePort backed by
// Milvus + ES + MySQL. Store: RenderDocument -> Chunk -> Embed -> write
// MySQL (source of truth) + Milvus + ES. Retrieve: delegate to Retriever.
type HybridKnowledgeAdapter struct {
	chunker   *Chunker
	embedder  Embedder
	repo      *ChunkRepository
	vec       VectorIndex
	lex       LexicalIndex
	retriever *Retriever
	pub       port.EventPublisher
	logger    *log.Logger
}

func NewHybridKnowledgeAdapter(
	chunker *Chunker,
	embedder Embedder,
	repo *ChunkRepository,
	vec VectorIndex,
	lex LexicalIndex,
	retriever *Retriever,
	pub port.EventPublisher,
) *HybridKnowledgeAdapter {
	return &HybridKnowledgeAdapter{
		chunker: chunker, embedder: embedder, repo: repo, vec: vec, lex: lex,
		retriever: retriever, pub: pub, logger: log.Default(),
	}
}

// Store renders the projection to a document and indexes it into the RAG
// pipeline. Idempotent by source_ref (case_id).
func (a *HybridKnowledgeAdapter) Store(ctx context.Context, proj *entity.CaseMemoryProjection) (port.StoreStats, error) {
	if proj == nil {
		return port.StoreStats{}, fmt.Errorf("nil projection")
	}
	return a.storeRaw(ctx, port.SourceCaseMemory, proj.CaseID, memory.RenderDocument(proj))
}

// StoreDocument indexes a user-uploaded knowledge document (title + content)
// into the RAG pipeline so agents and the memory search can retrieve it.
func (a *HybridKnowledgeAdapter) StoreDocument(ctx context.Context, doc *entity.KnowledgeDoc) (port.StoreStats, error) {
	if doc == nil {
		return port.StoreStats{}, fmt.Errorf("nil document")
	}
	render := doc.Title
	if doc.Content != "" {
		if render != "" {
			render += "\n\n"
		}
		render += doc.Content
	}
	return a.storeRaw(ctx, port.SourceKnowledgeDoc, doc.ID, render)
}

// DeleteSource removes every chunk of a source (case memory or knowledge doc)
// from MySQL + Milvus + ES.
func (a *HybridKnowledgeAdapter) DeleteSource(ctx context.Context, source, sourceRef string) error {
	_ = a.vec.DeleteBySourceRef(ctx, source, sourceRef)
	_ = a.lex.DeleteBySourceRef(ctx, source, sourceRef)
	return a.repo.DeleteBySourceRef(ctx, source, sourceRef)
}

// storeRaw renders, chunks, embeds, and writes one document to MySQL + Milvus
// + ES. Idempotent by (source, source_ref).
func (a *HybridKnowledgeAdapter) storeRaw(ctx context.Context, source, sourceRef, render string) (port.StoreStats, error) {
	// Delete old chunks first (idempotent).
	_ = a.vec.DeleteBySourceRef(ctx, source, sourceRef)
	_ = a.lex.DeleteBySourceRef(ctx, source, sourceRef)
	if err := a.repo.DeleteBySourceRef(ctx, source, sourceRef); err != nil {
		return port.StoreStats{}, fmt.Errorf("delete old: %w", err)
	}

	// Render + chunk.
	doc := render
	chunked := a.chunker.Chunk(doc, source, sourceRef)
	stats := port.StoreStats{
		Chunks300:  len(chunked.Chunks300),
		Chunks900:  len(chunked.Chunks900),
		Chunks1800: len(chunked.Chunks1800),
	}

	// Embed 300-level blocks.
	texts := make([]string, len(chunked.Chunks300))
	for i, c := range chunked.Chunks300 {
		texts[i] = c.Content
	}
	vecs, err := a.embedder.Embed(ctx, texts)
	if err != nil {
		return port.StoreStats{}, fmt.Errorf("embed: %w", err)
	}

	// Write MySQL (source of truth) first.
	if err := a.repo.WriteChunks(ctx, chunked); err != nil {
		return port.StoreStats{}, fmt.Errorf("write mysql: %w", err)
	}
	a.logger.Printf("rag store: mysql ok source_ref=%s chunks(300=%d 900=%d 1800=%d)", sourceRef, stats.Chunks300, stats.Chunks900, stats.Chunks1800)

	// Write Milvus (best-effort).
	if a.vec != nil && len(chunked.Chunks300) > 0 {
		recs := make([]VectorRecord, len(chunked.Chunks300))
		for i, c := range chunked.Chunks300 {
			recs[i] = VectorRecord{ChunkID: c.ID, Embedding: vecs[i], Source: c.Source, SourceRef: c.SourceRef}
		}
		if err := a.vec.Upsert(ctx, recs); err != nil {
			a.logger.Printf("rag store: milvus upsert FAILED source_ref=%s chunks=%d (re-indexable from mysql): %v", sourceRef, len(recs), err)
		} else {
			a.logger.Printf("rag store: milvus upsert ok source_ref=%s chunks=%d", sourceRef, len(recs))
		}
	}

	// Write ES (best-effort).
	if a.lex != nil && len(chunked.Chunks300) > 0 {
		recs := make([]TextRecord, len(chunked.Chunks300))
		for i, c := range chunked.Chunks300 {
			recs[i] = TextRecord{ChunkID: c.ID, Content: c.Content, Source: c.Source, SourceRef: c.SourceRef}
		}
		if err := a.lex.Upsert(ctx, recs); err != nil {
			a.logger.Printf("rag store: es upsert FAILED source_ref=%s chunks=%d (re-indexable from mysql): %v", sourceRef, len(recs), err)
		} else {
			a.logger.Printf("rag store: es upsert ok source_ref=%s chunks=%d", sourceRef, len(recs))
		}
	}
	if a.pub != nil {
		_ = a.pub.Publish(ctx, entity.NewEvent(sourceRef, "", nil, entity.EventMemoryIndexed, map[string]any{
			"source_ref": sourceRef, "chunks_300": stats.Chunks300,
			"chunks_900": stats.Chunks900, "chunks_1800": stats.Chunks1800,
		}))
	}
	return stats, nil
}

// Retrieve delegates to the Retriever and maps rag.MergedBlock -> port.MergedBlock.
func (a *HybridKnowledgeAdapter) Retrieve(ctx context.Context, req port.RetrieveRequest) (port.RetrieveResult, error) {
	if a.retriever == nil {
		return port.RetrieveResult{}, nil
	}
	opts := MergeOpts{TopK: req.TopK}
	if opts.TopK == 0 {
		opts.TopK = 15
	}
	opts.Thr900 = 3
	opts.Thr1800 = 2
	opts.Orphan = "keep_300"
	var filter *IndexFilter
	if len(req.Sources) > 0 || len(req.SourceRefs) > 0 {
		filter = &IndexFilter{Sources: req.Sources, SourceRefs: req.SourceRefs}
	}
	blocks, err := a.retriever.Retrieve(ctx, req.Query, opts, filter)
	if err != nil {
		return port.RetrieveResult{}, err
	}
	out := port.RetrieveResult{Blocks: make([]port.MergedBlock, len(blocks))}
	for i, b := range blocks {
		out.Blocks[i] = port.MergedBlock{Level: b.Level, Content: b.Content, SourceRef: b.SourceRef, ChunkIDs: b.ChunkIDs}
	}
	return out, nil
}

var _ port.KnowledgePort = (*HybridKnowledgeAdapter)(nil)
