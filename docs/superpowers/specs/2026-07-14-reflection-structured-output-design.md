# Reflection 结构化输出设计（Item 6）

> 设计日期：2026-07-14
> 关联：闭合 magi-design.md §9（最后一个 LLM 结构化产物 Reflection）、§17（Reflection）
> 前置：Reflection 结构体已有 json 标签 + UtilityDimensionReevaluation 字段 + ValidateReflection 4-of-1 规则（先前批次完成）。当前 Reflection 靠 InferReflection 从投票差推断，非 LLM 直接产出。

## 目标

让 reconsider 路径的 agent 直接产出结构化 Reflection JSON（经 TypedValidator schema 校验），捕获其改票理由（位置变化/接受拒绝 claim/新证据/效用重评），替代当前推断。Reflection 成为第 6 个经 TypedValidator 校验的 LLM 结构化产物（DecisionTask/EvidenceSummary/ClaimSubmission/Vote/FinalReportData/Reflection），§9 完全闭合。

## 范围

仅 Item 6。新增 reconsider_reflect 阶段 + Reflection 响应类型 + 两层校验（schema + 4-of-1）。

## 设计

### 流程变更

reconsider 路径：`reconsider_gather` ->（gate pass）-> `reconsider_reflect` [新] -> `vote`。

当前 gate pass 后直接进 vote；新增 `reconsider_reflect` 阶段，agent 产出 Reflection JSON，schema 校验通过后进 vote。

### 改动

1. **response_parser.go**：新增 `ResponseReflection` 类型；`ParsedResponse.Reflection *entity.Reflection` 字段；`parseResponse` 加 `reflectionVal *validation.TypedValidator[entity.Reflection]` 参数；fallback `case "reconsider_reflect"` 尝试 Reflection schema（与 EvidenceSummary 同模式，无 discriminator）。

2. **runtime.go**：`LoopResult` 新增 `Reflection *entity.Reflection` 字段。

3. **agent_loop.go**：
   - `AgentLoop` 结构体新增 `reflectionVal *validation.TypedValidator[entity.Reflection]`；`NewAgentLoop` 内部构造（与 summaryVal/voteVal/claimVal 同模式，不经 Deps）。
   - gate pass 后 `phase = "reconsider_reflect"`（reconsider 路径）；非 reconsider 路径仍 `phase = "vote"`。
   - 响应 switch 新增 `case ResponseReflection: result.Reflection = pr.Reflection; phase = "vote"`。
   - `parseResponse` 调用传入 `reflectionVal`。
   - `BuildAgentSystemPrompt` 调用传入 reflectionSchema。

4. **prompt.go**：`BuildAgentSystemPrompt` 加 `reflectionSchema []byte` 参数；reconsider 模式（debate != nil）描述 EvidenceSummary -> Reflection -> Vote 流程 + Reflection schema + 4-of-1 要求。

5. **orchestrator.go**：`EnforceReflectionRule` 优先用 `results[i].Reflection`（LLM 产出）；经 `ValidateReflection` 4-of-1 校验，失败则回退投票。无 Reflection 时退化为 `InferReflection`（mock 测试兼容）。

### 两层校验

- agent_loop 内 `TypedValidator[Reflection]`：schema 校验（json 结构/json 标签）。
- EnforceReflectionRule 内 `ValidateReflection`：4-of-1 语义校验（改票须满足：新 EV-ID / 接受 claim / 拒绝 claim / 效用重评之一）。

LLM Reflection schema 合法但 4-of-1 不满足 -> EnforceReflectionRule 回退该 agent 投票为上一轮。

### 兼容性

- 非 reconsider 路径（初始 gather）：无 Reflection 阶段，不变。
- mockMagiRuntime（orchestrator_test）返回的 LoopResult 无 Reflection -> EnforceReflectionRule 退化推断，现有测试不受影响。
- `TestAgentLoop_Reconsider` 脚本需加 Reflection JSON 响应。

## 数据流

```
reconsider: reconsider_gather (tool/summary) -> gate pass
  -> reconsider_reflect: LLM 产出 Reflection JSON -> TypedValidator 校验 -> result.Reflection
  -> vote: LLM 产出 Vote -> result.Vote
REVOTING: EnforceReflectionRule 用 result.Reflection (ValidateReflection 4-of-1) -> 不满足则回退投票
```

## 测试

- `agent_loop_test.go`：`TestAgentLoop_Reconsider` 脚本加 Reflection JSON；新增 Reflection 解析/存储测试（reconsider 产出 Reflection，result.Reflection 非空）。
- `orchestrator_test.go`：EnforceReflectionRule 用 LLM Reflection 的测试（LoopResult 带 Reflection，4-of-1 不满足回退）。

## 文件清单

- `domain/runtime/response_parser.go`：ResponseReflection + ParsedResponse.Reflection + parseResponse reflectionVal 参数 + reconsider_reflect fallback。
- `domain/runtime/runtime.go`：LoopResult.Reflection 字段。
- `domain/runtime/agent_loop.go`：reflectionVal + NewAgentLoop 构造 + phase 转换 + ResponseReflection case + prompt 调用。
- `domain/runtime/prompt.go`：BuildAgentSystemPrompt 加 reflectionSchema 参数 + reconsider 描述。
- `domain/orchestration/orchestrator.go`：EnforceReflectionRule 用 LLM Reflection。
- 对应 `_test.go` 更新/新增。
