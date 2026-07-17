package server

import (
	"context"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/cloudwego/hertz/pkg/protocol/consts"
)

// openAPISpec is a minimal OpenAPI 3.0 spec for MAGI's v1 API.
// It documents all 15 endpoints. For full SDK generation, use a tool
// like swaggo/swag with Hertz HTTP Adaptor (§14).
const openAPISpec = `{
  "openapi": "3.0.0",
  "info": {"title": "MAGI Decision Engine API", "version": "2.0.0"},
  "paths": {
    "/health": {"get": {"summary": "Health check", "responses": {"200": {"description": "ok"}}}},
    "/ready": {"get": {"summary": "Readiness check", "responses": {"200": {"description": "ready"}}}},
    "/version": {"get": {"summary": "Version info", "responses": {"200": {"description": "version"}}}},
    "/api/v1/cases": {
      "post": {"summary": "Create a decision case", "responses": {"201": {"description": "created"}}},
      "get": {"summary": "List all cases", "responses": {"200": {"description": "list"}}}
    },
    "/api/v1/cases/{id}": {"get": {"summary": "Get case details", "responses": {"200": {"description": "case"}}}},
    "/api/v1/cases/{id}/run": {"post": {"summary": "Run a decision case", "responses": {"200": {"description": "resolution"}}}},
    "/api/v1/cases/{id}/cancel": {"post": {"summary": "Cancel a case", "responses": {"200": {"description": "cancelled"}}}},
    "/api/v1/cases/{id}/report": {"get": {"summary": "Get final report", "responses": {"200": {"description": "report"}}}},
    "/api/v1/cases/{id}/events": {"get": {"summary": "Replay events", "responses": {"200": {"description": "events"}}}},
    "/api/v1/cases/{id}/timeline": {"get": {"summary": "Event timeline", "responses": {"200": {"description": "timeline"}}}},
    "/api/v1/cases/{id}/trace": {"get": {"summary": "Case trace", "responses": {"200": {"description": "trace"}}}},
    "/api/v1/cases/{id}/stream": {"get": {"summary": "SSE stream", "responses": {"200": {"description": "stream"}}}},
    "/api/v1/memory/{id}": {"get": {"summary": "Get case memory", "responses": {"200": {"description": "memory"}}}},
    "/api/v1/evaluation": {"post": {"summary": "Evaluate a case", "responses": {"200": {"description": "evaluation"}}}},
    "/api/v1/benchmark": {"post": {"summary": "Benchmark", "responses": {"200": {"description": "benchmark"}}}},
    "/api/v1/tools": {"get": {"summary": "List tools", "responses": {"200": {"description": "tools"}}}},
    "/api/v1/tools/{name}": {"get": {"summary": "Get tool details", "responses": {"200": {"description": "tool"}}}}
  }
}`

// OpenAPIHandler serves the OpenAPI 3.0 spec at GET /openapi.json.
func OpenAPIHandler(ctx context.Context, c *app.RequestContext) {
	c.Data(consts.StatusOK, "application/json", []byte(openAPISpec))
}
