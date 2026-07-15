# Reliability 真实 modifier 设计（§12，部分）

> 设计日期：2026-07-14
> 关联：闭合 magi-design.md §12（Evidence Adapter 与 Reliability）
> 前置：`FullReliabilityResolver` 已接入生产（先前批次），但 Directness/Extraction 用硬编码默认（0.7/0.8），Recency/Corroboration 也默认。Extraction 应区分 native/raw adapter，Directness 应区分来源类型。

## 目标

让 Extraction（native=1.0 / raw=0.3）与 Directness（来源类型）从真实上下文计算，而非硬编码默认。Base+Directness+Extraction 三 modifier 真实；Recency/Corroboration 因提取时不可获知保持默认（文档标注限制）。

## 范围

`domain/evidence/reliability.go` + `domain/evidence/adapter.go` + `agent_loop.go` 默认构造 + `adapter_test.go` 适配 + 测试。不改动 Recency/Corroboration（提取时无时间戳参照、claim 未形成）。

## 设计

### directnessFromSource

`reliability.go` 新增：

```go
func directnessFromSource(source entity.ToolSource) float64 {
	switch source {
	case entity.ToolSourceLocal, entity.ToolSourceCodeRunner:
		return 1.0 // primary/technical
	case entity.ToolSourcePlugin:
		return 0.8
	case entity.ToolSourceWorkflow:
		return 0.7
	case entity.ToolSourceKnowledge:
		return 0.6 // secondary
	default:
		return 0.7
	}
}
```

`FullReliabilityResolver` 的 `Directness` 从 `0.7` 改为 `directnessFromSource(b.Source)`（注册表 fallback 也用真实 Directness）。

### adapter 直接调 ComputeReliability

`adapter.go`：`NativeAdapter`/`RawObservationAdapter` 去掉 `resolver` 字段，`Extract` 直接构造 `ReliabilityInput` 调 `ComputeReliability`：

- NativeAdapter：`ExtractionConfidence: 1.0`（确定性结构化解析）。
- RawObservationAdapter：`ExtractionConfidence: 0.3`（原始回退）。
- 两者 `Directness: directnessFromSource(tool.Binding.Source)`。
- `NewNativeAdapter()` / `NewRawObservationAdapter()` 无参。

### agent_loop 默认构造

`NewAgentLoop` 中默认 adapter 构造从 `NewNativeAdapter(evidence.FullReliabilityResolver())` 改为 `NewNativeAdapter()`（Raw 同理）。注册表仍传 `FullReliabilityResolver()`（fallback 用）。

### 限制

- Recency：提取时无时间戳参照（工具结果不带时间戳），保持默认 0.5。
- Corroboration：证据提取时 claim 未形成，无法计数，保持默认 0.5。
- 这两项需后置重算（gate 或 ledger 阶段），另行处理。

## 测试

- `reliability_test.go`：`directnessFromSource` 各来源返回值。
- `adapter_test.go`：NativeAdapter 产出 `Reliability.Extraction == 1.0`；RawObservationAdapter `Extraction == 0.3`。
- 现有 `TestFullReliabilityResolver`、`TestAgentLoop_UsesFullReliabilityResolver`、`TestAgentLoop_ReliabilityFromBinding` 仍通过（Directness 非零、Final != Base、门禁通过）。

## 文件清单

- `domain/evidence/reliability.go`：`directnessFromSource` + `FullReliabilityResolver` 用之。
- `domain/evidence/adapter.go`：adapter 去 resolver、直接 ComputeReliability、设 Extraction。
- `domain/runtime/agent_loop.go`：默认 adapter 构造无 resolver。
- `domain/evidence/reliability_test.go` / `adapter_test.go`：新增 + 适配。
