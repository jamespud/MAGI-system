# DB 表补齐设计（§23）

> 设计日期：2026-07-14
> 关联：闭合 magi-design.md §23（数据库完整设计）
> 前置：s6（7 表）+ s7（3 表）= 10 表。设计要求 ~19 表。缺 9 表。

## 目标

补齐 9/19 缺失表，使核心查询字段关系化（设计 §23："不建议把所有内容都塞进 JSON 字段"）。

## 缺失表（9）

1. **magi_config**：MagiConfig 持久化（code/persona/objective_json/evidence_standard_json/model_ref_json/tools_json/loop_policy_json/reflection_policy_json/version）
2. **decision_task**：DecisionTask 独立表（canonical_question/decision_type/background/constraints_json/dimensions_json/information_needs_json/success_criteria_json/unknowns_json）
3. **magi_agent_checkpoint**：AgentState checkpoint（run_id/messages_json/step_count/token_used/phase）
4. **magi_tool_call**：ToolCallRecord（agent_run_id/tool_call_id/tool_name/arguments/valid/result/err/evidence_id/duration_ms）
5. **claim_evidence_relation**：Claim->Evidence supports 关系（claim_id/evidence_id/relation_type）
6. **claim_relation**：Claim->Claim contradicts 关系（claim_id_a/claim_id_b/relation_type）
7. **evidence_summary**：EvidenceSummary（agent_run_id/evidence_by_type_json/claims_json/ready）
8. **vote_utility_score**：Vote.UtilityScores 关系化（vote_id/dimension_code/score/evidence_ids_json/claim_ids_json/reasoning）
9. **magi_evaluation**：Evaluation 持久化（case_id + 全部 5 类指标字段）

## 实现

新建 `docker/atlas/migrations/magi_s8_tables.sql`（9 个 CREATE TABLE IF NOT EXISTS，MySQL 语法，与 s6/s7 风格一致）。Makefile `sync_db` 目标加 s8。

## 文件

- `docker/atlas/migrations/magi_s8_tables.sql`（新建）：9 表 DDL。
- `Makefile`：sync_db 加 s8。
