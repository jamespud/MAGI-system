package magi

import (
	"context"
	"database/sql"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"

	_ "github.com/mattn/go-sqlite3"
	"github.com/jamespud/magi/backend/domain/port"
)

func openSQLiteInternal(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite3", filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

func TestDBQueryTool_BinaryColumnPlaceholder(t *testing.T) {
	db := openSQLiteInternal(t)
	defer db.Close()
	_, err := db.Exec("CREATE TABLE t (id INTEGER, name TEXT, data BLOB)")
	if err != nil {
		t.Fatal(err)
	}
	db.Exec("INSERT INTO t VALUES (1, 'hello', x'deadbeef')")
	exec := &DBQueryToolExecutor{db: db, maxRows: 10, maxChars: 2000, timeout: 5 * time.Second, blocked: map[string]bool{}}
	result, err := exec.Execute(context.Background(), port.ToolExecutionRequest{
		ToolName: DBQueryToolName, ArgumentsJSON: `{"query":"SELECT * FROM t"}`,
	})
	if err != nil {
		t.Fatal(err)
	}
	rows := result.Structured.(map[string]any)["rows"].([]map[string]any)
	if rows[0]["name"] != "hello" {
		t.Errorf("name = %v, want hello", rows[0]["name"])
	}
	data := fmt.Sprint(rows[0]["data"])
	if !strings.Contains(data, "binary") || !strings.Contains(data, "bytes") {
		t.Errorf("data = %q, want <binary:N bytes> placeholder", data)
	}
}
