# 记忆/上下文接入设计（范围 A）

> 设计日期：2026-07-14
> 关联：闭合 magi-design.md §7（Context Builder 与 Memory）的接线缺口
> 前置审计：ContextBuilder.Build 从未被调用（dispatcher 手工构造 AgentContext）；BuildProjection 返回值被丢弃（从不 Store）

## 目标

让 ContextBuilder 真正接入 dispatcher（RAG 检索知识进入 AgentContext），并让 BuildProjection 的产物经 KnowledgePort.Store 持久化（写入 case_memory_projection 表，供未来案件 Retrieve）。闭合 §7 的 RAG 读/写接线缺口。

## 范围

- Item 1：ContextBuilder 接入 Dispatcher。
- Item 2：BuildProjection 经 KnowledgePort.Store 持久化。
- Item 3（延后）：AgentContext.HistoricalCases 字段分离--当前 KnowledgePort.Retrieve 返回通用 KnowledgeChunk，无法区分历史案件与通用知识，需丰富 Port，另行处理。

## 现状

- `memory.ContextBuilder.Build`（context_builder.go）已实现：经 KnowledgePort.Retrieve 取知识，构造 AgentContext。但 dispatcher 从不调用它，AgentContext 手工构造，KnowledgeCtx 恒空。
- `memory.BuildProjection`（projection.go）已实现。orchestrator SAVING_MEMORY 调用它但返回值丢弃。
- `KnowledgeAdapter.Store`（knowledge_adapter.go）实际可用：经 `memRepo.Save` 写入 `case_memory_projection` 表。注释所提"deferred"仅指 RAG embedding/索引管道（MinIO），DB 持久化本身工作。
- `OrchestratorDeps` 无 KnowledgePort / ContextBuilder 字段。main.go 未接 KnowledgePort。

---

## Item 1：ContextBuilder 接入 Dispatcher

- `OrchestratorDeps` 新增 `ContextBuilder *memory.ContextBuilder`（可选，nil-safe）。
- `NewDispatcher(agentLoop runtime.MagiRuntime, contextBuilder *memory.ContextBuilder)` 新增第二参数。
- Dispatch / DispatchReconsider 调用 `contextBuilder.Build(ctx, case_, task, nil, nil)` **一次**获取含检索知识的 base `*AgentContext`；每个 agent 复制 base（`actx := *base`）并设置 `RunID` + per-agent `DebateContext`（DispatchReconsider 才有）。
- nil-safe：contextBuilder 为 nil 时，退化为当前手工构造（`AgentContext{CaseID, Task, Constraints}`），独立 CLI 无知识检索但结构正确。
- 检索仅一次（高效），3 个 agent 共享同一 KnowledgeCtx。

### 依赖

orchestration 已 import memory（SAVING_MEMORY 调 BuildProjection），无新依赖方向问题。

## Item 2：BuildProjection 持久化

- `OrchestratorDeps` 新增 `Knowledge port.KnowledgePort`（可选，nil-safe）。
- SAVING_MEMORY：`proj := memory.BuildProjection(case_, resolution, ledger, votes)`；`if o.knowledge != nil { _ = o.knowledge.Store(ctx, proj) }`。
- nil-safe：独立 CLI 无 KnowledgePort 时不存储（投影仍构建，供 Resolution/ Evaluation 用）。
- 效果：Coze 部署下投影写入 `case_memory_projection` 表，未来案件经 Retrieve 取回。

---

## 数据流

```
INVESTIGATING -> dispatcher.Dispatch(round)
  -> contextBuilder.Build(case, task) [Retrieve RAG 知识，一次]
  -> base AgentContext{KnowledgeCtx: chunks}
  -> 每 agent: actx := *base; actx.RunID=...; Run(ctx, cfg, &actx)
...
SAVING_MEMORY -> proj := BuildProjection(case, resolution, ledger, votes)
  -> knowledge.Store(proj) [写入 case_memory_projection 表]
```

## 测试

- `orchestration/dispatcher_test.go`：注入 ContextBuilder（带 mock KnowledgePort），断言 Build 被调用、agent 的 AgentContext.KnowledgeCtx 非空。新增 nil-ContextBuilder 退化测试。
- `orchestration/orchestrator_test.go` 或新测试：注入 mock KnowledgePort，运行 Orchestrate，断言 SAVING_MEMORY 调用 Store 且传入 CaseMemoryProjection。

## 文件清单

- `domain/orchestration/orchestrator.go`：`OrchestratorDeps` 新增 `ContextBuilder` + `Knowledge` 字段；`NewOrchestrator` 传 ContextBuilder 给 NewDispatcher；SAVING_MEMORY 存投影。
- `domain/orchestration/dispatcher.go`：`NewDispatcher` 加 `contextBuilder` 参数；Dispatch/DispatchReconsider 用 Build 取 base context。
- `domain/orchestration/dispatcher_test.go`：适配 NewDispatcher 签名 + 新增 ContextBuilder 接入测试。
- `domain/orchestration/orchestrator_test.go`：新增 Store 调用测试。
