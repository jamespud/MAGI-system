package magi_test

import (
	"context"
	"strings"
	"testing"
	"time"

	magi "github.com/jamespud/magi/backend/adapter"
)

// MySQL stores DATETIME(3), so rows created in the same millisecond share a
// timestamp. Without a primary-key tie-breaker the list order is whatever the
// engine happens to return, and can change between refreshes.
func TestCaseList_IsStableForIdenticalTimestampsOnMySQL(t *testing.T) {
	db := openA2AMySQL(t)
	if err := db.AutoMigrate(&magi.CaseModel{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	suffix := time.Now().UTC().Format("20060102150405.000000000")
	ids := []string{"case-a-" + suffix, "case-b-" + suffix, "case-c-" + suffix}
	ts := time.Now().Truncate(time.Millisecond)
	t.Cleanup(func() {
		db.Exec("DELETE FROM decision_case WHERE id IN ?", ids)
	})
	for _, id := range ids {
		if err := db.Create(&magi.CaseModel{
			ID: id, UserID: 1, Status: "DRAFT", CreatedAt: ts, UpdatedAt: ts,
		}).Error; err != nil {
			t.Fatalf("seed %s: %v", id, err)
		}
	}

	repo := magi.NewRepository(db).CaseRepo()
	var first []string
	for i := 0; i < 5; i++ {
		cases, err := repo.List(context.Background())
		if err != nil {
			t.Fatalf("list: %v", err)
		}
		var got []string
		for _, c := range cases {
			if strings.HasSuffix(c.ID, suffix) {
				got = append(got, c.ID)
			}
		}
		if i == 0 {
			first = got
			continue
		}
		if strings.Join(got, ",") != strings.Join(first, ",") {
			t.Fatalf("order changed between calls: %v vs %v", first, got)
		}
	}
	if len(first) != 3 {
		t.Fatalf("expected the 3 seeded rows, got %v", first)
	}
	// created_at DESC, id DESC: the last id wins the tie.
	if !strings.HasPrefix(first[0], "case-c-") {
		t.Fatalf("order = %v, want the case-c row first", first)
	}
}
