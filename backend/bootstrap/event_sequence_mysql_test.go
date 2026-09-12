package bootstrap

import (
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	magi "github.com/jamespud/magi/backend/adapter"
	"gorm.io/driver/mysql"
	"gorm.io/gorm"
)

func openEventSequenceMySQL(t *testing.T) *gorm.DB {
	t.Helper()
	dsn := os.Getenv("MAGI_TEST_MYSQL_DSN")
	if dsn == "" {
		t.Skip("MAGI_TEST_MYSQL_DSN not set; skipping MySQL integration tests — a green run without it does NOT mean MySQL was verified")
	}
	db, err := gorm.Open(mysql.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Skipf("mysql unavailable: %v", err)
	}
	return db
}

func TestVerifyEventSequenceContract_MySQLRejectsEventCaseWithoutCursor(t *testing.T) {
	db := openEventSequenceMySQL(t)
	if err := db.AutoMigrate(&magi.EventModel{}, &magi.EventCursorModel{}); err != nil {
		t.Fatal(err)
	}
	caseID := fmt.Sprintf("s16-missing-cursor-%d", time.Now().UnixNano())
	t.Cleanup(func() {
		_ = db.Exec("DELETE FROM magi_event_cursor WHERE case_id = ?", caseID)
		_ = db.Exec("DELETE FROM magi_event WHERE case_id = ?", caseID)
	})
	if err := db.Create(&magi.EventModel{
		ID: fmt.Sprintf("event-%s", caseID), CaseID: caseID, Seq: 1,
		Type: "CASE_STATUS_CHANGED", Timestamp: time.Now(),
	}).Error; err != nil {
		t.Fatal(err)
	}
	if err := verifyEventSequenceContract(db); err == nil {
		t.Fatal("event-bearing case without cursor must fail startup validation")
	} else if !strings.Contains(strings.ToLower(err.Error()), "missing cursor") {
		t.Fatalf("missing cursor error = %v", err)
	}
}
