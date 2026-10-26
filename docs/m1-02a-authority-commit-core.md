# M1-02A：真实 MySQL Authority Commit Core

> 关联：[#47](https://github.com/jamespud/MAGI-system/issues/47)、[#54](https://github.com/jamespud/MAGI-system/issues/54)、M1-01 [#55](https://github.com/jamespud/MAGI-system/pull/55)。
>
> 状态：**Admission Commit Core implemented / default-off**。本切片只实现
> `JOURNAL_ADMIT_V2` 的真实共同权威提交。Exposure Gate、Settlement、独立
> Witness、Provider APPLY、`APPLIED_VERIFIED`、三方 Evidence Closure、受保护
> Egress 和生产启用仍未实现。

## 0. 结论先行

本切片交付一个默认关闭的 MySQL `CommitIfCurrent` 核心：

- 每个 trust domain 使用一个 `magi_authority_head` 共同权威序号；
- C2 撤销与 credential rotation 先锁同一 authority head，再修改 scoped epoch；
- `JOURNAL_ADMIT_V2` 在同一个事务内重新认证 C2、锁定运行 owner、执行 Journal/HWM
  与 pending CAS、分配 authority sequence，并写入完整 admission lineage；
- 相同唯一键和相同 canonical request 只返回原记录，`ExposurePermitted=false`；
- 相同唯一键但 target/token/request digest 不同返回 `CONFLICT`；
- 回复丢失后可由新 repository 实例恢复原 lineage，不重新分配序号；
- 所有 fence 行固定为 `DECLARED_BLOCKED`，没有任何可消费 Exposure Permit。

因此 Admission 获得了真实、可恢复的权威提交记录；这 **不等于** 已获得 Provider
暴露权限，也不使 M1 通过。

## 1. 范围与非目标

### 1.1 已实现

- `backend/domain/authoritycommit/`：精确 v2 admission request、canonical digest、
  durable unique key、lineage identity 与写集合校验。
- `magi_s33_authority_commit.sql`：common authority head、v2 Journal、admission、
  dependency、provider reservation declaration、blocked exposure fence、pending
  transition、unique binding、token consumption、lineage 与 blocked outbox。
- `AuthorityCommitRepository.CommitJournalAdmissionV2`：真实 MySQL 单事务消费者。
- C2 撤销与 rotation 对同一 authority head 的锁序和 sequence watermark。
- 真实 MySQL 的并发、C2 当前性、Generation/lease、故障切点、回复丢失、
  token 复用和撤销竞争测试。
- 默认 capability 仍由 `AuthorityRepositoryConfig.Enabled=false` 关闭；没有容器、
  server 或启动器接线。

### 1.2 明确排除

- 不调用 Provider，不创建 provider/session，不确认真实材料读取。
- 不创建可消费 Exposure Permit，不写 `APPLIED_VERIFIED`，不创建 settlement。
- 不实现 `JOURNAL_EXPOSURE_GATE_V2`、`JOURNAL_SETTLE_V2` 或独立 witness。
- 不实现 Candidate closure、SealedBallot、DecisionRecord、三方 Evidence Closure、
  受保护 Egress 或真实外发。
- 不启用生产信任根、issuer、recovery key 或 deploy 配置。

## 2. 共同权威顺序

锁序固定为：

```text
authority head
  -> C2 root/proof/credential/policy/scoped epochs
  -> runtime job/case owner + DB-time lease
  -> v2 Journal + local HWM/pending
  -> unique binding/token
  -> admission/dependencies/reservation/fence/pending
  -> lineage/outbox
```

`RevokeScope` 和 `RotateCredential` 在锁 active root 之前先锁同一
`magi_authority_head`。首次撤销或 rotation 同时推进：

```text
last_sequence = last_sequence + 1
last_revocation_sequence = last_sequence
```

rotation 被视为 revocation watermark，因为它使旧 credential scope 失效。精确 command
replay 只返回原结果，不再次分配 authority sequence。

`JOURNAL_ADMIT_V2` 在被 admission 占用期间持有 head lock，因此 revocation 与
admission 不会出现“双方都成功”的中间顺序：

- revocation 先取得 head：admission 的最终 C2 重验返回 `REVOKED`；
- admission 先取得 head：admission 提交后再执行 revocation，二者 sequence 可比较；
- 两种结果都不产生 exposure permission。

## 3. `JOURNAL_ADMIT_V2` 原子写集

单个 MySQL `READ COMMITTED` 事务写入：

```text
authority head sequence
journal_v2 high_watermark + pending_count
journal_event_v2
admission_v2
dependency_v2
provider_reservation_v2 (local declaration only)
exposure_fence_v2 (DECLARED_BLOCKED only)
pending_transition_v2
unique_binding_v2
token_consumed_v2
lineage_v2
outbox_v2 (BLOCKED only)
```

任何 insert/update 失败、故障切点、C2 过期或 owner/lease 失败都会回滚全部写集；
authority sequence、Journal HWM 和 pending count 一并回滚。

Authority sequence 与 local Journal HWM 是两个独立字段：

- `authority_sequence` 属于共同 C2/C3 总序；
- `journal_local_sequence` / `journal_high_watermark` 只表示该 v2 Journal 的本地连续前缀；
- 测试同时读取两者并验证不是同一来源或同一分配路径。

## 4. 幂等、冲突与未知结果

唯一键为：

```text
(trust_domain, case_id, round, context_manifest_id, persona_id, delivery_id)
```

`provider_request_id`、commit token、target、对象摘要和 ExecutionGeneration 不属于该
唯一键，但必须被同一 binding 精确绑定。

| 输入 | 结果 |
| --- | --- |
| 同键 + 同 canonical request | `VALID/ADMITTED`, `Disposition=REPLAY`，无 exposure permission |
| 同键 + 不同 target/token/request digest | `CONFLICT/ADMISSION_KEY_CONFLICT` |
| 同 token + 不同 admission key | `CONFLICT/COMMIT_TOKEN_REUSED` |
| commit 已成功但响应丢失 | `UNKNOWN/COMMIT_RESPONSE_LOST`；重试恢复原 lineage |
| replay 时 owner lease 已丢失 | 仍只读恢复原 lineage，不重做提交、不授权暴露 |
| C2 disabled | `DENIED/AUTHORITY_CAPABILITY_DISABLED` |
| C2 在 Journal 锁等待期间过期 | `EXPIRED`，Journal/HWM/pending 全部回滚 |

未知结果永远不等于“没有执行”；调用方只能按原键恢复，不能构造新的 key 或 token 重做。

## 5. C2 与运行 owner 当前性

admission 至少重新验证两次 C2：

1. 取得 common head 后、锁定 owner/Journal 前；
2. Journal 锁等待结束后、分配 authority sequence 前。

第二次验证使用新的 DB-trusted time，覆盖锁等待期间 proof/credential/policy 过期。
owner 检查同时绑定：

```text
CaseID + JobID + WorkerID + ExecutionGeneration
+ DecisionJob RUNNING + DB lease_until > CURRENT_TIMESTAMP(6)
```

authority sequence 的最终 UPDATE 再次用 SQL 联表条件校验 running job、worker、
generation 和 DB lease，因此 Lease 在最后线性化点过期时 sequence 不会推进。

## 6. 测试与 A-F 证据

### A. 基线与范围

- baseline main：`cbfe7668346cc2af1c7435601f19fd77915dd797`；
- 依赖：已合并的 C2 [#52](https://github.com/jamespud/MAGI-system/pull/52)、
  durable local registration [#53](https://github.com/jamespud/MAGI-system/pull/53)
  和 `JOURNAL_ADMIT_V2` 静态契约 [#55](https://github.com/jamespud/MAGI-system/pull/55)；
- 本切片没有修改 Rev4、C0/C1/C2 身份、冻结 Schema、Provider、Egress 或生产部署；
- 未依赖未合并分支；没有第二个 generation/epoch/sequence 子系统。

### B. 需求到实现

| 需求 | 证据 |
| --- | --- |
| 复用 C2 认证、授权范围、撤销与 scoped epochs | `AuthorityCommitRepository` 调用现有 `verifyProofInTx`；C2 撤销/rotation 共用 S33 head |
| 持久锁内重验 owner/generation/permission/epoch/lease | 两阶段 C2 重验、`lockActiveExecution`、最终 conditional head UPDATE |
| 一次事务完整写集 | S33 + `authority_commit_repository.go` + first-admission/fault tests |
| Authority sequence 与 Journal HWM 分离 | `AdmissionCommit` 双字段；admission/unique/lineage 独立持久列 |
| 同键同原件只返回原 lineage | concurrent/replay/restart tests |
| 同键不同 digest `CONFLICT` | `TestAuthorityCommitMySQL_SameKeyDifferentDigestConflictsWithoutMutation` |
| 撤销先于 admission 时拒绝 | `TestAuthorityCommitMySQL_VerifyThenRevokeRejectsOldAuthority` |
| Owner/Generation ABA 拒绝旧执行者 | `TestAuthorityCommitMySQL_GenerationChangeRejectsOldOwner` |
| 锁等待期间 lease 过期 | `TestAuthorityCommitMySQL_LeaseExpiryDuringHeadWaitRejects` |
| 关键写入点故障无半提交 | `TestAuthorityCommitMySQL_EveryWriteFenceRollsBackWholeCommit` |
| Commit 成功但响应丢失 | `TestAuthorityCommitMySQL_AfterCommitResponseLossReplaysOriginal` |
| C2/Witness 能力缺失时不可暴露 | disabled-C2 test、固定 `BlockedCapabilities`、所有 fence/outbox 均 `BLOCKED` |

### C. 实际 RED → GREEN

以下均在真实 MySQL 或 domain focused test 上观察到实际失败，不是静态推演：

| 阶段 | RED | GREEN |
| --- | --- | --- |
| domain contract 缺失 | `no non-test Go files` / undefined package | 13 个 domain tests + race PASS |
| S33 缺失 | `read S33 migration: ... no such file` | schema migration repeatable PASS |
| 撤销未进入共同顺序 | authority head missing / sequence 0 | revocation+rotation sequence `(1,1) -> (2,2)` PASS |
| admission consumer 缺失 | `undefined: AuthorityCommitRepository` | first complete write-set PASS |
| 并发/重放缺失 | `JOURNAL_HWM_STALE`、`OWNER_LEASE_LOST` | one NEW + seven REPLAY PASS |
| owner 错误码未区分 | 统一 `OWNER_LEASE_LOST` | `EXECUTION_GENERATION_STALE` / `OWNER_LEASE_EXPIRED` PASS |
| 故障切点无效 | 注入故障仍返回 `VALID/ADMITTED` | 12 个写集切点全部回滚 PASS |
| Journal 等待期间 proof 过期 | 错误返回 `VALID/ADMITTED` | 第二 C2 重验返回 `EXPIRED` 且写集回滚 PASS |
| duplicate dependency 接受 | `VALID/ADMISSION_REQUEST_VALID` | `DENIED/DEPENDENCY_INVALID` PASS |
| token 跨 key 复用 | `UNKNOWN/AUTHORITY_STORE_ERROR`（duplicate） | `CONFLICT/COMMIT_TOKEN_REUSED` PASS |

### D. 最终验证命令

以下命令均实际执行：

```bash
cd backend

GOCACHE=/tmp/magi-m1-02a-go-cache \
  go test ./domain/authoritycommit ./domain/authority -count=1

MAGI_TEST_MYSQL_DSN='root:root@tcp(127.0.0.1:33306)/magi_wave2?charset=utf8mb4&parseTime=True' \
GOCACHE=/tmp/magi-m1-02a-go-cache \
  go test ./adapter -run '^(TestAuthorityMySQL_|TestReviewMySQL_|TestAuthorityCommitMySQL_)' -count=1

MAGI_TEST_MYSQL_DSN='root:root@tcp(127.0.0.1:33306)/magi_wave2?charset=utf8mb4&parseTime=True' \
GOCACHE=/tmp/magi-m1-02a-go-cache \
  go test -race ./adapter -run '^TestAuthorityCommitMySQL_' -count=1

GOCACHE=/tmp/magi-m1-02a-go-cache go test ./... -count=1
GOCACHE=/tmp/magi-m1-02a-go-cache go test -race ./... -count=1

MAGI_TEST_MYSQL_DSN='root:root@tcp(127.0.0.1:33306)/magi_wave2?charset=utf8mb4&parseTime=True' \
GOCACHE=/tmp/magi-m1-02a-go-cache go test -p 1 ./adapter ./bootstrap -count=1

GOCACHE=/tmp/magi-m1-02a-go-cache go vet ./adapter ./domain/authority ./domain/authoritycommit
GOCACHE=/tmp/magi-m1-02a-go-cache python3 scripts/check-journal-admit-v2.py
GOCACHE=/tmp/magi-m1-02a-go-cache python3 scripts/test-journal-admit-v2.py
```

真实 MySQL 使用本机 `mysql:8.4.5`、`READ COMMITTED` 和隔离 schema。adapter focused
suite 与 race suite 均通过；MySQL-gated tests 没有被 skip。

最终结果：不带 DSN 的 `go test ./...` 和 `go test -race ./...` 全部 PASS；串行真实
MySQL `adapter` 与 `bootstrap` 全部 PASS；`go vet ./...`、`gofmt -l backend`、
`git diff --check`、authority/journal static declarations 全部 PASS。

一次并行 `MAGI_TEST_MYSQL_DSN go test ./...` 尝试耗尽本机 MySQL 连接并出现
`Error 1040: Too many connections`；失败在既有 bootstrap MySQL 建 schema 步骤。
按 CI 的串行方式重跑 `-p 1 ./adapter ./bootstrap` 后全部通过，未把该环境限制伪装成
通过或归因于本切片代码。

### E. E2E 场景与阻塞变化

- **E2E assertion unblocked：** `ROLLOUT-LAB-E2E-01` 的 `JOURNAL_ADMISSION` 阶段现在
  有一个可产生真实 durable admission lineage 的默认关闭实现入口。
- **Previous blocker：** 只有静态 `JOURNAL_ADMIT_V2` 契约，没有 `CommitIfCurrent`、
  common order 或 MySQL admission 写集。
- **Still BLOCKED：** `JOURNAL_EXPOSURE_GATE_V2`、Provider fence enforcement、
  independent witness、`JOURNAL_SETTLE_V2`、真实 delivery receipt、三方
  `EVIDENCE_CLOSED`、protected Egress 和生产启用。
- 本 PR 不修改 M1-00 runner 为 REAL green；`M1_GATE` 仍为独立验收边界。

### F. 已知限制

- v2 admission 使用 S33 的 authority journal 表；不声称与 #53 的 local registration
  表 wire-compatible，也不把 local `REGISTERED` 升级为 authoritative admission。
- Provider reservation 是 durable local declaration，不是 Provider 已接收/已应用。
- Witness 和 provider fence capability 没有任何可配置成功路径，因此 admission 结果
  固定包含两项 blocked capability。
- 没有真实 Provider、真实网络、真实业务发送或生产 root。
- 完整 #47 的 gate、settlement、witness 与运行时恢复仍由后续切片完成；#47 保持 Open。
