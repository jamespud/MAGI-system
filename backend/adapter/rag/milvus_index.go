package rag

import (
	"context"
	"fmt"

	"github.com/milvus-io/milvus-sdk-go/v2/client"
	"github.com/milvus-io/milvus-sdk-go/v2/entity"
)

// MilvusIndexer implements VectorIndex over Milvus. Collection schema:
// chunk_id (VARCHAR pk), embedding (FLOAT_VECTOR), source (VARCHAR), source_ref (VARCHAR).
type MilvusIndexer struct {
	client     client.Client
	collection string
	dim        int
}

func NewMilvusIndexer(addr, collection string, dim int) (*MilvusIndexer, error) {
	c, err := client.NewClient(context.Background(), client.Config{Address: addr})
	if err != nil {
		return nil, fmt.Errorf("milvus connect: %w", err)
	}
	idx := &MilvusIndexer{client: c, collection: collection, dim: dim}
	if err := idx.ensureCollection(context.Background()); err != nil {
		return nil, err
	}
	return idx, nil
}

func (m *MilvusIndexer) ensureCollection(ctx context.Context) error {
	has, err := m.client.HasCollection(ctx, m.collection)
	if err != nil {
		return fmt.Errorf("milvus has collection: %w", err)
	}
	if has {
		return nil
	}
	schema := entity.NewSchema().WithName(m.collection).
		WithField(entity.NewField().WithName("chunk_id").WithDataType(entity.FieldTypeVarChar).WithMaxLength(64).WithIsPrimaryKey(true).WithIsAutoID(false)).
		WithField(entity.NewField().WithName("embedding").WithDataType(entity.FieldTypeFloatVector).WithDim(int64(m.dim))).
		WithField(entity.NewField().WithName("source").WithDataType(entity.FieldTypeVarChar).WithMaxLength(32)).
		WithField(entity.NewField().WithName("source_ref").WithDataType(entity.FieldTypeVarChar).WithMaxLength(128))
	if err := m.client.CreateCollection(ctx, schema, 1); err != nil {
		return fmt.Errorf("milvus create collection: %w", err)
	}
	idx, err := entity.NewIndexIvfFlat(entity.L2, 128)
	if err != nil {
		return err
	}
	return m.client.CreateIndex(ctx, m.collection, "embedding", idx, false)
}

func (m *MilvusIndexer) Upsert(ctx context.Context, recs []VectorRecord) error {
	if len(recs) == 0 {
		return nil
	}
	ids := make([]string, len(recs))
	vecs := make([][]float32, len(recs))
	sources := make([]string, len(recs))
	refs := make([]string, len(recs))
	for i, r := range recs {
		ids[i] = r.ChunkID
		vecs[i] = r.Embedding
		sources[i] = r.Source
		refs[i] = r.SourceRef
	}
	cols := []entity.Column{
		entity.NewColumnVarChar("chunk_id", ids),
		entity.NewColumnFloatVector("embedding", m.dim, vecs),
		entity.NewColumnVarChar("source", sources),
		entity.NewColumnVarChar("source_ref", refs),
	}
	if _, err := m.client.Upsert(ctx, m.collection, "", cols...); err != nil {
		return fmt.Errorf("milvus upsert: %w", err)
	}
	return nil
}

func (m *MilvusIndexer) Search(ctx context.Context, queryVec []float32, topK int, f *IndexFilter) ([]VectorHit, error) {
	sp, err := entity.NewIndexIvfFlatSearchParam(64)
	if err != nil {
		return nil, err
	}
	results, err := m.client.Search(ctx, m.collection, []string{}, filterExpr(f),
		[]string{"chunk_id"}, []entity.Vector{entity.FloatVector(queryVec)},
		"embedding", entity.L2, topK, sp)
	if err != nil {
		return nil, fmt.Errorf("milvus search: %w", err)
	}
	if len(results) == 0 || results[0].IDs == nil {
		return nil, nil
	}
	r := results[0]
	hits := make([]VectorHit, 0, r.ResultCount)
	for i := 0; i < r.IDs.Len(); i++ {
		id, _ := r.IDs.GetAsString(i)
		score := float32(0)
		if i < len(r.Scores) {
			score = r.Scores[i]
		}
		hits = append(hits, VectorHit{ChunkID: id, Score: score})
	}
	return hits, nil
}

func (m *MilvusIndexer) DeleteBySourceRef(ctx context.Context, source, sourceRef string) error {
	expr := fmt.Sprintf("source == \"%s\" && source_ref == \"%s\"", source, sourceRef)
	return m.client.Delete(ctx, m.collection, "", expr)
}

func filterExpr(f *IndexFilter) string {
	if f == nil || len(f.Sources) == 0 {
		return ""
	}
	expr := ""
	for i, s := range f.Sources {
		if i > 0 {
			expr += " || "
		}
		expr += "source == \"" + s + "\""
	}
	return expr
}
