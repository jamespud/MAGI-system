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

> 参考能力域来自主流 AI harness（LangGraph、AutoGen、CrewAI、Claude Code、OpenHands 等）的功能空间。✅=已实现且较完整，🟡=部分实现/有缺陷，❌=缺失。

| 能力域 | 子能力 | 状态 | 说明 / 证据 |
| --- | --- | --- | --- |
| **决策编排** | 确定性 FSM / 硬投票 / 辩论 / 反思 / 复议 / 死锁 | ✅ | `domain/orchestration/orchestrator.go`，~20 状态 |
| | 角色契约（技术/风险/机会）门控 | ✅ | `evidence/role_gate.go`、`entity/role.go` |
| **Agent 执行** | 手写 agent loop、工具循环、终止策略、压缩 | ✅ | `domain/runtime/agent_loop.go` |
| | **多模型多样性**（不同 agent 用不同模型） | ❌ | 三 agent+commander+judge 全部共用 `cfg.Model`（`bootstrap/container.go`），设计文档列为"可选"但未实现 |
| | 多供应商路由/降级 | ❌ | 仅单个 OpenAI 兼容端点 |
| **工具生态** | Tavily web_search / MCP(stdio+http) / CodeRunner(WASM) | ✅ | `adapter/tavily_tool.go`、`adapter/mcp/mcp.go`、`coderunner_adapter.go` |
| | 原生文件/代码库/浏览器/DB 工具 | ❌ | 只能靠外部 MCP 引入，无内置代码库工具集 |
| | 多搜索引擎插件化 | 🟡 | 只绑 Tavily，无搜索提供商抽象 |
| **记忆/知识** | case 记忆投影 + RAG（Milvus+ES+MySQL 混合 RRF） | ✅ | `adapter/rag/` |
| | **语义检索接入 UI** | ❌ | `/api/v1/memory` 走 SQL LIKE（`adapter/repository.go memoryRepo.Search`），RAG 只用于 agent 上下文 |
| | **文档/URL 知识导入管理** | ❌ | KnowledgePort.Store 仅由 case 投影调用，无上传/导入/管理 API |
| | 长期记忆编辑/删除/标注 | ❌ | memory 只读查询 |
| **会话** | 单次 question→decision（`/assistant`） | ✅ | `application/assistant/service.go` |
| | **持久对话线程/多轮追问/对话内上下文** | ❌ | 无 conversation/thread 模型，无追问链路 |
| **身份与多租户** | API-Key 认证（常量时间比较）+ 按用户所有权 | ✅ | `application/auth`、`server/auth.go` |
| | **Web UI 认证通道 / 登录页** | ❌ | 前端 `api/client.ts` 不携带任何认证头，`auth.enabled=true` 时 UI 全 401 |
| | 用户管理 / 注册 / SSO / OAuth / 密钥轮换 | ❌ | key 静态写在 yaml |
| | 按用户配额/预算/限额 | ✅ | `application/admin`、`run_manager.go` |
| **安全** | 沙箱/审批门/注入防护/脱敏/审计事件 | ✅ | `toolpolicy`、`approval`、`redact` |
| | HTTP 通用限流 | ❌ | 只有 tool quota 与 run 并发，无 API 层限流 |
| | 敏感数据分级/租户隔离审计 | 🟡 | 有 ownership 检查，但列表接口先全表拉取再过滤（见缺陷#2） |
| **评估闭环** | 数据集评测 / benchmark / stability / regression gate / LLM judge | ✅ | `application/dataset`、`evaluation`、`judge` |
| | 持续评估 / CI 集成 / 线上 golden / 指标看板 | ❌ | 全部离线手动触发 |
| **可观测性** | OTel span / X-Trace-ID / 事件流 / Prometheus /metrics | ✅ | `application/tracing`、`server/metrics.go` |
| | 内置 trace 可视化 / 告警默认部署 | 🟡 | OTel 默认 log sink；alert 仅 example 文件 |
| **UI** | 决策工作台/证据图/时间线/审批/评测/数据集/模板/benchmark/history/memory/tools/settings | ✅ | `frontend/src/pages/` 11 页 |
| | 管理员用量可视化 / 用户管理 / 知识库管理 / 配置管理 | ❌ | `admin/usage` 仅 API；Settings 页为静态信息 |
| | 国际化 | ❌ | 全英文，无 i18n |
| **部署运营** | Docker compose / readiness / config fail-fast / 多实例 worker | ✅ | `docker/`、`bootstrap/config.go` |
| | K8s/Helm 清单、横向扩容文档 | ❌ | 无 |
| | 跨实例 SSE 实时推送 | ❌ | broker 进程内（见缺陷#4） |
| | 数据导出/备份方案 | 🟡 | 仅数据集 item export；无 case/memory 全量导出 |

---

## 三、现有功能实现缺陷清单

### P0（正确性 / 安全 / 多租户）

**D1. 前端无认证通道，多租户 Web UI 不可用**
- 位置：`frontend/src/api/client.ts`（`request()` 只发 `Content-Type`）、`backend/server/auth.go`（支持 `Authorization: Bearer` / `X-API-Key`）
- 问题：后端已实现完善的 API-Key 认证，但前端从不带认证头、也没有登录/API-Key 输入页。一旦 `auth.enabled=true`，浏览器 UI 所有 `/api/v1/*` 请求全部 401，多租户只能纯 API 使用。
- 影响：宣称的"multi-tenant API"与 Web UI 脱节；UI 实际只能跑在 open 模式。

**D2. case 列表全表加载 + 内存内越权过滤**
- 位置：`backend/adapter/repository.go`（`caseRepo.List` → `Order("created_at DESC").Find(&models)` 无 WHERE、无分页）、`backend/server/handler/decision.go`（`List` 循环 `AuthorizeCase`）
- 问题：把所有用户的全部 case 行读进内存再逐条过滤。数据量增长后是 O(N) 全表扫描 + 大内存 + 大响应体；且"先拉全量再过滤"在多租户下违背最小数据读取原则。前端 `PaginatedSection` 的分页只是客户端 slice。
- 同类问题：`memory` 搜索也是先查后按用户过滤（有 LIMIT 缓解）。

**D3. RAG 检索错误被静默吞掉**
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

**D15. 数据导出/备份弱**：只有数据集 item export；无 case/memory/评测结果全量导出、无迁移快照策略（DEPLOYMENT.md 提到 atlas SQL 仅文档用途）。

**D16. 缺 CRUD 完整性**：无 `DELETE /cases/:id`、无 `DELETE /datasets/:id`、无 `DELETE /evaluation` 等；数据集删除只能删 item。

**D17. 可观测性默认弱**：OTel 默认 log sink（`application/tracing`），`/metrics` 无认证（文档已警示），告警仅 example 文件，无内置 Grafana/Jaeger 栈。

---

## 四、对照"全面 AI Harness"的缺口（Roadmap 输入）

按用户视角的优先级排序：

1. **通用任务执行层（最重要）**：MAGI 是"决策引擎"，不是"任务执行 harness"。缺文件/代码库编辑、shell、浏览器/computer-use、长任务规划与 subagent 派生。这是从"决策产品"走向"AI harness 平台"的分水岭。
2. **会话与多轮追问**：`/assistant` 是一次性问答；无 conversation/thread 模型、无追问、无"基于上次决策继续"。
3. **身份与访问管理**：登录/SSO、用户自助、密钥管理、角色粒度（当前只有 admin/非 admin）、审计 UI。
4. **知识管理**：文档导入/URL 抓取/语料管理/删除/版本，并把语义检索接到 UI 与 `/assistant`。
5. **多模型编排**：per-agent/commander/judge 独立模型配置，多供应商路由与降级，模型参数管理。
6. **评估运营化**：CI 集成、线上 golden、自动回归、评测指标看板、prompt 版本管理。
7. **可观测性**：默认 trace 落库 + 前端 trace 视图、通用 HTTP 限流、告警默认部署。
8. **运营与交付**：K8s/Helm、跨实例 SSE（或改为轮询/WS）、数据导出/备份、i18n。

---

## 五、分阶段建议

### Phase 1（补齐 P0，约 2 周）
- D1 前端认证通道：登录页 + API-Key 输入 + `X-API-Key` header 注入 + 401 统一跳转。
- D2 case/memory 查询下沉到 DB（`WHERE user_id=?` + 游标分页），删除内存过滤。
- D3 RAG 检索失败改为显式日志 + `MEMORY_RETRIEVAL_FAILED` 事件 + 指标。
- D11 删掉 Pinned/Archived 死 UI 或补 pin/archive 字段+API。

### Phase 2（横向能力第一波，约 4-6 周）
- D4 跨实例实时：SSE 改 DB-backed 游标轮询或引入 Redis pub/sub。
- D5 per-role 模型配置（`magi.<role>.model` 覆盖全局）。
- D6/D7 知识管理：upload API + 文档入库走现有 RAG 管线 + Memory UI 接入语义检索。
- 会话模型：conversation/thread + 多轮追问，`/assistant` 升级为会话式。

### Phase 3（平台化，约 8-12 周）
- 身份体系（SSO/OAuth/用户自助/密钥轮换）。
- 通用任务执行层（基于现有 agent loop 扩展 code/file/shell 工具 + 审批门）。
- 评估运营化（CI 集成 + 看板 + prompt 管理）。
- K8s/Helm + 可观测性栈 + i18n。

---

## 六、结论

MAGI 的**决策核心是高质量、可测试、可解释**的，在"证据驱动多智能体决策引擎"这个垂直定位上几乎没有明显的规则漏洞（测试全绿、设计文档与实现高度一致）。但作为"**全面的 AI harness**"，它当前更像一个**专注于决策的专用 harness**，而非**通用的任务执行平台**：

- 最关键的横向缺口是：**通用任务执行能力、会话式交互、知识导入管理、身份体系、多模型编排、评估运营化**。
- 最需要优先修复的实现缺陷是：**前端无认证通道（D1）、列表全表加载+内存过滤（D2）、RAG 静默失败（D3）**，三者分别影响"多租户可用性""规模化与数据隔离"和"记忆可靠性"。

建议先清 P0，再按 Phase 2 的"知识管理 + 会话 + 多模型"补横向能力，即可在不破坏决策核心的前提下把 MAGI 从"决策引擎"升级为"AI harness 平台"。

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
| D15 数据导出弱 | `GET /cases/:id/export`（case+resolution+report+agents+evidence+claims+votes+tool_calls+events+memory 全量 JSON）、`GET /memory/export`（本人全部记忆）、`GET /evaluation/:id/export`（评测+judge）；前端 CaseHeader/记忆/评测页导出按钮 | `server/handler/export.go`、`server/dto/dto.go`、`frontend/src/api/client.ts` |
| D16 CRUD 不完整 | `DELETE /cases/:id`（级联清理全部 case_id 产物 + tool_call/reflection 经 agent_run 子查询）、`DELETE /datasets/:id`（级联 items/runs/results，先中止进行中 run） | `adapter/repository.go`、`adapter/dataset_repository.go`、`application/decision/service.go`、`application/dataset/service.go` |
| D17 可观测性默认弱 | `/metrics` 增加 `metrics.auth_required`（开启后要求 admin 角色，open 模式放行），限流/评测/导出等均接入日志；文档补充 alert 与授权说明 | `server/router.go`、`bootstrap/config.go`、`backend/conf/magi.yaml.example` |

> 以上修复与 `docs/ai-harness-gap-analysis.md` 第三章 P2 缺陷（D9–D17）逐条对应；P0（D1–D3）、P1（D4–D8）与 Phase 3 缺口不在本次“完成 P2”范围内。
