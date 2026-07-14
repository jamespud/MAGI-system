# 事件完整性设计（范围 B）

> 设计日期：2026-07-14
> 关联：闭合 magi-design.md §20（Trace/Audit/Replay）、ADR-008 的遗留偏差
> 前置审计：第二轮审计发现 agent_loop 的 9 处事件发布 RunID 恒为空；EventToolCallRequested / EventToolCallValidated 已定义但从未发布

## 目标

让代理级事件携带非空、确定性、跨轮唯一的 RunID（符合 ADR-008 可追溯要求），并补发 2 个已定义但未发布的工具调用事件类型，使 25 种事件类型全部有发布点。

## 范围

仅两项：
- Item 3：RunID 由 dispatcher 确定性生成并经 AgentContext 传入 agent_loop，publish 使用之。
- Item 4：agent_loop 补发 EventToolCallRequested（工具调用请求时）和 EventToolCallValidated（参数校验通过后）。

不包含：事件持久化到 DB、SSE 推送前端、事件 payload 细化（另行处理）。

---

## Item 3：RunID 传递

### 现状

`agent_loop.go` 的 `publish` 辅助方法接收 `runID` 参数，但全部 9 处调用传 `""`。`MagiEvent.RunID` 恒为空，违反 ADR-008（事件应携带 RunID 以便 trace/replay 关联同一 agent run）。

### 设计

- `AgentContext`（`domain/runtime/runtime.go`）新增 `RunID string` 字段。
- `Dispatcher.Dispatch` 与 `DispatchReconsider`（`domain/orchestration/dispatcher.go`）新增 `round int` 参数，构造 `AgentContext` 时设置：
  - `Dispatch`：`RunID = fmt.Sprintf("%s-%s-r%d-investigate", case_.ID, cfg.Code, round)`
  - `DispatchReconsider`：`RunID = fmt.Sprintf("%s-%s-r%d-reconsider", case_.ID, cfg.Code, round)`
- `agent_loop.publish` 的 9 处调用将 `""` 替换为 `actx.RunID`（publish 签名不变）。
- 编排器（`orchestrator.go`）：INVESTIGATING 调用 `Dispatch(..., round)`（round=1）；DEBATING 调用 `DispatchReconsider(..., round)`（当前 round）。

### 确定性与唯一性

RunID 由 caseID + agentCode + round + phase 构成。investigate 与 reconsider 用 phase 区分，跨轮用 round 区分，确定性且可重现，利于 replay/审计。

### 签名影响

`Dispatch` / `DispatchReconsider` 加 `round int` 参数。仅编排器调用这两个方法（无外部调用方；`integration_test.go` 经编排器间接调用）。`mockMagiRuntime`（orchestrator_test）实现 `MagiRuntime` 接口，不受 dispatcher 签名影响。

---

## Item 4：补发 2 个工具调用事件

### 现状

`entity/event.go` 定义了 `EventToolCallRequested` 和 `EventToolCallValidated`，但 `agent_loop.go` 的工具调用分支只发布 `EventToolCallStarted` / `EventToolCallCompleted` / `EventToolCallFailed`。2 个事件类型无发布点。

### 设计

在 `agent_loop.go` 的 `ResponseToolCall` 分支：
- `EventToolCallRequested`：`for _, tc := range resp.ToolCalls` 循环体开始处（权限检查 `nameToDef` 查找之前）发布，每次工具调用一次。
- `EventToolCallValidated`：args schema 校验通过后（`vr` 为 nil 或 `vr.Valid`）、执行工具之前（`EventToolCallStarted` 之前）发布。

工具调用生命周期事件序列变为：Requested -> (权限/校验) -> Validated -> Started -> Completed|Failed。

---

## 数据流

```
orchestrator INVESTIGATING -> dispatcher.Dispatch(round=1)
  -> actx.RunID = "case-melchior-r1-investigate"
  -> agent_loop.Run: 每次 publish 用 actx.RunID
  -> 工具调用: Requested -> Validated -> Started -> Completed
orchestrator DEBATING -> dispatcher.DispatchReconsider(round)
  -> actx.RunID = "case-melchior-r{round}-reconsider"
```

## 测试

- `runtime/agent_loop_test.go`：新增测试--设置 `actx.RunID`，用 recordingEventPub 运行 loop，断言发布的事件 `RunID == actx.RunID`（非空）。
- `runtime/agent_loop_test.go`：新增测试--驱动一次工具调用，断言 `EventToolCallRequested` 和 `EventToolCallValidated` 各发布一次。
- `orchestration/orchestrator_test.go` 或 `dispatcher` 测试：断言 `Dispatch` 设置的 `actx.RunID` 匹配 `"caseID-code-r1-investigate"` 格式。

## 文件清单

- `domain/runtime/runtime.go`：`AgentContext` 新增 `RunID` 字段。
- `domain/orchestration/dispatcher.go`：`Dispatch` / `DispatchReconsider` 加 `round` 参数 + 设置 RunID。
- `domain/orchestration/orchestrator.go`：调用处传 `round`。
- `domain/runtime/agent_loop.go`：9 处 publish 用 `actx.RunID`；补发 2 个事件。
- 对应 `_test.go` 新增测试。
