# MAGI v2 阶段 4：SSE + 日志链路

> 设计日期：2026-07-16
> 关联：magi.v2.md §八（SSE）、§十五（日志）
> 研究：Hertz SSE writer 必须同步在 handler 内使用（deep research 发现）

## 目标

实现 `GET /api/v1/cases/:id/stream` SSE 端点，通过 EventBroker（channel-buffer 层）桥接异步 EventPublisher 与同步 Hertz SSE writer。增加请求日志中间件。

## 设计

### EventBroker

实现 `port.EventPublisher` + `port.EventRepository` 双接口，替换 bootstrap 中的 InMemoryEventRepo + EventPublisherAdapter。

```go
// server/broker.go
type EventBroker struct {
    mu          sync.Mutex
    subscribers map[string][]chan *entity.MagiEvent
    stored      map[string][]*entity.MagiEvent  // 持久化（内存）
}
```

- `Publish(ctx, e)`：存入 stored + 转发到该 caseID 的所有订阅者 channel（非阻塞）
- `Subscribe(caseID) <-chan *entity.MagiEvent`：创建 buffered channel，加入订阅者列表
- `Unsubscribe(caseID, ch)`：关闭并移除 channel
- `ListByCase(ctx, caseID)`：从 stored 返回（满足 EventRepository）
- `Create(ctx, e)`：存入 stored（满足 EventRepository，与 Publish 共用）

### SSE Handler

`GET /api/v1/cases/:id/stream`：
1. `broker.Subscribe(caseID)` 获取 channel
2. `sse.NewWriter(c)` 创建 Hertz SSE writer
3. `select { case ev := <-ch: writer.WriteEvent(id, type, data) ; case <-ctx.Done(): break }`
4. defer `broker.Unsubscribe(caseID, ch)`

### 日志中间件

`server/middleware.go` 新增 `Logger()`：请求开始/结束日志含 RequestID + Method + Path + Status + Duration。CaseID/RunID/AgentID 通过 MagiEvent 自然贯穿（事件携带），无需额外 context 传递。

### Bootstrap 更新

- EventBroker 替换 InMemoryEventRepo + EventPublisherAdapter
- AgentLoop 的 EventPub = EventBroker（事件发布到 broker）
- ReplayService 的 EventRepository = EventBroker（从 stored 读取）
- SSE handler 注入 EventBroker
- Router 注册 `GET /cases/:id/stream`

## 文件清单

- `server/broker.go`（新建）：EventBroker
- `server/broker_test.go`（新建）：broker 测试
- `server/sse.go`（新建）：SSE handler
- `server/middleware.go`（修改）：新增 Logger
- `server/router.go`（修改）：注册 stream 端点 + 注入 broker
- `bootstrap/container.go`（修改）：EventBroker 替换旧 adapter
