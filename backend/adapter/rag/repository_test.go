package rag

import (
	"context"
	"testing"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func newTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.AutoMigrate(AllModels()...); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return db
}

func TestChunkModelsMigrateAndCRUD(t *testing.T) {
	db := newTestDB(t)

	c1800 := Chunk1800{ID: "c1800_1", Source: "case_memory", SourceRef: "case-1", Content: "top", TokenCount: 1800}
	if err := db.Create(&c1800).Error; err != nil {
		t.Fatalf("create 1800: %v", err)
	}
	c900 := Chunk900{ID: "c900_1", Parent1800ID: "c1800_1", Source: "case_memory", SourceRef: "case-1", Content: "mid", TokenCount: 900, Seq: 0}
	if err := db.Create(&c900).Error; err != nil {
		t.Fatalf("create 900: %v", err)
	}
	c300 := Chunk300{ID: "c300_1", Parent900ID: "c900_1", Source: "case_memory", SourceRef: "case-1", Content: "leaf", TokenCount: 300, Seq: 0}
	if err := db.Create(&c300).Error; err != nil {
		t.Fatalf("create 300: %v", err)
	}

	var got Chunk300
	if err := db.First(&got, "id = ?", "c300_1").Error; err != nil {
		t.Fatalf("query 300: %v", err)
	}
	if got.Parent900ID != "c900_1" {
		t.Errorf("parent_900_id = %q, want c900_1", got.Parent900ID)
	}
}

func TestChunkRepositoryWriteAndQuery(t *testing.T) {
	db := newTestDB(t)
	repo := NewChunkRepository(db)
	ctx := context.Background()

	doc := ChunkedDoc{
		Chunks1800: []ChunkBlock{{ID: "c1800_1", Source: "case_memory", SourceRef: "case-1", Content: "top", TokenCount: 1800, Seq: 0}},
		Chunks900:  []ChunkBlock{{ID: "c900_1", Parent1800ID: "c1800_1", Source: "case_memory", SourceRef: "case-1", Content: "mid", TokenCount: 900, Seq: 0}},
		Chunks300:  []ChunkBlock{{ID: "c300_1", Parent900ID: "c900_1", Source: "case_memory", SourceRef: "case-1", Content: "leaf", TokenCount: 300, Seq: 0}},
	}
	if err := repo.WriteChunks(ctx, doc); err != nil {
		t.Fatalf("write: %v", err)
	}

	parents, err := repo.Get300Parents(ctx, []string{"c300_1"})
	if err != nil {
		t.Fatalf("get300parents: %v", err)
	}
	if parents["c300_1"] != "c900_1" {
		t.Errorf("parent = %q, want c900_1", parents["c300_1"])
	}

	c900, err := repo.Get900Blocks(ctx, []string{"c900_1"})
	if err != nil {
		t.Fatalf("get900: %v", err)
	}
	if len(c900) != 1 || c900[0].Parent1800ID != "c1800_1" {
		t.Errorf("900 block = %+v", c900)
	}

	if err := repo.DeleteBySourceRef(ctx, "case_memory", "case-1"); err != nil {
		t.Fatalf("delete: %v", err)
	}
	var n int64
	db.Model(&Chunk300{}).Count(&n)
	if n != 0 {
		t.Errorf("after delete, 300 count = %d, want 0", n)
	}
}
