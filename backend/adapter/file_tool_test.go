package magi_test

import (
	"context"
	"fmt"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	magi "github.com/jamespud/magi/backend/adapter"
	"github.com/jamespud/magi/backend/domain/port"
)

func TestFileTool_ReadAndListWithinRoot(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "note.txt"), []byte("hello magi"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	if err := os.Mkdir(filepath.Join(root, "sub"), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	exec, err := magi.NewFileToolExecutor(magi.FileToolConfig{Enabled: true, Roots: []string{root}})
	if err != nil {
		t.Fatalf("build: %v", err)
	}

	res, err := exec.Execute(context.Background(), port.ToolExecutionRequest{
		ToolName: magi.FileToolName, ArgumentsJSON: `{"path":"note.txt","action":"read"}`,
	})
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	var out struct {
		Content string `json:"content"`
	}
	if err := json.Unmarshal([]byte(res.Output), &out); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if out.Content != "hello magi" {
		t.Fatalf("content = %q", out.Content)
	}

	res, err = exec.Execute(context.Background(), port.ToolExecutionRequest{
		ToolName: magi.FileToolName, ArgumentsJSON: `{"path":".","action":"list"}`,
	})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	var listing struct {
		Entries []map[string]any `json:"entries"`
	}
	if err := json.Unmarshal([]byte(res.Output), &listing); err != nil {
		t.Fatalf("decode list: %v", err)
	}
	if len(listing.Entries) != 2 {
		t.Fatalf("entries = %+v", listing.Entries)
	}
}

func TestFileTool_RejectsTraversal(t *testing.T) {
	root := t.TempDir()
	exec, err := magi.NewFileToolExecutor(magi.FileToolConfig{Enabled: true, Roots: []string{root}})
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	if _, err := exec.Execute(context.Background(), port.ToolExecutionRequest{
		ToolName: magi.FileToolName, ArgumentsJSON: `{"path":"../../etc/passwd","action":"read"}`,
	}); err == nil || !strings.Contains(err.Error(), "outside the configured roots") {
		t.Fatalf("traversal must be rejected, got %v", err)
	}
}

func TestFileTool_EnforcesSizeLimit(t *testing.T) {
	root := t.TempDir()
	big := filepath.Join(root, "big.bin")
	if err := os.WriteFile(big, make([]byte, 1024), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	exec, err := magi.NewFileToolExecutor(magi.FileToolConfig{Enabled: true, Roots: []string{root}, MaxFileBytes: 512})
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	if _, err := exec.Execute(context.Background(), port.ToolExecutionRequest{
		ToolName: magi.FileToolName, ArgumentsJSON: `{"path":"big.bin","action":"read"}`,
	}); err == nil || !strings.Contains(err.Error(), "exceeds 512 bytes") {
		t.Fatalf("size limit must be enforced, got %v", err)
	}
}

func TestFileTool_RequiresRoots(t *testing.T) {
	if _, err := magi.NewFileToolExecutor(magi.FileToolConfig{Enabled: false}); err == nil {
		t.Fatal("expected disabled error")
	}
	if _, err := magi.NewFileToolExecutor(magi.FileToolConfig{Enabled: true}); err == nil {
		t.Fatal("expected missing roots error")
	}
}

func TestFileTool_SymlinkTraversalBlocked(t *testing.T) {
	dir := t.TempDir()
	root := filepath.Join(dir, "root")
	outside := filepath.Join(dir, "outside")
	os.MkdirAll(root, 0755)
	os.MkdirAll(outside, 0755)
	secret := filepath.Join(outside, "secret.txt")
	os.WriteFile(secret, []byte("should-not-read"), 0644)
	link := filepath.Join(root, "escape")
	os.Symlink(outside, link)

	exec, err := magi.NewFileToolExecutor(magi.FileToolConfig{Enabled: true, Roots: []string{root}})
	if err != nil {
		t.Fatal(err)
	}
	args := fmt.Sprintf(`{"path":"%s","action":"read"}`, filepath.Join(root, "escape", "secret.txt"))
	_, err = exec.Execute(context.Background(), port.ToolExecutionRequest{ToolName: magi.FileToolName, ArgumentsJSON: args})
	if err == nil {
		t.Fatal("expected symlink traversal to be blocked")
	}
}
