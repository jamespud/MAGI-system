# 多候选决策协议领域契约 v1.0（冻结）

> 对应 GitHub Epic [#3](https://github.com/jamespud/MAGI-system/issues/3) / Linear CAS-9。
> 权威规范：[`docs/magi-final-personality-acceptance.md`](./magi-final-personality-acceptance.md)（下称“最终设计”），
> 主要追溯章节 §4、§9–12、§17–19；主要不变量 AGENDA-01、SELECT-01..03、VOTE-01..03、REPORT-01。
> 本文件冻结**契约形状、状态枚举与不变量**，作为拆分实施 PR 的基线；它不批准修改最终设计，
> 也不承诺任何生产环境的写操作或发送授权。

## 0. 状态与边界

- **本文档是契约冻结，不是实现。** 现有代码（`domain/entity/vote.go`、`domain/consensus`、
  `domain/orchestration/failure_policy.go`）仍是单候选、单维 `Decision` 的旧契约。
- **本次冻结决定**：四个工件的标识、字段、状态枚举、不变量与相互引用关系；旧字段的兼容规则；
  与 ContextManifest / AuthorityProof / ExecutionGeneration 的绑定方式。
- **明确非目标**：不实现候选议程、不实现成对偏好算法、不改动授权/发送/审计模块、不迁移历史数据。
  这些在实施切片中各自拆分为独立 PR。
- **需要 Design Change 才能改动的部分**：风险等级语义（R0–R3）、法定人数、批准阈值、
  `OWNER_ALL` 缺省规则。任何降低这些边界的行为都必须先走设计变更。

约定：`id` 为内容寻址标识（工件类型前缀 + 内容摘要）；`*_digest` 为规范化 JSON 的摘要；
版本号 `version` 仅用于人类识别，机器绑定一律使用 `id` 与 `*_digest`。所有时间使用 UTC，
所有枚举取大写稳定字符串，未知值一律拒绝而不是默认降级。

### 0.1 Rev4 条款映射

本契约是最终设计**修订 4（Rev4）**的落地细化，不是新规范。逐条追溯：

| 本契约章节 | Rev4 条款 | 关系 |
| --- | --- | --- |
| §2 DecisionCharter | §2、§3.1–3.4 | 把宪章必填项与优先顺序落成可校验 schema |
| §3 CandidateSnapshot | §4、§9 | 把候选风险分类、未决替代方案与议程规则落成工件与 AgendaEvent |
| §4 BallotSet | §6.1–6.2、§10.1–10.2、§17 | 把上下文绑定、密封票据与完整成对偏好落成 schema |
| §5 DecisionRecord | §4.3–4.4、§10.3–10.4、§11、§12.1 | 把候选裁决、全局选择与报告真实性落成记录 |
| §6 绑定 | §3.2、§6.2–6.3、§12.2 | 把 AuthorityProof / ContextManifest / ExecutionGeneration 的绑定显式化 |
| §7 迁移 | §12.1、§12.3 | 把六维状态与现有 FSM 的共处策略显式化 |
| §8 不变量索引 | §19 用例目录 | 把 Rev4 不变量映射到契约条款与验收方向 |

### 0.2 扩展不变量编号的定位

本契约使用的 `CHARTER-01..05`、`CAND-01..03`、`VOTE-04..08`、`DEC-01..05`、
`LEGACY-01..03` 属于**Rev4 原始要求的细化编号**，用于把 §19 的粗粒度不变量（AGENDA-01、SELECT-01..03、VOTE-01..03、REPORT-01
等）拆成可实现、可测试的条款。它们：

- **不新增或替换** Rev4 的原始验收要求；
- 若与 Rev4 原文冲突，以 Rev4 原文为准，并走 Design Change；
- 编号只能在对应 Rev4 不变量内细化，不得跨不变量重新归类。

---

## 1. 工件总览与权威

| 工件 | 生产者 | 权威内容 | 冻结性 |
| --- | --- | --- | --- |
| `DecisionCharter` | 有权主体（人）确认 | 目标、主体、偏好、范围、时限、候选上限 | 确认后不可原地修改 |
| `CandidateSnapshot` | 议程管理器 | 候选集合、议程处理记录、MATERIAL_PENDING | 冻结后不可变 |
| `BallotSet` | 三个 Persona（密封提交） | 每个候选的态度 + 全部成对偏好 | 每轮每人一份终票 |
| `DecisionRecord` | Decision Authority | 候选裁决、选择裁决、规则版本 | 追加式，唯一线性化 |

顺序与依赖（单向）：

```text
DecisionCharter ─┬─> ContextManifest ─> CandidateSnapshot ─> BallotSet ─> DecisionRecord
AuthorityProof  ─┘                                                          │
ExecutionGeneration ───────────────────────────────────────────────────────┘
```

规则：

- `DecisionCharter` 必须早于 `CandidateSnapshot`；`CandidateSnapshot` 冻结后才允许产生 `BallotSet`。
- `BallotSet` 必须绑定产生它的 `CandidateSnapshot` 与 `ContextManifest`。
- `DecisionRecord` 必须引用其 `CandidateSnapshot`、全部有效 `BallotSet`、`AuthorityProof` 与
  `ExecutionGeneration`；不得引用未冻结或已失效的输入。
- 工件修改产生**新标识**，不允许原地替换历史内容。历史工件保留可寻址性。

---

## 2. DecisionCharter（决策宪章）

必需字段（缺失即拒绝，不由模型补填）：

```text
charter_id            : string
version               : string            # 人类可读，如 "1.0"
objective             : string            # 目标
non_goals             : [string]          # 非目标
success_criteria      : [{code, description}]
hard_constraints      : [{code, statement, source}]     # 不可覆盖
soft_preferences      : [{code, statement, weight?}]
allowed_costs         : [{resource, bound, unit}]       # 允许代价
decision_owner        : SubjectRef        # 决策所有者
resource_owners       : [{resource, mode, owners:[SubjectRef]}]
authorizers           : [SubjectRef]      # 执行授权者（可与 owner 不同）
arbiter               : SubjectRef?       # 仲裁者
tool_scope            : [string]
risk_policy           : {default_level, escalation_level}
candidate_count       : int               # 本次快照的候选数 k，>= 1（含“不行动”）
candidate_limit       : int               # K_max：允许纳入的候选数量上限，>= 1
submission_deadline   : timestamp         # 提交截止（绝对时间）
objection_deadline    : timestamp         # 初次异议截止
revision_deadline     : timestamp         # 修订截止
freeze_deadline       : timestamp         # 冻结截止（必须早于总活动决策期限）
early_selection_policy: enum{WAIT, REVERSIBLE_TRIAL, HUMAN_WAIVER}   # 缺省 WAIT
validity_horizon      : timestamp         # 最终有效期
charter_digest        : string
```

优先级（不可调换，来自最终设计 §3.1）：

```text
外部约束与授权边界 > 已确认目标与硬约束 > 已授权偏好取舍 > 人格默认 > 表达风格
```

不变量：

- **CHARTER-01**：目标、偏好或范围缺失时不得由模型编造；需要修改时由有权主体确认并产生新 `charter_id`。
- **CHARTER-02**：`candidate_limit >= 1` 且必须存在；`candidate_count` 与 `candidate_limit` 分开校验，
  `candidate_count <= candidate_limit`。**单候选（k=1）合法**：Rev4 §4.2 要求仅剩一个 APPROVED 候选时
  仍须通过选择充分性门槛，因此不得用 `candidate_limit >= 2` 禁止单候选快照。
- **CHARTER-05**：四个阶段截止时间必须存在且严格递增
  （`submission < objection < revision < freeze`），使用同一时钟来源；提前结束窗口需要所有有权参与者
  显式确认（Rev4 §9、§18.5）。缺少任一阶段截止时间的宪章不可冻结。
- **CHARTER-03**：`hard_constraints` 的 `source` 必须指向外部约束或已确认的权威输入，不能是模型生成文本。
- **CHARTER-04**：宪章不得被任何人格多数、主持者或摘要器修改。

---

## 3. CandidateSnapshot（候选快照 / 议程）

必需字段：

```text
snapshot_id            : string
charter_id             : string
domain_profile_id      : string
candidates             : [Candidate]
material_pending       : [PendingItem]        # 实质性未决替代方案
agenda_log             : [AgendaEvent]        # 纳入/排除/合并/撤回/超限
frozen_at              : timestamp
freeze_signatures      : [{persona_id, spec_id}]   # 三角色确认
snapshot_digest        : string
```

`Candidate`：

```text
candidate_id           : string            # 稳定、与排序无关
proposal               : string            # 完整提案
is_no_action           : bool              # 默认必须存在“不行动”
originated_by          : enum{PERSONA, REQUESTER, CHARTER}
conditions             : [{statement, must_hold}]
parent_candidate_ids   : [string]          # 合并来源，保留原提案
state                  : enum{PROPOSED, MERGED, WITHDRAWN, EXCLUDED, INCLUDED}
risk_level             : enum{R0, R1, R2, R3}
```

`AgendaEvent`：`{event_id, kind: enum{INCLUDE, EXCLUDE, MERGE, WITHDRAW, OVER_LIMIT}, candidate_ids, rule_id, evidence_ids, reason, notified:[SubjectRef], recorded_at}`

议程状态与规则（最终设计 §9、AGENDA-01）：

- 候选纳入以结构、范围与已验证硬规则为依据；**不得**因预计得票少而排除。
- 合并必须保留原提案、差异与条件，原提出者与三角色显式确认无实质差异；沉默不是同意。
- 超出 `candidate_limit` 只能通过等价合并、自愿撤回或明确的新范围解决；否则整体为
  `AGENDA_UNRESOLVED`，不得先到先得或由主持者挑选。
- 冻结前三角色确认候选、版本、异议处理与 `material_pending`；存在未解决实质性异议则不冻结。
- 冻结后实质变化开启**新轮**（新 `snapshot_id`），旧快照不可变。

不变量：

- **AGENDA-01**：候选的纳入、排除、合并、超限与期限处理必须留下可审计的 `AgendaEvent`。
- **CAND-01**：`candidates` 至少包含一个 `is_no_action=true` 的候选，除非宪章显式说明其不适用并记录理由。
- **CAND-02**：`candidate_id` 在快照内唯一，且与 `candidates` 的任意排序无关。
- **CAND-03**：`state=EXCLUDED` 或 `MERGED` 的候选不得出现在后续 `BallotSet` 的计票中。

---

## 4. BallotSet（密封票据集）

必需字段：

```text
ballot_set_id          : string
case_id                : string
round                  : int                 # >= 1
charter_id             : string
domain_profile_id      : string
snapshot_id            : string              # 冻结的 CandidateSnapshot
context_manifest_id    : string
authority_proof_id     : string
execution_generation   : int64               # 必须 > 0
ballots                : [Ballot]
absences               : [Absence]           # 未提交的角色（不是弃权）
ballot_set_digest      : string
```

`Ballot`：

```text
ballot_id              : string
persona_id             : MagiCode            # melchior | balthasar | casper
persona_spec_id        : string
experience_snapshot_id : string
read_set_digest        : string              # 实际读取集合摘要
context_revocation_epoch: int64
attitudes              : {candidate_id -> enum{APPROVE, REJECT, ABSTAIN}}
preferences            : [PairPreference]    # 正式偏好的唯一权威来源
reasoning_summary      : string
submitted_at           : timestamp
```

`PairPreference`：

```text
candidate_a            : string              # 字典序保证无序对规范化
candidate_b            : string
relation               : enum{LEFT_PREFERRED, RIGHT_PREFERRED, TIED, INCOMPARABLE}
rationale              : enum{EVIDENCE_GAP, VALUE_INCOMPARABLE, CHARTER_PENDING}  # 仅 INCOMPARABLE 必填
```

`Absence`：`{persona_id, kind: enum{MISSING, TIMEOUT, INVALID, CANCELLED, FAILED}, detail}`

计票与验证规则（最终设计 §10、VOTE-01..03）：

- **VOTE-01**：对 k 个候选，`preferences` 必须恰好包含 `k(k-1)/2` 个无序对，每对一个关系；
  缺失或重复即整份票据 `INVALID`。`preferences` 是正式偏好的**唯一权威来源**；任何扁平展示顺序
  （如 UI 列表顺序）都只是由偏序**导出**的、非权威的表示，不得参与一致性校验、计数或裁决，
  也不得用它把 `TIED` / `INCOMPARABLE` 压成一条并不存在的严格偏好。
- **VOTE-02**：`TIED` 构成等价类，`INCOMPARABLE` 不是并列；严格偏好必须无环、可传递，且
  不得把已存在严格路径的端点写成 `INCOMPARABLE`。
- **VOTE-03**：`APPROVE` 必须严格偏好 `REJECT`；同态度或含 `ABSTAIN` 的对允许四种关系，
  但仍须满足偏序与证据条件。
- **VOTE-04**：同一 `persona_id` 同一 `round` 只有一份终票。相同内容重复提交幂等；
  不同内容重复提交为 `CONFLICTING_SUBMISSION`，不采用最后写入者。
- **VOTE-05**：缺席、超时、格式错误、非法票型**绝不**转换为 `ABSTAIN`；`Absence` 与真实
  `ABSTAIN` 必须保持可区分，且缺席使该轮不能产生裁决。
- **VOTE-06**：票据必须绑定 `snapshot_id`、`context_manifest_id`、`authority_proof_id` 与
  `execution_generation`；冻结后输入变化即整轮作废。
- **VOTE-07（提交边界）**：收集阶段不得公开其他人格的终票。终票在密封提交时一次性写入并密封；
  同一角色同一轮内容完全相同的重复提交幂等，内容不同的重复提交为 `CONFLICTING_SUBMISSION`
  并拒绝，不采用最后写入者。

---

## 5. DecisionRecord（裁决记录）

必需字段：

```text
decision_record_id     : string
case_id                : string
round                  : int
snapshot_id            : string
ballot_set_ids         : [string]            # 参与本轮裁决的有效票据集
context_manifest_id    : string
authority_proof_id     : string
execution_generation   : int64               # 必须 > 0
rules_version          : string              # 规则/策略版本
candidate_verdicts     : {candidate_id -> Verdict}
selection              : Selection
dissent                : [Dissent]           # 结构化少数意见
issued_at              : timestamp
decision_record_digest : string
```

`Verdict`：`enum{APPROVED, REJECTED, DEFERRED, BLOCKED, INCOMPLETE}`

`Selection`（**结果状态**与**政策**分离，Rev4 §4.4）：

```text
selection_state        : enum{NONE, SELECTED, HUMAN_SELECTED, SELECTION_DEFERRED}
early_selection_policy : enum{WAIT, REVERSIBLE_TRIAL, HUMAN_WAIVER}   # 与宪章一致，缺省 WAIT
candidate_id           : string?     # SELECTED / HUMAN_SELECTED 时必填
policy_basis           : string?     # 触发该结果的策略依据（WAIT / REVERSIBLE_TRIAL / HUMAN_WAIVER）
waiver                 : object?     # HUMAN_WAIVER 的豁免记录（接受者、放弃项、不可逆影响）
rationale              : string?
conditions             : [{statement, must_hold}]
```

- `early_selection_policy` 来自宪章，是**策略**；`selection_state` 是**裁决结果**。`WAIT` /
  `REVERSIBLE_TRIAL` / `HUMAN_WAIVER` 不得作为结果状态出现。
- `HUMAN_WAIVER` 场景的结果状态是 `HUMAN_SELECTED` 并附 `waiver` 依据，而不是把结果写成
  `HUMAN_WAIVER`；`REVERSIBLE_TRIAL` 场景的结果状态是 `SELECTED` 并受范围/期限/退出条件约束。
- `NONE` 表示没有可批准候选（无选择结果）；`SELECTION_DEFERRED` 表示存在可批准候选但选择充分性
  未达成。二者必须在报告与 API 中保持区分。

`Dissent`（沿用现有 `entity.Dissent` 形状，保持报告/导出/A2A 一致）：

```text
agent_code   : MagiCode
decision     : enum{APPROVE, REJECT, ABSTAIN}   # 与正式 Ballot 相同，见 VOTE-08
reasoning    : string
evidence_ids : [string]
claim_ids    : [string]
conditions   : [{statement, must_hold}]
```

- **VOTE-08**：新协议的正式票据与 Dissent 的决定值只允许 `APPROVE / REJECT / ABSTAIN`（Rev4 §10.1）。
  支持条件写入完整提案与 `conditions`，**不得**用 `CONDITIONAL_APPROVE` 之类的额外终票枚举表达。
  旧 `Resolution` 中可能存在的该枚举只允许出现在标记为兼容投影的读取路径中（见 §6.3），
  不得进入新协议的 `BallotSet` / `DecisionRecord`。

裁决状态与顺序（最终设计 §10.3–10.4、SELECT-01..03）：

- 先处理硬阻断、决定性未决条件与完整性，再计票。
- 单候选态度裁决：已确认硬约束违规 → `BLOCKED`；决定性前提未决 → `DEFERRED`；缺有效终票 →
  `INCOMPLETE`；R0/R1 至少两票赞成 → `APPROVED`；任意等级至少两票反对 → `REJECTED`；
  R2/R3 三票赞成 → `APPROVED`；其余完整组合 → `DEFERRED`。
- 全局选择：先执行 Rev4 §4.4 的选择充分性门槛与 `early_selection_policy`，再对可批准候选计算：
  没有可批准候选 → `NONE`；唯一可批准候选也必须通过充分性门槛（Rev4 §4.2）；
  多个候选时只有对每个其他可批准候选都获得 ≥2 个严格偏好的候选才是 `SELECTED`；
  并列/不可比较不计严格偏好；循环、无唯一胜者或存在未决替代方案 → `SELECTION_DEFERRED`。
- 人类选择 → `HUMAN_SELECTED`，保留原票据与未决项，**不生成**缺失的执行授权。

不变量：

- **SELECT-01**：唯一 `APPROVED` 且存在 `material_pending` 时不得自动选择。
- **SELECT-02**：`WAIT`、`REVERSIBLE_TRIAL`、`HUMAN_WAIVER` 的合法组合与前提必须可枚举。
- **SELECT-03**：R2/R3 不得绕过未决替代方案政策；`HUMAN_SELECTED` 不生成执行授权。
- **DEC-01**：`DecisionRecord` 是唯一线性化裁决；同一 `(case_id, round)` 只能有一条权威记录。
- **DEC-02**：`dissent` 从 `BallotSet` 确定性地派生并持久化；报告/HTTP/A2A/导出读取同一条记录，
  不得各自重新推导（见 [issue #13](https://github.com/jamespud/MAGI-system/issues/13) 已修复契约）。
- **DEC-03**：`candidate_verdicts` 必须覆盖快照中全部 `INCLUDED` 候选；缺少任一即整条记录无效。
- **DEC-04（状态分离）**：候选 `APPROVED`、全局 `SELECTED`、人工 `HUMAN_SELECTED` 与执行
  `AUTHORIZED` 是四个独立事实，必须分别存储与报告。旧 `ConsensusConditional`（条件赞成隐含
  等同于批准）不得被继承：条件赞成只能作为候选态度输入，绝不产生全局选择或执行授权。
- **DEC-05（提交绑定）**：`DecisionRecord` 的唯一提交必须绑定冻结的 `snapshot_id`、参与本轮裁决的
  全部有效 `BallotSet`、`context_manifest_id` 与该代次；绑定不完整时整条记录不得提交。

---

## 6. 与 ContextManifest / AuthorityProof / ExecutionGeneration 的关联

### 6.1 绑定关系

```text
Ballot  ↔ ContextManifest        : ballot.context_manifest_id, read_set_digest, context_revocation_epoch
Ballot  ↔ AuthorityProof         : ballot_set.authority_proof_id
Record  ↔ ContextManifest        : decision_record.context_manifest_id
Record  ↔ AuthorityProof         : decision_record.authority_proof_id
Record  ↔ ExecutionGeneration    : decision_record.execution_generation
Snapshot↔ DecisionCharter        : snapshot.charter_id（同一宪章版本）
```

### 6.2 有效性检查

- 终票接收、裁决发布、动作授权与最终外发都必须检查其依赖仍有效。
- `ContextManifest` 中的源条目被撤销时，其派生摘要同步失效：受影响任务进入 `CONTEXT_INVALIDATED`，
  停止新票据与动作，标记未执行裁决不再有效，重建上下文并开启新轮。
- `AuthorityProof` 必须证明主体绑定身份源、认证时效、授权签发者、完整委托链、逐跳资源/动作范围、
  是否允许再委托、到期时间与当前撤销代次；身份/授权/时间源不可信时失败关闭。
- `ExecutionGeneration` 是 Case 生命周期的所有权纪元。`BallotSet` 与 `DecisionRecord` 只能由持有
  当前 `execution_generation` 的持久执行所有者写入；旧代次写入必须被拒绝（`ErrLeaseLost`）。

### 6.3 旧契约的兼容绑定（LegacyProjection，禁止默认升级）

现有 `entity.Resolution` **不能**被等价为一个新式 `DecisionRecord`，也不得默认补出
`selection_state=SELECTED`。旧系统没有保存 `CandidateSnapshot`、`BallotSet`、选择充分性证明，
也无法证明当时是否存在 `MATERIAL_PENDING`；单候选产生了旧式 Resolution 并不能推出它满足 Rev4
的全局选择门槛。

兼容规则：

- **LEGACY-01**：旧记录只能通过显式的 `LegacyProjection` 读取，标记 `projection=UNVERIFIED_LEGACY`，
  并携带派生来源（`resolution_id`）。它不产生 `candidate_verdicts` / `selection_state` 等新协议事实。
- **LEGACY-02**：除非完成可验证的数据导入（补齐快照、票据与充分性证据），旧记录不得被当作正式
  `DecisionRecord`，也不得出现在新协议的裁决计数、报告或出口校验中。
- **LEGACY-03**：旧 `Resolution` 的 `Consensus.Votes`、`CONDITIONAL_APPROVE` 等旧枚举只在
  `LegacyProjection` 内可见，不得据此外推新式票据或选择结果。

### 6.4 身份边界（三种身份不可互相替代）

系统同时存在三种“身份”，它们回答不同问题，任一都不得当作另一个使用：

| 身份 | 权威来源 | 回答的问题 | 不可替代性 |
| --- | --- | --- | --- |
| `execution_generation` | Case 生命周期所有权纪元（`domain/entity/case.go`、持久租约） | 谁有权写入这一代次的权威工件 | 运行所有权；**不**决定票的唯一性，也**不**是协议轮次身份 |
| 决策 `round` | Case 内的协议轮次（冻结快照后开启） | 这是第几轮决策 | 协议身份；**不**证明写入所有权 |
| `BallotSet` / `Ballot` 身份 | `snapshot_id` + `persona_spec_id` + `experience_snapshot_id` + `context_manifest_id` + `context_revocation_epoch` | 这份终票是谁、在哪一轮、依哪套冻结上下文签发的 | 投票唯一性依据；**不**等同于运行代次 |

约束：

- **ID-01**：不得仅凭字符串形式的 ID 判定权威性。任何“这个 ID 是权威的”结论必须由持久来源核验
  （代次、轮次、签发身份与上下文版本一致），而不是由格式或调用方声明推断。
- **ID-02**：投票唯一性以 `(case_id, round, persona_id)` 为键，与 `execution_generation` 正交；
  代次更迭必须开启新轮，不得在同一轮内以新代次覆盖旧票。
- **ID-03**：`BallotSet` 与 `DecisionRecord` 的写入必须同时满足当前代次所有权与冻结快照绑定；
  二者缺一即拒绝（失败关闭）。

---

## 7. 状态机与迁移

新协议与现有 FSM（`domain/entity/case.go` 的 `CaseStatus`）的迁移策略：

- 现有阶段 `COLLECTING_VOTES → CONSENSUS_CHECK → (DEBATING→REFLECTING→REVOTING)* →
  RESOLVING → GENERATING_REPORT` 保留，作为**流程/生命周期**维度。
- 新协议的**决策结果**维度（候选裁决、选择、授权、动作、上下文、工作流）与流程状态分开存储，
  不得再把它们合成单一状态（最终设计 §12.1）。
- `CandidateSnapshot` 冻结发生在进入 `COLLECTING_VOTES` 之前；冻结后才创建 `BallotSet`。
- `DecisionRecord` 在 `RESOLVING` 阶段由 Decision Authority 原子提交，与现有
  `Resolution` 终态提交同一事务边界。
- 旧 `Resolution` 与 `CaseStatus` 字段保留为读路径兼容；新字段以追加方式加入，
  历史行缺少新字段时按“不可证明”处理，而不是补默认值。

迁移状态枚举（工件级）：

```text
CandidateSnapshot: DRAFT → GATHERING → FROZEN | AGENDA_UNRESOLVED
BallotSet:         OPEN → SEALED | INCOMPLETE | CONFLICTING_SUBMISSION
DecisionRecord:    PENDING → ISSUED | SELECTION_DEFERRED | BLOCKED
```

---

## 8. 不变量索引（不变量 → 契约条款 → 验收方向）

| 不变量 | 契约条款 | 验收方向（实施 PR 需具名 RED→GREEN） |
| --- | --- | --- |
| AGENDA-01 | §3 | 纳入/排除/合并/超限/期限均留下可审计 AgendaEvent |
| SELECT-01 | §5 | 唯一 APPROVED 且有 MATERIAL_PENDING 不自动选择 |
| SELECT-02 | §5 | WAIT / REVERSIBLE_TRIAL / HUMAN_WAIVER 合法组合可枚举 |
| SELECT-03 | §5 | R2/R3 不绕过未决政策；HUMAN_SELECTED 不生成执行授权 |
| VOTE-01 | §4 | k=1..4 全部合法成对偏好 + TIED/INCOMPARABLE/循环/未决 |
| VOTE-02 | §4 | 缺失、冲突提交、重复/伪造票据不改变唯一裁决 |
| VOTE-03 | §4 | 缺席/失败/非法票型绝不静默转为 ABSTAIN |
| CHARTER-05 | §2 | submission/objection/revision/freeze 四截止严格递增；提前结束需全同意 |
| VOTE-07 | §4 | 收集期不公开他人终票；同内容重复幂等、异内容冲突拒绝 |
| VOTE-08 | §4 | 正式票据/Dissent 仅 APPROVE/REJECT/ABSTAIN，不含条件赞成枚举 |
| DEC-04 | §5 | APPROVED / SELECTED / HUMAN_SELECTED / AUTHORIZED 四态分离；不沿用条件赞成隐含批准 |
| DEC-05 | §5 | DecisionRecord 唯一提交绑定快照、全部有效 BallotSet 与上下文版本 |
| ID-01..03 | §6.4 | 运行代次、协议轮次、票据签发身份互不替代；权威性由持久来源核验 |
| LEGACY-01..03 | §6.3 | 旧 Resolution 只经 LegacyProjection 读取，不默认升级为 SELECTED |
| REPORT-01 | §5 | 不隐藏少数意见；dissent 在报告/HTTP/A2A/导出一致（#13 结构化部分已修复） |

---

## 9. 实施切片（拆 PR 基线）

每个切片都标注规范条款、代码基线、依赖、可运行正反例，禁止一次性大重写。

1. **契约类型与验证器**：为四个工件落地 Go 类型 + JSON Schema + 语义验证器
   （含 CHARTER-01..05、VOTE-01..08）。仅新增类型与验证，不改 FSM。
2. **CandidateSnapshot 议程**：候选纳入/排除/合并/超限/冻结与 AgendaEvent 持久化
   （AGENDA-01、CAND-01..03），并提供裁决器所需的**冻结快照读取接口**。
3. **BallotSet 密封终票**：成对偏好、幂等/冲突提交、缺席与非法票型（VOTE-01..08）。
4. **DecisionRecord 裁决**：候选裁决 + 全局选择 + dissent 绑定（SELECT-01..03、DEC-01..05）。
   裁决器同时消费切片 2 的冻结快照（候选状态、MATERIAL_PENDING）与切片 3 的密封票据，
   因此**依赖切片 2 和切片 3**。
5. **维度拆分与迁移**：候选/选择/授权/动作/上下文/工作流分维存储；`LegacyProjection`
   （LEGACY-01..03）读兼容与迁移策略。

依赖关系：

```text
切片 1（契约类型与验证器）
 ├─> 切片 2（CandidateSnapshot 议程）──────────┐
 └─> 切片 3（BallotSet 密封终票）──────────────┴─> 切片 4（DecisionRecord 裁决）
                                                      │
切片 1 ───────────────────────────────────────────────┴─> 切片 5（维度拆分与迁移）
```

- 切片 1 是关键路径：它冻结类型与语义验证器，2–4 依赖其接口。
- **模块开发**：切片 2 与切片 3 在切片 1 完成后可并行开发（各自的类型与验证器互不阻塞）。
- **端到端集成必须串行**：切片 4 需要切片 2 产出的冻结 `CandidateSnapshot`（候选状态、
  `MATERIAL_PENDING`）**和**切片 3 产出的密封 `BallotSet`，因此其集成依赖 2 与 3 都就绪，
  不能只依赖切片 3。
- 切片 5 依赖 1–4，且必须晚于切片 4 合并。

切片完成定义（DoD）：结构校验 + 语义校验 + 独立参考判定 + 合法/非法输入测试，而不只是 Go struct
与数据库表存在；投票相关协议必须以“独立真值表先于全面编排器接入”的顺序推进。

---

## 10. 冻结决议与待定项

**本次冻结**：

1. 四个工件的标识、字段、状态枚举与不变量（§2–§5）。
2. 与 ContextManifest / AuthorityProof / ExecutionGeneration 的绑定方式（§6），含代次 / 轮次 /
   票据签发身份三种身份的边界（§6.4）。
3. 与现有 FSM 的迁移策略（§7）。
4. 实施切片顺序与依赖关系（§9）。
5. 扩展不变量编号对 Rev4 的定位：细化而非替换（§0.2）。

**审阅修订（PR #32 首轮）**：本版按首轮契约审阅意见修订 F32-01（单候选合法化）、F32-02（去除权威
ranking，偏序为唯一来源）、F32-03（选择结果与提前选择政策分离）、F32-04（LegacyProjection，
禁止默认升级为 SELECTED）、F32-05（四阶段截止时间）、F32-06（Dissent 决定值限定）、F32-07（依赖图
补入切片 2）。修订不改变冻结意图，仍需第二轮独立审阅。

**本次冻结不构成独立设计审阅通过**：本文件为待审阅的冻结草案，需与 Rev4 逐条对照后由独立审阅确认，
才能作为切片 1 的实现基线。

**待定（需 Design Change 或实测证据，不阻塞切片 1）**：

- 候选 `risk_level` 的判定来源（人工标注 vs 领域规则）——切片 1 只冻结字段与枚举，不冻结判定算法。
- `HUMAN_WAIVER` 的同意阈值——沿用最终设计 §4.4，实施前需具名验收用例复核。
- 历史 `Resolution` 的批量迁移脚本——由切片 5 单独设计与验证，不在本次冻结内。
