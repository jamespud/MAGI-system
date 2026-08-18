package magi

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/jamespud/magi/backend/domain/port"
)

// RepoToolName is the built-in read-only repository query tool.
const RepoToolName = "repo_query"

const (
	defaultRepoMaxResults = 50
	defaultRepoMaxBytes   = 4 * 1024 * 1024
)

// repoArgsSchema is the JSON Schema for repo_query arguments.
const repoArgsSchema = `{"type":"object","properties":{"action":{"type":"string","enum":["grep","files"]},"query":{"type":"string","description":"substring to search (grep) or glob (files)"},"include":{"type":"array","items":{"type":"string"}},"max_results":{"type":"integer"}},"required":["action","query"],"additionalProperties":false}`

// defaultRepoIncludes are the file extensions searched by default.
var defaultRepoIncludes = []string{"*.go", "*.ts", "*.tsx", "*.py", "*.js", "*.md", "*.sql", "*.yaml", "*.yml", "*.json"}

// RepoToolConfig bounds the read-only repository query tool.
type RepoToolConfig struct {
	Enabled      bool
	Roots        []string
	Includes     []string
	MaxResults   int
	MaxFileBytes int64
}

// RepoQueryToolExecutor greps and lists files inside allow-listed roots.
// Searches are read-only, bounded by result/file-size limits, and never
// follow symlinks outside the roots.
type RepoQueryToolExecutor struct {
	roots        []string
	includes     []string
	maxResults   int
	maxFileBytes int64
}

// NewRepoQueryToolExecutor normalizes roots and defaults.
func NewRepoQueryToolExecutor(cfg RepoToolConfig) (port.ToolExecutorPort, error) {
	if !cfg.Enabled {
		return nil, fmt.Errorf("repo_query tool is not enabled")
	}
	if len(cfg.Roots) == 0 {
		return nil, fmt.Errorf("repo_query: at least one root is required")
	}
	roots := make([]string, 0, len(cfg.Roots))
	seen := map[string]bool{}
	for _, root := range cfg.Roots {
		abs, err := filepath.Abs(filepath.Clean(root))
		if err != nil {
			return nil, fmt.Errorf("repo_query: resolve root %q: %w", root, err)
		}
		if !seen[abs] {
			seen[abs] = true
			roots = append(roots, abs)
		}
	}
	includes := cfg.Includes
	if len(includes) == 0 {
		includes = defaultRepoIncludes
	}
	maxResults := cfg.MaxResults
	if maxResults <= 0 {
		maxResults = defaultRepoMaxResults
	}
	maxBytes := cfg.MaxFileBytes
	if maxBytes <= 0 {
		maxBytes = defaultRepoMaxBytes
	}
	return &RepoQueryToolExecutor{
		roots: roots, includes: includes, maxResults: maxResults, maxFileBytes: maxBytes,
	}, nil
}

func (e *RepoQueryToolExecutor) Execute(ctx context.Context, req port.ToolExecutionRequest) (*port.ToolExecutionResult, error) {
	var args struct {
		Action     string   `json:"action"`
		Query      string   `json:"query"`
		Include    []string `json:"include,omitempty"`
		MaxResults int      `json:"max_results,omitempty"`
	}
	if err := json.Unmarshal([]byte(req.ArgumentsJSON), &args); err != nil {
		return nil, fmt.Errorf("repo_query: parse args: %w", err)
	}
	if strings.TrimSpace(args.Query) == "" {
		return nil, fmt.Errorf("repo_query: query is required")
	}
	includes := args.Include
	if len(includes) == 0 {
		includes = e.includes
	}
	maxResults := args.MaxResults
	if maxResults <= 0 {
		maxResults = e.maxResults
	}
	switch args.Action {
	case "grep":
		return e.grep(ctx, args.Query, includes, maxResults)
	case "files":
		return e.files(ctx, args.Query, includes, maxResults)
	default:
		return nil, fmt.Errorf("repo_query: action must be \"grep\" or \"files\"")
	}
}

// grep scans matching files for a substring and returns per-line hits.
func (e *RepoQueryToolExecutor) grep(ctx context.Context, query string, includes []string, maxResults int) (*port.ToolExecutionResult, error) {
	needle := strings.ToLower(query)
	hits := make([]map[string]any, 0, maxResults)
	err := e.walk(func(path string, info fs.FileInfo) error {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if len(hits) >= maxResults {
			return nil
		}
		if info.IsDir() || !matchAnyExt(path, includes) || info.Size() > e.maxFileBytes {
			return nil
		}
		file, err := os.Open(path)
		if err != nil {
			return nil
		}
		defer file.Close()
		scanner := bufio.NewScanner(file)
		scanner.Buffer(make([]byte, 0, 64*1024), 256*1024)
		lineNo := 0
		for scanner.Scan() {
			lineNo++
			if len(hits) >= maxResults {
				break
			}
			if strings.Contains(strings.ToLower(scanner.Text()), needle) {
				hits = append(hits, map[string]any{
					"file": relPath(e.roots, path), "line": lineNo, "text": scanner.Text(),
				})
			}
		}
		return nil
	})
	if err != nil && err != context.Canceled {
		return nil, fmt.Errorf("repo_query: grep: %w", err)
	}
	out := map[string]any{"matches": hits, "truncated": len(hits) == maxResults}
	raw, _ := json.Marshal(out)
	return &port.ToolExecutionResult{Output: string(raw), Structured: out}, nil
}

// files returns matching file paths under the roots.
func (e *RepoQueryToolExecutor) files(ctx context.Context, pattern string, includes []string, maxResults int) (*port.ToolExecutionResult, error) {
	results := make([]string, 0, maxResults)
	err := e.walk(func(path string, info fs.FileInfo) error {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if len(results) >= maxResults {
			return nil
		}
		if info.IsDir() {
			return nil
		}
		if !matchAnyExt(path, includes) {
			return nil
		}
		matched := false
		if pattern == "" {
			matched = true
		} else if ok, _ := filepath.Match(pattern, filepath.Base(path)); ok {
			matched = true
		} else if strings.Contains(filepath.Base(path), pattern) {
			matched = true
		}
		if matched {
			results = append(results, relPath(e.roots, path))
		}
		return nil
	})
	if err != nil && err != context.Canceled {
		return nil, fmt.Errorf("repo_query: files: %w", err)
	}
	out := map[string]any{"files": results, "truncated": len(results) == maxResults}
	raw, _ := json.Marshal(out)
	return &port.ToolExecutionResult{Output: string(raw), Structured: out}, nil
}

// walk visits files under every root, skipping symlinked paths that escape
// the root so traversal cannot read outside the allow-list.
func (e *RepoQueryToolExecutor) walk(fn func(path string, info fs.FileInfo) error) error {
	for _, root := range e.roots {
		err := filepath.Walk(root, func(path string, info fs.FileInfo, err error) error {
			if err != nil {
				return nil
			}
			if info.Mode()&os.ModeSymlink != 0 {
				return filepath.SkipDir
			}
			return fn(path, info)
		})
		if err != nil {
			return err
		}
	}
	return nil
}

func matchAnyExt(path string, includes []string) bool {
	for _, pattern := range includes {
		if ok, _ := filepath.Match(pattern, filepath.Base(path)); ok {
			return true
		}
		if strings.HasPrefix(pattern, "*.") && strings.HasSuffix(strings.ToLower(path), strings.ToLower(pattern[1:])) {
			return true
		}
	}
	return false
}

func relPath(roots []string, path string) string {
	for _, root := range roots {
		if rel, err := filepath.Rel(root, path); err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			return rel
		}
	}
	return path
}

var _ port.ToolExecutorPort = (*RepoQueryToolExecutor)(nil)
