package magi_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	magi "github.com/jamespud/magi/backend/adapter"
	"github.com/jamespud/magi/backend/domain/port"
)

func TestWebFetchTool_FetchesAllowedHostAndStripsHTML(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte("<html><body><h1>Hello</h1><p>MAGI is <b>great</b>.</p></body></html>"))
	}))
	defer srv.Close()

	exec, err := magi.NewWebFetchToolExecutor(magi.WebFetchToolConfig{
		Enabled: true, AllowedDomains: []string{"127.0.0.1"}, MaxBytes: 4096, TimeoutSeconds: 5,
	})
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	res, err := exec.Execute(context.Background(), port.ToolExecutionRequest{
		ToolName: magi.WebFetchToolName, ArgumentsJSON: `{"url":"` + srv.URL + `/page"}`,
	})
	if err != nil {
		t.Fatalf("fetch: %v", err)
	}
	var out struct {
		Content string `json:"content"`
	}
	if err := json.Unmarshal([]byte(res.Output), &out); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !strings.Contains(out.Content, "Hello") || !strings.Contains(out.Content, "MAGI is great") ||
		strings.Contains(out.Content, "<b>") {
		t.Fatalf("content = %q", out.Content)
	}
}

func TestWebFetchTool_RejectsDisallowedHostAndScheme(t *testing.T) {
	exec, err := magi.NewWebFetchToolExecutor(magi.WebFetchToolConfig{
		Enabled: true, AllowedDomains: []string{"example.com"},
	})
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	if _, err := exec.Execute(context.Background(), port.ToolExecutionRequest{
		ToolName: magi.WebFetchToolName, ArgumentsJSON: `{"url":"https://evil.example/x"}`,
	}); err == nil || !strings.Contains(err.Error(), "not in the allowed domains") {
		t.Fatalf("expected domain rejection, got %v", err)
	}
	if _, err := exec.Execute(context.Background(), port.ToolExecutionRequest{
		ToolName: magi.WebFetchToolName, ArgumentsJSON: `{"url":"ftp://example.com/file"}`,
	}); err == nil || !strings.Contains(err.Error(), "only http/https") {
		t.Fatalf("expected scheme rejection, got %v", err)
	}
}

func TestWebFetchTool_RejectsNonTextAndOversized(t *testing.T) {
	binSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/octet-stream")
		_, _ = w.Write([]byte("data"))
	}))
	defer binSrv.Close()
	exec, err := magi.NewWebFetchToolExecutor(magi.WebFetchToolConfig{
		Enabled: true, AllowedDomains: []string{"127.0.0.1"},
	})
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	if _, err := exec.Execute(context.Background(), port.ToolExecutionRequest{
		ToolName: magi.WebFetchToolName, ArgumentsJSON: `{"url":"` + binSrv.URL + `"}`,
	}); err == nil || !strings.Contains(err.Error(), "unsupported content type") {
		t.Fatalf("expected content-type rejection, got %v", err)
	}

	bigSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		_, _ = w.Write([]byte(strings.Repeat("x", 2048)))
	}))
	defer bigSrv.Close()
	small, err := magi.NewWebFetchToolExecutor(magi.WebFetchToolConfig{
		Enabled: true, AllowedDomains: []string{"127.0.0.1"}, MaxBytes: 512,
	})
	if err != nil {
		t.Fatalf("build small: %v", err)
	}
	if _, err := small.Execute(context.Background(), port.ToolExecutionRequest{
		ToolName: magi.WebFetchToolName, ArgumentsJSON: `{"url":"` + bigSrv.URL + `"}`,
	}); err == nil || !strings.Contains(err.Error(), "exceeds 512 bytes") {
		t.Fatalf("expected size rejection, got %v", err)
	}
}

func TestWebFetchTool_RequiresEnabledAndDomains(t *testing.T) {
	if _, err := magi.NewWebFetchToolExecutor(magi.WebFetchToolConfig{Enabled: false}); err == nil {
		t.Fatal("expected disabled error")
	}
	if _, err := magi.NewWebFetchToolExecutor(magi.WebFetchToolConfig{Enabled: true}); err == nil {
		t.Fatal("expected missing domains error")
	}
}
