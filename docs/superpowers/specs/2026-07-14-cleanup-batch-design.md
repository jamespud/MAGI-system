# 低优先级清理包设计

> 设计日期：2026-07-14
> 关联：闭合 §5（配置字段）、§19（errgroup）、§12（Recency）、NilConflictFinder 命名澄清

## 4 项独立小改

### 1. 配置字段填充（§5）
- `EvidenceSpec` 加 `MinReliability float64` + `RequireOwnCollected bool`；`toConfig` 填入 EvidenceStandard。
- `MagiSpec` 加 `PersonaDef PersonaDefSpec{SystemPrompt,Voice}` + `RiskPolicy RiskPolicySpec{MaxAcceptableRisk}`；`toConfig` 填入 MagiConfig.PersonaDef（非空时）+ RiskPolicy{Tendency, MaxAcceptableRisk}。
- `magi.yaml.example` 加可选字段示例。

### 2. NilConflictFinder 重命名
- 类型 `NilConflictFinder` -> `GraphConflictFinder`；`NewNilConflictFinder` 保留为别名返回 `*GraphConflictFinder`。

### 3. errgroup 替换 WaitGroup（§19）
- `dispatcher.go` 用 `errgroup.WithContext(ctx)`；goroutine 始终返回 nil（保持全部 agent 完成的行为）；`g.Wait()` 忽略返回的 error（结果在 slice 中）。

### 4. Recency=1.0（§12 余）
- `adapter.go` 的 NativeAdapter/RawObservationAdapter ComputeReliability 调用加 `Recency: 1.0`（新鲜证据）。

## 文件
- `main.go`：EvidenceSpec/MagiSpec 字段 + toConfig。
- `conf/magi.yaml.example`：可选字段。
- `domain/claim/conflict.go`：重命名。
- `domain/orchestration/dispatcher.go`：errgroup。
- `domain/evidence/adapter.go`：Recency。
- 对应测试。
