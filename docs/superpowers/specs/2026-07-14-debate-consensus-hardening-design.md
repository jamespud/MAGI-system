# 辩论与共识硬化设计（范围 A）

> 设计日期：2026-07-14
> 关联：闭合 magi-design.md §15（Consensus Engine）、§16（Debate Engine）的遗留偏差
> 前置审计：第二轮审计发现 ConflictFinder 实际依赖 agent 显式断言、CONDITIONAL_APPROVE 条件被丢弃且 ConsensusConditional 为 dead 常量

## 目标

让辩论针对具体冲突 Claim（而非泛泛多数/少数派切分），并让 CONDITIONAL_APPROVE 投票的条件进入决议链（而非被静默重映射丢弃）。两项均遵守"LLM=语义，代码=规则"：确定性代码负责检测/收集/配对，语义判断留给 LLM/人工。

## 范围

仅两项：
- Item 1：辩论包 `ConflictingClaims` 在无 agent 显式断言时由投票分裂确定性合成。
- Item 2：`ConsensusConditional` 结果类型启用，携带条件，路由到带条件决议。

不包含：语义级冲突检测（embedding）、条件满足性的自动判定（语义）、Claim Graph 重构为真正图结构（另行处理）。

---

## Item 1：辩论聚焦冲突 Claim

### 现状

`DebateEngine.BuildPacket`（`domain/debate/engine.go`）调用 `finder.Find(claims)` 填充 `ConflictingClaims`。默认 finder 为 `NilConflictFinder`，其 `Find` 调用 `NewGraph(claims).Conflicts()`，仅返回 agent 显式断言的 `Claim.Contradicts` 边。实践中 LLM 常不填 `Contradicts`，导致 `ConflictingClaims` 为空，辩论退化为纯多数/少数派投票切分。

### 设计

**保留** `NilConflictFinder` 作为 agent-asserted 冲突来源（重命名为 `GraphConflictFinder` 以消除"nil=no-op"误解；保留 `NewNilConflictFinder` 作别名避免破坏现有调用）。

**新增** `BuildPacket` 内的确定性合成：当 `finder.Find()` 返回空，且 majority 与 minority 均非空时，用双方 `Vote.KeyClaimIDs` 按索引配对合成 `ClaimConflict`：

- 对 majority 中每个投票的 `KeyClaimIDs` 与 minority 中对应投票的 `KeyClaimIDs` 按索引配对。
- 每个 `ClaimConflict{ClaimA: <majority claim>, ClaimB: <minority claim>, Reason: "opposing-vote"}`。
- 上限 3 对，避免膨胀；超出按出现顺序截断。
- 跨阵营配对优先 majority[0]↔minority[0]、majority[0]↔minority[1]…，确保 minority 的异议 claim 被纳入。

合成逻辑放在 `BuildPacket` 内（而非 finder），因为它需要 majority/minority 投票信息，而 `ConflictFinder.Find` 签名只接收 claims。

### 行为影响

非 conditional 的 2:1 分裂进入辩论时，辩论包携带具体冲突 Claim 对。`DispatchReconsider` 将该包发给所有 agent；agent 通过 `DebateContext.Packet.ConflictingClaims` 看到具体冲突，可针对性辩论。

### 局限

合成配对不保证语义上真正矛盾（如"性能优" vs "可逆性差"不直接矛盾，仅来自对立阵营）。但比空 `ConflictingClaims` 更有用--给辩论具体靶子。语义级冲突检测留待未来（embedding-based，设计 §16 提及 S3+）。

---

## Item 2：CONDITIONAL_APPROVE 评估

### 现状

`ConsensusEngine.Evaluate`（`domain/consensus/engine.go`）对 `VoteDecisionConditionalApprove` 按 `policy.ConditionalAsApprove` 重映射为 approve 或 abstain 后计数。条件 `Vote.Conditions` 从不收集，`ConsensusConditional` 结果常量定义于 `entity/resolution.go:36` 但任何代码路径都不返回。

### 设计

**`ConsensusResult` 新增字段** `Conditions []DecisionCondition`（`entity/resolution.go`）。

**`Evaluate` 逻辑调整**：
1. 计数阶段**分离计数**：`pureApprove`（decision=approve）、`reject`、`abstain`、`conditional`（decision=conditional_approve），并收集所有 conditional 投票的 `Conditions`。不再先重映射再计数。
2. `approvalCamp = pureApprove + conditional`。
3. **ConsensusConditional 判定**（优先）：若 `conditional > 0 && approvalCamp >= 2 && approvalCamp > reject`，返回 `ConsensusConditional`，`Conditions` = 聚合的所有 conditional 投票条件。
4. **否则**按现有规则判定，但 conditional 投票按 `policy.ConditionalAsApprove` 重映射参与计数（quorum/strong/majority/deadlock 逻辑不变）。

`ConsensusConditional` 优先级：在 approval 倾向（approvalCamp 占多数）时覆盖 strong/majority approval。rejection 倾向不受影响（`approvalCamp > reject` 为假时不触发）。

**编排器路由**（`domain/orchestration/orchestrator.go` 的 `CaseStatusConsensusCheck`）：`ConsensusConditional` -> `CaseStatusResolving`（带条件决议，不进辩论、不死锁）。

**`finalDecision`**（orchestrator.go）：新增 `case entity.ConsensusConditional: return entity.VoteDecisionConditionalApprove`。

### 行为影响

含 conditional 投票的批准倾向决议从 `StrongApproval`/`MajorityApprovalDissent` 变为 `ConsensusConditional`，决议 `FinalDecision` = `conditional_approve`，`Resolution.Consensus.Conditions` 携带条件。报告生成（Commander.GenerateReport）可读取条件并呈现。

现有测试用 `approve()`/`reject()`（无 conditional），不受影响。

### 局限

条件**满足性**不自动判定（"团队是否有 2+ Rust 工程师"是语义事实，需外部数据/人工）。确定性代码只负责检测 conditional 投票、收集条件、产出 Conditional 结果--条件是否真正满足留给决议消费者（报告/人工）。这符合"LLM=语义，代码=规则"。

---

## 数据流

```
投票（含 conditional）-> ConsensusEngine.Evaluate
  -> 若 approval 倾向且含 conditional: ConsensusConditional{Conditions}
  -> 否则: 现有结果（conditional 重映射计数）
ConsensusConditional -> orchestrator -> Resolving -> Resolution{FinalDecision: conditional_approve, Consensus.Conditions}

非 conditional 2:1 分裂 -> ConsensusMajorityDissent -> Debating
  -> DebateEngine.BuildPacket
     -> finder.Find(claims) 空 -> 合成跨阵营 ClaimConflict 对
  -> DispatchReconsider(packet 含 ConflictingClaims) -> agent 针对性辩论
```

## 测试

- `debate/engine_test.go`：新增"无 agent 断言时合成跨阵营冲突对"测试；现有 `TestBuildPacket_*` 不受影响。
- `consensus/engine_test.go`：新增"含 conditional 的批准倾向返回 ConsensusConditional + 条件"测试；新增"rejection 倾向不受 conditional 影响"测试。
- `orchestration/orchestrator_test.go`：新增"ConsensusConditional 路由到 Resolving"测试（mock 投票含 conditional_approve）。

## 文件清单

- `domain/entity/resolution.go`：`ConsensusResult` 新增 `Conditions` 字段。
- `domain/consensus/engine.go`：`Evaluate` 检测 conditional、产出 `ConsensusConditional`。
- `domain/debate/engine.go`：`BuildPacket` 合成跨阵营冲突；`NilConflictFinder` 重命名说明。
- `domain/claim/conflict.go`：`NewNilConflictFinder` 保留别名，注释澄清非 no-op。
- `domain/orchestration/orchestrator.go`：`ConsensusConditional` 路由 + `finalDecision` 映射。
- 对应 `_test.go` 文件新增测试。
