# C2 认证权威切片（#46）

2026-10-24（Asia/Shanghai）。起点为已合并 #45/#42 的
`c2c5691210e736a632c0033c196d3305e356ee9a`，工作分支
`codex/46-authenticated-authority`。本切片默认禁用、没有生产恢复密钥、
治理根、issuer 或启动器接线。`VALID` 只表示**当前事务内的本地 C2 认证与
权限范围核对通过**，不是 commit token、Journal 关闭、`EVIDENCE_CLOSED`、
witness、Egress、发送资格或远端执行事实。

## 0. Design Change boundary：proof wire format

#45 冻结的是 C2/C3 语义和内部 typed commit contract，没有冻结生产
`AuthorityProof` wire schema；Decision v2 也没有该 Schema。因此本切片定义
独立的实现边界 **DC-46-01**：

- proof body 版本固定为 `magi-authority-proof/1`；
- body 使用既有 C0 `canonical.TypeAuthorityProof` 计算 digest，ID 固定为
  `aprf_<sha256-domain-separated-digest>`；
- signature/key/envelope 与 proof body 分离，外层为
  `{proof, proof_id, signature}`；
- 这不是 #45 `traces.json` 的 trace declaration，不声明与 #45 静态夹具的
  wire compatibility；只复用其 typed outcome 和安全语义；
- 未来若将该实现提升为生产 AuthorityProof 格式，必须单独设计变更、评审和
  capability 激活，不能从本文件自动推导为可签发协议。

## 1. 范围与非目标

实现范围：

- 带外、固定 recovery key 与 identity source/version 的 root bootstrap；
- versioned Ed25519 credentials、validity window、retire/rotation；
- C0 proof body identity、domain-separated signature、原始字节回读；
- 多跳 delegation、owner consent threshold、scoped epoch；
- MySQL root/key/proof/delegation/consent/revocation/epoch 持久化；
- `VALID` / `DENIED` / `REVOKED` / `EXPIRED` / `STALE` / `CONFLICT` /
  `UNKNOWN` typed outcomes。

明确非目标：

- 不实现 `CommitIfCurrent`；
- 不实现最终 Journal、`EVIDENCE_CLOSED`、Candidate closure、witness、Egress、
  发送或远端执行；
- 不创建第二 authority sequence；C2 仅以 active-root row 锁和 scoped epoch
  CAS 串行化自身状态，跨 C2/C3 共同顺序仍是 #47 的阻断前提；
- 不把 ExecutionGeneration 用作 round 或 scoped epoch；
- 不把 JSON Schema、Persona、Charter、Context、模型输出或缓存当成权限；
- 不安装任何生产 root/recovery key。

## 2. 不变量

| ID | 不变量 |
| --- | --- |
| A46-01 | root bootstrap 必须使用部署预置 recovery public key/version 验证；缺失为 `UNKNOWN`，坏签名为 `DENIED`，同域第二个不同 root 为 `CONFLICT`。 |
| A46-02 | bootstrap 固定 identity source ID/version；subject/credential 来源不一致一律拒绝。 |
| A46-03 | proof body 必须是 exact canonical bytes，proof version 固定；proof ID 是 C0 `authority_proof` 域 digest 的 `aprf_` 身份。 |
| A46-04 | proof signature 使用 Ed25519，签名输入为固定 domain prefix + canonical body；不把 envelope 或任意缓存字节当签名输入。 |
| A46-05 | signer credential 必须 exact binding `(root incarnation, credential id, credential version)`，状态当前有效，且 credential window 覆盖 proof window。 |
| A46-06 | proof subject、identity source、resource、action、tenant、operation、case、round、execution generation 与验证请求精确相等。 |
| A46-07 | scoped epochs 恰有 CONTEXT/AUTHORITY/CREDENTIAL 三类，按 `(kind, scope id)` 唯一稳定排序；不允许把另一 kind、generation 或 round 代入。 |
| A46-08 | delegation 逐跳签名、issuer 连续、delegate 连续；重复 delegation/subject 形成 loop 时拒绝。 |
| A46-09 | 逐跳 resources/actions/tenants 必须是父集合子集；非最后 hop 必须 `delegable=true`；所有 validity 不得扩大到 parent/proof 之外。 |
| A46-10 | owner policy 由 bootstrap 认证并单独持久化；ALL/THRESHOLD/SINGLE_DELEGATE 语义固定，重复 owner approval 不增加票数。 |
| A46-11 | owner approval 必须绑定 consent body digest、exact owner、credential ref 与 proof 目标；错 owner、坏签名、阈值不足均拒绝。 |
| A46-12 | revocation/key rotation 由 current governance credential 签名，使用 expected epoch + row version CAS；epoch 只增不减，revoked row 不删除、不复用。 |
| A46-13 | 所有 C2 写者与在线验证先锁同一 pinned active-root row；随后锁 proof/evidence/credential/policy/epoch；可信时间在全部所需行锁之后读取。 |
| A46-14 | MySQL 原始 proof/delegation/consent/scope evidence 必须 exact canonical bytes 且与 digest/列值一致；坏行失败关闭。 |
| A46-15 | 不存在内存 authority cache；重启后 root fingerprint、proof version、key status 和 revocation 从 MySQL 重新读取。 |
| A46-16 | 任何非 `VALID` 均不产生权限；`VALID` 也仅设置 `LocalAuthenticationOnly=true`，commit/mutation flags 恒为 false。 |

## 3. 威胁模型

覆盖的攻击/竞态：

- 合法 JSON、错误 schema 版本、未知字段、非 canonical 重排或修复；
- 伪造/替换 proof body、proof ID、digest、signature 或 key version；
- replay 已撤销 proof、重放旧 epoch、旧 credential version、A-B-A epoch；
- 同名不同 identity source、跨 root incarnation、跨 trust domain；
- 未授权 delegation、scope widening、缺 hop、loop、non-delegable 继续委托；
- 伪造 owner、重复 owner signature、阈值替换、policy/root 混用；
- 验证与 revocation/key rotation 并发；
- MySQL 证据行、digest、proof scope、delegation、consent 被篡改；
- root/proof reply loss、进程重建和旧版本降级。

信任边界内但仍未部署验收：

- MySQL、Authority 进程、DB UTC 时钟和部署 recovery key 的正确管理；
- 对 current governance key/recovery key 同时失陷的抵抗；
- 独立 witness/Egress 故障域、网络隔离、生产 root recovery quorum；
- 跨 C2/C3 的单一总序和 CommitIfCurrent。以上均不能由本切片证明。

## 4. 版本化签名 transcript

所有时间均为严格 UTC 毫秒 `YYYY-MM-DDTHH:mm:ss.SSSZ`；所有 signature 为
standard base64 Ed25519。所有签名 body 使用既有 C0 canonical encoder。

| 对象 | Version | Domain separation |
| --- | --- | --- |
| Root bootstrap | `magi-authority-bootstrap/1` | `magi:authority-bootstrap-signature:v1\n` |
| Root fingerprint | bootstrap body | `magi:authority-root:v1\n` |
| Authority proof | `magi-authority-proof/1` | C0 `authority_proof` digest + `magi:authority-proof-signature:v1\n` |
| Delegation | `magi-authority-delegation/1` | `magi:authority-delegation-signature:v1\n` |
| Consent evidence | `magi-authority-consent/1` | `magi:authority-consent-evidence:v1\n` |
| Consent approval | consent version | `magi:authority-consent-approval:v1\n` + consent digest |
| Revocation command | `magi-authority-revocation-command/1` | `magi:authority-revocation-command-signature:v1\n` |
| Credential rotation command | `magi-authority-credential-rotation-command/1` | `magi:authority-credential-rotation-command-signature:v1\n` |

Proof 身份只覆盖 canonical proof body，不覆盖外层 signature；外层
`proof_id` 必须等于 body 的 C0 `aprf_` 身份，signature 必须覆盖 body，
因此 envelope 替换无法绕过。root bootstrap signature、root fingerprint 和
proof signature 使用不同 domain。

## 5. 验证顺序与 typed outcome

在线验证顺序：

1. 锁 active root；缺失返回 `UNKNOWN/TRUST_ROOT_MISSING`。
2. 锁 proof row，严格 canonical parse，核对 C0 ID、stored digest/version/root。
3. 锁 proof scope、delegation、consent evidence，核对原始 bytes 与 digest。
4. 锁 signer/hop/owner credentials、owner policy、全部 scoped epochs。
5. 在全部所需锁之后读取 MySQL `UTC_TIMESTAMP(3)`。
6. re-authenticate pinned bootstrap raw/fingerprint，但不复活历史 credential；
   当前 credential/policy 期限另行检查。
7. 验证 proof signature、逐跳 delegation、owner consent、request binding、
   window containment 和 scoped epochs。
8. 仅当全部通过返回 `VALID`，并固定：
   `LocalAuthenticationOnly=true`、`CommitEligible=false`、
   `MutationAuthorized=false`。

| Outcome | 典型条件 |
| --- | --- |
| `VALID` | 仅本地 C2 认证/绑定全部通过；不授予提交或发送 |
| `DENIED` | 坏签名、身份/key/scope/consent/issuer/binding 不匹配 |
| `REVOKED` | root/credential/scope 已撤销 |
| `EXPIRED` | proof/credential/policy/consent/delegation/command window 过期 |
| `STALE` | epoch、round、generation、key version 或请求状态不再当前 |
| `CONFLICT` | 重复 root/证据、delegation loop、ambiguous policy/owner/evidence |
| `UNKNOWN` | root/credential/policy/epoch/时间/存储不可确定；绝不能当 VALID |

`ParseSignedProof` 的 `PROOF_BYTES_VALID` 只表示结构、canonical 和 C0 身份可
解析；它不是 authority 结果，调用者必须再执行 live-state
`VerifySignedProof`。

## 6. MySQL 状态、锁与恢复

迁移：`docker/atlas/migrations/magi_s32_authority_proof.sql`。全部表为
`magi_authority_*` InnoDB 表，仅新增，不 seed 任何 root/key/policy/epoch。

- `magi_authority_root`：pinned root raw/fingerprint、identity source、
  governance subject、status；`(trust_domain, active_slot)` 保证单个 active root。
- `magi_authority_credential`：exact credential ref/version、subject/source、
  public key、window、status、credential scope、原始 credential evidence。
- `magi_authority_owner_policy`：版本化 policy raw/digest/status/阈值。
- `magi_authority_epoch`：`(trust_domain, kind, scope id)` 唯一；epoch、
  revoked、row_version、reason。
- `magi_authority_proof`：原始 proof、C0 digest、version、root、目标绑定、
  signer ref 和 registration time。
- `magi_authority_proof_scope`：proof 的三类 epoch evidence。
- `magi_authority_proof_delegation` / `..._consent`：逐跳和 owner consent
  原始 evidence。

没有 `magi_authority_sequence`。C2 的串行化锁顺序是：

```text
active root
→ proof row
→ proof scope / delegation / consent evidence
→ credentials (canonical key order)
→ owner policy
→ scoped epochs (canonical kind/scope order)
→ SELECT UTC_TIMESTAMP(3)
```

root bootstrap、proof registration、revocation 和 key rotation 均先锁同一
active root；首次安装由 active-slot unique constraint 处理并发。跨 C2/C3 的
共同 authority order 不在本迁移内，必须由 #47 接入。

Epoch update 必须同时匹配 exact `epoch`、`row_version`、`revoked=0`；成功后
仅推进该 kind/scope。Revocation 永久保留 revoked row；rotation 将旧
credential 设为 `RETIRED`、插入精确下一版本并推进 CREDENTIAL epoch。

重启不依赖内存状态：新 repository 从 MySQL 重新读取并验证 pinned root raw、
proof raw/digest、credential status、policy 和 revocation。相同 proof
registration/root bootstrap 原始 bytes 的 reply-loss retry 只回读原记录；
状态已撤销或 key 已轮换时，旧 proof replay 返回 `REVOKED`/`STALE`。

## 7. Capability 与回滚

- `bootstrap.Config.Authority.Enabled` 的 zero value 为 `false`。
- `AuthorityRepositoryConfig{}.Enabled` 为 `false`；任何调用返回
  `DENIED/AUTHORITY_CAPABILITY_DISABLED`，不会落 root。
- 当前没有 `bootstrap/container.go` 或 server 接线，也没有生产 recovery key、
  governance root、issuer 或 remote caller。
- 启用前提：单独授权 root bootstrap、固定 identity source、owner policy、
  当前 credential/policy、所有 C2/C3 写者接入 #47 共同顺序、部署网络/权限隔离。
- 回滚：停止新 C2 写和调用方，保持 capability off；保留 root、proof、
  delegation、consent、epoch、revocation 和原始 bytes，不回退版本、不删除
  revoked row、不重置 epoch、不把旧 credential 恢复为 ACTIVE。旧 binary
  只能只读保留历史，不能消费为 authority。
- S32 未提供自动 down migration。删除表只能作为经批准的全域恢复/灾难操作，
  且必须确认没有 #47 lineage 或审计引用；普通回滚不得 drop。

## 8. A–F 证据

### A. 基线与范围

读取 `CLAUDE.md`、issue #46、#45 合约/审查/schema/typed API 以及 #42
ReadSet identity/epoch 文档。初始 `git status --short --branch` 为干净分支
`codex/46-authenticated-authority`，HEAD `c2c5691`。未修改
`.github/workflows`、Rev4、C0/Golden、#42/#45 schema 或脚本。

### B. 需求到实现

| 需求 | 证据 |
| --- | --- |
| out-of-band root + fixed identity source | `backend/domain/authority/bootstrap.go`、bootstrap tests |
| Ed25519/C0/raw readback | `proof.go`、proof tests |
| delegation/consent/epoch | `proof.go`、delegation/consent/epoch/window tests |
| signed revocation/rotation | `commands.go`、commands tests |
| MySQL persistence/locking | `authority_repository.go`、S32 migration、MySQL tests |
| default-off capability | `bootstrap/config.go`、config/MySQL tests |
| no second sequence | migration idempotence test asserts no sequence table |
| no C3 capability | 本文件 §0/§1 与 outcome flags |

### C. 真实 RED → GREEN

域 proof RED（在仍返回 `UNKNOWN/UNIMPLEMENTED` 的 skeleton 上）：

```text
GOCACHE=/tmp/magi-wave2-46-go-cache go test ./domain/authority -run '^TestProofVerification' -count=1 -v
FAIL: parse outcome=UNKNOWN code=UNIMPLEMENTED
```

Delegation RED：

```text
GOCACHE=/tmp/magi-wave2-46-go-cache go test ./domain/authority -run '^TestDelegationAuthenticatedChainValid$' -count=1 -v
FAIL: verify outcome=CONFLICT code=DELEGATION_CYCLE
```

Window/set RED（错误地得到 `VALID`）：

```text
GOCACHE=/tmp/magi-wave2-46-go-cache go test ./domain/authority -run '^(TestProofValidityIsBoundedByEveryAuthorityWindow|TestDelegationRequiresStableSetEncoding)$' -count=1 -v
FAIL: credential, owner consent, owner policy 和 unsorted delegation 均返回 VALID
```

MySQL/证据 RED：

```text
MAGI_TEST_MYSQL_DSN='root:root@tcp(127.0.0.1:33306)/magi_wave2?charset=utf8mb4&parseTime=True' \
GOCACHE=/tmp/magi-wave2-46-go-cache go test ./adapter -run '^TestAuthorityMySQL_' -count=1 -v
FAIL: missing root outcome=UNKNOWN code=UNIMPLEMENTED
FAIL: TestAuthorityMySQL_EvidenceTamperFailsClosed 的 scope/consent/delegation tamper 仍返回 VALID
```

GREEN：

```text
GOCACHE=/tmp/magi-wave2-46-go-cache go test ./domain/authority -count=1 -v
PASS

MAGI_TEST_MYSQL_DSN='root:root@tcp(127.0.0.1:33306)/magi_wave2?charset=utf8mb4&parseTime=True' \
GOCACHE=/tmp/magi-wave2-46-go-cache go test ./adapter -run '^TestAuthorityMySQL_' -count=1 -v
PASS
```

### D. 回归、race 与 vet

实际执行并通过：

```text
GOCACHE=/tmp/magi-wave2-46-go-cache go test ./... -count=1
PASS（backend 全部 package）

GOCACHE=/tmp/magi-wave2-46-go-cache go test -race ./domain/authority -count=1
PASS

MAGI_TEST_MYSQL_DSN='root:root@tcp(127.0.0.1:33306)/magi_wave2?charset=utf8mb4&parseTime=True' \
GOCACHE=/tmp/magi-wave2-46-go-cache go test -race ./adapter -run '^TestAuthorityMySQL_' -count=1
PASS

GOCACHE=/tmp/magi-wave2-46-go-cache go vet ./...
PASS

GOCACHE=/tmp/magi-wave2-46-go-cache python3 scripts/test-authority-contract.py
Ran 4 tests; OK

GOCACHE=/tmp/magi-wave2-46-go-cache python3 scripts/test-authority-review.py
Ran 9 tests; OK

GOCACHE=/tmp/magi-wave2-46-go-cache python3 scripts/check-authority-contract.py
PASS 108 schema/static trace declarations

python3 backend/domain/canonical/testdata/verify_golden.py backend/domain/canonical/testdata/golden_vectors.json
OK (0 failures)
```

另运行 `gofmt -l` 为空、`git diff --check` 为空。

### E. MySQL tests actually run

`go test ./adapter -run '^TestAuthorityMySQL_'` 覆盖：

- bootstrap、proof registration、root/proof raw readback、restart、reply loss；
- missing/untrusted/ambiguous root、capability off；
- revocation monotonicity/duplicate/stale A-B-A、CONTEXT/AUTHORITY/CREDENTIAL；
- key rotation、old proof/key replay、restart、next-version proof；
- concurrent proof+revocation、proof+key rotation；
- scope widening、delegation cycle、bad owner consent、wrong subject、expired；
- stale epoch、generation/epoch 混淆、round/generation/epoch 独立；
- proof/digest/scope/delegation/consent evidence tamper；
- unauthenticated revocation/rotation/proof；
- reused consent conflict、migration 幂等且无 seed/no sequence table。

### F. 边界与依赖

本切片的 authoritative store 是单一 MySQL trust domain 的 C2 子状态，不是
C3 的 CommitIfCurrent store，也不声称是 #47 common-order 实现。所有
`a45_*`/#45 trace 名称、Commit token、Journal、witness 和 send 均未被本次
实现消费或创建。

## 9. NOT RUN / BLOCKED matrix

| 项目 | 状态 | 原因 |
| --- | --- | --- |
| `CommitIfCurrent`、conditional current-state write | BLOCKED #47 | C2 不创建 commit token、不持有共同总序 |
| 最终 Journal、`EVIDENCE_CLOSED`、Candidate closure | BLOCKED #47/C3 | 不在 C2 范围 |
| witness、Egress、SEND 资格、远端执行 | BLOCKED #47/C3 | 未实现且 `VALID` 无发送含义 |
| 生产 root/recovery key 安装 | NOT RUN / REQUIRES SEPARATE AUTHORIZATION | 仓库无默认 key，迁移无 seed |
| 正常 root rotation / root compromise recovery quorum | BLOCKED future Design Change | 本切片仅 root bootstrap pin 和 credential rotation |
| C2/C3 total order、all-writer fencing | BLOCKED #47 | 明确没有第二 sequence |
| 真实进程 crash 中途事务/网络分区/DB failover | NOT RUN | MySQL transaction/restart/readback 已测，未做生产故障注入 |
| 部署级身份源、KMS/HSM、密钥保管和双人 bootstrap | NOT RUN | 本切片是代码契约与本地 MySQL 验证 |
| actual PR-head CI、review、Ready/merge、生产激活 | NOT RUN / external | 本实现不 push、不开 PR、不激活 |

## 10. 风险与后续

- P0：无已知；但生产激活前若把本地 `VALID` 当成 commit/send 资格即为 P0。
- P1：C2/C3 共享总序仍缺失，任何真实 mutation 必须等 #47 原子接桥。
- P1：生产 recovery/governance 密钥的带外流程、root recovery quorum、KMS/
  HSM、部署身份源和审计保留尚未验证。
- P2：解析 proof 的返回仍使用同一 typed `Outcome` 类型，但以
  `PROOF_BYTES_VALID` 和不设置 local-authority flag 区分；调用方必须只把
  live-state `VerifySignedProof` 的 `VALID` 当作本地认证结果。
- P2：namespace 未覆盖 root rotation/retirement API；root pin 与同 root
  credential rotation 已验证，root 更新需独立设计。
