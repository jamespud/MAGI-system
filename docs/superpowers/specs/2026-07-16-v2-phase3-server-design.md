# MAGI v2 阶段 3：Server Layer + DTO + REST API

> 设计日期：2026-07-16
> 关联：magi.v2.md §六-七（Server Layer、HTTP API）、§十二-十三（DTO、错误体系）

## 目标

实现完整 REST API（14 个端点），DTO 隔离 Domain 与 HTTP，统一错误映射，RequestID 中间件。Handler 只调 Application Service，禁直接调 Domain。

## 范围

- `server/dto/`：6 个 DTO 类型
- `server/handler/`：6 个 handler 文件
- `server/router.go`：注册全部端点（替换 RegisterRoutes）
- `server/middleware.go`：RequestID + Recovery
- `server/error.go`：错误映射
- `bootstrap/container.go`：注入 Services 到 Handlers

暂不实现：GET /cases（列表）、GET /cases/{id}/trace、POST /benchmark（Phase 5）。

## 设计

### DTO

```go
// server/dto/dto.go
type CaseResponse struct { ID, Question, Status, FinalDecision string; Round int }
type CreateCaseRequest struct { Question string `json:"question"` }
type DecisionReport struct { Report string `json:"report"` }
type ReplayEvent struct { Type, AgentCode, RunID string; Timestamp time.Time }
type EvaluationResponse struct { /* entity.Evaluation 字段镜像 */ }
type ToolResponse struct { Name, Desc string }
type ErrorResponse struct { Error string `json:"error"` }
```

### Handler

每个 handler 文件接收 Application Service（通过构造函数注入），方法签名为 Hertz 标准 `func(ctx context.Context, c *app.RequestContext)`。

### Router

`RegisterRoutes(h *hzserver.Hertz, deps RouteDeps)` 注册 `/api/v1` 路由组 + `/health`、`/ready`、`/version`。`RouteDeps` 含所有 5 个 Application Service。

### 错误映射

`respondError(c, err)`：根据 error 字符串内容映射 HTTP 状态码（404 for "not found"/"not configured"，400 for parse errors，500 for others）。

### 中间件

RequestID：为每个请求生成 UUID，存入 context。Recovery：panic -> 500。

## 文件清单

- `server/dto/dto.go`（新建）
- `server/handler/decision.go`、`replay.go`、`evaluation.go`、`memory.go`、`tool.go`、`health.go`（新建）
- `server/router.go`（新建，替换 server.go 的 RegisterRoutes）
- `server/middleware.go`（新建）
- `server/error.go`（新建）
- `server/server.go`（修改：RegisterRoutes 改为调 router）
- `bootstrap/container.go`（修改：注入 Services 到 RouteDeps）
