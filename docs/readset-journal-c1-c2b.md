# C1-C2B：Durable ReadSetJournal 本地预暴露登记切片（#44）

日期：2026-10-24（Asia/Shanghai）

基线：`c2c5691210e736a632c0033c196d3305e356ee9a`（#42 ReadSet v2 与 #45 Draft 已合入）

分支：`codex/44-durable-readset-journal`

## 0. 结论先行

本切片交付一个 **默认关闭、未生产接线** 的本地 durable pre-exposure
registration primitive：只有新建登记的 `Register` 结果同时满足
`Created=true`、`ExposurePermitted=true`、`RequiresReconciliation=false` 时，调用方
才可考虑首次 provider 提交。任何 exact duplicate（包括 pending/reply-loss 和
UNKNOWN/UNVERIFIABLE）都是 `ExposurePermitted=false` 且必须 reconciliation，不能
据此重发。Journal 登记本身不证明 provider 已应用，也不证明模型真实读取。

本实现 **不是** #45 `AuthorityContract` 的实现，**不声称与其 wire-compatible**。当前
`JOURNAL_ADMIT` 的 `CompleteWritePlan` 表示已取得可信应用证据后的 eventual
post-policy settlement write，不包含“登记先于可能暴露”的 typed 原子事务。因此：

- 本地只写 `REGISTERED` + provider `PENDING/UNKNOWN/UNVERIFIABLE`；
- 没有写 `APPLIED_VERIFIED` 或 `FAILED` 的应用入口；
- `authority_sequence`、provider verification bytes、`injected_at`、`verified_at`
  保持 NULL；
- 真实 provider apply、C2/C3、权威序、候选关闭与生产启用均为 **BLOCKED**；
- 要进入生产，必须先完成 #45/#47 的 pre-exposure admission transaction
  variant/Design Change，而不是复用或扩展一个语义不相容的本地协议。

## 1. 范围与非目标

### 1.1 本切片范围

- 真实 MySQL 迁移、GORM model 与 adapter repository。
- 冻结合同 `inputartifact.JournalKey`/`SourceNode` 的复用。
- exact canonical registration payload、每 Journal 连续本地前缀、HWM CAS。
- 依赖节点、parent edge 与 reverse index；orphan/cycle/missing ancestor 拒绝。
- 既有 DecisionJob lease + Case `ExecutionGeneration` owner fence；锁等待后重新校验。
- context/authority/credential 三类 `(scope_id, epoch)` 与 generation 分离。
- delivery ID 幂等：同 ID/同字节返回原结果但绝不返回 exposure permit；同 ID/不同字节冲突。
- Journal generation 在首次 append 锚定；新 generation 只能 read/replay/reconcile。
- restore 使用锁后 DB time 对所有 required node 做 half-open freshness 检查。
- provider timeout/unknown/unverifiable 分类；unknown 不减少 pending，不自动重发。
- crash/restart 的 read-only reconciliation 与 post-revocation read fence。
- 仓库内 unit RED→GREEN 和真实 MySQL 并发/故障测试。
- 把已持久事实保守适配到既有 #42 `ReadSetValidationOptions.Deliveries`；不改
  冻结 Schema、Golden、身份或 validator 语义。

### 1.2 非目标

- 不调用 provider、不发送材料、不恢复会话、不宣称真实模型读取。
- 不实现 #45 `AuthorityContract`、token、issuer、authority sequence 或 shared order。
- 不写 `APPLIED_VERIFIED`/`FAILED`，不签发 `TrustedDeliveryReceipt`。
- 不实现 Journal `QUIESCING`/`INPUT_CLOSED` 的生产转换、三方 quiescence、
  Candidate closure、ReadSet seal、SealedBallot、C2/C3 或 protected action。
- 不做生产 bootstrap/module 注册，不使用 env/config 默认开启。

## 2. #45 缺口与所需 Design Change

### 2.1 为何不能直接复用 `JOURNAL_ADMIT`

#45 当前模型要求：

1. 先有暴露前来源/权限/scope 资格；
2. 已有持久 provider/session PENDING 登记；
3. 取得可信 provider 原件后，才提交 applied event、settlement 与 pending 转换；
4. authority sequence 由 Authority 独立分配，本地不能伪造。

因此它是 **post-apply settlement** 计划，不是本切片需要的
**pre-exposure registration + provider reservation before expose** 协议。直接复用会：

- 把“登记”误当“已应用”；
- 把本地连续序号误当 authority sequence；
- 在 registry commit 与 provider exposure 之间留下未定义窗口；
- 无法让 runtime owner/lease/Generation 与 shared authority 锁序共同验证。

### 2.2 必须在 #45/#47 明确的 typed transaction variant

名称可以另行冻结，但语义至少必须绑定：

- authenticated principal、exact source/method/permission/scope/epochs；
- exact JournalKey/role/Manifest、runtime owner/job/worker/generation；
- durable provider/session reservation + `PENDING` registration；
- local Journal append、dependency edges、delivery idempotency row；
- expected HWM/state/row-version、trusted window/lease；
- all-writer lock order，以及锁后重新验证 owner/lease/generation/scope；
- commit ordering：durable registration 成功是允许 provider 暴露的必要条件；
- response-loss reconciliation 原 lineage，不自动重发；
- 真实应用后由独立 trusted provider/C2/C3 证据提交 settlement；
- 撤销与发送/登记的共享顺序，或给出等价原子证明。

在这些字段/写集/锁序冻结前，本地实现只能作为 staged/disabled primitive。

## 3. 威胁模型

| 威胁 | 本切片控制 | 仍不能证明 |
| --- | --- | --- |
| 调用方伪造 JournalKey/role/generation/epoch | frozen key shape、positive generation、独立 scope 比较、owner fence | Manifest/provider 身份真实性；C2 授权 |
| stale worker 在锁等待后继续写 | 先锁 DecisionJob/Case，再读 DB time 与 owner/generation/lease；之后才写 Journal | 长事务跨越 lease 截止时的外部执行 |
| owner A→B→A 字符串 ABA | generation 必须相等且单调前进；旧 generation 返回 `ErrLeaseLost` | 真实部署接管演练 |
| 新 generation 追加 event 2 | journal/event generation 锚定；新 generation 只可 read/replay/reconcile | 真实多实例接管演练 |
| 并发 append 重号/跳号 | same-case owner lock + Journal row lock + HWM/state-version CAS；读时验证 `1..HWM` | 未接入旧写者 |
| 同 delivery 不同 bytes | `(journal_id,delivery_id)` 唯一；payload/request digest 与 exact bytes 比较 | provider 外部 idempotency |
| 回包丢失 | 同 ID/同字节可恢复原事实，但 `ExposurePermitted=false`、`RequiresReconciliation=true`；不得重发 | provider 是否已暴露 |
| timeout/unknown | `UNKNOWN` 保留 pending；不生成 validator delivery；无 resend API | true external outcome |
| 无独立 provider 证据 | `UNVERIFIABLE`；APPLIED/authority sequence 不可达 | reconciliation 完整性 |
| 派生材料先于 parent 或 orphan | 完整 ancestor closure、edge 精确匹配、cycle/extra-node 拒绝 | parent 原件真实性/撤销 |
| post-revocation cache replay | 当前 scope/epoch 与登记精确不等即 `ErrRevoked` | 当前 scope 的可信来源；生产 resolver BLOCKED |
| 超大/截断 | event 32768B、members 512、string 8192B、depth 16、nodes 2048、parents 64、events 512、closure/journal 16MiB | 未经版本审阅调整阈值 |
| future/expired source | 锁后 DB time 对全部 required nodes 做 `[not_before, valid_until)` 检查 | 真实 C2 freshness/revocation source |
| 存储损坏 | canonical bytes + raw SHA-256、columns 与 payload 交叉核对、graph 重建 | MySQL 管理员恶意破坏持久性 |

信任边界：本切片信任既有 DecisionJob/Case 表与其锁语义，并把 caller 提供的
`AdmissionScope` 作为测试/未来 trusted resolver 的输入。后者在无 C2 时不能被本地
代码证明为当前值，因此生产启用为 BLOCKED。

## 4. 状态与入口

### 4.1 唯一本地 ingress

`ReadSetJournalRepository.Register(ctx, owner, request)` 是唯一登记写入入口：

- `readsetjournal.PrepareRegistration` 先做纯校验/canonicalization；
- adapter 在一个 MySQL transaction 中锁 owner、Journal、event/delivery 相关行；
- 成功提交后才返回 `ReadSetJournalRegistration`；
- 只有新事务创建的登记返回 `Created=true, ExposurePermitted=true,
  RequiresReconciliation=false`；
- exact duplicate 一律返回 `Created=false, ExposurePermitted=false,
  RequiresReconciliation=true`，即使 delivery 仍为 PENDING；
- 不调用 provider；失败/回滚/duplicate 都不返回可暴露 permit。

`Register` 的 exact duplicate 判定包含 canonical payload、closure node digests 和
provider request binding；只相同 payload digest 不足以重放不同 closure。新 generation
可以用原 event bytes + 当前 owner fence 做 exact replay，但 duplicate 分支在 append
generation 检查之前返回，且永不授权重发。

### 4.2 状态分离

| 层 | 状态 | 本切片写入 |
| --- | --- | --- |
| Journal lifecycle | `OPEN`, `QUIESCING`, `INPUT_CLOSED` | 只创建 `OPEN`；不做生产关闭 |
| Registration | `REGISTERED` | 是 |
| Provider delivery | `PENDING` | 是 |
| Provider delivery | `UNKNOWN` | 是，timeout/ambiguous |
| Provider delivery | `UNVERIFIABLE` | 是，无独立 reconciliation source |
| Provider delivery | `APPLIED_VERIFIED` | **否**，无写入 API |
| Provider delivery | `FAILED` | **否**，无写入 API |

`UNKNOWN`/`UNVERIFIABLE` 不减少 `pending_count`，因为材料可能已经暴露。它们不会
被 `JournalView.ReadSetDeliveries()` 提升为 `ReadEvent`。

### 4.3 crash/restart

- 新 generation 的当前 owner 可以 `ReadJournal`/`ReconcileDelivery`/exact replay 原登记。
- 旧 canonical bytes 不会静默改写为新 generation；新 generation 不能 append event 2。
- same-generation response-loss retry 可按 delivery ID 恢复原 sequence，但结果仍是
  reconciliation-only，`ExposurePermitted=false`。
- `ReadJournal` 在 owner/journal 锁后读取 MySQL `CURRENT_TIMESTAMP(6)`；任一 required
  node 未生效或在 `valid_until` 半开边界之外即拒绝恢复。
- 没有自动 resend、自动 retry provider 或“unknown 视为未执行”的路径。

## 5. 持久不变量

| ID | 不变量 |
| --- | --- |
| RJ-01 | `JournalID = rsj_ + SHA-256(local key domain + canonical JournalKey)`；SQL key 四元组唯一。 |
| RJ-02 | `local_sequence` 每 Journal 从 1 连续；`high_watermark == event count == delivery count`。 |
| RJ-03 | event payload 必须已是 `magi-cj/1` canonical bytes；raw SHA-256 与列一致。 |
| RJ-04 | registration 与 provider apply 分离；本地状态不进入 #42 Deliveries。 |
| RJ-05 | owner/job/worker/generation 使用既有 durable DecisionJob lease；generation 必须正数；首个 append 锚定 journal generation。 |
| RJ-06 | context/authority/credential 各自绑定 `(scope_id,epoch)`；零 epoch 合法；不得与 generation 或 round 互代。 |
| RJ-07 | 同一 node ID 描述不可变；同 source/version 不得映射不同 node；parent IDs 排序且唯一。 |
| RJ-08 | DERIVED 必须有 parent；RAW/EMPTY_RESULT 必须无 parent；边表必须精确等于 node parent 集。 |
| RJ-09 | 所有持久 node 必须属于某个 event 的 ancestor closure；cycle/missing/extra/orphan 拒绝。 |
| RJ-10 | `(journal_id,delivery_id)` 与 `(journal_id,provider_request_id)` 唯一；同 ID 不同 bytes/closure 冲突。 |
| RJ-11 | `UNKNOWN`/`UNVERIFIABLE` 保留 pending 与预算，不自动重发。 |
| RJ-12 | 当前 scope 任一 ID/epoch 不同即拒绝 read/settlement；实际撤销源 BLOCKED。 |
| RJ-13 | `APPLIED_VERIFIED` 若持久记录缺 authority sequence/canonical verification ref/injected/verified time，读路径 fail closed。 |
| RJ-14 | `ReadSetDeliveries` 只为 APPLIED fact 构造 frozen `ReadEvent`；不构造 Sources、不提升 ActualReads/Authority。 |
| RJ-15 | duplicate 永不允许 exposure；仅新建登记可 `ExposurePermitted=true`。 |
| RJ-16 | restore 锁后取 DB time，对 raw/derived/empty 全部 node 做 half-open freshness。 |
| RJ-17 | RequestDigest 从 event bytes + exact ancestor closure 重算，预算由 node/closure 上限而非 event 上限决定。 |
| RJ-18 | node FirstSequence/AdmissionGeneration/AdmissionScopeID、edge CreatedSequence 与 parent-admission order 必须与 journal anchor 一致。 |
| RJ-19 | 拒绝 `rset_` source、DEBATE/INITIAL_REVIEW 和 phase regression。 |

## 6. MySQL 存储与事务

迁移：`docker/atlas/migrations/magi_s31_readset_journal.sql`

| 表 | 作用 |
| --- | --- |
| `readset_journal` | JournalKey、scope/epochs、owner/lease/generation、HWM、pending/event bytes、lifecycle、row version |
| `readset_journal_event` | 连续本地序号、event version、delivery/provider request、exact canonical bytes/digests、owner/generation |
| `readset_journal_delivery` | 与 registration 分离的 provider lifecycle；future trusted evidence 字段保持 NULL |
| `readset_journal_node` | immutable node bytes/digest、source/version、admission generation/scope |
| `readset_journal_edge` | child→parent 唯一边与 `(journal,parent,child)` reverse index |

写事务顺序：

1. `lockActiveExecution`：锁 `decision_job` 再锁 `decision_case`，锁等待后取 DB 时间重验；
2. 按 JournalKey `FOR UPDATE` 取/建 Journal；
3. 锁 delivery/event unique identity，做 exact replay 或 conflict；
4. 插入/核对 exact dependency nodes；
5. 插入 event、delivery、精确 edges；
6. 以 `high_watermark + state_version` CAS 更新 HWM、pending、bytes、owner/lease；
7. 全部成功才 COMMIT。

读事务使用同一 owner lock 和 Journal row lock，重建事件前缀/edge DAG 后才返回。

### 6.1 迁移与回滚

- 迁移只新增五张表，不修改 frozen artifact、既有 Event/DecisionJob/Case 表或旧 writer。
- 上线前必须先关闭本地 ingress；由于 `APPLIED_VERIFIED` 未实现，不能把 rollout 当作
  provider 能力上线。
- 回滚不是“没有暴露”：登记可能先于 provider 暴露。必须先停写、导出/保留五张表并
  审计 pending/unknown，再按 FK 依赖逆序删除：
  `readset_journal_edge` → `readset_journal_node` → `readset_journal_delivery` →
  `readset_journal_event` → `readset_journal`。
- 不允许 generation-unaware 或与 #45 语义不兼容的 writer 继续共享/重建这些表。
- 若已有 APPLIED/authority 事实，回滚必须保留只读审计副本，不能删除后重做。

## 7. #42 frozen validator 适配

`JournalView.ReadSetDeliveries()` 把持久 fact 转为既有
`inputartifact.ReadSetValidationOptions.Deliveries`：

- 只为 provider APPLIED fact 生成 `inputartifact.ReadEvent`；
- `sequence` 用本地连续序，`authority_sequence` 只能取 future trusted provider fact；
- 缺 authority sequence、verification ref、injected time 时返回
  `ErrAppliedUnverified`，不创作值；
- PENDING/UNKNOWN/UNVERIFIABLE/FAILED 不进入 map；
- 不构造 `Sources`，不把 local sequence 冒充 `AdmittedAtSequence`；
- 不改 `ReadReadSetSnapshot`、canonical bytes、artifact ID/digest 或 Golden。

由于 APPLIED 路径 BLOCKED，本切片没有“真实 provider 读取已适配通过”的声明。

## 8. 独立审查修正（CHANGES REQUIRED）

| 审查项 | 实际 RED | 修正与 GREEN |
| --- | --- | --- |
| P1 exact replay 不得成为 exposure permit | `TestReadSetJournal_ReplayNeverPermitsExposureMySQL` build failed：`ExposurePermitted`/`RequiresReconciliation` 未定义 | 新登记唯一 permit；所有 duplicate reconciliation-only。测试覆盖 PENDING/UNKNOWN/UNVERIFIABLE，PASS |
| P1 restore source freshness | MySQL stale test 实际返回 journal 并失败；domain helper 测试 build failed | 锁后 DB time + 全部节点 half-open validity；expired/not-yet-valid/future-ancestor PASS |
| P1 禁止跨 generation append | `TestReadSetJournal_GenerationCannotAppendSecondEventMySQL` 中 event 2 被接受，返回 `nil`；HWM 增至 2 | journal generation 锚定；新 generation exact replay/read 可用、append 返回 `ErrOwnerFence`、HWM 保持 1，PASS |
| P2 2048-node RequestDigest | 2048-node near-limit test 先因 closure 1MiB cap 被拒绝 | `RequestDigestFor` 使用 node/closure budget；closure cap 16MiB；2048-node closure PASS |
| P2 readback closure/evidence 重算 | corruption 子测试均被接受 | 重算 exact ancestor RequestDigest；校验 FirstSequence/Generation/Scope/CreatedSequence/parent ordering；六个 corruption 子测试 PASS |
| P2 frozen #42 declaration rejection | `rset_`、DEBATE/INITIAL、phase regression append/readback 被接受 | append/readback 双重拒绝；domain/readback/MySQL 测试 PASS |

本轮修复没有修改 frozen #42 Schema/Golden/身份，也没有扩大 provider/C2/C3 能力。

## 9. A–F 证据

### A. 问题与边界

- 问题是 pre-exposure durable registration 与 provider apply 的语义隔离，不是 provider
  已应用证明。
- 起点/依赖已核对：#42 `c2c5691` 含 full-body ReadSet v2 consumer；#45 为
  declaration-only Draft。
- 本切片不实现生产 activation。

### B. 设计与不变量

- 见第 2–5 节；本地协议名 `magi-readset-journal-registration/v1`。
- generation 与三类 epoch 独立；local sequence 与 authority sequence 独立。
- `JOURNAL_ADMIT` wire compatibility 被明确否定，并列出 Design Change 必要语义。

### C. 实现

- domain：`backend/domain/readsetjournal/`
- adapter：`backend/adapter/readset_journal*.go`
- migration：`docker/atlas/migrations/magi_s31_readset_journal.sql`
- docs：本文件

### D. 实际 RED/GREEN 命令与结果

均从 `backend/` 运行，`GOCACHE=/tmp/magi-wave2-44-go-cache`。

| 阶段 | 命令 | 实际结果 |
| --- | --- | --- |
| domain RED | `go test ./domain/readsetjournal -count=1` | 首次 build failed：`RegisterRequest`、`Config`、`AdmissionScope` 等未定义 |
| domain RED refine | 同上 | oversize string 被错误分类为 `ErrInvalidJournal`，断言 `ErrLimit` 真实失败 |
| domain RED shape | 同上 | nil parent array / 非 UTC 时间被接受，frozen-array/time-shape 测试真实失败 |
| domain GREEN | 同上 | PASS |
| adapter RED | `go test ./adapter -run ReadSetJournal -count=1` | build failed：`ReadSetJournalRepository`/result 未定义 |
| review P1/P2 REDs | 见第 8 节；日志保存在 `/tmp/magi-wave2-44-fix{1..6}-*.log` | 六项均先观察实际失败，再实现 |
| adapter MySQL GREEN | 下述 DSN 命令 | 14 个顶层验收测试全部 PASS（含 review regression 子测试） |
| domain regression | `go test ./domain/readsetjournal ./domain/inputartifact ./domain/canonical -count=1` | PASS；未修改 #42 frozen tests |
| domain race | `go test -race ./domain/readsetjournal -count=1` | PASS |
| MySQL race | 下述 DSN 命令加 `-race` | PASS |
| full Go | `go test ./... -count=1` | PASS；该命令未设置 DSN，因此其中 MySQL 条件测试按既有 helper 跳过，不能替代下一行的真实 MySQL 结果 |
| vet | `go vet ./...` | PASS，无输出 |
| frozen golden | `python3 domain/inputartifact/testdata/verify_readset_golden.py` | PASS：ReadSet v2 canonical bytes/digest/rset 与 immutable record/candidate identities |
| schema parity | `python3 ../scripts/check-readset-schema-parity.py` | PASS：37 definitions exact mirror |
| parity negatives | `python3 ../scripts/test-readset-schema-parity.py` | PASS：6 independent drift cases rejected |
| format/diff | `gofmt -l ...`、`git diff --check` | 无输出 |

真实 MySQL：

```bash
MAGI_TEST_MYSQL_DSN='root:root@tcp(127.0.0.1:33306)/magi_wave2?charset=utf8mb4&parseTime=True' \
GOCACHE=/tmp/magi-wave2-44-go-cache \
go test ./adapter -run ReadSetJournal -count=1 -v
```

实际顶层测试：

- `TestReadSetJournal_ConcurrentAppendAndContinuousPrefixMySQL`
- `TestReadSetJournal_DuplicateSameBytesAndConflictMySQL`
- `TestReadSetJournal_OwnerABAAndGenerationChangeMySQL`
- `TestReadSetJournal_LockAfterWaitRevalidatesLeaseMySQL`
- `TestReadSetJournal_UnknownRestartReadbackAndPostRevocationMySQL`
- `TestReadSetJournal_AppendBeforeExposureFaultCutAndResponseLossMySQL`
- `TestReadSetJournal_DAGReverseIndexAndOrphanRejectionMySQL`
- `TestReadSetJournal_DefaultDisabledDoesNotWriteMySQL`
- `TestReadSetJournal_MigrationShapeMySQL`
- `TestReadSetJournal_ReplayNeverPermitsExposureMySQL`
- `TestReadSetJournal_ReadRejectsStaleSourceMySQL`
- `TestReadSetJournal_GenerationCannotAppendSecondEventMySQL`
- `TestReadSetJournal_ReadbackRecomputesClosureEvidenceMySQL`
- `TestReadSetJournal_FrozenDeclarationRejectionsMySQL`

覆盖：24 并发 append 的连续 `1..24` 前缀；same-ID/same-bytes replay 与
same-ID/different-bytes conflict；scope conflict；same-worker owner ABA、generation
change、stale unknown settlement；等锁期间 lease 到期后 `ErrLeaseLost` 且零 event；
重启后新 generation readback；unknown → unverifiable、pending 保留、validator
deliveries 为空；insert failure 整体回滚；post-commit reply loss 后 durable row 已
存在且 exact replay 恢复原 sequence；DAG/reverse index 与 orphan rejection；默认
disabled 零写入；真实 migration 列和 reverse index shape；duplicate 永不 permit；
expired/future source restore 拒绝；跨 generation append 保持 HWM；2048-node closure
RequestDigest；RequestDigest/node/edge/parent-order corruption 拒绝；future `rset_`
source、DEBATE/INITIAL 和 phase regression 拒绝。

### E. 自审与残余风险

| 风险 | 级别 | 状态 |
| --- | --- | --- |
| 当前 scope 由调用方提供，未接 C2 可信 resolver | P0（生产启用） | BLOCKED；默认关闭 |
| #45 无 pre-exposure typed transaction | P0（生产启用） | Design Change required |
| 无真实 provider apply/authority sequence | P0（C1-C2B 完整性） | BLOCKED / NOT RUN |
| 无自动 resend 是设计约束，不是恢复已完成 | P1 | 已实现“不重发”与 duplicate 永不 permit；真实 provider 恢复 NOT RUN |
| DB-time freshness 不替代 C2 当前撤销源 | P1 | 仅 half-open declared validity；真实 revocation BLOCKED |
| 16MiB closure budget 的内存/CPU 上限未压测 | P2 | 2048-node identity 已测；大 body load test 未运行 |
| same-case 写者通过 DecisionJob 串行，吞吐而非细粒度并发 | P2 | 有界 slice 可接受；需压测 |
| APPLIED 读路径只能做 shape/fail-closed 映射 | P1 | provider/C2/C3 BLOCKED |
| Journal QUIESCING/INPUT_CLOSED 无生产转换 | P1 | 后续关闭切片 |
| 真实撤销传播、候选/ReadSet seal | P0（Stage） | BLOCKED |

### F. BLOCKED / NOT RUN 矩阵

| 验收项 | 状态 | 原因 |
| --- | --- | --- |
| 真实 provider 会话/应用证据核对 | NOT RUN / BLOCKED | 无 provider/session adapter |
| provider `APPLIED_VERIFIED` | BLOCKED | 无 authenticated trusted receipt、C2/C3 |
| authoritative sequence/shared order | BLOCKED | #45 只有声明，无生产 issuer |
| 真实模型读取/响应 | NOT RUN | 本切片不暴露、不调用模型 |
| post-revocation trusted current scope | BLOCKED | 无 C2 revocation resolver；仅测试 caller scope 变化拒绝 |
| 撤销反向传播至 ReadSet/vote/action | BLOCKED | 后续 C1-C/C2/C3 |
| QUIESCING/INPUT_CLOSED/三方 pending=0 | BLOCKED | 后续 closure 事务与 provider 记录器 |
| Candidate closure/ReadSet seal/ballot | BLOCKED | 非本切片 |
| provider CI/independent review/Draft PR | 未执行 | 用户明确禁止 push/PR；由协调执行器完成 |
| 全量 DoD / C2 / C3 | 未完成 | 本切片只报告本地 C1-C2B staged evidence |
