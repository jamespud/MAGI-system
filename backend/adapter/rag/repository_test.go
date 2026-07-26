package rag

import (
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
