我会把这次改造定义为 **MAGI v2：Server Architecture Migration**。

这不是把 CLI 换成 HTTP，而是让 MAGI 真正成为一个长期演进的平台。因此，整个改造目标只有一句话：

> **CLI 消失，Server 成为唯一入口；Coze 降级为基础设施；Domain 保持纯净；Application 成为新的编排层。**

---

# 一、总体架构

改造完成后的整体架构如下：

```text
                                Browser
                                   │
                            React / Mobile
                                   │
                      HTTP / SSE / WebSocket
                                   │
                    ┌─────────────────────────┐
                    │       MAGI Server       │
                    │      (Hertz / Coze)     │
                    └─────────────────────────┘
                                   │
                         Request / Response
                                   │
                    ┌─────────────────────────┐
                    │    Application Layer    │
                    └─────────────────────────┘
          DecisionService
          ReplayService
          MemoryService
          EvaluationService
          DatasetService
          ToolService
                                   │
                    ┌─────────────────────────┐
                    │      Domain Layer       │
                    └─────────────────────────┘
        Orchestrator
        Dispatcher
        Agent Runtime
        Debate
        Consensus
        Evidence
        Claim
        Validation
        Memory
                                   │
                    ┌─────────────────────────┐
                    │      Adapter Layer      │
                    └─────────────────────────┘
        Model Adapter
        Plugin Adapter
        Knowledge Adapter
        Repository Adapter
        Event Adapter
                                   │
                    ┌─────────────────────────┐
                    │ Coze Infrastructure     │
                    └─────────────────────────┘
```

整个过程中，**Domain 基本不修改**。

---

# 二、项目目录重构

建议彻底调整目录。

```text
backend/

├── cmd/
│
│   └── magi-server/
│       └── main.go
│
├── bootstrap/
│   ├── container.go
│   ├── provider.go
│   ├── config.go
│   └── wiring.go
│
├── server/
│   ├── router.go
│   ├── middleware.go
│   ├── server.go
│   ├── sse.go
│   ├── websocket.go
│   ├── error.go
│   │
│   ├── dto/
│   │
│   └── handler/
│       ├── decision.go
│       ├── replay.go
│       ├── evaluation.go
│       ├── memory.go
│       ├── tool.go
│       └── health.go
│
├── application/
│   ├── decision/
│   ├── replay/
│   ├── evaluation/
│   ├── dataset/
│   ├── memory/
│   └── tool/
│
├── adapter/
│
├── domain/
│
├── conf/
│
└── docker/
```

删除：

```text
backend/main.go
```

CLI 不再保留。

---

# 三、启动流程重构

新的 main 只允许存在启动逻辑。

```text
main

↓

Load Config

↓

Build Container

↓

Register HTTP

↓

Start Server
```

main 不允许：

* 创建 Case
* 调用 Orchestrator
* 输出 Report
* 执行 Agent

所有业务逻辑必须移出。

---

# 四、新增 Bootstrap

新增：

```text
bootstrap/
```

负责整个 Dependency Injection。

例如：

```text
Config

↓

Repository

↓

Adapter

↓

Runtime

↓

Dispatcher

↓

Orchestrator

↓

Application

↓

HTTP Handler

↓

Server
```

所有对象生命周期全部放在这里。

以后任何新增组件都只修改 bootstrap。

---

# 五、增加 Application Layer

这是整个改造最重要的一步。

目前：

```text
HTTP

↓

Orchestrator
```

这是错误的。

应该变成：

```text
HTTP

↓

Application

↓

Domain
```

Application 负责：

* 参数校验

* DTO 转换

* Case 生命周期

* 权限

* Repository 调用

* 调用 Orchestrator

* 结果聚合

* Event 管理

* Transaction

Domain 不允许关心这些。

---

建议新增：

```text
application/

decision/

replay/

evaluation/

memory/

dataset/

tool/
```

每个目录只有 Service。

例如：

DecisionService：

```go
Create()

Run()

Cancel()

Get()

List()

Report()
```

Replay：

```go
Replay()

Timeline()

Trace()
```

Evaluation：

```go
Evaluate()

Compare()

Benchmark()
```

以后：

CLI

HTTP

SDK

全部调用 Application。

---

# 六、Server Layer

Server 不直接访问 Domain。

只能：

```text
Handler

↓

Application
```

Handler 禁止：

```go
orchestrator.Run(...)
```

所有 Handler 都只能：

```go
decisionService.Run(...)
```

Server 负责：

HTTP

SSE

WebSocket

Middleware

Error

DTO

OpenAPI

Authentication

Logging

除此之外什么都不能做。

---

# 七、HTTP API

建议第一版 API。

## Decision

```text
POST /api/v1/cases
```

创建 Case。

---

```text
POST /api/v1/cases/{id}/run
```

执行。

---

```text
POST /api/v1/cases/{id}/cancel
```

取消。

---

```text
GET /api/v1/cases/{id}
```

详情。

---

```text
GET /api/v1/cases
```

列表。

---

```text
GET /api/v1/cases/{id}/report
```

最终报告。

---

## Replay

```text
GET /api/v1/cases/{id}/events
```

Replay。

---

```text
GET /api/v1/cases/{id}/timeline
```

Timeline。

---

```text
GET /api/v1/cases/{id}/trace
```

Trace。

---

## Memory

```text
GET /api/v1/memory/{id}
```

Memory。

---

## Evaluation

```text
POST /api/v1/evaluation
```

单次评估。

---

```text
POST /api/v1/benchmark
```

批量评估。

---

## Tool

```text
GET /api/v1/tools
```

工具列表。

---

```text
GET /api/v1/tools/{name}
```

工具详情。

---

## Health

```text
GET /health

GET /ready

GET /version
```

---

# 八、SSE

保留已有 EventPublisher。

增加：

```text
GET

/api/v1/cases/{id}/stream
```

流程：

```text
Runtime

↓

EventPublisher

↓

Server

↓

SSE

↓

Browser
```

Runtime 不允许知道：

SSE

HTTP

WebSocket

Browser

---

# 九、Coze 的定位

保留引用：

```text
infra/

pkg/

component/

plugin/

knowledge/

model/

logger/

config/

sse/

repository/
```

全部作为基础设施。

禁止依赖：

```text
workflow/

bot/

conversation/

chat/

react/

agent/

workflow runtime/
```

MAGI Runtime 必须继续保持独立。

---

# 十、Adapter 调整

继续保留：

```text
adapter/

model_adapter

plugin_adapter

knowledge_adapter

repository_adapter

event_adapter
```

新增：

```text
server_adapter
```

负责：

Coze Server

↓

MAGI Handler

这样以后如果脱离 Coze，

只需要替换：

```text
server_adapter
```

---

# 十一、Repository

Repository 全部保持 Port。

Application：

只依赖：

```go
CaseRepository

EventRepository

MemoryRepository

KnowledgeRepository
```

绝不能依赖：

MySQL

Redis

Atlas

实现全部留在 Adapter。

---

# 十二、DTO

Server 与 Domain 之间增加 DTO。

禁止：

```go
entity.Case
```

直接作为 HTTP 返回。

新增：

```text
server/dto

CaseResponse

CaseSummary

DecisionReport

ReplayEvent

TraceResponse

EvaluationResponse
```

避免 Domain 与 HTTP 耦合。

---

# 十三、错误体系

统一：

```text
Domain Error

↓

Application Error

↓

HTTP Error
```

例如：

```text
ErrToolNotFound

↓

404
```

而不是 Handler 到处判断。

---

# 十四、OpenAPI

所有 API 自动生成：

```text
OpenAPI

↓

Swagger

↓

SDK
```

以后：

Python SDK

Go SDK

TS SDK

全部自动生成。

---

# 十五、日志

统一：

```text
RequestID

↓

CaseID

↓

RunID

↓

AgentID
```

贯穿：

HTTP

Application

Runtime

方便 Replay。

---

# 十六、生命周期

统一：

```text
Server Start

↓

Load Config

↓

Build Container

↓

Connect Repository

↓

Connect Plugin

↓

Connect Knowledge

↓

Build Runtime

↓

Build Application

↓

Register Handler

↓

Start Hertz
```

关闭：

```text
Stop HTTP

↓

Drain SSE

↓

Stop Runtime

↓

Close Repository

↓

Exit
```

---

# 十七、完成后的调用链

最终唯一调用链应为：

```text
HTTP Request
        │
        ▼
Hertz Router
        │
        ▼
Handler
        │
        ▼
Application Service
        │
        ▼
Orchestrator
        │
        ▼
Dispatcher
        │
        ▼
Agent Runtime
        │
        ▼
Model / Tool / Knowledge Adapter
        │
        ▼
Coze Infrastructure
        │
        ▼
Repository
        │
        ▼
Application
        │
        ▼
Handler
        │
        ▼
HTTP Response
```

SSE 则通过 EventPublisher 并行推送事件，不影响主调用链。

---

# 改造完成标准（Definition of Done）

完成后，MAGI 应满足以下要求：

* CLI 完全删除，`cmd/magi-server` 成为唯一入口。
* `main.go` 仅负责配置加载、依赖注入和 Server 启动，不包含任何业务逻辑。
* 引入独立的 Application Layer，所有业务能力统一通过 Service 暴露。
* HTTP、SSE、未来的 WebSocket 都通过 Server Layer 对外提供，不直接访问 Domain。
* Domain 保持纯净，不依赖 HTTP、CLI、Hertz 或 Coze 的业务模块。
* 继续复用 Coze 的基础设施（Hertz、Plugin、Knowledge、Model、SSE 等），但不再依赖其 Workflow、Bot、Agent Runtime。
* 所有 REST API、SSE、未来 SDK 均建立在统一的 Application Layer 之上，为后续 Decision Center、Benchmark、MAGI Studio 等产品能力提供稳定接口。
