# Corroboration 后置重算设计（§12余）

> 设计日期：2026-07-14
> 关联：闭合 magi-design.md §12 最后一个 modifier
> 前置：Base/Directness/Recency/Extraction 已真实（先前批次）。Corroboration 在提取时不可获知（claim 未形成）。

## 目标

claim 形成后（gate 前）重算 Corroboration modifier：每条证据被多少 claim 引用 -> 越多越可信。闭合 §12 全部 5 modifier 真实。

## 设计

### RecomputeFinal 辅助函数

`reliability.go` 新增 `RecomputeFinal(s *entity.ReliabilityScore)`：用 5 modifier 加权重平均重算 Final（clamped [0,1]）。

### EvidenceLedger.RecomputeCorroboration

`ledger.go` 新增方法 `RecomputeCorroboration(summaryClaims []entity.EvidenceSummaryClaim)`：
1. 统计每条 EV-ID 被多少 claim 支持（含 ledger 已记录的 claims + summary 中尚未记录的 claims）。
2. 每条 evidence 的 `Corroboration = 0.5 + 0.1 * count`（capped 1.0，与 ComputeReliability 一致）。
3. 调 `RecomputeFinal` 更新 Final。

### 调用点

`agent_loop.go` 的 `ResponseEvidenceSummary` case：在 `gate.Evaluate` **之前**调用 `ledger.RecomputeCorroboration(pr.Summary.Claims)`，使门禁的 MinReliability 检查使用真实 Corroboration。

### nil-safe

无 summary claims 时 count=0，Corroboration=0.5（与当前默认一致）。

## 测试

- `ledger_test.go`（或 `reliability_test.go`）：创建 ledger + 2 EV + claims 支持 EV-1（2条），RecomputeCorroboration，断言 EV-1.Corroboration=0.7、EV-2=0.5、Final 重算。

## 文件

- `domain/evidence/reliability.go`：`RecomputeFinal`。
- `domain/evidence/ledger.go`：`RecomputeCorroboration`。
- `domain/runtime/agent_loop.go`：gate 前调用。
- 测试。
