# Decision Artifact v2：跨工件绑定与证据关闭（独立审阅草案）

> #38 / CAS-9，基线 `37ec45997a62e4e2829f69e7384ed6f1a6c4447e`，2026-10-23。
> 权威顺序：Rev4 → C0 → 决策协议 v1 的未修订语义 → 本版本化细化。
> **DRAFT：供独立审阅，不是生产协议实现、签发资格或 Rev4 验收。**
> 本契约承接 [C1-A §6.5](input-artifact-contract.md#65-唯一绑定共享终审交付与提交点d-02)。
> v1 原文、C0/C0-R、DomainProfile/PersonaSpec 的既有字节和 Golden 全部保留。

## 1. 版本、类型与无环身份（D2-01/02/07）

机器字段清单为 [schema.json](contracts/decision-artifacts-v2/schema.json)。Schema 2020-12，
封闭对象，全部列出字段必填；仅 `Validity.valid_until` 可 null。无隐式默认、无 latest 解析，
未知版本、类型、字段、额外角色、缺字段、错误时间精度均拒绝。正文引用是精确不可变 ID 或
C1-A §2.1 的 `ExternalRef` / §5.1 的 `SnapshotRef`，不能用可变 URL 或人类版本替代。

本版四种正文 `artifact_type` 为 `decision_charter`、`candidate_snapshot`、
`read_set_snapshot`、`sealed_ballot`，均 `schema_version="2.0"`。
`version` 是人类发布版本，仍入摘要；它与 Schema 版本分别校验。
DomainProfile、PersonaSpec、ContextManifest 继续使用现有 `schema_version="1.0"`；
Manifest v1 的 `charter_id` 已能明确引用 v2 Charter，须新增消费者能力协商才允许使用。
ReadSet v1.0 仅有 C1-A 语义提案及 C0-R 最小身份探针，没有可自动迁移的权威实例。

```text
canonical = C0.CanonicalizeWithLimits(body)
SchemaAndSemanticValidate(canonical, expected_type, external_trust_state)
digest = SHA256(UTF8("magi:canonical:v1:" + artifact_type + "\n") || canonical)
artifact_id = prefix + "_" + lowercase_hex(digest)
Envelope = {artifact_id,digest,canonical_profile:"magi-cj/1",digest_alg:"sha256",body}
```

类型前缀仍是 `chtr`、`csnp`、`rset`、`sbll`。**不注册新 C0 类型，不改域分隔符**；
`schema_version`、`artifact_type` 及所有权威字段入摘要，因此同类 v1/v2 身份分离。
自己的 `charter_id/charter_digest`、`snapshot_id/snapshot_digest`、`ballot_id/ballot_digest`
是 Envelope 的领域别名，**不在正文**。C0 以前公布的任何输入字节仍计算以前的 ID；
不能将旧 ID 套在去掉自引用或补字段的正文上。子引用的 digest 不剔除。

存储回读：限长 → C0 Adopt 原 canonical bytes → Schema/语义 → 类型/版本/请求 ID/包装
逐项复算比较。不得反序列化再编码掩盖不规范字节。四种正文工程上限分别为
262144 / 262144 / 1048576 / 262144 bytes，depth 24、string 16384 bytes；成员上限分别为
4096 / 4096 / 32768 / 4096，另继承 C1-A §2.5 的事件/闭包上限。超限失败，禁止截断。

### 1.1 v1 → v2 完整字段差异

共同新增的 `artifact_type,schema_version,version,created_by,issuance_policy_ref,validity`
均入正文摘要；既有 `frozen_at` 保留，票据新增 `frozen_at` 且等于 `submitted_at`。
`created_by:SubjectRef` 只是声明；资格由先存治理根、批准和提交时的外部校验决定。
字段具体嵌套类型见机器 Schema；本节未更名的 v1 字段与语义全部保留。

| 对象 | 保留正文 | 新增/明确 | 移出正文或消歧 |
| --- | --- | --- | --- |
| Charter | objective, non_goals, success_criteria, hard_constraints, soft_preferences, allowed_costs, decision_owner, resource_owners, authorizers, arbiter, tool_scope, risk_policy, candidate_limit, submission_deadline, objection_deadline, revision_deadline, freeze_deadline, early_selection_policy, validity_horizon | `domain_profile_id:DomainID` 必须是已批准精确版本；`frozen_at:Time`；`no_action_exception_reasons:[string]`（显式空数组表示无例外） | 自己 charter_id/charter_digest；issuer/approval proof 在外部，绑定已算出的 Charter ID |
| Candidate | charter_id, domain_profile_id, candidate_count, candidate_entries, candidates, material_pending, agenda_log, frozen_at | `case_id:Token,round:PositiveSafeInt,context_manifest_id:ContextID,final_shared_inputs:SnapshotRef,final_shared_delivery_receipt_refs:{Role:ExternalRef},persona_views:{Role:RoleView}` | 自己 snapshot_id/snapshot_digest；删除歧义 freeze_signatures，原 persona/spec 声明进入 persona_views，真实三方确认和 closure 在外部 |
| ReadSet | C1-A §6.2 全部字段，包括事件、闭包、本人视图、共享包、cutoff、回执、closure、completeness | 正文版本改 2.0；新增 `execution_generation:PositiveSafeInt,authority_scope_id:Token,authority_epoch:SafeUInt,credential_scope_id:Token,credential_epoch:SafeUInt`；外部 closure 明确三个关闭高水位 | 自己 ID/digest 在包装；completeness 不得包含未来 rset ID |
| SealedBallot | case_id, round, persona_id, persona_spec_id, charter_id, domain_profile_id, snapshot_id, context_manifest_id, experience_snapshot_id, user_preference_snapshot_id, read_set_digest, context_revocation_epoch, execution_generation, authority_proof_id, attitudes, preferences, reasoning_summary, submitted_at | `read_set_snapshot_id:ReadSetID` 必须等于 `rset_` + digest；新增 context_revocation_scope_id、authority_scope_id/epoch、credential_scope_id/epoch、evidence_closure_record_ref | 自己 ballot_id/ballot_digest 在包装；签名/验权结果外置 |
| BallotSet / DecisionRecord | v1 全部语义、引用与 VOTE-09/11/12 | v2 消费模式要求完整闭包可遍历到 v2 终票/ReadSet；不能从绿色 CI 推断已有生产消费者 | 本 PR 不冻结其新机器正文或签发；未来实现须单独版本审阅 |

精确类型补全（原 v1 使用简记的部分）：`SubjectRef` 沿用 C1-A；`arbiter:[SubjectRef]`
长度 0..1，避免 null/缺失双义；`SoftPreference.weight:[Quantity]` 长度 0..1；
`PendingItem={code,description}`；Candidate 的 `conditions=[{statement,must_hold:bool}]`；
`Candidate.risk_classification_ref:ExternalRef` 新增为必填，指向先存的风险分类记录
（候选稳定 ID、domain ID、rules_version、input_attribute_values、input_evidence_refs、
matched_rule_ids、risk_level、rule_ref、classified_by/classified_at、valid_until）；
输入属性以 `{attribute_id,knowledge,value:[bool|Token|Quantity]}` 表示，KNOWN 恰好一值，
UNKNOWN 显式空数组，禁止猜测补值。分类记录在提交时须仍有效；规则解析与独立风险
算法由 C1-B/候选切片核验，规则/属性证据依赖进入后继闭包。该记录不得引用未来
CandidateSnapshot ID。`AgendaEvent.notified:[SubjectRef]`；`source` 为外部权威引用；资源 owners 不得为空；`resource_owners[].owner_rule_ref:ExternalRef` 新增为必填，
精确绑定预先批准的 OWNER_ALL / OWNER_THRESHOLD / SINGLE_DELEGATE 规则，
不能把任何一种模式替换成临时多数。阈值和委托资格按该外部规则验证。
`PairPreference.rationale:[enum]` 长度 0..1：INCOMPARABLE 恰好一项，其余为空，
这是对 v1 可选值的显式表示，不改变票型。金额和权重使用带量纲的安全整数，不引入 float。set 数组按稳定代码/ID 的 UTF-16 序排序，
事件按 sequence；偏好对按 `(candidate_a,candidate_b)` 排序。Schema 不代替完整语义验证。

## 2. 持久唯一身份与各代次（D2-03）

| 权威唯一键 | 永久绑定 | 同键重试 / 冲突 |
| --- | --- | --- |
| `(case_id,round)` | 一个 ContextManifest ID | 相同 ID 幂等；不同 ID `ROUND_BINDING_CONFLICT` |
| `(case_id,round)` | 一个 committed Candidate ID | 准备草案可多份，只有一个可提交；相同已提交 ID 返回原提交；不同 ID 冲突 |
| `(case_id,round,context_manifest_id,persona_id)` | 一个 seal/ReadSet ID 及一个 ballot ID | 相同内容幂等；同键不同 seal/票拒绝，不能 last-write-wins |

Manifest 精确 charter/domain 与 Charter.domain 必须一致；Candidate 的 case/round/charter/domain/
Manifest、三角色 spec/experience/allowed contexts 与 Manifest 逐项相同。角色槽位恰好
`melchior,balthasar,casper`；不得把自己的经验视图填给他人。

决策 round 是语义输入/终票生命周期。ExecutionGeneration 是持久 owner claim 的运行栅栏；
context epoch 是上下文撤销；authority epoch 是权限撤销；credential epoch 是凭据轮换/撤销。
后面三者均比较 `(scope_id,epoch)`，绝不拿不同 scope 的裸 epoch 比较。
新 Generation 不产生新 round，不解开旧唯一键、不重置已提交快照/终票。
相同提交的查询可以在恢复后只读返回原结果，但旧代次工件不能在新代次重新签发或计票；
需要重新决策时显式开新 round，保留旧 round 的 INVALIDATED/审计记录。

## 3. 先交付、再封正文、后确认（D2-02/04）

```text
已批准 Domain → Charter body/ID → 外部 Charter approval
  → Manifest body/ID → 唯一 round 绑定 / 三角色受控 Journal
  → OPEN → QUIESCING → final shared 包 / 注册注入 / 三份核验回执
  → Candidate body/ID → 三份对该 ID 的外部确认
  → 条件提交 closure + Candidate 可消费 + EVIDENCE_CLOSED + 三 Journal INPUT_CLOSED
  → completeness → 各角色 ReadSet body/ID → SealedBallot → 聚合 → 裁决
```

回执 `TrustedDeliveryReceipt` 的正式封闭字段见 Schema：JournalKey、persona_spec_id、
experience_snapshot_id、user_preference_snapshot_id、controlled_session_id、provider_request_id、
final_shared_inputs、registered_injection_sequence、execution_generation、三个 scope/epoch、
delivery_status、verification_record_ref、verified_by、verified_at、quiescence_fence_id。
`delivery_status` 只接受 `APPLIED_VERIFIED`。时间为 UTC 毫秒。

合格注入/会话边界须核验指定包已应用到指定角色受控上下文，登记 sequence 与持久提供方请求/
会话记录、权限、完整依赖一一对应，然后签发原始字节可验真的回执。
时间排序为 `injected_at <= verified_at <= Candidate.frozen_at <= confirmation.confirmed_at <= commit`。
回执不包含尚未计算的 Candidate ID。模型自报、网络 send ack、unknown delivery 均不够；
提供方无法证明应用到精确上下文时，关闭被阻断。这不证明模型理解或忠实执行。

`persona_views` 仅声明与 Manifest 相同的角色 spec/experience/allowed contexts；不是签名。
`CandidateConfirmation` 外部记录绑定 Candidate ID、Manifest/JournalKey、对应 spec、共享包、
本人的 receipt ref、`confirmation_injection_sequence`、controlled_session_id、provider_request_id、
verification_record_ref、generation/epochs、合格确认者及 UTC-ms 时间。确认交付必须在本人的
共享交付之后、关闭高水位之内，精确指向 Candidate 正文版本和摘要。确认需可追到真实角色会话，
不由主持者代签。三个确认必须指向**同一 Candidate ID**。回执与确认均不回填 Candidate 正文。
Candidate 不含 closure ref、ReadSet/票/确认 ID，彻底避免 `Candidate → 签名 → Candidate`。

Charter approval 同样后向引用 Charter ID、domain ID、issuer、先存 policy/AuthorityProof 与
批准 scope/epoch。该先存治理批准的 proof/subject/scope 不要求等于后续执行/终票的
AuthorityProof；各自按操作和权限链核验。正文 created_by/issuance_policy_ref 不自授权。签名真实性、批准者资格及
授权链必须由 C2 核验；本 PR 的合成原件与标记不证明这些事实。

## 4. 唯一证据关闭边界（D2-05）

`OPEN` 接受合格新材料。`QUIESCING` 必须先持久化 fence，阻止普通新材料生产者；
只允许该 fence 下的共享终审包和 Candidate 确认上下文交付，均登记。
关闭前等待所有登记、提供方交付、确认上下文交付及依赖反向边结算。
未知/失败交付、缺日志或未结算注入不能通过删除记录补救。

Candidate ID 算出且三确认齐备后调用权威 **CommitIfCurrent**。在同一个可序列化的条件提交
线性化点，必须同时：

1. 校验持久 owner/有效 lease（锁后权威时间）、generation、三个 scope/epoch、权限/依赖、
   Charter 窗口与包新鲜性、fence、三份回执/确认、pending_injections=0 和唯一键。
2. 固定一个 Candidate ID 为 COMMITTED/可消费。
3. 写 `EVIDENCE_CLOSED` 及共同 `evidence_cutoff_sequence`（权威全序序号）。
4. 将三个 Journal 变为 `INPUT_CLOSED`，固定各自 `close_high_watermarks`（本地序号）。
5. 保存不可变 `EvidenceClosureRecord`、唯一提交 token、序号/UTC-ms 时间与对应全部引用。

`EvidenceClosureRecord` Schema 固定 case/round/Manifest/Candidate、三视图、共享包、三回执/
确认、generation/epochs、fence、cutoff、三关闭高水位、提交状态、commit_id 与时间。
它在 Candidate ID 之后产生；只能由 ReadSet 后向引用，不写回祖先。
不存在候选先可读、再异步补 EVIDENCE_CLOSED/Journal 的合法窗口。跨服务实现须提供等价
不可半提交协议及可验证提交见证；普通异步消息/补偿事务本身不能充当证明。

ROLLOUT-LAB 新鲜性为 30000 ms：从可信源**观测时间**计到线性化提交时刻，等于上限允许，
大于上限拒绝；复制/重发/回执时间不刷新它。外部时间与依赖有效期均按半开区间核验，
锁后再检查；其他 Domain 按精确已批准配置。普通更新可在截止前重准备，重新获取三回执、
候选及确认；已提交候选不可替换。撤销/决定性变化使旧轮失效并开新轮。

### 4.1 失败、恢复与撤销

| 情形 | 必须的结果 | 当前证据级别 |
| --- | --- | --- |
| 写任何子项前失败 | 全部未提交，候选不可消费 | 设计事务义务；MySQL NOT RUN |
| 中途 worker 崩溃/超时 | 原子回滚或提交见证整体可读，禁止半提交 | 设计事务义务；MySQL NOT RUN |
| 提交成功但响应丢失 | 按同一唯一键+Candidate ID+commit token 查权威结果；返回原 closure，不能再关闭或重发 | 静态重复记录检查；真实 reply-loss NOT RUN |
| 不同 Candidate/Manifest 的重放 | 唯一键冲突；旧 committed 内容不可覆盖 | 离线绑定反例；真实并发 NOT RUN |
| fence 后仍有在途注入 | 拒绝 commit，先结算；unknown 状态阻断 | 离线声明一致性；真实会话 BLOCKED C1-C |
| 验权后、提交前 epoch/generation/owner 改变 | 锁内 CAS 重验失败；不产生可消费候选 | 离线条件声明；真实锁/CAS BLOCKED C2/C3 |
| source/spec/permission 撤销 | 反向依赖传播至 ReadSet→票→裁决→待执行动作；停止污染会话并重建；缺边保守失效 | 设计义务；真实传播 BLOCKED C1-C/C2/C3 |

## 5. ReadSet 与终票（D2-06）

ReadSet v2 完整保留 C1-A §6.2 的事件/SourceNode、JournalKey、persona/spec/experience/preference、
源依赖索引、共享包/本人回执、共同 closure/cutoff、完整性证明。events 是实际注入的连续前缀，
`journal_high_watermark == len(events) >= close_high_watermarks[role] > 0`。
Manifest 的 allowed views 是准入上限，不能拿它代替实际观察记录。
每个事件精确匹配 JournalKey、角色和代次，SourceNode 必须在闭合无环 dependency_nodes 中，
同一 node ID 不能换内容；RAW/EMPTY_RESULT 无父，DERIVED 有父且有固定方法版本。

INPUT_CLOSED 后只可重读关闭前已准入的精确节点/派生版本，仍新增 ReadEvent；须能从
关闭前的准入记录证明节点已存在，`authority_sequence > cutoff` 不能引入新节点。
ReadSet 与 closure 共同包/Manifest/Candidate/cutoff/epochs 必须相同，各人 ReadSet 可有不同
经验和合法重读。封存前停该角色所有输入，核对提供方日志与事件前缀，再写外部 completeness
记录（绑定完整事件、高水位及结算；不引用未来 rset ID），最后派生 ReadSet ID。

终票同时引用 ReadSet ID 与 digest，逐项校验角色、视图、case/round/Manifest/Candidate、
共同 closure、AuthorityProof、generation/epochs。裸的任意 64 位 digest 不足够。
独立提交、BallotSet 聚合、DecisionRecord 发布各自调用 VerifyAuthority / CommitIfCurrent；
不能只在聚合层验证，不能用旧缓存的“有效”绕过撤销。

VOTE-09 不变：三固定槽位各 SEALED 或 ABSENT 才能形成集合，集合摘要覆盖完整权威内容；
ABSENT 明确无 ballot/read-set，绝不补造 ABSTAIN。VOTE-11/12 不变：每票独立绑定持久
generation/当前 owner 和 AuthorityProof，提交/聚合/裁决分别验证。v1 的 k(k-1)/2 成对偏好、
偏序、硬阻断和选择充分性全部继承；本 PR 静态检查不实现完整投票算法。

## 6. C1/C2/C3 接口与依赖 DAG（D2-08）

接口为后续实现要求，**现在不存在 v2 生产实现**：

```text
VerifyAuthority(exact_dependencies, operation, subject, case_round,
                generation, scoped_epochs, trusted_now)
  -> verified expected-state token | fail-closed
CommitIfCurrent(unique_key, target_id, commit_token, expected_state,
                complete_write_set)
  -> original-or-new committed record | conflict/stale/unknown
```

验证 token 必须绑定操作/完整输入、subject、proof、唯一键、generation、scope/epoch、期限及
权威状态版本；不是授权模型凭声明自签的 bearer token。提交在锁定权威行后复验当前状态、
lease/可信时间和完整写集。两个独立 RPC 之间无条件写入不满足此接口。
C2 负责认证根/委托链/撤销及资格；C3 负责可比较权威顺序、提交见证和出口；C1-C 负责
受控注入、实际读取与依赖完整性。截止、epoch 和 owner 变化都使旧 token 失效。

| ID / ref | producer → persistent authority → consumer | 撤销/失效触发 | 测试责任 |
| --- | --- | --- | --- |
| dprf / pspc | 编辑/人格维护 → C2 先存治理根+发布登记 → Charter/Manifest/所有下游 | 治理撤销/资格/期限，普通新版不热替换旧版 | C1-B1/B2 离线已有；C2 发布 BLOCKED |
| chtr / approval | 有权主体 → 外部批准登记 → Manifest/Candidate/票 | 目标/范围改动、主体权限撤销 | #38 结构/绑定；C2 资格 BLOCKED |
| ctxm / round binding | 上下文管理器 → 唯一轮次登记 → Journals/Candidate/ReadSet | source/spec/经验/偏好/权限撤销 | #38 静态；C1-C 并发 BLOCKED |
| SnapshotRef / SourceNode | 可信采集/派生 → 不可变原件+准入/反向边 → 注入包/Journal | 原件/父/方法撤销、freshness/权限到期 | C1-C/C2；#38 合成闭包 |
| receipt | 注入/会话边界 → 持久请求+交付核对 → Candidate/closure/ReadSet | unknown 交付、错误视图/epoch、伪造来源 | #38 声明反例；C1-C 真实 provider BLOCKED |
| csnp / confirmation | 议程器/三受控角色 → 外部确认+唯一条件提交 → closure/票 | 议程/共享输入/权限改变、同键冲突 | #38 离线；C1-C/C2/C3 事务 BLOCKED |
| closure / cutoff / fence | C2/C3 commit authority → 原子提交日志+3 Journal → ReadSet/票/裁决 | owner/代次/epoch 变化、缺写集 | #38 静态完整性；MySQL 故障 NOT RUN |
| completeness / rset | 受信任记录器 → 原件核对+唯一 seal → 本人终票 | 缺事件/未登记交付/闭包污染 | #38 静态；C1-C 真实 seal BLOCKED |
| sbll / aprf / bset | 受控 Persona / C2 → 独立接票+聚合登记 → 裁决 | 错视图/旧 round/代次/撤销证明 | #38 绑定；票据/C2/C3 集成 BLOCKED |
| drec → ActionIntent/witness/send | 裁决器 → C2/C3 顺序与出口 → 执行 | 依赖撤销、FINAL CHECK/授权失败 | C2/C3；本 PR NOT RUN |

箭头表示下游依赖上游，不要求父正文引用后继。反向索引是外部增补登记，必须与材料交付/
下游发布不可漏登记；缺边、无法核对可信高水位时保守失效。

## 7. 兼容、协商与回滚（D2-07）

| 记录 / 消费者 | v2 权威使用 | 允许的行为 |
| --- | --- | --- |
| 旧 v1 缺 domain/Manifest/receipt | 拒绝 `UNSUPPORTED_CONTRACT` | 保留原字节和原算法只读展示，标为 LEGACY/NON_AUTHORITATIVE |
| v1 ContextManifest + 已批准 v2 Charter | 仅全部消费者显式支持时 | 保留 ctxm v1 Schema，校验完整新引用链；不能拿旧消费者绿灯替代能力 |
| v2 工件 / v1 消费者 | 拒绝 | 不把 schema_version 降写或丢字段转回权威 v1 |
| v2 工件 / v2 离线检查器 | 结构/身份/绑定可以检查 | 不授予生产签发、投票、发送权限 |
| legacy ExecutionGeneration=0 | v2 一律拒绝 | 保留 legacy 读取；不自动补 1 或当前 generation |
| 普通发布新 Domain/Persona | 旧轮不热替换 | 新任务显式解析一次后冻结，旧轮仍需在线撤销检查 |

启用前 producer、store、reader、受控注入、seal、submit、aggregate、adjudicate、C2、C3
均声明接受的 `(artifact_type,schema_version,canonical_profile)`，包含输入 v1 和上述四类 v2。
任何环节未知/不支持则阻断新模式，不启用后自动降级。Capability 本身来自部署权威，不能由
模型或工件正文授予。现有 C1 Schema 仍拒绝新类型；未来消费者须另行实现。

回滚仅停止新 v2 签发/写入，保留不可变原件、唯一键、撤销序列和审计；旧消费者不能接管
未完成 v2 轮次，须停用/失效并由支持版本的实现恢复。绝不改旧摘要或把 Generation 0 数据
重编码为当前权威记录。重发或删字段不构成迁移。

## 8. 静态验收与证据边界

执行入口 `python3 scripts/check-decision-artifact-v2.py`，内部调用现有 Go C0/JSON Schema，
Python 独立复算新增完整正文 Golden。合成 bundle 含可解引用的外部记录和权威状态**声明**，
正例/负例只证明离线结构、身份及引用一致性。外部合成 digest 只证字节相等，不能证签名/
发布资格、提供方交付或真实原子性。该工具不接入生产、不签发工件、不是新协议 Go 实现。

负例经过重新寻址，尽可能绕过单纯 hash 不匹配去触发语义检查；独立保留未重哈希篡改用例。
期望拒绝错误码也固定，不能把异常、panic、缺文件或任意失败计为负例通过。
契约门禁不读取报告中的 PASS 字样决定成功，状态机义务由本文件 §4 审阅。

必须独立验收且本 PR 明确 **NOT RUN / BLOCKED**：三个真实 provider 交付核对、锁后时间/
epoch CAS、唯一键并发、每个写步骤 kill、提交后回复丢失与恢复、未登记交付、撤销跨会话传播、
独立提交/聚合/裁决授权、C3 witness/受保护发送、完整 k=1..4 投票模型、4800 轮行为实验。
已有 MySQL CI 即使通过，也只证明旧协议测试，不能证明上述 v2 义务。

A–F 交付记录见 [独立审阅报告](decision-artifact-v2-review.md)。Draft 保持 Draft，待独立审阅
明确认可范围与 P0/P1 闭合后才能讨论 Ready；合并、关闭 Epic/CAS-9、改 Rev4 均不在本交付授权内。
