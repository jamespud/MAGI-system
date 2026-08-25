# MAGI A2A Server 技术设计

> 日期：2026-08-25
>
> 状态：方向已确认，待设计文档评审
>
> 协议基线：A2A v1.0.1，协议版本 `1.0`
>
> SDK 基线：`github.com/a2aproject/a2a-go/v2` v2.5.0

## 1. 结论

MAGI 首期实现入站 A2A Server，使外部系统能够发现、调用、订阅、查询和取消
MAGI 决策任务。A2A 作为新的协议适配层加入现有 Go 进程，使用官方 Go SDK 提供
HTTP+JSON/REST 和 SSE 的协议编解码；MAGI 的 `DecisionCase`、`DecisionJob`、
`MagiEvent`、`Conversation` 与 `Resolution` 继续作为唯一事实源。

首期不实现 A2A Client，不采用 SDK 默认的内存 task store，也不建立一套独立的
A2A 任务运行时。这样能够保持 MAGI 已有的租约、重试、checkpoint、进程恢复、
多租户配额和审计语义，避免 A2A Task 与 MAGI Job 两套状态机发生分叉。

## 2. 目标与非目标

### 2.1 目标

1. 外部 A2A Client 可通过 Agent Card 发现 MAGI 的能力和认证方式。
2. 支持异步提交、流式提交、任务查询、任务列表、取消和重新订阅。
3. A2A Task 在进程重启、worker 重试和多副本切换后仍能恢复到正确状态。
4. 同一租户重复提交相同 `messageId` 只创建一个 Case 和一个 durable Job。
5. A2A `contextId` 复用 MAGI Conversation，支持跨任务多轮追问。
6. 输出同时提供适合人类阅读的 Markdown 报告和适合系统消费的 JSON 结果。
7. A2A 调用遵守现有认证、租户隔离、预算、并发、工具审批、审计和指标规则。

### 2.2 非目标

首期明确不包含：

- MAGI 主动调用其他 A2A Agent；
- 文件、图片、音频等非文本输入；
- push notification；
- JSON-RPC 或 gRPC 绑定；
- A2A 专用前端页面；
- 使用 A2A 协议暴露 MAGI 的内部 Agent、Claim、Vote 或 Tool 为独立远程 Agent；
- 改变 MAGI 的确定性 FSM、证据门、投票或共识规则。

## 3. 设计原则

### 3.1 单一事实源

A2A Task 是 `DecisionCase` 的协议投影，不是新的领域实体：

- Task 状态由 Case 和 Job 状态计算；
- Task 结果由 Resolution、Dissent、Evidence 和 Claim 计算；
- Task 流由持久 MagiEvent 计算；
- Task 所有权由 Case.UserID 决定；
- A2A 层只持久化协议幂等、外部标识和投影所需的绑定信息。

不得使用 SDK 默认 `AgentExecutor + in-memory taskstore` 承担实际执行。否则服务
重启后 MAGI Job 能恢复，而 executor 上下文已经消失，A2A Task 可能永久停留在
`working`。

### 3.2 协议层不进入 domain

A2A SDK 类型仅允许出现在 `application/a2a`、`server/a2a` 和适配器边界。
`domain/entity`、`domain/orchestration` 和 `domain/runtime` 不导入 A2A 包。
状态、Artifact 和错误映射集中在 projector/mapper 中，避免协议字段扩散到核心领域。

### 3.3 提交与执行分离

创建 A2A 绑定、Conversation、ConversationMessage 和 DecisionCase 必须在同一个
数据库事务中完成。事务提交后，再通过现有 RunManager 的可重入启动入口执行预算、
并发准入和 durable Job 入队。`PREPARED` binding 是启动前崩溃的恢复凭据，
`decision_job.case_id` 唯一约束保证重试只产生一个 Job。HTTP/SSE 请求取消只终止
传输，不取消已经提交的决策；只有显式 CancelTask 才取消任务。

### 3.4 协议兼容由官方 SDK 负责

固定使用官方 `a2a-go/v2` v2.5.0 的类型、REST transport、SSE 编解码、版本协商
和标准错误。MAGI 自定义 `a2asrv.RequestHandler`，但不自行拼装 ProtoJSON、SSE
事件格式或协议错误响应。

## 4. 总体架构

```text
External A2A Client
        |
        | Agent Card / HTTP+JSON / SSE
        v
+---------------------- server/a2a -----------------------+
| Hertz route + net/http adapter + auth/rate/audit         |
| Official A2A REST transport                              |
+------------------------------+---------------------------+
                               |
                               v
+------------------- application/a2a ----------------------+
| RequestHandler                                             |
| SubmissionService | TaskProjector | StreamProjector       |
| StatusMapper      | ArtifactBuilder | CursorCodec          |
+-----------+--------------------+-------------------------+
            |                    |
            v                    v
  Assistant/Decision       A2A binding repository
  application services     Event repository by seq
            |                    |
            +----------+---------+
                       v
              Shared MySQL database
                       |
                       v
             Durable RunManager/FSM
```

### 4.1 新增组件

| 组件 | 职责 | 依赖 |
| --- | --- | --- |
| `server/a2a` | 注册 Agent Card 和 A2A transport，接入中间件 | Hertz、官方 SDK |
| `application/a2a.RequestHandler` | 实现 SDK 请求接口并协调各用例 | SubmissionService、TaskProjector |
| `SubmissionService` | 校验输入、幂等提交、启动 durable job | A2ASubmissionRepository、RunManager |
| `TaskProjector` | 从 MAGI 实体构造 A2A Task | Case、Job、Resolution、Binding 仓库 |
| `StreamProjector` | 按持久 seq 将 MagiEvent 投影为 A2A 更新 | EventRepository、TaskProjector |
| `ArtifactBuilder` | 构造稳定 ID 的 Markdown/JSON Artifact | Resolution、Dissent、Evidence、Claim |
| `CursorCodec` | 编解码 ListTasks 的 opaque keyset cursor | 无外部状态 |

这些组件按当前项目分层约束放置：application 依赖 domain port；数据库实现位于
adapter；Hertz 和 A2A SDK transport 位于 server。

## 5. 对外协议面

### 5.1 Agent Card

公开路径：

```text
GET /.well-known/agent-card.json
```

Agent Card 至少声明：

- 协议版本 `1.0`；
- HTTP+JSON 接口地址，基础路径默认为 `/a2a`；
- streaming=true，pushNotifications=false；
- 输入模式 `text/plain`；
- 输出模式 `text/markdown`、`application/json`；
- Bearer 与 `X-API-Key` 安全方案；
- 一个 `evidence-driven-decision` skill，描述 MAGI 的证据调查、三方评估、投票、
  异议和结构化决议能力。

Agent Card 可公开访问，但不得包含密钥、内部服务地址、模型名称、租户信息或工具
配置。生产环境中的对外 URL 必须来自显式 `a2a.public_url`，不得信任请求 Host
头推导，避免代理层 Host 注入。

### 5.2 MVP 操作

| A2A 操作 | MAGI 行为 |
| --- | --- |
| SendMessage | 持久提交任务并立即返回 `submitted` 或 `working` Task |
| SendStreamingMessage | 提交后保持 SSE，发送初始 Task、状态更新和最终 Artifact |
| GetTask | 按 Task ID 查询 Case 并实时投影 |
| ListTasks | 按租户使用 keyset cursor 列出 Task |
| CancelTask | 幂等取消本地或远端 durable job |
| SubscribeToTask | 查询当前快照，再订阅该 Task 的后续持久事件 |

SDK 注册的具体 REST 子路径和消息编码以固定版本 SDK 为准，MAGI 不复制一套手写
路由。`/a2a` 基础路径可配置，但 Agent Card 和实际 transport 必须使用同一地址。

### 5.3 输入约束

MVP 只接受一个或多个 text part，按原顺序以换行拼接。空文本、非文本 part、超过
配置上限的消息以及无法识别的扩展返回 SDK 对应的标准错误。支持 A2A metadata
中的以下 MAGI 扩展：

```json
{
  "magi": {
    "background": "optional background",
    "constraints": [
      {"type": "budget", "description": "optional constraint"}
    ]
  }
}
```

扩展对象必须通过严格 schema 校验；未知字段首期拒绝，避免调用方误以为参数已生效。

## 6. 标识和会话映射

| A2A 概念 | MAGI 概念 | 规则 |
| --- | --- | --- |
| `Task.id` | `DecisionCase.ID` | 一对一，直接使用 Case ID |
| `contextId` | `Conversation.ID` | 一对一，跨 Task 多轮上下文 |
| `messageId` | A2A submission binding | 按租户唯一，用于幂等 |
| Task history | 当前 Task 的输入消息 | MVP 不返回整个 Conversation，防止跨 Task 泄漏 |
| Artifact ID | Case ID + 固定后缀 | 重试和重连时保持稳定 |

### 6.1 新会话

请求不带 `taskId` 和 `contextId` 时，SubmissionService 在事务中生成 Conversation
ID、Case ID 和两个 ConversationMessage ID，创建新 Conversation 与 Case，并将
返回 Task 的 `contextId` 设为该 Conversation ID。

### 6.2 同一上下文中的追问

请求带 `contextId`、不带 `taskId` 时：

1. 按当前 principal 检查 Conversation 所有权；
2. 使用现有 Assistant 逻辑水合最近历史和关联 Resolution；
3. 在同一 Conversation 中创建新的 Case；
4. 返回新的 Task ID，并保留原 contextId。

每次追问都是一个新 Task，因为每个 DecisionCase 都有独立的证据、投票、状态和
最终决议。

### 6.3 带 taskId 的消息

MVP 不支持向正在执行或已终结的 Task 追加消息。SendMessage 携带 `taskId` 时返回
标准“不支持该交互状态”错误。未来只有当工具审批映射为 `input-required` 或
`auth-required` 后，才扩展为在同一 Task 中提交恢复输入。

## 7. 幂等提交与事务边界

新增 `a2a_submission` 表，不修改 ConversationMessage 领域实体：

| 字段 | 说明 |
| --- | --- |
| `id` | 内部主键 |
| `user_id` | 租户/调用主体，open mode 为 0 |
| `message_id` | 外部 A2A messageId |
| `request_hash` | 规范化输入、contextId 和 MAGI metadata 的 SHA-256 |
| `task_id` | 预分配 Case ID |
| `context_id` | Conversation ID |
| `input_message_id` | MAGI user ConversationMessage ID |
| `state` | `PREPARED`、`STARTED`、`REJECTED` |
| `error_code` | 可重放的稳定拒绝原因 |
| `created_at/updated_at` | 审计时间 |

唯一约束为 `(user_id, message_id)`，`task_id` 也必须唯一。

提交算法：

1. 规范化请求并计算 `request_hash`。
2. 开启数据库事务，尝试读取 `(user_id, message_id)`。
3. 已存在且 hash 相同：返回已有 Task，不创建任何新资源。
4. 已存在但 hash 不同：返回 invalid request，防止同一幂等键复用不同内容。
5. 不存在：分配稳定 ID，锁定并校验 context 所有权，在事务中水合有序历史，然后
   创建 binding、Conversation（需要时）、ConversationMessage 和 DecisionCase。
6. 提交事务。
7. 调用 RunManager 执行权威预算/并发准入并入队 Job；启动调用必须可重复，
   `decision_job.case_id` 唯一约束负责 Job 幂等。
8. 入队成功、already-running 或 already-completed 都将 binding 标记为 `STARTED`。
   若进程在第 6、7 步之间退出，启动恢复器扫描 `PREPARED` 记录并重新执行第 7 步。
9. 若预算、限额或输入约束形成稳定拒绝，将 binding 和 Case 标记为 `REJECTED/FAILED`，
   后续相同 messageId 重放同一结果；瞬时存储错误保持 `PREPARED` 等待恢复。

该事务需要专用 `A2ASubmissionRepository`，不得在 Handler 中串联多个普通 repository
调用伪装成原子操作。对于追问，事务先锁定 Conversation 行、验证所有权，再读取
有序历史并完成水合和写入。这样同一 Conversation 的并发提交会确定性串行化，避免
两个追问都基于过期的相同历史快照。

预算或并发限制可以在事务前预检以快速失败，但事务后的 RunManager 准入才是权威
结果。瞬时启动失败不删除 Case；binding 保持 `PREPARED`，由恢复器接管。

## 8. Task 状态投影

| MAGI Case/Job 状态 | A2A Task 状态 | 说明 |
| --- | --- | --- |
| binding `PREPARED`，Case `DRAFT`，Job 尚未创建或 queued | `submitted` | 已耐久接收，尚未 claim |
| `NORMALIZING` 至 `EVALUATING` | `working` | metadata 暴露具体 MAGI 状态和 round |
| `PAUSED` | `working` | metadata.magi.paused=true；MVP 不伪造 input-required |
| `RESOLVED`、`MEMORY_INDEXED` | `completed` | 返回 Markdown 与 JSON Artifact |
| `INSUFFICIENT_EVIDENCE`、`DEADLOCKED` | `completed` | 有效业务结论，在 JSON 中表达 outcome |
| `CANCELLED` | `canceled` | 不返回最终 Artifact |
| `FAILED`、`TIMED_OUT` | `failed` | status message 返回脱敏错误摘要 |

状态投影优先级：

1. Case 的 canceled/failed/terminal 状态；
2. durable Job 的 queued/running/paused/canceled/failed/succeeded 状态；
3. Case 的处理中 FSM 状态。

优先级用于处理短暂的跨表可见性窗口。例如 Job 已取消而 worker 尚未观察到取消时，
A2A 必须立即投影为 canceled，不得继续暴露 working 或晚到的 completed。

所有 Task metadata 使用带版本的命名空间：

```json
{
  "magi": {
    "schemaVersion": "1",
    "caseStatus": "INVESTIGATING",
    "jobStatus": "running",
    "round": 1,
    "paused": false,
    "eventSeq": 42
  }
}
```

## 9. Artifact 设计

Task 终结为 completed 时生成两个 Artifact。

### 9.1 Markdown 报告

- Artifact ID：`<case-id>-decision-report`
- media type：`text/markdown`
- 内容：正常决议直接使用 Resolution.FinalReport
- 文件名：`decision-report.md`

### 9.2 JSON 结构化结果

- Artifact ID：`<case-id>-decision-result`
- media type：`application/json`
- 文件名：`decision-result.json`
- schemaVersion：`1`

```json
{
  "schemaVersion": "1",
  "caseId": "case-...",
  "contextId": "conv-...",
  "outcome": "resolved",
  "decision": "approve",
  "consensus": {
    "outcome": "majority_approval_with_dissent",
    "round": 2
  },
  "confidence": 86.5,
  "dissent": [],
  "evidenceRefs": ["EV-001"],
  "claimRefs": ["CL-001"]
}
```

`confidence` 使用 MAGI 已有确定性聚合结果，不让 A2A 层重新计算。Dissent 使用结构化
少数意见；证据和主张首期只输出 ID 与可公开摘要，不默认输出原始工具结果，防止
敏感信息通过嵌入接口外泄。

`DEADLOCKED` 或 `INSUFFICIENT_EVIDENCE` 没有常规 Resolution 时，ArtifactBuilder
不得报内部错误：Markdown 使用固定模板说明终态、轮次和可用证据摘要；JSON 的
`outcome` 分别为 `deadlocked` 或 `insufficient_evidence`，`decision` 和无法确定的
`confidence` 为 null。该降级模板由 Go 代码生成，不额外调用 LLM。

ArtifactBuilder 必须是纯投影：相同数据库快照始终产生相同 Artifact ID、顺序和内容。

## 10. 持久事件顺序与流式恢复

### 10.1 当前缺口

`MagiEvent.Seq` 当前由进程内原子计数生成，但 `EventModel` 没有保存 Seq；数据库
仅按 timestamp 查询。进程重启会重置计数，多副本会生成冲突序号，相同时间戳也
没有稳定次序，因此不能作为 A2A 重连游标。

### 10.2 持久 per-case sequence

新增：

```text
magi_event.seq BIGINT NOT NULL
UNIQUE (case_id, seq)

magi_event_cursor
  case_id  VARCHAR(64) PRIMARY KEY
  next_seq BIGINT NOT NULL
```

EventRepository.Create 在同一事务中锁定或创建 `magi_event_cursor` 行，取得 next_seq，
递增 cursor，并插入事件。写入成功后将分配的 seq 回填到 `MagiEvent`，再由现有
EventPublisherAdapter fan-out，所以本地订阅和数据库轮询观察到同一序号。

Repository 接口改为：

```go
ListAfterSeq(ctx context.Context, caseID string, afterSeq uint64, limit int) ([]*entity.MagiEvent, error)
```

查询固定使用 `WHERE case_id=? AND seq>? ORDER BY seq ASC LIMIT ?`。旧 timestamp
接口在现有 REST/SSE 迁移完成后删除，不能长期维护两个不同 watermark。

### 10.3 发送算法

SendStreamingMessage：

1. 提交 Task；
2. 发送当前完整 Task 快照；
3. 记录快照对应的最高 event seq；
4. 从数据库补读更大 seq，消除订阅建立窗口；
5. 合并本地 broker 和跨实例 DB poller，按 seq 排序并去重；
6. 高价值 FSM 事件投影为 TaskStatusUpdate；
7. completed 时发送 ArtifactUpdate 和 final TaskStatusUpdate，然后关闭流；
8. 客户端断开只结束订阅，不影响 Job。

SubscribeToTask 总是先返回当前快照，再从快照 watermark 订阅。若 SDK transport 支持
SSE `Last-Event-ID`，其值使用持久 seq；否则当前快照仍保证调用方不会错过最新 Task
状态，内部 seq 保证快照之后的增量不丢失。

不是每个内部 MagiEvent 都需要对外发送。模型请求、原始工具结果等敏感或高频事件
只更新内部 watermark；状态切换、审批请求、终态和 Artifact 才生成 A2A 更新。

### 10.4 数据迁移

采用 expand/backfill/contract，并安排一个短暂的 event writer 维护窗口。旧版本实例
不会写 seq，不能与新 writer 在回填阶段长期混跑：

1. 增加 nullable seq 和 cursor 表；
2. drain worker 并暂停事件写入；
3. 按 `(case_id, timestamp, id)` 为历史事件回填 `ROW_NUMBER()`；
4. 将每个 cursor 初始化为 `MAX(seq)+1`；
5. 校验每 Case 无 null、无重复、序号严格递增；
6. 增加 NOT NULL 和唯一约束并部署新 writer；
7. 恢复 worker，切换所有读取到 seq cursor；
8. 删除 timestamp 增量接口。

生产迁移提供显式 Atlas SQL；GORM AutoMigrate 继续包含最终模型，但不依赖它完成
历史回填和约束收紧。

## 11. 取消、租约与晚到结果

CancelTask 必须满足“取消优先”：

1. 校验 Task 所有权和可取消状态；
2. 原子地把 durable Job 标记 canceled，并把 Case 标记 CANCELLED；
3. 本地 worker 立即 cancel context；
4. 远端 worker 的 heartbeat 发现 canceled、lease 丢失或 owner 不匹配时 cancel context；
5. 短暂数据库错误可以重试，但 worker 不得越过 lease 安全期限继续执行；
6. Case 状态写入使用条件更新，CANCELLED 之后拒绝普通 FSM 转移；
7. MarkSucceeded 仅在 worker 仍持有 running lease 时成功；
8. A2A projector 永远不为 canceled Task 暴露晚到 Resolution Artifact。

当前 heartbeat 丢弃 repository 错误，跨实例 Cancel 只能更新数据库，远端 context
可能继续运行。实施时需让 Heartbeat 区分 lease/status 丢失与瞬时存储故障，并把
终止信号传回执行 context。

重复 CancelTask 对 canceled Task 返回当前 canceled Task。对 completed、failed 等
不可取消终态使用 SDK 的标准 task-not-cancelable 错误。

## 12. ListTasks 与 cursor

现有 Case 列表使用 offset pagination，不直接用于 A2A ListTasks。新增 owner-scoped
keyset 查询，排序固定为：

```sql
ORDER BY created_at DESC, id DESC
```

cursor 是版本化、base64url 编码的 opaque JSON：

```json
{"v":1,"createdAt":"2026-08-25T06:00:00Z","id":"case-..."}
```

下一页条件使用 `(created_at < ?) OR (created_at = ? AND id < ?)`。limit 默认 50，
最大 100。所有查询先绑定 user_id；cursor 不是授权边界，篡改 cursor 也不能跨租户。
格式错误、版本未知或时间非法返回 invalid request。

## 13. 认证、安全与治理

### 13.1 路由策略

- `/.well-known/agent-card.json` 加入 public paths；
- `/a2a` 下所有操作必须经过 Auth；
- 复用 Bearer 和 `X-API-Key` principal；
- RateLimit 为 A2A 使用独立 bucket，避免流式连接挤占普通 REST 配额；
- Send、Cancel 记录审计事件；Get、List、Subscribe 记录指标但默认不写高频审计日志；
- 所有 Get/List/Cancel/Subscribe 在读取 Artifact 或事件前校验 Case.UserID。

### 13.2 数据最小化

- 标准错误和 failed status message 必须脱敏；
- 不把内部模型输出、prompt、原始工具响应或 secret 写入 Task metadata；
- Agent Card 不暴露内部拓扑；
- Artifact 中的 evidence 摘要经过现有 redactor；
- 日志只记录 taskId/contextId/messageId hash，不记录完整用户消息。

### 13.3 资源限制

- 限制输入总字节、part 数、metadata 深度和 constraints 数；
- 限制单个 SSE principal 的连接数；
- 慢客户端使用有界缓冲，溢出时关闭连接，Task 继续后台运行；
- SubscribeToTask 对 terminal Task 返回快照后立即结束；
- 继续复用 MAGI 的 per-user 并发、token 和成本预算。

## 14. 错误映射

协议错误优先使用 SDK 提供的标准构造器：

| MAGI 情况 | A2A/HTTP 表达 |
| --- | --- |
| malformed request、hash 冲突、非法 cursor | invalid request |
| 非文本 part | content type not supported |
| Case 不存在或对当前租户不可见 | task not found，避免枚举其他租户资源 |
| terminal Task 取消 | task not cancelable |
| 未认证 | HTTP 401，由 Auth middleware 返回 |
| 已认证但无操作权限 | HTTP 403 |
| HTTP/A2A 速率限制 | HTTP 429，带 Retry-After |
| 预算或并发耗尽 | 可重试的 rejected/failed 提交错误，metadata 给稳定 MAGI 错误码 |
| 数据库或内部投影错误 | SDK internal error，日志关联 traceId |

请求 context 取消只返回传输中断；如果提交事务已经成功，不回滚或取消后台 Task。

## 15. 配置与装配

新增配置：

```yaml
a2a:
  enabled: false
  public_url: ""
  base_path: "/a2a"
  max_message_bytes: 65536
  max_parts: 16
  max_page_size: 100
  max_streams_per_user: 8
  cross_instance_poll_interval: 2s
```

规则：

- 默认 disabled，避免升级后意外扩大外部攻击面；
- production 开启时 `public_url` 必填且必须为 HTTPS；
- `base_path` 必须与 Agent Card 声明一致；
- 轮询间隔设下限，防止错误配置压垮数据库；
- bootstrap 通过 Fx 装配 A2A repositories、services、handler 和 lifecycle recovery；
- Hertz 使用内置 `github.com/cloudwego/hertz/pkg/common/adaptor` 的 `adaptor.HertzHandler` 挂载官方 SDK 的 `net/http.Handler`；
- nginx 对 `/a2a` SSE 关闭 buffering，并配置长读超时和禁用响应缓存。

## 16. 可观测性

新增 Prometheus 指标：

```text
magi_a2a_requests_total{operation,result}
magi_a2a_request_duration_seconds{operation}
magi_a2a_active_streams
magi_a2a_stream_duration_seconds{result}
magi_a2a_idempotency_hits_total
magi_a2a_projection_errors_total{kind}
magi_a2a_event_lag_seconds
magi_a2a_recovery_total{result}
```

OTel span 以 A2A operation 为入口，附加 taskId、contextId、principal user ID 和
trace ID，不记录消息正文。A2A 提交产生的 trace 与后台 Run trace 通过 taskId 和
jobId 关联，而不假设二者共享同一个 HTTP context。

## 17. 测试策略

### 17.1 单元测试

- Case/Job 状态到 A2A 状态的完整表驱动映射；
- terminal、deadlock、insufficient evidence 和 paused 投影；
- Markdown/JSON Artifact 稳定性和 schema；
- cursor 编解码、边界、非法输入；
- request hash 规范化；
- MAGI 错误到 SDK 标准错误映射；
- MagiEvent 到外部状态更新的过滤和投影。

### 17.2 Repository 测试

- 同租户并发提交相同 messageId 只生成一条 binding、一个 Case 和一个 Job；
- 相同 messageId 不同 hash 被拒绝；
- 不同租户可使用相同 messageId；
- 多 goroutine、多 Case 事件 seq 单调且 `(case_id, seq)` 唯一；
- keyset pagination 无重复、无遗漏；
- SQLite 快速测试与 MySQL 集成测试都覆盖事务/锁行为。

### 17.3 协议与集成测试

- 使用官方 A2A Go client 读取 Agent Card 并调用每个 MVP 操作；
- SendMessage 返回后断开 HTTP，请求对应 Job 仍完成；
- SendStreamingMessage 从 submitted 走到 terminal 并返回两个 Artifact；
- 流断开后 SubscribeToTask 能读取当前状态并继续接收；
- 进程在提交后、worker 启动前退出，重启后 Task 自动运行；
- worker 执行中退出，lease 到期后另一副本恢复并完成同一 Task；
- 在副本 A 调 CancelTask，副本 B 的 worker 停止且 Task 不暴露晚到 Artifact；
- 所有 Get/List/Cancel/Subscribe 验证跨租户不可见；
- Agent Card URL、能力、模式和安全声明做快照测试；
- 现有 `/api/v1` REST、SSE 和 Assistant e2e 不回归。

### 17.4 验收标准

1. 官方 A2A Go client 无自定义补丁即可发现并调用 MAGI。
2. 100 个并发相同 messageId 请求只产生一个 Case 和一个 DecisionJob。
3. 提交成功后强制终止进程，重启后同一 Task 最终进入 terminal 状态。
4. 每个 Case 的持久事件 seq 严格递增，跨实例订阅不遗漏 terminal 更新。
5. 远端取消在一个 heartbeat 周期加容错宽限内停止 worker，且 canceled Task 无 Artifact。
6. 不同租户无法通过 Task ID、contextId 或 cursor 观察彼此数据。
7. A2A feature flag 关闭时，不注册 A2A transport，现有行为保持不变。

## 18. 发布顺序

### 阶段一：可靠性基础

- 持久 per-case event seq 与迁移；
- seq-based EventRepository 和现有 SSE 切换；
- lease/status 丢失传播到 worker context；
- CANCELLED 条件状态写与晚到结果屏蔽；
- A2A submission binding、事务提交与恢复器。

该阶段先在现有 REST/SSE 上验证，不暴露 A2A 路由。

### 阶段二：A2A Server MVP

- 引入并固定官方 SDK；
- Agent Card；
- RequestHandler、SubmissionService、TaskProjector；
- SendMessage、GetTask、ListTasks、CancelTask；
- 认证、限流、审计、指标和配置。

### 阶段三：流式与结构化结果

- SendStreamingMessage、SubscribeToTask；
- StreamProjector 和 seq catch-up；
- Markdown/JSON Artifact；
- 多副本、重启、断线和慢客户端测试。

### 阶段四：按真实需求扩展

- 工具审批映射 input-required/auth-required；
- push notification；
- JSON-RPC 或 gRPC transport；
- A2A Client 与外部 Agent 编排；
- 非文本 part。

后续能力必须由明确消费者需求驱动，不在 MVP 中预埋第二套运行时。

## 19. 主要风险与决策

| 风险 | 设计处理 |
| --- | --- |
| SDK task store 与 MAGI Job 分叉 | 不使用 SDK task store，Task 按请求实时投影 |
| 幂等键记录晚于 Case 创建 | submission、Case、Conversation 同事务；Job 以 Case ID 幂等入队 |
| 多实例事件乱序或丢失 | DB 分配 per-case seq，按 seq catch-up 和去重 |
| 取消后 worker 写入完成结果 | lease 失效 cancel、条件状态写、Artifact 屏蔽 |
| offset 分页期间新增 Case 导致漂移 | owner-scoped keyset cursor |
| Agent Card URL 被 Host 头污染 | production 必须配置 public_url |
| A2A 类型侵入核心 domain | SDK 依赖限制在 application/server 边界 |
| 流式连接消耗资源 | 独立限流、有界缓冲、连接数和超时限制 |

## 20. 实施边界总结

本设计把 A2A 定义为 MAGI 的标准外部协议，而不是新的调度器：

```text
A2A owns: discovery, wire protocol, external IDs, Task projection
MAGI owns: identity, conversation, execution, state, evidence, result, recovery
```

只有保持这个边界，A2A 接入才能继承 MAGI 已有的可靠性与治理能力，并在嵌入其他
系统时提供稳定、可恢复、可审计的任务语义。
