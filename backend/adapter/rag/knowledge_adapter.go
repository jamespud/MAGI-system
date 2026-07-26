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
	logger    *log.Logger
}

func NewHybridKnowledgeAdapter(
	chunker *Chunker,
	embedder Embedder,
	repo *ChunkRepository,
	vec VectorIndex,
	lex LexicalIndex,
	retriever *Retriever,
) *HybridKnowledgeAdapter {
	return &HybridKnowledgeAdapter{
		chunker: chunker, embedder: embedder, repo: repo, vec: vec, lex: lex,
		retriever: retriever, logger: log.Default(),
	}
}

// Store renders the projection to a document, chunks it, embeds 300-level
// blocks, and writes to MySQL + Milvus + ES. Idempotent by source_ref (case_id).
func (a *HybridKnowledgeAdapter) Store(ctx context.Context, proj *entity.CaseMemoryProjection) error {
	if proj == nil {
		return fmt.Errorf("nil projection")
	}
	source := "case_memory"
	sourceRef := proj.CaseID

	// Delete old chunks first (idempotent).
	_ = a.vec.DeleteBySourceRef(ctx, source, sourceRef)
	_ = a.lex.DeleteBySourceRef(ctx, source, sourceRef)
	if err := a.repo.DeleteBySourceRef(ctx, source, sourceRef); err != nil {
		return fmt.Errorf("delete old: %w", err)
	}

	// Render + chunk.
	doc := memory.RenderDocument(proj)
	chunked := a.chunker.Chunk(doc, source, sourceRef)

	// Embed 300-level blocks.
	texts := make([]string, len(chunked.Chunks300))
	for i, c := range chunked.Chunks300 {
		texts[i] = c.Content
	}
	vecs, err := a.embedder.Embed(ctx, texts)
	if err != nil {
		return fmt.Errorf("embed: %w", err)
	}

	// Write MySQL (source of truth) first.
	if err := a.repo.WriteChunks(ctx, chunked); err != nil {
		return fmt.Errorf("write mysql: %w", err)
	}

	// Write Milvus (best-effort).
	if a.vec != nil && len(chunked.Chunks300) > 0 {
		recs := make([]VectorRecord, len(chunked.Chunks300))
		for i, c := range chunked.Chunks300 {
			recs[i] = VectorRecord{ChunkID: c.ID, Embedding: vecs[i], Source: c.Source, SourceRef: c.SourceRef}
		}
		if err := a.vec.Upsert(ctx, recs); err != nil {
			a.logger.Printf("rag store: milvus upsert failed (re-indexable from mysql): %v", err)
		}
	}

	// Write ES (best-effort).
	if a.lex != nil && len(chunked.Chunks300) > 0 {
		recs := make([]TextRecord, len(chunked.Chunks300))
		for i, c := range chunked.Chunks300 {
			recs[i] = TextRecord{ChunkID: c.ID, Content: c.Content, Source: c.Source, SourceRef: c.SourceRef}
		}
		if err := a.lex.Upsert(ctx, recs); err != nil {
			a.logger.Printf("rag store: es upsert failed (re-indexable from mysql): %v", err)
		}
	}
	return nil
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
	blocks, err := a.retriever.Retrieve(ctx, req.Query, opts)
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
