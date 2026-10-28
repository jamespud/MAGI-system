# `JOURNAL_ADMIT` v2：预暴露准入、暴露围栏与结算（Design Change）

> 关联：[Roadmap #2](https://github.com/jamespud/MAGI-system/issues/2)、[Stage #43](https://github.com/jamespud/MAGI-system/issues/43)、[#45](https://github.com/jamespud/MAGI-system/issues/45)、[#47](https://github.com/jamespud/MAGI-system/issues/47)、M1-00 [#54](https://github.com/jamespud/MAGI-system/issues/54)。
>
> **状态：M1-02A Admission Commit Core implemented / default-off；Gate 与 Settlement 仍为 Draft。**
> M1-02A 分支实现已提供真实 MySQL
> `JOURNAL_ADMIT_V2` 共同权威提交、原子 admission 写集和不可消费的 blocked fence。
> 本文件不实现 `JOURNAL_EXPOSURE_GATE_V2`、`JOURNAL_SETTLE_V2`、独立 witness、真实
> Provider 暴露或生产启用；`APPLIED_VERIFIED` 仍无入口。

## 0. 结论先行

现有 v1 `JOURNAL_ADMIT` 是 **post-apply settlement**：它假设来源准入、provider/session pending 登记和可信应用证据已经存在。M1 的真实材料读取需要的是相反的提交顺序：

```text
pre-exposure admission -> enforceable exposure fence -> provider exposure -> post-apply settlement
```

因此本 Design Change **不原地重定义 v1**，而是新增三个显式版本化操作：

| 操作 | 作用 | 不允许做的事 |
| --- | --- | --- |
| `JOURNAL_ADMIT_V2` | 在可能暴露前持久登记来源、依赖、provider reservation、HWM、pending 和 exposure fence | 不写 `APPLIED_VERIFIED`，不声明真实读取，不把 admission 当成 exposure permission |
| `JOURNAL_EXPOSURE_GATE_V2` | 在权威总序上取得一次可强制执行的暴露围栏；Provider 必须能校验围栏和撤销水位 | 不允许仅查询一次 epoch 后直接暴露；Provider 不支持围栏时 `BLOCKED` |
| `JOURNAL_SETTLE_V2` | 独立核验 provider 原件后结算 pending | 不把网络 ACK、模型自报或重复回执当作真实应用 |

v1 的 `JOURNAL_ADMIT` 语义、Schema、Golden 和历史记录保持不变。v2 使用独立契约版本：

```text
magi-authority-journal-v2/1
```

## 1. 为什么需要 v2

#53 已交付默认关闭的本地 durable pre-exposure registration：它能在本地 MySQL 中先登记 delivery 和 pending，但明确不实现 authority sequence、共同总序或 `APPLIED_VERIFIED` 入口。v1 `JOURNAL_ADMIT` 的 `CompleteWritePlan` 则把 `provider_settlement(PENDING -> APPLIED_VERIFIED)` 和 post-apply Journal 结算放在同一计划中。

把两者直接拼接会产生三个错误：

1. **把登记误当应用**：本地 `REGISTERED` 不代表 Provider 已应用，更不代表模型读取。
2. **把本地序号误当权威序**：本地连续 HWM 不能与 C2/C3 的 authority sequence 比较。
3. **在登记与暴露之间留下未定义窗口**：admission 提交后、真正暴露前，权限可能被撤销或 Generation 改变。

v2 把三个事实分开：

- `ADMITTED`：已取得持久准入和 provider reservation；
- `EXPOSURE_GATED`：已在权威总序取得可执行的 exposure fence；
- `APPLIED_VERIFIED` / `UNVERIFIABLE`：已取得可信 provider 原件，完成结算。

## 2. 版本与兼容边界

| 项目 | v1 | v2 |
| --- | --- | --- |
| contract version | `magi-authority/1` | `magi-authority-journal-v2/1` |
| operation | `JOURNAL_ADMIT` | `JOURNAL_ADMIT_V2`, `JOURNAL_EXPOSURE_GATE_V2`, `JOURNAL_SETTLE_V2` |
| 暴露前登记 | 未纳入 typed plan | `JOURNAL_ADMIT_V2` 原子写集 |
| provider settlement | 与 admission 计划同阶段 | 独立 `JOURNAL_SETTLE_V2` |
| 暴露围栏 | 无 | 权威序 gate 或 Provider-enforced lease |
| 旧记录 | 保留原身份 | 不自动升级、不回写 |

v1 记录只能作为 `LEGACY`/历史展示；不得因为 v2 代码存在而自动获得 v2 权威身份。v2 consumer 拒绝 v1 的 `JOURNAL_ADMIT` 作为 pre-exposure admission。

## 3. 状态机与唯一线性化点

```text
ABSENT
  --JOURNAL_ADMIT_V2 commit--> ADMITTED
  --JOURNAL_EXPOSURE_GATE_V2 commit--> EXPOSURE_GATED
  --provider exposure (external)--> possibly exposed
  --JOURNAL_SETTLE_V2 commit--> APPLIED_VERIFIED | UNVERIFIABLE

ADMITTED --revocation before gate--> REVOKED
EXPOSURE_GATED --revocation before/at gate--> REVOKED
EXPOSURE_GATED --revocation after gate, settlement uncommitted--> UNKNOWN
```

线性化点：

- admission 的线性化点是 `JOURNAL_ADMIT_V2` 的权威 commit；不是本地 INSERT，也不是 provider reservation。
- gate 的线性化点是 `JOURNAL_EXPOSURE_GATE_V2` 的 authority sequence；不是发送前查询。
- settlement 的线性化点是 `JOURNAL_SETTLE_V2` 的权威 commit；不是 provider ACK 或 HTTP 200。
- 外部 Provider 调用不属于本地事务。v2 不声称 DB、Provider 和网络三者原子；它要求 Provider 强制执行 fence，或在能力不足时保持 `BLOCKED`。

## 4. TOCTOU 与暴露围栏

仅在发送前读取一次当前 revocation epoch **不能**消除以下窗口：

```text
read epoch -> revocation commits -> provider receives material
```

v2 只接受两种可实施模式：

1. **`AUTHORITY_ORDERED_GATE`**：`JOURNAL_EXPOSURE_GATE_V2` 与 revocation 在同一权威总序；Provider 或出口在暴露时校验 gate sequence 不低于当前 revocation watermark。
2. **`PROVIDER_ENFORCED_LEASE`**：admission 生成的 lease 绑定 admission commit、authority watermark、Generation、scoped epochs、Provider session scope 和过期时间；Provider 在真正接收材料时强制校验 lease 未过期、未撤销、Generation/epoch 匹配。

不允许的模式：

- `QUERY_ONLY`：没有 Provider 侧强制执行；
- 只把 lease ID 放进请求，但 Provider 不校验；
- 把“本地 admission 已提交”当成“允许暴露”；
- lease 过期后重发相同 admission 并自动续期。

Provider 必须满足以下能力，否则结果只能是 `BLOCKED`：

- 能验证 fence/lease；
- 能验证 revocation watermark 或等价的权威序水位；
- 能验证 ExecutionGeneration 和 scoped epochs；
- 能提供可独立核验的应用/读取原件；
- 能区分网络 ACK、请求接受、实际应用和读取完成。

## 5. 撤销与提交顺序

v2 将 revocation、admission、gate、settlement 放在同一可比较的 authority sequence 中。撤销判定必须在持久锁内重新读取，不得使用调用方时间或本地缓存。

| 顺序 | 结果 |
| --- | --- |
| revocation sequence < admission sequence | admission `REVOKED`，不得产生 fence 或暴露 |
| admission sequence < revocation sequence <= gate sequence | gate `REVOKED`，不得暴露 |
| gate sequence < revocation sequence < settlement sequence | 已发生的暴露保留事实；settlement 只能记录可验证结果，不能生成新的暴露许可 |
| settlement 已提交后收到重复 settlement | 返回原 lineage；不重复扣 pending、不重复写结算 |
| revocation 与 gate 并发 | 由同一 authority sequence 的先后决定；没有“双方都成功”的中间态 |
| revocation 状态不可读 | `UNKNOWN`/失败关闭，不得继续暴露 |

暴露 gate 必须满足：

- `gate.linearization_sequence > authority.admission_sequence`，同序号或更早序号拒绝；
- `gate.trusted_now` 必须与 `authority.trusted_now` 一致；
- lease 过期和 Provider 最大 lease 上限均使用 `authority.trusted_now` 判断；
- settlement 引用的 gate 必须与当前权威记录精确同一 `authority.gate_sequence`，不能引用更早 gate。

R0 只读场景也必须遵守这一顺序。R0 的“无受保护写入”不等于“无数据暴露”，因此仍需要 admission、fence 和 settlement 的真实证据。

## 6. 幂等、重放与响应丢失

Admission 唯一键：

```text
(trust_domain, case_id, round, context_manifest_id, persona_id, delivery_id)
```

`provider_request_id`、来源/方法/权限、scoped epochs、exact dependency digest、admission plan digest 必须绑定到同一记录。ExecutionGeneration 是当前性检查，不是“重置唯一键”的手段。

规则：

- 同键同字节 commit 重试不得仅凭原请求或 commit token 返回历史 lineage；调用方必须
  通过独立的 `JOURNAL_AUDIT_READ` C2 operation 读取历史记录。审计读取返回原
  admission lineage、`exposure_permitted=false`，只能 reconciliation，且不要求原
  execution owner lease。
- admission 的 pending transition 为 `N -> N+1`，`0 <= N < 512`；允许同一 Journal 存在多个尚未结算的 delivery。
- settlement 只有在 `APPLIED_VERIFIED` 时执行 `N -> N-1`；`UNKNOWN` 与 `UNVERIFIABLE` 必须保持 `N -> N`，不得释放可能已暴露的 pending。
- 即使 `UNKNOWN`/`UNVERIFIABLE` 不能确定真实读取，也必须保留 pending 和 reconciliation 线索。
- settlement 必须有独立的不可变 `settlement_digest`，覆盖完整 settlement 请求正文和 provider receipt；摘要域为 `magi:journal-settlement-v2`，计算时排除 `settlement_digest` 自身。
- `admission_binding` 与 `settlement_binding` 是两种不同的持久身份。Admission 绑定保留原始 admission request digest、Generation 和 admission commit；Settlement 绑定独立保存 settlement digest、admission commit、provider request、Generation 和 settlement commit。
- 首次 Settlement 前 `settlement_binding` 必须不存在；首次提交在创建 settlement lineage 时原子创建它。`authority.settlement_sequence > 0` 但 binding 不存在时必须 `CONFLICT`，不能把缺失 binding 当作首次提交。
- 同键且完整原件摘要相同才返回 `REPLAY`；同一 Admission 下更换 receipt/verification 原件必须返回 `CONFLICT`，不能仅凭共享的 admission request digest 判为重放。
- 同键不同字节：`CONFLICT`，不得覆盖。
- 旧 Generation 重放同一键：`STALE`，不得取得新 fence。
- admission 响应丢失：按同键查询原 lineage，不得再次暴露。
- gate 响应丢失：查询原 gate；同 fence/同 sequence 返回原记录，不生成第二个 gate。
- settlement 响应丢失：同键查询原 settlement；不得重复扣 pending。
- Provider 返回 `UNKNOWN`：保留可能暴露的事实，不自动重发，不得写成 `APPLIED_VERIFIED`。
- Provider 返回 `UNVERIFIABLE`：保留 pending/unknown 状态，不得关闭 Journal 或产生可信读取声明。

## 7. 写集与事务顺序

### 7.1 `JOURNAL_ADMIT_V2` 写集

```text
admission
dependency_edges
exposure_fence
journal_event
journal_high_watermark
lineage
outbox
pending_transition
provider_reservation
token_consumed
unique_binding
```

禁止出现：

```text
provider_settlement
APPLIED_VERIFIED
read_completed
```

### 7.2 `JOURNAL_EXPOSURE_GATE_V2` 写集

```text
exposure_gate
fence_consumed
authority_sequence
lineage
outbox
token_consumed
unique_binding
```

### 7.3 `JOURNAL_SETTLE_V2` 写集

```text
provider_settlement
pending_transition
lineage
outbox
token_consumed
unique_binding
```

settlement 不修改 admission 正文、不替换 fence、不删除 unknown 记录。settlement plan 的 `settlement_digest` 必须绑定完整 settlement 正文、receipt 和 verification ref；首次 Settlement 原子创建 `settlement_binding`，之后的精确重放只使用该独立 binding，而不是 admission request digest。

### 7.4 锁序

固定锁序建议：

```text
authority sequence
  -> scoped epochs
  -> owner / ExecutionGeneration
  -> journal
  -> dependency nodes and reverse edges
  -> token / unique key
  -> provider reservation
  -> exposure fence
  -> settlement / pending transition
```

锁等待后必须使用 DB 可信时间重新检查：

- owner/lease 与 Generation；
- authority/context/credential scoped epochs；
- revocation sequence；
- fence lease 和 Provider capability；
- exact dependencies 与 pending 数量；
- 同键 token/unique binding。

### 7.5 MySQL 草图

以下只是静态事务草图，不是可运行 SQL：

```sql
-- admission
SELECT authority_sequence FROM authority_head
 WHERE trust_domain=? FOR UPDATE;
SELECT ... FROM a45_epoch_scope WHERE ... FOR UPDATE;
SELECT ... FROM a45_journal WHERE ... FOR UPDATE;
SELECT ... FROM a45_unique_binding
 WHERE trust_domain=? AND unique_key=? FOR UPDATE;
INSERT INTO a45_admission (... state='ADMITTED', commit_id=..., fence_id=...);
INSERT INTO a45_provider_reservation (... state='RESERVED');
INSERT INTO a45_exposure_fence (... state='UNCONSUMED', lease_until=...);
UPDATE a45_journal
   SET high_watermark=?, pending_count=pending_count+1, version=version+1
 WHERE ... AND high_watermark=? AND version=?;
INSERT INTO a45_token_consumed (...);
INSERT INTO a45_lineage (...);
INSERT INTO a45_outbox (...);

-- gate
SELECT ... FROM a45_admission, a45_exposure_fence, authority_head
 WHERE ... FOR UPDATE;
-- assert revocation_sequence <= admission_sequence => REVOKED
-- assert provider capability and lease current
UPDATE a45_exposure_fence SET state='CONSUMED'
 WHERE ... AND state='UNCONSUMED' AND lease_until > DB_NOW();
INSERT INTO a45_exposure_gate (... authority_sequence=...);

-- settlement
SELECT ... FROM a45_provider_reservation, a45_exposure_gate, a45_journal
 WHERE ... FOR UPDATE;
-- assert verified provider original and exact request/session/admission binding
UPDATE a45_provider_reservation
   SET state=? WHERE ... AND state='RESERVED' AND version=?;
-- Only APPLIED_VERIFIED decrements one pending delivery.
UPDATE a45_journal SET pending_count=pending_count-1, version=version+1
 WHERE ... AND pending_count>0;
-- UNKNOWN/UNVERIFIABLE keep pending_count unchanged and retain reconciliation state.
INSERT INTO a45_provider_settlement (...);
```

任何一步 rows affected 不等于 1 都必须整体回滚；不能以“已写部分行”作为成功。

## 8. 类型与静态契约

声明文件：

- [`scripts/journal-admit-v2-api.go`](../scripts/journal-admit-v2-api.go)
- [`docs/contracts/authority-journal-v2/schema.json`](../docs/contracts/authority-journal-v2/schema.json)
- [`docs/contracts/authority-journal-v2/traces.json`](../docs/contracts/authority-journal-v2/traces.json)
- [`scripts/check-journal-admit-v2.py`](../scripts/check-journal-admit-v2.py)
- [`scripts/test-journal-admit-v2.py`](../scripts/test-journal-admit-v2.py)

静态覆盖（46 个 trace）包括：

- v1 身份不被重定义；
- admission 与 settlement 分离写集；
- `QUERY_ONLY` fence 被 Schema 拒绝；
- 三类 scoped epoch 必须各自唯一且与当前状态一致；
- 同一 Journal 的多个未结算 delivery 可执行 `N -> N+1` 和 `N -> N-1`，并受 512 上限约束；
- `UNKNOWN`/`UNVERIFIABLE` 不得减少 pending；
- gate 必须晚于 admission，使用 authority 可信时间，并与 settlement 引用的 gate 序号精确一致；
- Provider 缺少 fence/revocation 强制能力时 `BLOCKED`；
- revocation 先于 admission/gate 时 `REVOKED`；
- gate 后撤销、settlement 未提交时 `UNKNOWN`；
- 旧 Generation 不能重新取得 admission；
- 同键同字节 commit 重试只返回 `AUDIT_AUTHORIZATION_REQUIRED`；独立 authenticated
  `JOURNAL_AUDIT_READ` 成功后只返回 lineage；
- 同键不同字节 `CONFLICT`；
- settlement 的 admission/fence 绑定不一致时 `TOKEN_BINDING`；
- Admission → Gate → 首次 Settlement 在 admission binding 不变、settlement binding 不存在时成功；
- 首次 Settlement 回复丢失后，同 settlement binding/原件返回 `REPLAY`；
- `settlement_sequence > 0` 但 settlement binding 缺失时返回 `CONFLICT`；
- 已结算后更换 receipt/verification 原件返回 `CONFLICT`；
- settlement 自身摘要与正文不一致时 `TOKEN_BINDING`；
- 未验证 receipt `DENIED`；
- `UNKNOWN` 不能伪装成 `APPLIED_VERIFIED`；
- 缺 fence/receipt 被 Schema 拒绝。

## 9. 结果与证据边界

静态检查结果只用于证明声明、字段和语义分支：

| 结果 | 含义 |
| --- | --- |
| `STATIC_VALID` | 声明通过静态顺序/字段检查 |
| `BLOCKED` | 所需 Provider/fence/authority 能力不可用 |
| `REVOKED` | 撤销先于相应线性化点 |
| `EXPIRED` | 可信时间或 lease 已过期 |
| `STALE` | Generation/epoch/版本不再当前 |
| `CONFLICT` | 同键/目标/状态冲突 |
| `TOKEN_BINDING` | 目标、provider request 或 token 绑定不一致 |
| `DENIED` | 权限或回执真实性不足 |
| `UNKNOWN` | 结果无法判定，不能当作未执行或成功 |
| `REPLAY` | 返回原 lineage；不产生新暴露许可 |
| `WRITE_SET` | 写集不完整、混入错误阶段或派生计划不合法 |

**M1-02A 已解除 / 仍 BLOCKED：**

- `JOURNAL_ADMIT_V2` 的 MySQL common order、C2 重验、owner/lease/generation、
  Journal/HWM/pending CAS、幂等、response-loss 与写点故障已实现并验证；证据见
  [`m1-02a-authority-commit-core.md`](m1-02a-authority-commit-core.md)。
- 真实 Provider fence/lease/revocation enforcement 仍为 BLOCKED；
- `JOURNAL_EXPOSURE_GATE_V2`、`JOURNAL_SETTLE_V2`、独立 witness、
  真实暴露/读取与 provider original reconciliation 仍为 BLOCKED；
- 三份 genuine receipt、Evidence Closure、SealedBallot、DecisionRecord 仍为 NOT RUN；
- 生产启用、受保护 Egress 与外部发送仍未实现。

M1-02A 只解除 #47 的 Admission Commit Core 子集；它不使 M1 通过，也不授予
`PASS_M1` 或真实 Provider 暴露权限。
