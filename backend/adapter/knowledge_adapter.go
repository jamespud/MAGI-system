package magi

import (
	"context"
	"fmt"

	crossknowledge "github.com/coze-dev/coze-studio/backend/crossdomain/knowledge"
	knowledgemodel "github.com/coze-dev/coze-studio/backend/crossdomain/knowledge/model"
	"github.com/jamespud/magi/backend/domain/entity"
	"github.com/jamespud/magi/backend/domain/port"
)

// KnowledgeAdapter implements port.KnowledgePort.
// Retrieve: via Coze crossknowledge (RAG). Store: via MemoryRepository (DB).
// crossknowledge.Store (RAG indexing) deferred -- requires MinIO FileURL pipeline.
type KnowledgeAdapter struct {
	svc      crossknowledge.Knowledge
	memRepo  port.MemoryRepository
}

func NewKnowledgeAdapter(svc crossknowledge.Knowledge, memRepo port.MemoryRepository) *KnowledgeAdapter {
	return &KnowledgeAdapter{svc: svc, memRepo: memRepo}
}

func (a *KnowledgeAdapter) Retrieve(ctx context.Context, query string, knowledgeIDs []int64) ([]port.KnowledgeChunk, error) {
	if a.svc == nil {
		return nil, nil
	}
	resp, err := a.svc.Retrieve(ctx, &knowledgemodel.RetrieveRequest{Query: query, KnowledgeIDs: knowledgeIDs})
	if err != nil {
		return nil, fmt.Errorf("retrieve failed: %w", err)
	}
	if resp == nil {
		return nil, nil
	}
	chunks := make([]port.KnowledgeChunk, 0, len(resp.RetrieveSlices))
	for _, rs := range resp.RetrieveSlices {
		if rs == nil || rs.Slice == nil {
			continue
		}
		chunks = append(chunks, port.KnowledgeChunk{
			Content: fmt.Sprintf("%+v", rs.Slice),
			Score:   rs.Score,
		})
	}
	return chunks, nil
}

func (a *KnowledgeAdapter) Store(ctx context.Context, proj *entity.CaseMemoryProjection) error {
	if a.memRepo == nil {
		return fmt.Errorf("memory repository is nil")
	}
	return a.memRepo.Save(ctx, proj)
}

var _ port.KnowledgePort = (*KnowledgeAdapter)(nil)
