// Package a2aerror writes A2A-compatible google.rpc.Status error responses.
// It mirrors the REST error envelope produced by a2a-go/v2's internal
// `internal/rest` package, which is under Go's `internal/` import boundary and
// therefore is not importable from MAGI's server middleware. Both the parent
// `server` package (Auth, RateLimit) and `server/a2a` (request body limit)
// import this so pre-handler rejections keep the same protocol shape the A2A
// SDK client expects.
package a2aerror

import (
	"encoding/json"
	"strings"
	"time"

	"github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/cloudwego/hertz/pkg/app"
)

// ErrorInfo mirrors google.rpc.ErrorInfo in the protocol error details.
type ErrorInfo struct {
	Type     string            `json:"@type"`
	Reason   string            `json:"reason"`
	Domain   string            `json:"domain"`
	Metadata map[string]string `json:"metadata,omitempty"`
}

// StatusError is the inner error object in a google.rpc.Status response.
type StatusError struct {
	Code    int         `json:"code"`
	Status  string      `json:"status"`
	Message string      `json:"message"`
	Details []ErrorInfo `json:"details"`
}

// Envelope is the outer body: {"error": {...}} per AIP-193.
type Envelope struct {
	Error StatusError `json:"error"`
}

// IsProtocolPath reports whether path is on the A2A surface (the fixed /a2a
// base path and its descendants). The well-known Agent Card discovery path is
// public and is never authenticated, but it is still an A2A surface; it is
// matched separately by the caller which leaves it unauthenticated.
func IsProtocolPath(path string) bool {
	return path == "/a2a" || strings.HasPrefix(path, "/a2a/")
}

// Write emits a protocol-shaped REST error on the given Hertz context.
func Write(c *app.RequestContext, httpStatus int, grpcStatus, reason, message string) {
	body, _ := json.Marshal(Envelope{
		Error: StatusError{
			Code:    httpStatus,
			Status:  grpcStatus,
			Message: message,
			Details: []ErrorInfo{{
				Type:   "type.googleapis.com/google.rpc.ErrorInfo",
				Reason: reason,
				Domain: a2a.ProtocolDomain,
				Metadata: map[string]string{
					"timestamp": time.Now().UTC().Format(time.RFC3339),
				},
			}},
		},
	})
	c.Response.Header.Set("Content-Type", "application/json")
	c.Response.SetStatusCode(httpStatus)
	c.Response.SetBody(body)
}
