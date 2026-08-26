package a2aerror

import (
	"encoding/json"
	"testing"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/cloudwego/hertz/pkg/protocol"
)

func TestWriteProducesProtocolEnvelope(t *testing.T) {
	ctx := &app.RequestContext{}
	ctx.Request = protocol.Request{}
	ctx.Response = protocol.Response{}
	Write(ctx, 401, "UNAUTHENTICATED", "UNAUTHENTICATED", "unauthorized")

	if got := string(ctx.Response.Header.ContentType()); got != "application/json" {
		t.Fatalf("content-type = %q, want application/json", got)
	}
	if got := ctx.Response.StatusCode(); got != 401 {
		t.Fatalf("status = %d, want 401", got)
	}
	var parsed struct {
		Error struct {
			Code    int         `json:"code"`
			Status  string      `json:"status"`
			Message string      `json:"message"`
			Details []ErrorInfo `json:"details"`
		} `json:"error"`
	}
	if err := json.Unmarshal(ctx.Response.Body(), &parsed); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if parsed.Error.Code != 401 || parsed.Error.Status != "UNAUTHENTICATED" || parsed.Error.Message != "unauthorized" {
		t.Fatalf("error = %+v", parsed.Error)
	}
	if len(parsed.Error.Details) != 1 {
		t.Fatalf("details = %+v, want exactly one ErrorInfo", parsed.Error.Details)
	}
	info := parsed.Error.Details[0]
	if info.Type != "type.googleapis.com/google.rpc.ErrorInfo" ||
		info.Reason != "UNAUTHENTICATED" ||
		info.Domain != "a2a-protocol.org" {
		t.Fatalf("error info = %+v", info)
	}
}

func TestIsProtocolPath(t *testing.T) {
	cases := map[string]bool{
		"/a2a":                 true,
		"/a2a/message:send":    true,
		"/a2a/tasks/t1:cancel": true,
		"/api/v1/decision":     false,
		"/health":              false,
	}
	for path, want := range cases {
		if got := IsProtocolPath(path); got != want {
			t.Errorf("IsProtocolPath(%q) = %v, want %v", path, got, want)
		}
	}
}

func TestWriteFor413UsesINVALIDARGUMENT(t *testing.T) {
	ctx := &app.RequestContext{}
	ctx.Request = protocol.Request{}
	ctx.Response = protocol.Response{}
	Write(ctx, 413, "INVALID_ARGUMENT", "INVALID_ARGUMENT", "request body too large")
	if got := ctx.Response.StatusCode(); got != 413 {
		t.Fatalf("status = %d, want 413", got)
	}
	var parsed struct {
		Error struct {
			Code    int         `json:"code"`
			Status  string      `json:"status"`
			Message string      `json:"message"`
			Details []ErrorInfo `json:"details"`
		} `json:"error"`
	}
	if err := json.Unmarshal(ctx.Response.Body(), &parsed); err != nil {
		t.Fatalf("decode 413: %v", err)
	}
	if parsed.Error.Code != 413 || parsed.Error.Status != "INVALID_ARGUMENT" || parsed.Error.Message != "request body too large" {
		t.Fatalf("413 error = %+v", parsed.Error)
	}
	if len(parsed.Error.Details) != 1 || parsed.Error.Details[0].Reason != "INVALID_ARGUMENT" {
		t.Fatalf("413 details = %+v", parsed.Error.Details)
	}
}
