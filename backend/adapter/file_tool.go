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

// FileToolName is the built-in file query tool. Reads are always allowed within
// the configured roots; every mutation (write/append/delete/mkdir) is opt-in
// and disabled by default.
const FileToolName = "file_query"

const (
	defaultFileMaxBytes = 256 * 1024
	defaultFileMaxItems = 100
)

// fileArgsSchema is the JSON Schema for file_query arguments.
const fileArgsSchema = `{"type":"object","properties":{"path":{"type":"string"},"action":{"type":"string","enum":["read","list","write","append","delete","mkdir"]},"content":{"type":"string"}},"required":["path","action"],"additionalProperties":false}`

// FileToolConfig bounds the file tool to configured roots. Read and list are
// always available; mutations are gated individually and default to off so an
// operator enabling "file query" does not silently grant write access.
type FileToolConfig struct {
	Enabled      bool
	Roots        []string
	MaxFileBytes int64
	MaxListItems int
	AllowWrite   bool
	AllowAppend  bool
	AllowDelete  bool
	AllowMkdir   bool
}

// FileToolExecutor reads, lists, or (when explicitly enabled) mutates files
// inside configured allow-listed roots. Paths are resolved and
// containment-checked so `..` traversal cannot escape the roots; reads are
// size-bounded and lists are item-bounded.
type FileToolExecutor struct {
	roots        []string
	maxFileBytes int64
	maxListItems int
	allowWrite   bool
	allowAppend  bool
	allowDelete  bool
	allowMkdir   bool
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
	return &FileToolExecutor{
		roots:        roots,
		maxFileBytes: maxBytes,
		maxListItems: maxItems,
		allowWrite:   cfg.AllowWrite,
		allowAppend:  cfg.AllowAppend,
		allowDelete:  cfg.AllowDelete,
		allowMkdir:   cfg.AllowMkdir,
	}, nil
}

func (e *FileToolExecutor) Execute(ctx context.Context, req port.ToolExecutionRequest) (*port.ToolExecutionResult, error) {
	var args struct {
		Path    string `json:"path"`
		Action  string `json:"action"`
		Content string `json:"content"`
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
	case "write":
		if !e.allowWrite {
			return nil, fmt.Errorf("file_query: write is disabled (allow_write=false)")
		}
		return e.write(ctx, resolved, args.Content)
	case "append":
		if !e.allowAppend {
			return nil, fmt.Errorf("file_query: append is disabled (allow_append=false)")
		}
		return e.append(ctx, resolved, args.Content)
	case "delete":
		if !e.allowDelete {
			return nil, fmt.Errorf("file_query: delete is disabled (allow_delete=false)")
		}
		return e.delete(ctx, resolved)
	case "mkdir":
		if !e.allowMkdir {
			return nil, fmt.Errorf("file_query: mkdir is disabled (allow_mkdir=false)")
		}
		return e.mkdir(ctx, resolved)
	default:
		return nil, fmt.Errorf("file_query: unknown action %q", args.Action)
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
		// Resolve symlinks on the deepest existing ancestor and re-append the
		// not-yet-existing tail, so mkdir/write to nested new paths are still
		// containment-checked (EvalSymlinks alone fails on a missing path).
		real, ok := resolveDeepestExisting(candidate)
		if !ok {
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

// resolveDeepestExisting resolves symlinks on the deepest existing ancestor of
// candidate and re-joins the remaining (non-existent) path segments. The
// returned path is fully symlink-resolved on its existing prefix, so a
// containment check against it cannot be fooled by a symlinked ancestor.
func resolveDeepestExisting(candidate string) (string, bool) {
	var tail []string
	current := filepath.Clean(candidate)
	for {
		real, err := filepath.EvalSymlinks(current)
		if err == nil {
			if len(tail) == 0 {
				return real, true
			}
			parts := make([]string, 0, len(tail)+1)
			parts = append(parts, real)
			for i := len(tail) - 1; i >= 0; i-- {
				parts = append(parts, tail[i])
			}
			return filepath.Join(parts...), true
		}
		parent := filepath.Dir(current)
		if parent == current {
			return "", false
		}
		tail = append(tail, filepath.Base(current))
		current = parent
	}
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

func (e *FileToolExecutor) write(ctx context.Context, path, content string) (*port.ToolExecutionResult, error) {
	if len([]byte(content)) > int(e.maxFileBytes) {
		return nil, fmt.Errorf("file_query: write exceeds %d bytes", e.maxFileBytes)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		return nil, fmt.Errorf("file_query: write %s: %w", path, err)
	}
	out := map[string]any{"path": path, "written": len([]byte(content))}
	raw, _ := json.Marshal(out)
	return &port.ToolExecutionResult{Output: string(raw), Structured: out}, nil
}

func (e *FileToolExecutor) append(ctx context.Context, path, content string) (*port.ToolExecutionResult, error) {
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return nil, fmt.Errorf("file_query: open %s: %w", path, err)
	}
	defer f.Close()
	if info, err := f.Stat(); err == nil && info.Size()+int64(len([]byte(content))) > e.maxFileBytes {
		return nil, fmt.Errorf("file_query: append exceeds %d bytes", e.maxFileBytes)
	}
	n, err := f.WriteString(content)
	if err != nil {
		return nil, fmt.Errorf("file_query: append %s: %w", path, err)
	}
	out := map[string]any{"path": path, "appended": n}
	raw, _ := json.Marshal(out)
	return &port.ToolExecutionResult{Output: string(raw), Structured: out}, nil
}

func (e *FileToolExecutor) delete(ctx context.Context, path string) (*port.ToolExecutionResult, error) {
	if err := os.Remove(path); err != nil {
		return nil, fmt.Errorf("file_query: delete %s: %w", path, err)
	}
	out := map[string]any{"path": path, "deleted": true}
	raw, _ := json.Marshal(out)
	return &port.ToolExecutionResult{Output: string(raw), Structured: out}, nil
}

func (e *FileToolExecutor) mkdir(ctx context.Context, path string) (*port.ToolExecutionResult, error) {
	if err := os.MkdirAll(path, 0o755); err != nil {
		return nil, fmt.Errorf("file_query: mkdir %s: %w", path, err)
	}
	out := map[string]any{"path": path, "created": true}
	raw, _ := json.Marshal(out)
	return &port.ToolExecutionResult{Output: string(raw), Structured: out}, nil
}

var _ port.ToolExecutorPort = (*FileToolExecutor)(nil)
