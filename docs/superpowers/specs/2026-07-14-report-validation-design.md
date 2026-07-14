# 验证完整性设计（范围 C - Item 5）

> 设计日期：2026-07-14
> 关联：闭合 magi-design.md §9（Validation Architecture）的 FinalReportData 验证缺口
> 前置审计：第二轮审计发现 Commander.GenerateReport 返回未校验的纯字符串，设计 §9 要求 FinalReportData 经 Schema 验证

## 目标

让 Commander 的最终报告成为经 TypedValidator 校验的结构化产物：LLM 输出 FinalReportData JSON，校验通过后确定性渲染为 markdown 报告。闭合 §9 中 FinalReportData 这一 LLM 结构化输出的验证缺口。

## 范围

仅 Item 5（FinalReport 校验）。不含 Item 6（Reflection 作为 LLM 结构化输出，需重构 reconsider 流程，另行处理）。

---

## 设计

### 新结构体 FinalReportData

新建 `domain/entity/report.go`：

```go
type FinalReportData struct {
    Decision   string   `json:"decision"`
    Summary    string   `json:"summary"`
    KeyReasons []string `json:"key_reasons"`
    Risks      []string `json:"risks"`
    NextSteps  []string `json:"next_steps"`
}
```

`Decision` 为最终决定（approve/reject/conditional_approve）；`Summary` 为一段摘要；`KeyReasons`/`Risks`/`NextSteps` 为要点列表。全部字段含 json 标签（ADR-003 要求）。

### Commander.GenerateReport 改造

`domain/service/commander.go`：
1. `Commander` 结构体新增 `reportVal *validation.TypedValidator[entity.FinalReportData]`，`NewCommander` 中构造（与 `taskVal` 同模式）。
2. `GenerateReport` prompt 改为要求 LLM 输出 FinalReportData JSON，逐字段描述（decision/summary/key_reasons/risks/next_steps）。
3. 校验：`reportVal.ValidateAndUnmarshal([]byte(resp.Content))`，失败则重试，最多 3 次（与 `Normalize` 重试模式一致）。
4. 校验通过后调用 `RenderReport(data)` 渲染为 markdown 字符串返回。3 次仍失败返回 error（编排器回退为 "report generation failed"）。

### 渲染函数 RenderReport

`domain/service/report.go` 新增：

```go
func RenderReport(d *entity.FinalReportData) string
```

确定性把 FinalReportData 渲染为 markdown：`# Decision Report` 标题 + Decision/Summary/Key Reasons/Risks/Next Steps 各节（列表项用 `- `）。空列表节省略。

### 测试适配

`domain/orchestration/orchestrator_test.go` 的 `newCommander` 脚本模型第 2 个响应当前为纯文本 `"decision report"`，会校验失败。改为合法 FinalReportData JSON（如 `{"decision":"approve","summary":"decision report summary","key_reasons":["r1"],"risks":[],"next_steps":[]}`）。

现有断言 `res.FinalReport == "decision report"`（TestOrchestrate_UnanimousApprove 等）改为检查渲染输出含摘要字段（如 `strings.Contains(res.FinalReport, "decision report summary")`），并断言非 "report generation failed"。

新增 `domain/service/commander_test.go`（或扩展）：focused 测试 GenerateReport 对合法 JSON 校验通过并渲染、对非法 JSON 重试后失败。

---

## 数据流

```
orchestrator GENERATING_REPORT -> commander.GenerateReport
  -> LLM 输出 FinalReportData JSON
  -> TypedValidator[FinalReportData] 校验（重试 3 次）
  -> RenderReport(data) -> markdown 字符串
  -> resolution.FinalReport
```

## 文件清单

- `domain/entity/report.go`（新建）：`FinalReportData` 结构体。
- `domain/service/commander.go`：`reportVal` 字段 + `NewCommander` 构造 + `GenerateReport` 改造（prompt + 校验 + 重试 + 渲染）。
- `domain/service/report.go`：新增 `RenderReport`。
- `domain/service/commander_test.go`（新建或扩展）：GenerateReport 校验测试。
- `domain/orchestration/orchestrator_test.go`：`newCommander` 脚本模型 JSON 适配 + 断言更新。
