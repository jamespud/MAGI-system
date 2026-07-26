package rag

import "time"

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
