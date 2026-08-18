package magi_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	magi "github.com/jamespud/magi/backend/adapter"
	"github.com/jamespud/magi/backend/domain/entity"
	"github.com/jamespud/magi/backend/domain/port"
	_ "github.com/mattn/go-sqlite3"
)

func newDBQueryFixture(t *testing.T) *magi.DBQueryToolExecutor {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "fixture.db")
	db, err := sql.Open("sqlite3", path)
	if err != nil {
		t.Fatalf("open fixture: %v", err)
	}
	defer db.Close()
	if _, err := db.Exec(`CREATE TABLE accounts (id INTEGER PRIMARY KEY, name TEXT, balance REAL)`); err != nil {
		t.Fatalf("create table: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO accounts (name, balance) VALUES ('alice', 100), ('bob', 250)`); err != nil {
		t.Fatalf("seed: %v", err)
	}
	exec, err := magi.NewDBQueryToolExecutor(magi.DBQueryToolConfig{
		Enabled: true, Driver: "sqlite3", DSN: path,
		MaxRows: 10, MaxQueryChars: 1000, TimeoutSeconds: 5,
	})
	if err != nil {
		t.Fatalf("build executor: %v", err)
	}
	t.Cleanup(func() { _ = exec.Close() })
	return exec
}

func TestDBQueryToolExecutorRunsSelect(t *testing.T) {
	exec := newDBQueryFixture(t)
	res, err := exec.Execute(context.Background(), port.ToolExecutionRequest{
		ToolName: magi.DBQueryToolName, ArgumentsJSON: `{"query":"SELECT name, balance FROM accounts ORDER BY id"}`,
	})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	var out struct {
		Columns   []string         `json:"columns"`
		Rows      []map[string]any `json:"rows"`
		Truncated bool             `json:"truncated"`
	}
	if err := json.Unmarshal([]byte(res.Output), &out); err != nil {
		t.Fatalf("decode output: %v", err)
	}
	if len(out.Rows) != 2 || out.Rows[0]["name"] != "alice" || out.Rows[1]["balance"] != float64(250) {
		t.Fatalf("rows = %+v", out.Rows)
	}
	if out.Truncated {
		t.Fatal("10-row limit should not truncate 2 rows")
	}
}

func TestDBQueryToolExecutorRejectsWrites(t *testing.T) {
	exec := newDBQueryFixture(t)
	for _, query := range []string{
		`INSERT INTO accounts (name, balance) VALUES ('x', 1)`,
		`UPDATE accounts SET balance = 0`,
		`DELETE FROM accounts`,
		`DROP TABLE accounts`,
		`SELECT 1; DROP TABLE accounts`,
	} {
		_, err := exec.Execute(context.Background(), port.ToolExecutionRequest{
			ToolName: magi.DBQueryToolName, ArgumentsJSON: `{"query":"` + query + `"}`,
		})
		if err == nil {
			t.Fatalf("expected rejection for %q", query)
		}
	}
}

func TestDBQueryToolExecutorLimitsRowsAndChars(t *testing.T) {
	exec, err := magi.NewDBQueryToolExecutor(magi.DBQueryToolConfig{
		Enabled: true, Driver: "sqlite3", DSN: ":memory:",
		MaxRows: 1, MaxQueryChars: 8, TimeoutSeconds: 5,
	})
	if err != nil {
		t.Fatalf("build executor: %v", err)
	}
	defer exec.Close()
	if _, err := exec.Execute(context.Background(), port.ToolExecutionRequest{
		ToolName: magi.DBQueryToolName, ArgumentsJSON: `{"query":"SELECT 1, 2, 3"}`,
	}); err == nil || !strings.Contains(err.Error(), "exceeds 8 characters") {
		t.Fatalf("expected length rejection, got %v", err)
	}
}

func TestDBQueryToolExecutorRequiresConfiguration(t *testing.T) {
	if _, err := magi.NewDBQueryToolExecutor(magi.DBQueryToolConfig{Enabled: false}); err == nil {
		t.Fatal("expected disabled error")
	}
	if _, err := magi.NewDBQueryToolExecutor(magi.DBQueryToolConfig{Enabled: true}); err == nil {
		t.Fatal("expected missing driver/dsn error")
	}
	if _, err := magi.NewDBQueryToolExecutor(magi.DBQueryToolConfig{Enabled: true, Driver: "mysql"}); err == nil {
		t.Fatal("expected missing dsn error")
	}
}

func TestLocalToolMuxRoutesByToolName(t *testing.T) {
	exec, err := magi.NewLocalToolMux(map[string]port.ToolExecutorPort{
		magi.DBQueryToolName: &stubLocalExec{name: magi.DBQueryToolName},
	})
	if err != nil {
		t.Fatalf("build mux: %v", err)
	}
	if _, err := exec.Execute(context.Background(), port.ToolExecutionRequest{ToolName: "web_search"}); err == nil {
		t.Fatal("expected unknown local tool error")
	}
}

func TestLocalToolRegistryResolvesEnabledLocalTools(t *testing.T) {
	bindings := []entity.ToolBinding{
		{Source: entity.ToolSourceLocal, ToolName: "web_search"},
		{Source: entity.ToolSourceLocal, ToolName: magi.DBQueryToolName},
	}

	all := magi.NewLocalToolRegistry()
	defs, err := all.List(context.Background(), bindings)
	if err != nil {
		t.Fatalf("list all: %v", err)
	}
	if len(defs) != 2 {
		t.Fatalf("all-local registry should resolve both tools, got %+v", defs)
	}

	dbOnly := magi.NewLocalToolRegistry(magi.DBQueryToolName)
	defs, err = dbOnly.List(context.Background(), bindings)
	if err != nil {
		t.Fatalf("list db only: %v", err)
	}
	if len(defs) != 1 || defs[0].Name != magi.DBQueryToolName {
		t.Fatalf("db-only registry resolved %+v", defs)
	}

	searchOnly := magi.NewLocalToolRegistry("web_search")
	defs, err = searchOnly.List(context.Background(), bindings)
	if err != nil {
		t.Fatalf("list search only: %v", err)
	}
	if len(defs) != 1 || defs[0].Name != "web_search" {
		t.Fatalf("search-only registry resolved %+v", defs)
	}
}

type stubLocalExec struct {
	name string
}

func (s *stubLocalExec) Execute(ctx context.Context, req port.ToolExecutionRequest) (*port.ToolExecutionResult, error) {
	return &port.ToolExecutionResult{Output: s.name}, nil
}

var _ port.ToolExecutorPort = (*stubLocalExec)(nil)
