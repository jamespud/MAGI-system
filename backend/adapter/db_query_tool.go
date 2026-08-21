package magi

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	_ "github.com/mattn/go-sqlite3"

	"github.com/jamespud/magi/backend/domain/port"
)

const (
	// DBQueryToolName is the local read-only database query tool.
	DBQueryToolName = "db_query"

	defaultDBQueryMaxRows    = 50
	defaultDBQueryMaxChars   = 2000
	defaultDBQueryTimeoutSec = 10
)

// defaultBlockedDBQueryPrefixes are statement families that the read-only DB
// tool rejects regardless of the configured extra prefixes.
var defaultBlockedDBQueryPrefixes = []string{
	"INSERT", "UPDATE", "DELETE", "DROP", "ALTER", "CREATE", "TRUNCATE",
	"GRANT", "REVOKE", "SET", "CALL", "EXEC", "MERGE", "REPLACE", "RENAME",
	"ATTACH", "DETACH", "VACUUM", "REINDEX", "LOAD", "COMMIT", "BEGIN",
}

// DBQueryToolConfig is the deterministic guardrail configuration for the
// read-only database query tool.
type DBQueryToolConfig struct {
	Enabled         bool
	Driver          string
	DSN             string
	MaxRows         int
	MaxQueryChars   int
	TimeoutSeconds  int
	BlockedPrefixes []string
}

// DBQueryToolExecutor executes single-statement SELECT queries inside a
// read-only transaction. The tool never returns write results: both the
// statement-prefix guard and the SQL transaction read-only flag are enforced.
type DBQueryToolExecutor struct {
	db       *sql.DB
	maxRows  int
	maxChars int
	timeout  time.Duration
	blocked  map[string]bool
}

// NewDBQueryToolExecutor opens a connection and validates the policy.
func NewDBQueryToolExecutor(cfg DBQueryToolConfig) (*DBQueryToolExecutor, error) {
	if !cfg.Enabled {
		return nil, errors.New("db_query tool is not enabled")
	}
	driver := strings.TrimSpace(cfg.Driver)
	if driver == "" {
		return nil, errors.New("db_query: driver is required")
	}
	if strings.TrimSpace(cfg.DSN) == "" {
		return nil, errors.New("db_query: dsn is required")
	}
	maxRows := cfg.MaxRows
	if maxRows <= 0 {
		maxRows = defaultDBQueryMaxRows
	}
	maxChars := cfg.MaxQueryChars
	if maxChars <= 0 {
		maxChars = defaultDBQueryMaxChars
	}
	timeoutSec := cfg.TimeoutSeconds
	if timeoutSec <= 0 {
		timeoutSec = defaultDBQueryTimeoutSec
	}
	db, err := sql.Open(driver, cfg.DSN)
	if err != nil {
		return nil, fmt.Errorf("db_query: open %s: %w", driver, err)
	}
	blocked := map[string]bool{}
	for _, prefix := range append(append([]string{}, defaultBlockedDBQueryPrefixes...), cfg.BlockedPrefixes...) {
		prefix = strings.ToUpper(strings.TrimSpace(prefix))
		if prefix != "" {
			blocked[prefix] = true
		}
	}
	return &DBQueryToolExecutor{
		db: db, maxRows: maxRows, maxChars: maxChars,
		timeout: time.Duration(timeoutSec) * time.Second, blocked: blocked,
	}, nil
}

// Close releases the underlying connection pool.
func (e *DBQueryToolExecutor) Close() error {
	if e == nil || e.db == nil {
		return nil
	}
	return e.db.Close()
}

// Execute runs a single read-only SELECT and returns rows as JSON.
func (e *DBQueryToolExecutor) Execute(ctx context.Context, req port.ToolExecutionRequest) (*port.ToolExecutionResult, error) {
	var args struct {
		Query string `json:"query"`
	}
	if err := json.Unmarshal([]byte(req.ArgumentsJSON), &args); err != nil {
		return nil, fmt.Errorf("db_query: parse args: %w", err)
	}
	query := strings.TrimSpace(args.Query)
	if query == "" {
		return nil, errors.New("db_query: query cannot be empty")
	}
	if len([]rune(query)) > e.maxChars {
		return nil, fmt.Errorf("db_query: query exceeds %d characters", e.maxChars)
	}
	if err := e.validateQuery(query); err != nil {
		return nil, err
	}

	runCtx := ctx
	var cancel context.CancelFunc
	if e.timeout > 0 {
		runCtx, cancel = context.WithTimeout(ctx, e.timeout)
		defer cancel()
	}
	tx, err := e.db.BeginTx(runCtx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return nil, fmt.Errorf("db_query: begin read-only tx: %w", err)
	}
	defer tx.Rollback()

	rows, err := tx.QueryContext(runCtx, query)
	if err != nil {
		return nil, fmt.Errorf("db_query: query: %w", err)
	}
	defer rows.Close()
	columns, err := rows.Columns()
	if err != nil {
		return nil, fmt.Errorf("db_query: columns: %w", err)
	}

	results := make([]map[string]any, 0, e.maxRows)
	for rows.Next() {
		if len(results) >= e.maxRows {
			break
		}
		values := make([]any, len(columns))
		dest := make([]any, len(columns))
		for i := range values {
			dest[i] = &values[i]
		}
		if err := rows.Scan(dest...); err != nil {
			return nil, fmt.Errorf("db_query: scan: %w", err)
		}
		row := map[string]any{}
		for i, col := range columns {
			switch v := values[i].(type) {
			case []byte:
				if utf8.Valid(v) {
					row[col] = string(v)
				} else {
					row[col] = fmt.Sprintf("<binary:%d bytes>", len(v))
				}
			case time.Time:
				row[col] = v.Format(time.RFC3339)
			default:
				row[col] = v
			}
		}
		results = append(results, row)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("db_query: rows: %w", err)
	}

	out := map[string]any{
		"columns":   columns,
		"rows":      results,
		"truncated": len(results) == e.maxRows,
	}
	raw, err := json.Marshal(out)
	if err != nil {
		return nil, fmt.Errorf("db_query: encode result: %w", err)
	}
	return &port.ToolExecutionResult{Output: string(raw), Structured: out}, nil
}

// validateQuery enforces SELECT-only, single-statement queries with a
// configurable block-list on top of the built-in write prefixes.
func (e *DBQueryToolExecutor) validateQuery(query string) error {
	trimmed := strings.TrimRight(query, "; \t\r\n")
	if strings.Contains(trimmed, ";") {
		return errors.New("db_query: only a single statement is allowed")
	}
	upper := strings.ToUpper(strings.TrimSpace(trimmed))
	if !strings.HasPrefix(upper, "SELECT") {
		return errors.New("db_query: only SELECT statements are allowed")
	}
	for prefix := range e.blocked {
		if prefix == "" {
			continue
		}
		// Only reject when the statement actually starts with a blocked
		// keyword (SELECT is excluded by construction above, so this covers
		// nested starts after comments/whitespace are handled by TrimSpace).
		if upper == prefix || strings.HasPrefix(upper, prefix+" ") || strings.HasPrefix(upper, prefix+"\t") {
			return fmt.Errorf("db_query: blocked statement prefix %q", prefix)
		}
	}
	return nil
}

var _ port.ToolExecutorPort = (*DBQueryToolExecutor)(nil)

// localToolMux routes local tool execution (web_search, db_query) to the
// configured executor for that tool name.
type localToolMux struct {
	executors map[string]port.ToolExecutorPort
}

// NewLocalToolMux wires local executors by tool name.
func NewLocalToolMux(executors map[string]port.ToolExecutorPort) (port.ToolExecutorPort, error) {
	if len(executors) == 0 {
		return nil, nil
	}
	return &localToolMux{executors: executors}, nil
}

func (m *localToolMux) Execute(ctx context.Context, req port.ToolExecutionRequest) (*port.ToolExecutionResult, error) {
	executor, ok := m.executors[req.ToolName]
	if !ok {
		return nil, fmt.Errorf("local tool: no executor for %q", req.ToolName)
	}
	return executor.Execute(ctx, req)
}

var _ port.ToolExecutorPort = (*localToolMux)(nil)
