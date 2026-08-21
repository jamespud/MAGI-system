package magi

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/jamespud/magi/backend/domain/port"
)

// FileToolName is the built-in read-only file query tool.
const FileToolName = "file_query"

const (
	defaultFileMaxBytes = 256 * 1024
	defaultFileMaxItems = 100
)

// fileArgsSchema is the JSON Schema for file_query arguments.
const fileArgsSchema = `{"type":"object","properties":{"path":{"type":"string"},"action":{"type":"string","enum":["read","list"]}},"required":["path","action"],"additionalProperties":false}`

// FileToolConfig bounds the read-only file tool to configured roots.
type FileToolConfig struct {
	Enabled      bool
	Roots        []string
	MaxFileBytes int64
	MaxListItems int
}

// FileToolExecutor reads or lists files inside configured allow-listed roots.
// Paths are resolved and containment-checked so `..` traversal cannot escape
// the roots; reads are size-bounded and lists are item-bounded.
type FileToolExecutor struct {
	roots        []string
	maxFileBytes int64
	maxListItems int
}

// NewFileToolExecutor normalizes and validates the allow-listed roots.
func NewFileToolExecutor(cfg FileToolConfig) (port.ToolExecutorPort, error) {
	if !cfg.Enabled {
		return nil, fmt.Errorf("file_query tool is not enabled")
	}
	if len(cfg.Roots) == 0 {
		return nil, fmt.Errorf("file_query: at least one root is required")
	}
	roots := make([]string, 0, len(cfg.Roots))
	seen := map[string]bool{}
	for _, root := range cfg.Roots {
		abs, err := filepath.Abs(filepath.Clean(root))
		if err != nil {
			return nil, fmt.Errorf("file_query: resolve root %q: %w", root, err)
		}
		if !seen[abs] {
			seen[abs] = true
			roots = append(roots, abs)
		}
	}
	maxBytes := cfg.MaxFileBytes
	if maxBytes <= 0 {
		maxBytes = defaultFileMaxBytes
	}
	maxItems := cfg.MaxListItems
	if maxItems <= 0 {
		maxItems = defaultFileMaxItems
	}
	return &FileToolExecutor{roots: roots, maxFileBytes: maxBytes, maxListItems: maxItems}, nil
}

func (e *FileToolExecutor) Execute(ctx context.Context, req port.ToolExecutionRequest) (*port.ToolExecutionResult, error) {
	var args struct {
		Path   string `json:"path"`
		Action string `json:"action"`
	}
	if err := json.Unmarshal([]byte(req.ArgumentsJSON), &args); err != nil {
		return nil, fmt.Errorf("file_query: parse args: %w", err)
	}
	if strings.TrimSpace(args.Path) == "" {
		return nil, fmt.Errorf("file_query: path is required")
	}
	resolved, err := e.resolve(args.Path)
	if err != nil {
		return nil, err
	}
	switch args.Action {
	case "read":
		return e.read(ctx, resolved)
	case "list":
		return e.list(ctx, resolved)
	default:
		return nil, fmt.Errorf("file_query: action must be \"read\" or \"list\"")
	}
}

// resolve returns the absolute, containment-checked path for the request.
func (e *FileToolExecutor) resolve(path string) (string, error) {
	return resolveInRoots(e.roots, path)
}

func resolveInRoots(roots []string, path string) (string, error) {
	var candidates []string
	if filepath.IsAbs(path) {
		candidates = []string{filepath.Clean(path)}
	} else {
		for _, root := range roots {
			candidates = append(candidates, filepath.Join(root, path))
		}
	}
	for _, candidate := range candidates {
		real, err := filepath.EvalSymlinks(candidate)
		if err != nil {
			continue
		}
		for _, root := range roots {
			if withinRoot(root, real) {
				return real, nil
			}
		}
	}
	return "", fmt.Errorf("path %q is outside the configured roots", path)
}

func withinRoot(root, candidate string) bool {
	rel, err := filepath.Rel(root, candidate)
	if err != nil {
		return false
	}
	return rel == "." || (rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)))
}

func (e *FileToolExecutor) read(ctx context.Context, path string) (*port.ToolExecutionResult, error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, fmt.Errorf("file_query: stat %s: %w", path, err)
	}
	if info.IsDir() {
		return nil, fmt.Errorf("file_query: %s is a directory; use action=list", path)
	}
	if info.Size() > e.maxFileBytes {
		return nil, fmt.Errorf("file_query: %s exceeds %d bytes", path, e.maxFileBytes)
	}
	content, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("file_query: read %s: %w", path, err)
	}
	out := map[string]any{"path": path, "content": string(content)}
	raw, _ := json.Marshal(out)
	return &port.ToolExecutionResult{Output: string(raw), Structured: out}, nil
}

func (e *FileToolExecutor) list(ctx context.Context, path string) (*port.ToolExecutionResult, error) {
	entries, err := os.ReadDir(path)
	if err != nil {
		return nil, fmt.Errorf("file_query: list %s: %w", path, err)
	}
	items := make([]map[string]any, 0, e.maxListItems)
	for _, entry := range entries {
		if len(items) >= e.maxListItems {
			break
		}
		info, ierr := entry.Info()
		size := int64(0)
		if ierr == nil {
			size = info.Size()
		}
		items = append(items, map[string]any{
			"name": entry.Name(), "is_dir": entry.IsDir(), "size": size,
		})
	}
	out := map[string]any{"path": path, "entries": items, "truncated": len(entries) > len(items)}
	raw, _ := json.Marshal(out)
	return &port.ToolExecutionResult{Output: string(raw), Structured: out}, nil
}

var _ port.ToolExecutorPort = (*FileToolExecutor)(nil)
