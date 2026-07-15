# MAGI v2 阶段 1：Bootstrap + 目录重构 + main.go 迁移

> 设计日期：2026-07-15
> 关联：magi.v2.md §二-四（目录重构、启动流程、Bootstrap）
> 研究：Uber Fx（DI，Wire 已归档）、Hertz（HTTP，已有 indirect dep）

## 目标

建立 v2 目录结构 + Uber Fx DI 容器 + cmd/magi-server 入口 + DecisionService 骨架 + 最小 Hertz /health 端点。删除 CLI main.go。Domain 不动。

## 范围

1. 新建 `cmd/magi-server/main.go`（仅 Load Config -> fx.New -> app.Run）
2. 新建 `bootstrap/config.go`（从 main.go 移入 Config/MagiSpec/loadConfig/toConfig）
3. 新建 `bootstrap/container.go`（fx.Provide 所有依赖 + fx.Invoke 注册路由）
4. 新建 `application/decision/service.go`（DecisionService: Create/Run/Get）
5. 新建 `server/server.go`（Hertz 最小 server + GET /health）
6. 删除 `backend/main.go`
7. 更新 Makefile（新 build target）
8. 添加 `go.uber.org/fx` + 促进 `cloudwego/hertz` 为 direct dep

## 设计

### bootstrap/config.go
从 main.go 移入全部 config structs + loadConfig + toConfig。无业务逻辑。

### bootstrap/container.go
Uber Fx DI 容器。fx.Provide 注册：
- Config（从 config.go 加载）
- validation.Validator + SchemaGenerator
- adapter.ModelAdapter + EventPublisherAdapter + InMemoryEventRepo
- stubToolRegistry + stubToolExecutor（standalone 模式；Coze 模式后续替换）
- runtime.AgentLoop（AgentLoopDeps 注入）
- service.Commander
- []*entity.MagiConfig（3 个 Magi 从 Config 构建）
- orchestration.Orchestrator（OrchestratorDeps 注入）
- application/decision.Service
- server.Server（Hertz）

fx.Invoke 注册 Hertz 路由。

### application/decision/service.go
```go
type Service struct {
    orch *orchestration.Orchestrator
    cfg  *bootstrap.Config
}
func NewService(orch *orchestration.Orchestrator, cfg *bootstrap.Config) *Service
func (s *Service) Create(ctx, question string) (*entity.DecisionCase, error)
func (s *Service) Run(ctx, caseID string) (*entity.Resolution, error)
```
从 main.go 的 main() 提取 case 创建 + Orchestrate 调用。

### server/server.go
最小 Hertz server：GET /health -> {"status":"ok"}。Phase 3 扩展完整 API。

### cmd/magi-server/main.go
```go
func main() {
    cfg := bootstrap.LoadConfig()
    app := fx.New(
        fx.Provide(func() *bootstrap.Config { return cfg }),
        bootstrap.Module,
    )
    app.Run()
}
```

### main.go 删除
旧 CLI main.go 删除。Makefile `server` target 改为 `cd backend && go build -o ../bin/magi-server ./cmd/magi-server`。

## 限制

- stubToolRegistry/Executor 保留（standalone 模式；Coze 集成在后续阶段）
- 无 HTTP API（仅 /health）；完整 API 在 Phase 3
- 无 SSE；Phase 4
- DecisionService.Run 同步执行（异步在 Phase 3）

## 测试

- bootstrap: config 加载测试
- application/decision: Service.Create + Run 测试（mock orchestrator）
- server: /health 端点测试
- 集成: cmd/magi-server 编译 + 启动 + /health 200

## 文件清单

- `cmd/magi-server/main.go`（新建）
- `bootstrap/config.go`（新建，从 main.go 移入）
- `bootstrap/container.go`（新建）
- `application/decision/service.go`（新建）
- `server/server.go`（新建）
- `backend/main.go`（删除）
- `Makefile`（更新）
- `backend/go.mod`（添加 fx + hertz direct）
