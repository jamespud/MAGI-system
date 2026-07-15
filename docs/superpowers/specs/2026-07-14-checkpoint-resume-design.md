# checkpoint/resume 设计（§18，per-agent-run）

> 设计日期：2026-07-14
> 关联：闭合 magi-design.md §18（Workflow Orchestrator 完整状态机 - Checkpoint/Resume）
> 前置：`entity.AgentState`（RunID/Messages/StepCount/TokenUsed/Phase）+ `port.CheckpointRepository`（Save/Load）已存在但未使用。

## 目标

让 agent_loop 每步保存工作内存快照（checkpoint），启动时从快照恢复（resume），实现 per-agent-run 级别的中断恢复。闭合 §18 的 Checkpoint/Resume 工作内存部分。

## 范围

仅 agent_loop 级 checkpoint/resume。orchestrator 级 FSM resume（持久化 task/results/votes 跨进程重启）为后续。

## 设计

### AgentState 扩展

`entity/agent_run.go` 的 `AgentState` 新增 `MessagesJSON string` 字段（完整 `[]*schema.Message` 的 JSON 序列化，非有损；entity 不 import schema，用 JSON 字符串保持依赖方向）。

### AgentLoop 注入 CheckpointRepo

- `AgentLoopDeps` 新增 `CheckpointRepo port.CheckpointRepository`（可选）。
- `AgentLoop` 结构体新增 `checkpointRepo port.CheckpointRepository`。
- `NewAgentLoop` 设置。

### Run 启动 resume

Run 构建 messages 之后、循环之前：若 `checkpointRepo != nil && actx.RunID != ""`，`Load(actx.RunID)`；若返回的 `MessagesJSON` 非空，`json.Unmarshal` -> messages，恢复 `ts.tokenUsed = cp.TokenUsed`、`phase = cp.Phase`、`startStep = cp.StepCount + 1`。失败/无 checkpoint 则正常启动（startStep=1）。

### 循环从 startStep

`for step := startStep; step <= maxSteps; step++`。

### 每步后 checkpoint

循环体末尾（每次 step 处理完、continue 之前）：若 `checkpointRepo != nil && actx.RunID != ""`，`json.Marshal(messages)` -> `MessagesJSON`，`Save(&entity.AgentState{RunID, MessagesJSON, StepCount: step, TokenUsed: int(ts.tokenUsed), Phase: phase})`。失败非致命（log 即可）。

### nil-safe

独立 CLI（checkpointRepo=nil）-> 不 checkpoint/resume，行为不变。

### orchestrator 不改

resume 在 Run 内部。case 状态已持久化（UpdateStatus），重调 Orchestrate 从保存状态继续；agent run 经 checkpoint 恢复工作内存。

## 测试

- `agent_loop_test.go`：注入 mock CheckpointRepository，运行 loop，断言每步 Save 被调用、AgentState 字段正确。
- resume 测试：mock Load 返回 checkpoint（含 MessagesJSON + StepCount=2），运行 loop，断言从 step 3 继续（不重做 step 1-2，scripted 模型响应数对应）。

## 文件清单

- `domain/entity/agent_run.go`：AgentState 新增 MessagesJSON。
- `domain/runtime/agent_loop.go`：checkpointRepo 注入 + Run resume + 每步 Save。
- `domain/runtime/agent_loop_test.go`：checkpoint + resume 测试。
