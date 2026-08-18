package magi_test

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	magi "github.com/jamespud/magi/backend/adapter"
	"github.com/jamespud/magi/backend/domain/port"
)

func TestRepoQueryTool_GrepFindsHits(t *testing.T) {
	root := t.TempDir()
	write := func(rel, content string) {
		t.Helper()
		path := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatalf("mkdir: %v", err)
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatalf("write: %v", err)
		}
	}
	write("main.go", "package main\nfunc f() { println(\"hello magi\") }\n")
	write("notes.md", "# notes\nnothing here\n")
	write("main.py", "print('hello magi again')\n")

	exec, err := magi.NewRepoQueryToolExecutor(magi.RepoToolConfig{Enabled: true, Roots: []string{root}})
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	res, err := exec.Execute(context.Background(), port.ToolExecutionRequest{
		ToolName: magi.RepoToolName, ArgumentsJSON: `{"action":"grep","query":"hello magi"}`,
	})
	if err != nil {
		t.Fatalf("grep: %v", err)
	}
	var out struct {
		Matches []map[string]any `json:"matches"`
	}
	if err := json.Unmarshal([]byte(res.Output), &out); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(out.Matches) != 2 {
		t.Fatalf("matches = %+v", out.Matches)
	}
	// notes.md is not in the default include set.
	for _, m := range out.Matches {
		file := m["file"].(string)
		if file == "notes.md" {
			t.Fatalf("non-included file must not be searched: %v", m)
		}
	}
}

func TestRepoQueryTool_GrepRespectsMaxResults(t *testing.T) {
	root := t.TempDir()
	for i := 0; i < 5; i++ {
		if err := os.WriteFile(filepath.Join(root, "f"+string(rune('a'+i))+".go"), []byte("hit\n"), 0o644); err != nil {
			t.Fatalf("write: %v", err)
		}
	}
	exec, err := magi.NewRepoQueryToolExecutor(magi.RepoToolConfig{Enabled: true, Roots: []string{root}, MaxResults: 2})
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	res, err := exec.Execute(context.Background(), port.ToolExecutionRequest{
		ToolName: magi.RepoToolName, ArgumentsJSON: `{"action":"grep","query":"hit"}`,
	})
	if err != nil {
		t.Fatalf("grep: %v", err)
	}
	var out struct {
		Matches   []map[string]any `json:"matches"`
		Truncated bool             `json:"truncated"`
	}
	if err := json.Unmarshal([]byte(res.Output), &out); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(out.Matches) != 2 || !out.Truncated {
		t.Fatalf("matches=%d truncated=%v", len(out.Matches), out.Truncated)
	}
}

func TestRepoQueryTool_FilesListsMatchingPaths(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "a.go"), []byte("x"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "b.ts"), []byte("x"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	exec, err := magi.NewRepoQueryToolExecutor(magi.RepoToolConfig{Enabled: true, Roots: []string{root}})
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	res, err := exec.Execute(context.Background(), port.ToolExecutionRequest{
		ToolName: magi.RepoToolName, ArgumentsJSON: `{"action":"files","query":"*.go"}`,
	})
	if err != nil {
		t.Fatalf("files: %v", err)
	}
	var out struct {
		Files []string `json:"files"`
	}
	if err := json.Unmarshal([]byte(res.Output), &out); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(out.Files) != 1 || out.Files[0] != "a.go" {
		t.Fatalf("files = %+v", out.Files)
	}
}

func TestRepoQueryTool_RequiresRoots(t *testing.T) {
	if _, err := magi.NewRepoQueryToolExecutor(magi.RepoToolConfig{Enabled: false}); err == nil {
		t.Fatal("expected disabled error")
	}
	if _, err := magi.NewRepoQueryToolExecutor(magi.RepoToolConfig{Enabled: true}); err == nil {
		t.Fatal("expected missing roots error")
	}
	if _, err := magi.NewRepoQueryToolExecutor(magi.RepoToolConfig{Enabled: true, Roots: []string{"/definitely/not/here"}}); err != nil {
		t.Fatalf("roots need not exist at build time: %v", err)
	}
}

func TestRepoQueryTool_RejectsInvalidAction(t *testing.T) {
	exec, err := magi.NewRepoQueryToolExecutor(magi.RepoToolConfig{Enabled: true, Roots: []string{t.TempDir()}})
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	_, err = exec.Execute(context.Background(), port.ToolExecutionRequest{
		ToolName: magi.RepoToolName, ArgumentsJSON: `{"action":"delete","query":"x"}`,
	})
	if err == nil || !strings.Contains(err.Error(), "action must be") {
		t.Fatalf("expected action error, got %v", err)
	}
}
