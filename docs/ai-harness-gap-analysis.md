# MAGI 作为全面 AI Harness 的能力缺口与缺陷分析

> 分析日期：2026-08-16
> 范围：全仓库（backend ~29k 行 Go / 265 文件；frontend ~6.8k 行 TS/TSX；含 server、application、domain、adapter、bootstrap、docker、docs）
> 验证基线：`go test ./...` 33 个包全过；`go vet ./...` 干净；`vitest` 27 文件 / 140 用例全过
> 结论先行：MAGI 在"**证据驱动的多智能体决策引擎**"这一垂直定位上是成熟且高质量的；但要升格为"**全面 AI harness**"，横向能力（通用任务执行、会话、知识管理、身份体系、多模型编排、评估闭环运营化）存在系统性缺口，并伴随若干 P0/P1 级实现缺陷。

---

## 一、项目现状快照

| 维度 | 现状 |
| --- | --- |
| 定位 | 证据驱动的三智能体（Melchior/Balthasar/Casper）决策引擎，确定性 FSM 编排（LLM=语义，Go=规则） |
| 核心循环 | 调查(tools→evidence→claims) → 证据门 → 投票 → 共识检查 → 辩论 → 反思 → 复议 → 报告 → 记忆 → 评估 |
| 分层 | `domain`（entity/evidence/consensus/debate/claim/runtime/orchestration/memory）→ `application`（决策/评测/记忆/审批/限额/管理员）→ `adapter`（GORM/MySQL、MCP、RAG(Milvus+ES)、CodeRunner、Tavily）→ `server`（Hertz + SSE + OTel） |
| 韧性 | 事件溯源（MagiEvent ADR-008）、checkpoint 断点续跑、durable job + 租约、失败重试、上下文压缩、多租户配额/预算/工具限额（跨实例共享 DB） |
| 质量 | 全部单测/集成测试通过；e2e 覆盖 auth/生命周期/数据集评测/插件绑定/周期任务/assistant/metrics/admin |
| 交付 | Docker compose（MySQL+Milvus+ES+nginx）、Makefile 目标齐全、部署与多租户边界文档齐全 |

### 亮点（保持并发扬）

1. **证据台账是"脊柱"**：工具结果 → EvidenceRecord → Claim → Vote 全链确定性；可靠性是加权公式而非 LLM 自报数（`domain/evidence/reliability.go`）。
2. **角色=可执行边界**：RolePolicy 硬编码审批阈值（技术分/残差风险/机会分），RoleGate 在结构上挡掉越界决策（`domain/evidence/role_gate.go`）。
3. **事件驱动+断点续跑**：跨重试/重启按 case/agent/round 恢复而非从头再来（`domain/runtime/checkpoint`）。
4. **多租户治理**：并发、token、成本、工具配额全走共享 DB，多副本下仍成立（`run_counter.go`/`scheduler_lock`）。
5. **防注入设计**：工具输出按"不可信数据"框定、EV-ID 真实性校验、secret 脱敏（`application/redact`）。

---

## 二、"全面 AI Harness" 能力矩阵对照

> 参考定义：**Agent = Model + Harness**。Harness 不是 Prompt 或应用框架本身，而是围绕模型构建的完整运行时环境与控制基础设施，覆盖上下文/记忆、前馈引导/反馈传感器、工具连接/沙箱、权限护栏、可观测性/断点续传，以及评估回归六大部分。
> 参考能力域来自主流 AI harness（LangGraph、AutoGen、CrewAI、Claude Code、OpenHands 等）的功能空间。✅=已实现且较完整，🟡=部分实现/有缺陷，❌=缺失。
> 更新记录：2026-08-18 补充 Prompt / Agent Framework / Agent Harness 概念边界对照与六大核心组件级对照，并按 AI Harness 6 大核心组件（Context & Memory / Feedforward & Sensors / Tool & Sandbox / Guardrails & Permissions / Observability & Checkpoint / Evaluation）复核；D1–D17 已修复项状态同步为 ✅/🟡，并新增通用任务执行、反馈传感器、设计模式等缺口行。

### 2.1 概念边界与组件级总览

按 `Agent = Model + Harness` 校准：Model 是概率性推理大脑；Harness 是智能体的运行时躯干、感官、手脚与规则边界，在概率性模型推理和确定性外部系统之间充当控制与反馈闭环。概念边界对照如下：

| 对照对象 | 定位 | 工作方式 | 类比 | 与 MAGI 的关系 |
| --- | --- | --- | --- | --- |
| Prompt Engineering | 给模型的文本指令输入，解决“模型怎么想” | 编写和调优 System/User Prompt | 乐谱 | MAGI 的版本化 prompt 是前馈引导的一部分，不构成 Harness 全部 |
| Agent Framework | 开发 AI 应用所需的 SDK/组件库，解决“代码怎么组装” | 在 Python/TS 中调用封装好的模型、工具与执行 API | 乐器组件库 | MAGI 不是 LangChain/CrewAI 这类框架复用问题，而是已落地的运行时服务 |
| Agent Harness | 智能体在真实环境中的完整运行时与控制基础设施，解决“如何可靠、安全地完成任务” | 配置与编排上下文、记忆、沙箱、工具、传感器、权限和反馈循环 | 音乐厅声学环境、指挥家控制台与录音设备 | MAGI 的评估目标：以确定性 Go 控制面驾驭概率性 LLM，并横向补齐通用 Harness 能力 |

六大核心组件的组件级状态如下（组件级状态用于总体判断，不计入下方明细行状态统计）：

| Harness 核心组件 | 定义拆解 | MAGI 已覆盖 | 仍未覆盖 / 待补齐 | 组件状态 |
| --- | --- | --- | --- | --- |
| **1. Context & Memory** | 短期工作记忆、上下文压缩、长期记忆持久化、跨会话水合、文件/状态树 | agent loop 工作记忆、context compaction、case 记忆投影、Milvus+ES+MySQL 混合 RAG、知识库导入、记忆编辑/删除/标注与索引同步 | 更通用的任务级文件/状态树 | 🟡 |
| **2. Feedforward & Sensors** | 事前规范/模板/架构约束，事后编译、Lint、测试、语义审查并回喂自愈 | 版本化 prompt、角色契约/RoleGate、FSM 编排、反思/复议/LLM judge 等推理型反馈、内置 `check_output` 计算型反馈传感器 | 更完整外部传感器（代码 linter/编译/单测，依赖 MCP/coderunner）与自我改进 Harness | 🟡 |
| **3. Tool & Sandbox** | MCP/API 连接器、文件/代码库/浏览器/DB 工具，Docker/WASM/MicroVM 隔离 | Tavily/Brave 搜索插件、MCP stdio/http、CodeRunner WASM 沙箱、工具策略、只读 DB 查询工具 | 文件/代码库/浏览器工具；Docker/MicroVM 沙箱 | 🟡 |
| **4. Guardrails & Permissions** | 最小权限、HITL 审批、策略执行、租户/身份治理、敏感操作拦截 | API Key 认证、资源所有权校验、审批门、配额/预算/工具限额、注入防护与脱敏、admin/operator/user 细粒度角色路由 | SSO/OAuth/自助注册、更完整的敏感数据分级与审计 UI | 🟡 |
| **5. Observability & Checkpointing** | Thought/Action/Observation 追踪、成本观测、检查点休眠/唤醒 | OTel、Trace ID、事件流、Prometheus 指标、case/agent/round checkpoint、durable job、前端 trace 视图、默认 Prometheus/Grafana/Alertmanager 栈、任务级 pause->hibernate->wake | 无 | 🟡 |
| **6. Evaluation & Regression** | 自定义/行业基准、指标看板、CI 回归、线上 golden、Harness 变更评估 | 数据集评测、benchmark/stability/regression gate、LLM judge、GitHub Actions backend/frontend/ops CI 门禁、内置行业基准集与聚合看板 | 线上 golden、自动回归运营闭环 | 🟡 |

### 2.2 明细能力矩阵

| 能力域 | 子能力 | 状态 | 说明 / 证据 |
| --- | --- | --- | --- |
| **决策编排** | 确定性 FSM / 硬投票 / 辩论 / 反思 / 复议 / 死锁 | ✅ | `domain/orchestration/orchestrator.go`，~20 状态 |
| | 角色契约（技术/风险/机会）门控 | ✅ | `evidence/role_gate.go`、`entity/role.go` |
| **Agent 执行** | 手写 agent loop、工具循环、终止策略、上下文压缩 | ✅ | `domain/runtime/agent_loop.go`、`domain/runtime/compaction.go` |
| | **多模型多样性**（per-role 独立模型） | ✅ | `magi.<role>.model` / `commander.model` / `judge.model` 逐字段覆盖全局、未设字段继承（D5，`bootstrap/config.go`），含 fail-fast 校验 |
| | 多供应商路由/降级（failover） | ✅ | `model.providers` / per-role `model.providers` 有序 provider 链；Build / Generate / 工具绑定失败自动尝试下一家，Stream 可在首块前 failover；取消的请求不重试；`magi_model_failovers_total` 观测；fallback 按实际 provider 单价核算 usage 成本（`adapter/model_failover.go`） |
| | **长任务规划 / 动态 subagent 派生** | ❌ | 三 agent 固定（`entity/role.go`），无 subagent 派生框架、无任务规划层 |
| **工具生态** | Tavily/Brave web_search / MCP(stdio+http) / CodeRunner(WASM) | ✅ | `adapter/web_search_executor.go`、`adapter/tavily_tool.go`、`adapter/mcp/mcp.go`、`coderunner_adapter.go` |
| | 原生文件/代码库/浏览器/DB 工具 | 🟡 | `db_tool` 内置只读 `db_query`：单条 SELECT + 只读事务 + 行数/长度/超时限制 + 写语句拒绝（`adapter/db_query_tool.go`）；文件/代码库/浏览器工具仍只能靠外部 MCP |
| | Docker / MicroVM 沙箱 | ❌ | 仅 WASM 沙箱（CodeRunner），无容器 / MicroVM 隔离 |
| | 多搜索引擎插件化 | ✅ | `search.providers` 支持 Tavily/Brave 有序配置；本地 `web_search` 统一 provider 中立结果结构，失败自动切换并暴露 `magi_web_search_failovers_total`；`tavily.api_key` 兼容并作为 primary |
| **记忆/知识** | case 记忆投影 + RAG（Milvus+ES+MySQL 混合 RRF） | ✅ | `adapter/rag/` |
| | 语义检索接入 UI | ✅ | `/api/v1/memory` 先 Milvus+ES 语义检索（限定 `case_memory` 源）再 LIKE 兜底、去重、Owner 过滤（D6，`application/memory/service.go`） |
| | 文档/URL 知识导入管理 | ✅ | `POST/GET/DELETE /api/v1/knowledge`，文档经 `StoreDocument` 索引进 RAG（独立 `knowledge_doc` 命名空间）（D7，`application/knowledge/`、`server/router.go`） |
| | 长期记忆编辑/删除/标注 | ✅ | `PATCH/DELETE /api/v1/memory/:id` 支持摘要/结论/经验/标注/标签编辑、校验与 Owner 隔离；更新/删除同步 RAG case-memory chunks，失败回滚 SQL 投影；前端 Memory 页支持标注、标签与删除 |
| **会话** | 单次 question→decision（`/assistant`） | ✅ | `application/assistant/service.go` |
| | **持久对话线程/多轮追问/对话内上下文** | ✅ | `magi_conversation`/`magi_conversation_message` + owner-scoped List/Get/Delete API；`POST /assistant` 支持 `conversation_id`，追问水合最近历史与关联 case resolution；前端 Conversations 页支持线程列表、追问、删除与 case 跳转 |
| **身份与多租户** | API-Key 认证（常量时间比较）+ 按用户所有权 | ✅ | `application/auth`、`server/auth.go` |
| | Web UI 认证通道 / 登录页 | ✅ | `X-API-Key` header 注入 + 401 `magi:unauthorized` 事件 + `/login` 页（D1，`frontend/src/api/client.ts`、`pages/Login.tsx`、`api/stream.ts` fetch-streaming） |
| | 用户管理 / 密钥轮换（DB 用户 + SHA-256 存储） | ✅ | `/admin/users`、`/admin/keys/:id/revoke` + `rotate`、`/me`；明文仅展示一次（D8，`application/users/`、`pages/Users.tsx`） |
| | SSO / OAuth / 自助注册 / 细粒度 RBAC | 🟡 | 角色体系扩展为 admin/operator/user（`entity.Role*`、`IsValidRole`）；`RequireAnyRole` 中间件把运营端点（usage/prompts/benchmark seed/eval summary）开放给 operator，用户/密钥管理保持 admin-only（`server/auth.go`、`server/router.go`）；SSO/OAuth/自助注册仍缺 |
| | 按用户配额/预算/限额 | ✅ | `application/admin`、`run_manager.go` |
| **安全** | 沙箱/审批门/注入防护/脱敏/审计事件 | ✅ | `toolpolicy`、`approval`、`redact` |
| | HTTP 通用限流 | ✅ | `http_rate_limit` 配置 + 中间件（按用户 ID，open 模式按 IP），429 + Retry-After（D13，`server/ratelimit.go`） |
| | 敏感数据分级/租户隔离审计 | ✅ | case/memory 列表查询下沉到 DB（`WHERE user_id=?` + LIMIT/OFFSET 分页），e2e 断言跨用户隔离（D2，`adapter/repository.go`） |
| | **计算型反馈传感器**（Linter / 编译器 / 单测回喂 → AI 自愈） | ✅ | 内置 `check_output` 工具：模型可对自己的 JSON 输出运行 JSON Schema lint 与字段约束规则（eq/ne/gt/gte/lt/lte/contains），违规以 ToolMessage 回喂并自愈（`domain/runtime/feedback.go`、`adapter/feedback_tool.go`、`feedback_tool` 配置默认开启）；代码 linter/编译/单测回喂仍可经 MCP/coderunner 扩展 |
| | **NLAH**（自然语言驱动控制规范） | 🟡 | 提示词注册表可版本化/可编辑（D12，`domain/prompt/`）是雏形；FSM/投票/RoleGate 仍为硬编码 Go 规则 |
| | **自我改进 Harness**（分析失败 → 自动改 AGENTS.md / 规则） | ❌ | 无 |
| **评估闭环** | 数据集评测 / benchmark / stability / regression gate / LLM judge | ✅ | `application/dataset`、`evaluation`、`judge` |
| | 持续评估 / CI 集成 / 线上 golden / 自动回归 | 🟡 | `.github/workflows/ci.yml` 提供 backend gofmt/test/race/vet、frontend tsc/vitest/build、ops scripts 语法与备份恢复 dry-run 门禁；线上 golden 与自动回归触发仍缺 |
| | 标准基准集 / 评测指标看板 | ✅ | `POST /admin/benchmarks/seed` 幂等植入内置 "MAGI Decision Sanity Suite"（跨 DB/采购/SRE/安全/战略的 approve/reject/conditional 用例，`application/dataset/service.go`）；`GET /admin/eval/summary` 聚合总运行/成功/失败、平均准确率/稳定性、回归失败数与按数据集/最近运行明细；Benchmark 页评测看板卡片 + seed 按钮 |
| **可观测性** | OTel span / X-Trace-ID / 事件流 / Prometheus /metrics | ✅ | `application/tracing`、`server/metrics.go` |
| | 前端 trace 可视化 | ✅ | Replay 页 Trace 模式：按 run/agent 分泳道的时间轴可视化（事件按时间定位、按类型着色）、事件计数/运行数/智能体数/错误数统计、点击 marker 查看事件详情（`frontend/src/pages/Replay.tsx`、`api.getTrace`） |
| | 告警默认部署 / 看板栈 | ✅ | `docker/docker-compose-monitoring.yml` 一键启动 Prometheus（抓取 `magi-server:8080/metrics`）+ Alertmanager + Grafana（自动 provisioning 数据源与 MAGI Overview 看板）；告警规则单一来源 `deploy/prometheus-alerts.example.yml`；`make monitoring-up` |
| | **Hibernate-and-Wake** 长任务休眠/唤醒 | ✅ | `POST /cases/:id/pause|resume`：暂停取消 worker 并持久化 PAUSED + PausedFromStatus，durable job 置为 paused（不重试不计活跃）；唤醒恢复暂停前状态、重新入队并重启 worker（`RunManager.Pause/Resume`、`caseRepo.UpdatePaused`、`decision_job.MarkPaused/ResumeQueued`）；前端工作台暂停/唤醒按钮 |
| **UI** | 决策工作台/证据图/时间线/审批/评测/数据集/模板/benchmark/history/memory/tools/settings | ✅ | `frontend/src/router.tsx`，14 个功能页 |
| | 管理员用量可视化 / 用户管理 / 知识库管理 / 配置管理 | ✅ | `/me/usage` + `/admin/usage` 用量卡片（D9）、Users 页（D8）、Knowledge 页（D7）、prompt 管理（D12） |
| | 国际化 | ✅ | `frontend/src/i18n/` en/zh + 语言切换器（D14），主要页面字符串抽取 |
| **部署运营** | Docker compose / readiness / config fail-fast / 多实例 worker | ✅ | `docker/`、`bootstrap/config.go` |
| | K8s/Helm 清单、横向扩容文档 | ✅ | `deploy/magi/` Helm chart（Deployment/Service/ConfigMap/Secret/Ingress/HPA/PDB、SSE-aware nginx、readiness/liveness probes）；`deploy/k8s/magi.yaml` 渲染清单；`deploy/magi/README.md` 与 `DEPLOYMENT.md` 提供镜像、外部密钥、外部依赖与多副本扩容说明 |
| | 跨实例 SSE 实时推送 | ✅ | SSE + DB 轮询兜底（`EventRepository.ListAfter`，按时间戳增量拉取 + ID 去重）（D4，`server/sse.go`） |
| | 数据导出 / 备份方案 | ✅ | case/memory/eval 全量导出（D15，`server/handler/export.go`）；`scripts/backup.sh` 输出 MySQL 单事务 dump + Milvus/ES/etcd/MinIO 卷快照 + SHA256 清单并支持保留策略；`scripts/restore.sh` 提供安全校验、dry-run、数据库重建与 RAG 卷恢复 |

---
## 三、现有功能实现缺陷清单

### P0（正确性 / 安全 / 多租户）

> **状态：2026-08-16 已全部修复并验证**（backend `go test ./...` 全通过 + `go vet` 干净；frontend vitest 全通过；e2e 新增跨用户隔离与分页断言）。

**D1. 前端无认证通道，多租户 Web UI 不可用** — ✅ 已修复：`frontend/src/api/client.ts` 新增 API-Key 存取（localStorage）+ `X-API-Key` header 注入 + 401 统一 `magi:unauthorized` 事件；新增 `/login` 页（`frontend/src/pages/Login.tsx`）与 `authStore`；AppShell 监听 401 跳转登录；SSE 改为 fetch-streaming 以携带认证头（`frontend/src/api/stream.ts`）；Settings 页新增登录/登出卡片。
- 位置：`frontend/src/api/client.ts`（`request()` 只发 `Content-Type`）、`backend/server/auth.go`（支持 `Authorization: Bearer` / `X-API-Key`）
- 问题：后端已实现完善的 API-Key 认证，但前端从不带认证头、也没有登录/API-Key 输入页。一旦 `auth.enabled=true`，浏览器 UI 所有 `/api/v1/*` 请求全部 401，多租户只能纯 API 使用。
- 影响：宣称的"multi-tenant API"与 Web UI 脱节；UI 实际只能跑在 open 模式。

**D2. case 列表全表加载 + 内存内越权过滤** — ✅ 已修复：`port.CaseListFilter` 可选接口 + `adapter` 的 `ListForUser`（SQL `WHERE user_id` + LIMIT/OFFSET 分页）；`GET /cases` 支持 `limit`/`offset` 并移除处理器内内存过滤；未实现该接口的旧/测试仓库保留兼容回退（`decision.Service.ListScoped`）。
- 位置：`backend/adapter/repository.go`（`caseRepo.List` → `Order("created_at DESC").Find(&models)` 无 WHERE、无分页）、`backend/server/handler/decision.go`（`List` 循环 `AuthorizeCase`）
- 问题：把所有用户的全部 case 行读进内存再逐条过滤。数据量增长后是 O(N) 全表扫描 + 大内存 + 大响应体；且"先拉全量再过滤"在多租户下违背最小数据读取原则。前端 `PaginatedSection` 的分页只是客户端 slice。
- 同类问题：`memory` 搜索也是先查后按用户过滤（有 LIMIT 缓解）。

**D3. RAG 检索错误被静默吞掉** — ✅ 已修复：`domain/memory/context_builder.go` 检索失败时显式 log + 新增 `magi_memory_retrieval_failures_total` 指标 + 发布 `MEMORY_RETRIEVAL_FAILED` 事件（container 已接线）。
- 位置：`backend/domain/memory/context_builder.go`（`if err == nil { ... }`）
- 问题：`knowledge.Retrieve` 出错时直接降级为空上下文，无日志、无事件、无指标。长期记忆在 Milvus/ES 故障时会静默失效，排查成本极高（与该用户之前诊断的 Codex 日志风暴同类隐患：静默失败最危险）。

### P1（架构 / 能力完整性）

**D4. SSE 实时推送是进程内 broker，多实例部署丢失实时性**
- 位置：`backend/server/broker.go`（`stored`/`subscribers` 均为内存 map）
- 问题：case 运行在实例 A，事件只推到连接实例 A 的订阅者；连到实例 B 的用户只能靠 DB 事件回放补历史（`SSEHandlerWithHistory`），拿不到实时增量。与 README"multi-instance operation"声明存在落差。

**D5. 三个 agent + commander + judge 共用同一个模型**
- 位置：`backend/bootstrap/container.go`（`provideMagiConfigs`/`provideCommander`/judge 全部取 `cfg.Model`）
- 问题：设计文档把 "Model Diversity（可选）" 列为认知多样性的一环，但生产实现没有 per-role model 配置。三 agent 同模型 = 共享盲区与偏差，"对抗/独立推演"的独立性打折扣。

**D6. 记忆搜索 UI 用的是 SQL LIKE 而非 RAG**
- 位置：`backend/adapter/repository.go memoryRepo.Search`、`backend/server/handler/memory.go`、`frontend/src/pages/Memory.tsx`
- 问题：`/api/v1/memory?q=` 走 `question_summary/context_summary/resolution LIKE` 词面匹配；耗资搭建的 Milvus+ES 混合 RAG 只服务于 agent 上下文构建（`ContextBuilder`）。用户在前端 Memory 页得到的不是语义检索。

**D7. 无知识库文档管理 / 导入能力**
- 位置：全仓库无 upload/multipart/knowledge POST；`port.KnowledgePort.Store` 仅被 case 记忆投影调用
- 问题：harness 的长期知识来源单一（仅历史 case），无法上传文档/URL/批量语料，也无法管理、删除、标记知识条目。对"全面 harness"这是最明显的横向缺口之一。

**D8. 无用户体系：静态 API-Key、无 SSO/OAuth/注册/密钥轮换/管理 UI**
- 位置：`backend/application/auth/service.go`、`backend/bootstrap/config.go Auth.APIKeys`
- 问题：key 静态写在 yaml（支持 hash），无自助签发、无过期、无撤销、无角色管理 UI。`/admin/usage` 是唯一管理接口且无 UI。

### P2（完善度 / UX / 工程化）

**D9. Settings 页是静态信息页**（`frontend/src/pages/Settings.tsx`）：无 admin usage 可视化、无用量/预算查看、无配置查看。`admin/usage` API 存在但无前端页面。

**D10. case 列表无服务端分页**：`GET /cases` 全量返回；前端分页是客户端 slice（`PaginatedSection` pageSize=10）。

**D11. 前端 Pinned / Archived 是死 UI**：`frontend/src/components/layout/LeftNav.tsx` 中 Archived 过滤 `() => false`，Pinned 依赖 `CaseSummary.pinned`，但后端模型（`entity/DecisionCase`）没有 pinned/archived 字段，也无 pin/archive API。分区永远为空。

**D12. 提示词硬编码在 Go 代码**（`domain/runtime/prompt.go`、`domain/service/commander.go`）：无提示词版本管理、无 A/B、无按环境切换。对依赖评测调优的 harness 是硬伤。

**D13. 无 HTTP 层通用限流**：只有 run 并发 + tool quota；公共 API（含 `/assistant`、`/cases`）无 RPS 限制。

**D14. 无 i18n**：UI 全英文；中文用户（设计文档本身是中文）使用成本高。

**D15. 数据导出/备份弱** - ✅ 已修复：case/memory/eval 全量导出已实现；`scripts/backup.sh` / `scripts/restore.sh` 提供校验、保留策略、MySQL 逻辑备份、RAG 数据卷快照与 destructive-restore 防护（DEPLOYMENT.md 提供灾备流程）。

**D16. 缺 CRUD 完整性**：无 `DELETE /cases/:id`、无 `DELETE /datasets/:id`、无 `DELETE /evaluation` 等；数据集删除只能删 item。

**D17. 可观测性默认弱**：OTel 默认 log sink（`application/tracing`），`/metrics` 无认证（文档已警示），告警仅 example 文件，无内置 Grafana/Jaeger 栈。

---

## 四、对照"全面 AI Harness"的缺口（Roadmap 输入）

按用户视角的优先级排序：

1. **通用任务执行层（最重要）**：MAGI 是"决策引擎"，不是"任务执行 harness"。缺文件/代码库编辑、shell、浏览器/computer-use、长任务规划与 subagent 派生。这是从"决策产品"走向"AI harness 平台"的分水岭。
2. **会话与多轮追问（已补齐）**：`/assistant` 已升级为持久会话入口，支持 conversation/thread、追问、历史与既往结论水合、前端线程管理。
3. **身份与访问管理**：登录/SSO、用户自助、密钥管理、角色粒度（当前只有 admin/非 admin）、审计 UI。
4. **知识管理**：文档导入/URL 抓取/语料管理/删除/版本，并把语义检索接到 UI 与 `/assistant`。
5. **多模型编排**：per-agent/commander/judge 独立模型配置与多供应商自动降级已完成；剩余集中式模型参数管理。
6. **评估运营化**：CI 门禁、内置基准集与聚合看板已完成；剩余线上 golden、自动回归与 prompt 版本运营。
7. **可观测性**：前端 trace 视图、默认 Prometheus/Grafana/Alertmanager 看板栈已完成；剩余默认 trace 落库与告警通道（邮件/IM）接入。
8. **运营与交付**：K8s/Helm 与备份/恢复已完成；剩余 i18n 覆盖度与灾备演练自动化。

---

## 五、分阶段建议

### Phase 1（补齐 P0，约 2 周）
- D1 前端认证通道：登录页 + API-Key 输入 + `X-API-Key` header 注入 + 401 统一跳转。
- D2 case/memory 查询下沉到 DB（`WHERE user_id=?` + 游标分页），删除内存过滤。
- D3 RAG 检索失败改为显式日志 + `MEMORY_RETRIEVAL_FAILED` 事件 + 指标。
- D11 删掉 Pinned/Archived 死 UI 或补 pin/archive 字段+API。

### Phase 2（横向能力第一波，约 4-6 周）
- D4 跨实例实时：SSE 改 DB-backed 游标轮询或引入 Redis pub/sub。
- D5 per-role 模型配置与多供应商 failover（已完成）：`magi.<role>.model` 覆盖全局，`model.providers` / role providers 按序自动降级。
- D6/D7 知识管理：upload API + 文档入库走现有 RAG 管线 + Memory UI 接入语义检索。
- 记忆治理（已完成）：memory PATCH/DELETE、标注/标签、RAG 索引同步与前端管理。
- 会话模型（已完成）：conversation/thread + 多轮追问，`/assistant` 已升级为会话式，前端提供 Conversations 页面。

### Phase 3（平台化，约 8-12 周）
- 身份体系（SSO/OAuth/用户自助/密钥轮换）。
- 通用任务执行层（基于现有 agent loop 扩展 code/file/shell 工具 + 审批门）。
- 评估运营化（线上 golden + 看板 + prompt 运营；CI 门禁已完成）。
- 可观测性栈 + 灾备演练自动化（K8s/Helm 与备份/恢复已完成）。

---

## 六、结论

MAGI 的**决策核心是高质量、可测试、可解释**的，在"证据驱动多智能体决策引擎"这个垂直定位上几乎没有明显的规则漏洞（测试全绿、设计文档与实现高度一致）。但作为"**全面的 AI harness**"，它当前更像一个**专注于决策的专用 harness**，而非**通用的任务执行平台**：

- 当前最关键的横向缺口是：**通用任务执行能力、身份与细粒度权限、计算型反馈传感器、评估运营化、默认可观测性栈**。
- D1–D17 已完成修复；后续重点从实现缺陷转向平台能力缺口：通用工具/沙箱、IAM/RBAC、反馈传感器、线上评估与可观测性栈。

知识管理、会话、多模型、CI、K8s/Helm 与备份恢复已经补齐；下一步应以通用任务执行、IAM/RBAC、反馈传感器与评估运营化为主线继续升级。

---

## 附：P1 缺陷修复记录（2026-08-16，分支 `codex/p1-harness`）

P1 级缺陷（D4–D8）已全部实现并随测试提交，提交均通过 `go build` / `go vet` / `go test ./...`（37 包）与前端 `vitest`（27 文件 / 140 用例）：

| 缺陷 | 修复 | 提交 |
| --- | --- | --- |
| D4 跨实例 SSE 丢实时性 | SSE 增加 DB 轮询兜底（新增 `EventRepository.ListAfter`，按时间戳增量拉取副本持久化事件，按 ID 去重；含真实 HTTP SSE 集成测试） | `25e37dd` |
| D5 全角色共用单模型 | 支持 `magi.<role>.model` / `commander.model` / `judge.model` 逐字段覆盖全局模型（未设字段继承全局），含 fail-fast 校验 | `dd35f64` |
| D6 记忆搜索走 SQL LIKE | `/api/v1/memory` 先走 Milvus+ES 语义检索（限定 `case_memory` 源），再以 LIKE 兜底未索引投影；检索错误优雅降级，Owner 过滤覆盖语义结果 | `bd8c378` |
| D7 无知识库 | 新增用户知识库：`POST/GET/DELETE /api/v1/knowledge`，文档经 `StoreDocument` 索引进 RAG（独立 `knowledge_doc` 命名空间），失败保留文档并记录错误；前端 Knowledge 页 | `a676ec0` |
| D8 无用户体系 | DB 用户 + API Key 生命周期：`/admin/users`、`/admin/keys/:id/revoke|rotate`、`/me`；密钥仅存 SHA-256，明文仅展示一次；auth 中间件静态密钥优先、DB 密钥按哈希认证；前端 Users 管理页 + Settings 显示当前身份 | `6fc4712` |

> 以上修复与 `docs/ai-harness-gap-analysis.md` 第三、四、五章逐条对应；P0 缺陷（D1–D3）与 Phase 2/3 缺口不在本次"完成 P1"目标范围内。

---

## 附：P2 缺陷修复记录（2026-08-16，分支 `codex/p2-harness`）

P2 级缺陷（D9–D17）已全部实现，通过 `go build` / `go vet` / `go test ./...`（38 包）与前端 `tsc --noEmit` / `vitest`（28 文件 / 146 用例）：

| 缺陷 | 修复 | 关键文件 |
| --- | --- | --- |
| D9 Settings 静态页/无用量查看 | 新增 `GET /me/usage`（当前用户累计 cases/runs/tokens/cost + 预算上限与超限标志）；Settings 页展示我的用量，admin 角色展示 `GET /admin/usage` 全用户表格；新增 `AgentRunRepository.CountByUser` | `application/admin/service.go`、`server/handler/admin.go`、`frontend/src/pages/Settings.tsx` |
| D10 case 列表无服务端分页 | `GET /cases?page=&page_size=` 在 DB 内按 `user_id` 过滤 + LIMIT/OFFSET + 总数（`CaseRepository.ListPaged`），返回 `{cases,total,page,page_size}`；前端 caseStore 分页拉取 + LeftNav “Load more” | `adapter/repository.go`、`server/handler/decision.go`、`frontend/src/stores/caseStore.ts`、`LeftNav.tsx` |
| D11 Pinned/Archived 死 UI | `DecisionCase` 增加 `pinned/archived`，`PATCH /cases/:id` 更新标志；前端 CaseSummary/Case 增加字段，LeftNav Pinned/Running/Completed/Archived 分区真实生效，CaseHeader 增加置顶/归档/取消归档按钮 | `domain/entity/case.go`、`adapter/model.go`、`server/handler/decision.go`、`frontend/src/components/workspace/CaseHeader.tsx` |
| D12 提示词硬编码 | 版本化提示词注册表：`prompt_template` 表 + `PromptRepository/Provider`，启动从内置默认种子，`GET/PUT /admin/prompts/:key`、`POST /admin/prompts/:key/restore`；Commander（normalize/report）与 agent workflow 模板运行时经 provider 加载、内置兜底 | `domain/prompt/`、`adapter/prompt_repository.go`、`domain/service/commander.go`、`domain/runtime/prompt.go` |
| D13 无 HTTP 限流 | `http_rate_limit` 配置 + 中间件（按用户 ID，open 模式按 IP），429 + Retry-After，含单测 | `server/ratelimit.go`、`bootstrap/config.go`、`server/ratelimit_test.go` |
| D14 无 i18n | 内置轻量 i18n（无新依赖）：`frontend/src/i18n/`，en/zh 字典 + `t()/useT()/useLang()`，TopNav 语言切换器（localStorage 持久化），导航/侧栏/工作台/记忆/工具/审批/历史/模板/基准/评测/数据集/设置等主要页面完成字符串抽取 | `frontend/src/i18n/`、`frontend/src/components/layout/TopNav.tsx`、各 pages |
| D15 数据导出/备份弱 | `GET /cases/:id/export`（case+resolution+report+agents+evidence+claims+votes+tool_calls+events+memory 全量 JSON）、`GET /memory/export`（本人全部记忆）、`GET /evaluation/:id/export`（评测+judge）；`scripts/backup.sh` / `scripts/restore.sh` 覆盖 MySQL + Milvus/ES/etcd/MinIO、SHA256 校验、保留策略、dry-run 与恢复防护；前端 CaseHeader/记忆/评测页导出按钮 | `server/handler/export.go`、`server/dto/dto.go`、`frontend/src/api/client.ts`、`scripts/backup.sh`、`scripts/restore.sh` |
| D16 CRUD 不完整 | `DELETE /cases/:id`（级联清理全部 case_id 产物 + tool_call/reflection 经 agent_run 子查询）、`DELETE /datasets/:id`（级联 items/runs/results，先中止进行中 run） | `adapter/repository.go`、`adapter/dataset_repository.go`、`application/decision/service.go`、`application/dataset/service.go` |
| D17 可观测性默认弱 | `/metrics` 增加 `metrics.auth_required`（开启后要求 admin 角色，open 模式放行），限流/评测/导出等均接入日志；文档补充 alert 与授权说明 | `server/router.go`、`bootstrap/config.go`、`backend/conf/magi.yaml.example` |

> 以上修复与 `docs/ai-harness-gap-analysis.md` 第三章 P2 缺陷（D9–D17）逐条对应；P0（D1–D3）、P1（D4–D8）与 Phase 3 缺口不在本次“完成 P2”范围内。
