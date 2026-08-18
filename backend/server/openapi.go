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
    "/api/v1/assistant": {"post": {"summary": "Ask MAGI: run a decision from a message", "responses": {"200": {"description": "decision"}}}},
    "/api/v1/conversations": {"get": {"summary": "List my conversation threads", "responses": {"200": {"description": "conversations"}}}},
    "/api/v1/conversations/{conversationID}": {
      "get": {"summary": "Get a conversation with messages", "responses": {"200": {"description": "conversation"}}},
      "delete": {"summary": "Delete a conversation thread (cases remain)", "responses": {"204": {"description": "deleted"}}}
    },
    "/api/v1/cases": {
      "post": {"summary": "Create a decision case", "responses": {"201": {"description": "created"}}},
      "get": {"summary": "List all cases", "responses": {"200": {"description": "list"}}}
    },
    "/api/v1/cases/{id}": {"get": {"summary": "Get case details", "responses": {"200": {"description": "case"}}}},
    "/api/v1/cases/{id}/run": {"post": {"summary": "Run a decision case", "responses": {"200": {"description": "resolution"}}}},
    "/api/v1/cases/{id}/cancel": {"post": {"summary": "Cancel a case", "responses": {"200": {"description": "cancelled"}}}},
    "/api/v1/cases/{id}/pause": {"post": {"summary": "Pause (hibernate) a running case", "responses": {"200": {"description": "paused"}}}},
    "/api/v1/cases/{id}/resume": {"post": {"summary": "Resume (wake) a paused case", "responses": {"200": {"description": "resumed"}}}},
    "/api/v1/cases/{id}/report": {"get": {"summary": "Get final report", "responses": {"200": {"description": "report"}}}},
    "/api/v1/cases/{id}/events": {"get": {"summary": "Replay events", "responses": {"200": {"description": "events"}}}},
    "/api/v1/cases/{id}/timeline": {"get": {"summary": "Event timeline", "responses": {"200": {"description": "timeline"}}}},
    "/api/v1/cases/{id}/trace": {"get": {"summary": "Case trace", "responses": {"200": {"description": "trace"}}}},
    "/api/v1/cases/{id}/stream": {"get": {"summary": "SSE stream", "responses": {"200": {"description": "stream"}}}},
    "/api/v1/memory": {"get": {"summary": "Search case memory", "responses": {"200": {"description": "memories"}}}},
    "/api/v1/memory/glm-5.3_common": {
      "get": {"summary": "Get case memory", "responses": {"200": {"description": "memory"}}},
      "patch": {"summary": "Edit, annotate, or tag case memory", "responses": {"200": {"description": "memory"}}},
      "delete": {"summary": "Delete case memory and its RAG chunks", "responses": {"204": {"description": "deleted"}}}
    },
    "/api/v1/evaluation": {"post": {"summary": "Evaluate a case (case_id in body/query)", "responses": {"200": {"description": "evaluation"}}}},
    "/api/v1/evaluation/{id}": {"post": {"summary": "Evaluate a case by ID", "responses": {"200": {"description": "evaluation"}}}},
    "/api/v1/evaluation/{id}/judge": {"post": {"summary": "Run LLM-as-a-Judge on a case", "responses": {"200": {"description": "judge"}}}},
    "/metrics": {"get": {"summary": "Prometheus metrics", "responses": {"200": {"description": "metrics"}}}},
    "/api/v1/benchmark": {"post": {"summary": "Benchmark", "responses": {"200": {"description": "benchmark"}}}},
    "/api/v1/datasets": {
      "post": {"summary": "Create a ground-truth dataset", "responses": {"201": {"description": "created"}}},
      "get": {"summary": "List datasets", "responses": {"200": {"description": "list"}}}
    },
    "/api/v1/datasets/{id}": {"get": {"summary": "Get dataset", "responses": {"200": {"description": "dataset"}}}},
    "/api/v1/datasets/{id}/items": {
      "post": {"summary": "Add ground-truth items to a dataset", "responses": {"200": {"description": "added"}}},
      "get": {"summary": "List dataset items", "responses": {"200": {"description": "items"}}}
    },
    "/api/v1/datasets/{id}/runs": {
      "post": {"summary": "Run a dataset benchmark", "responses": {"202": {"description": "run"}}},
      "get": {"summary": "List dataset runs", "responses": {"200": {"description": "runs"}}}
    },
    "/api/v1/benchmarks/{runID}": {"get": {"summary": "Get benchmark run detail", "responses": {"200": {"description": "detail"}}}},
    "/api/v1/benchmarks/{runID}/results/{resultID}": {"patch": {"summary": "Record user feedback on a benchmark result", "responses": {"200": {"description": "updated"}}}},
    "/api/v1/plugins": {
      "get": {"summary": "List my plugin bindings", "responses": {"200": {"description": "bindings"}}},
      "post": {"summary": "Create a plugin binding", "responses": {"201": {"description": "created"}}}
    },
    "/api/v1/plugins/{id}": {
      "patch": {"summary": "Enable/disable a plugin binding", "responses": {"200": {"description": "updated"}}},
      "delete": {"summary": "Delete a plugin binding", "responses": {"204": {"description": "deleted"}}}
    },
    "/api/v1/recurring": {
      "get": {"summary": "List my recurring decision templates", "responses": {"200": {"description": "templates"}}},
      "post": {"summary": "Create a recurring decision template", "responses": {"201": {"description": "created"}}}
    },
    "/api/v1/recurring/{id}": {
      "get": {"summary": "Get recurring template", "responses": {"200": {"description": "template"}}},
      "patch": {"summary": "Enable/disable recurring template", "responses": {"200": {"description": "updated"}}},
      "delete": {"summary": "Delete recurring template", "responses": {"204": {"description": "deleted"}}}
    },
    "/api/v1/recurring/{id}/run": {"post": {"summary": "Trigger a recurring run now", "responses": {"202": {"description": "started"}}}},
    "/api/v1/admin/usage": {"get": {"summary": "Admin usage aggregate (admin only)", "responses": {"200": {"description": "usage"}}}},
    "/api/v1/tools": {"get": {"summary": "List tools", "responses": {"200": {"description": "tools"}}}},
    "/api/v1/tools/{name}": {"get": {"summary": "Get tool details", "responses": {"200": {"description": "tool"}}}},
    "/api/v1/approvals": {"get": {"summary": "List tool approval requests", "responses": {"200": {"description": "approvals"}}}},
    "/api/v1/approvals/{id}": {"get": {"summary": "Get approval request", "responses": {"200": {"description": "approval"}}}},
    "/api/v1/approvals/{id}/approve": {"post": {"summary": "Approve a tool request", "responses": {"200": {"description": "approval"}}}},
    "/api/v1/approvals/{id}/reject": {"post": {"summary": "Reject a tool request", "responses": {"200": {"description": "approval"}}}}
  }
}`

// OpenAPIHandler serves the OpenAPI 3.0 spec at GET /openapi.json.
func OpenAPIHandler(ctx context.Context, c *app.RequestContext) {
	c.Data(consts.StatusOK, "application/json", []byte(openAPISpec))
}
