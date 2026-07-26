package rag

import (
	"context"
	"time"

	"gorm.io/gorm"
)

// Chunk1800 is the top-level hierarchy block (= 2x900 = 6x300).
type Chunk1800 struct {
	ID         string `gorm:"primaryKey"`
	Source     string
	SourceRef  string `gorm:"column:source_ref;index"`
	Content    string `gorm:"type:mediumtext"`
	TokenCount int
	CreatedAt  time.Time
}

func (Chunk1800) TableName() string { return "rag_chunk_1800" }

// Chunk900 is the middle hierarchy block (= 3x300).
type Chunk900 struct {
	ID           string `gorm:"primaryKey"`
	Parent1800ID string `gorm:"column:parent_1800_id;index"`
	Source       string
	SourceRef    string `gorm:"column:source_ref;index"`
	Content      string `gorm:"type:text"`
	TokenCount   int
	Seq          int
	CreatedAt    time.Time
}

func (Chunk900) TableName() string { return "rag_chunk_900" }

// Chunk300 is the leaf block (vectorized).
type Chunk300 struct {
	ID          string `gorm:"primaryKey"`
	Parent900ID string `gorm:"column:parent_900_id;index"`
	Source      string
	SourceRef   string `gorm:"column:source_ref;index"`
	Content     string `gorm:"type:text"`
	TokenCount  int
	Seq         int
	CreatedAt   time.Time
}

func (Chunk300) TableName() string { return "rag_chunk_300" }

// AllModels returns all RAG GORM models for AutoMigrate.
func AllModels() []any {
	return []any{&Chunk1800{}, &Chunk900{}, &Chunk300{}}
}

// ChunkRepository reads/writes the 3-level hierarchy tables.
type ChunkRepository struct{ db *gorm.DB }

func NewChunkRepository(db *gorm.DB) *ChunkRepository { return &ChunkRepository{db: db} }

// WriteChunks persists a ChunkedDoc to all 3 tables in a transaction.
func (r *ChunkRepository) WriteChunks(ctx context.Context, doc ChunkedDoc) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		now := time.Now()
		for _, c := range doc.Chunks1800 {
			m := Chunk1800{ID: c.ID, Source: c.Source, SourceRef: c.SourceRef, Content: c.Content, TokenCount: c.TokenCount, CreatedAt: now}
			if err := tx.Create(&m).Error; err != nil {
				return err
			}
		}
		for _, c := range doc.Chunks900 {
			m := Chunk900{ID: c.ID, Parent1800ID: c.Parent1800ID, Source: c.Source, SourceRef: c.SourceRef, Content: c.Content, TokenCount: c.TokenCount, Seq: c.Seq, CreatedAt: now}
			if err := tx.Create(&m).Error; err != nil {
				return err
			}
		}
		for _, c := range doc.Chunks300 {
			m := Chunk300{ID: c.ID, Parent900ID: c.Parent900ID, Source: c.Source, SourceRef: c.SourceRef, Content: c.Content, TokenCount: c.TokenCount, Seq: c.Seq, CreatedAt: now}
			if err := tx.Create(&m).Error; err != nil {
				return err
			}
		}
		return nil
	})
}

// DeleteBySourceRef removes all chunks for a source/source_ref across all 3 tables.
func (r *ChunkRepository) DeleteBySourceRef(ctx context.Context, source, sourceRef string) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("source = ? AND source_ref = ?", source, sourceRef).Delete(&Chunk300{}).Error; err != nil {
			return err
		}
		if err := tx.Where("source = ? AND source_ref = ?", source, sourceRef).Delete(&Chunk900{}).Error; err != nil {
			return err
		}
		if err := tx.Where("source = ? AND source_ref = ?", source, sourceRef).Delete(&Chunk1800{}).Error; err != nil {
			return err
		}
		return nil
	})
}

// Get300Parents returns chunk_id -> parent_900_id for the given 300 IDs.
func (r *ChunkRepository) Get300Parents(ctx context.Context, ids []string) (map[string]string, error) {
	if len(ids) == 0 {
		return map[string]string{}, nil
	}
	var rows []Chunk300
	if err := r.db.WithContext(ctx).Where("id IN ?", ids).Find(&rows).Error; err != nil {
		return nil, err
	}
	out := make(map[string]string, len(rows))
	for _, row := range rows {
		out[row.ID] = row.Parent900ID
	}
	return out, nil
}

// Get900Blocks returns 900-level rows for the given IDs.
func (r *ChunkRepository) Get900Blocks(ctx context.Context, ids []string) ([]Chunk900, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	var rows []Chunk900
	if err := r.db.WithContext(ctx).Where("id IN ?", ids).Find(&rows).Error; err != nil {
		return nil, err
	}
	return rows, nil
}

// Get1800Blocks returns 1800-level rows for the given IDs.
func (r *ChunkRepository) Get1800Blocks(ctx context.Context, ids []string) ([]Chunk1800, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	var rows []Chunk1800
	if err := r.db.WithContext(ctx).Where("id IN ?", ids).Find(&rows).Error; err != nil {
		return nil, err
	}
	return rows, nil
}
