# C1-C1：ContextManifest v1 离线语义验证器

## A. 基线与边界

2026-10-24（Asia/Shanghai）核对：执行起点和远端 `main` 均为 `f0277e15c356b426ee25be995d9f49c929534ec8`，PR #39 已合并，#40 为 OPEN，开放 PR 为 0。当前工作在已有独立 worktree 的 `codex/c1-c1-context-manifest` 分支。初始工作区干净。默认 Go 缓存只读，首次命令未进入测试；改用 `/tmp/magi-c1c1-go-cache` 后基线 `go test ./...` 通过。

依据：#40、C1-A §2/§5、冻结的 input-artifacts Schema、Decision Artifact v2 中的 ContextManifest v1 镜像，以及已有 C0/C0-R、DomainProfile、PersonaSpec 验证器。Rev4 和全部既有 Schema/Golden/Legacy 字节未修改。未读取或更新 Linear；本文不把用户提供的 CAS-9 状态当成本轮独立核验。

本交付只检查不可变初始允许视图。真实注入、模型实际读取、终审交付、Evidence Closure 和治理授权均未实现。

## B. 接口与实现

代码位于 `backend/domain/inputartifact/context_*.go`：

```go
ValidateContextManifest(raw []byte, expectedID string, opts ContextValidationOptions) (ValidatedContextManifest, error)
ReadContextManifest(raw []byte, expectedID string, opts ContextValidationOptions) (ValidatedContextManifest, error)
ReadContextManifestEnvelope(raw []byte, expectedID string, opts ContextValidationOptions) (ValidatedContextManifest, error)
```

顺序：C0 严格解析/资源限制 → 原 v1 Schema → 本地集合/时间/描述符闭包 → 调用方的精确选择、已验证领域/人格和外部字节 → 未核验依赖 → `ctxm_` 内容身份。失败返回零值结果和带 Kind/Path 的错误；不修复正文、不填默认身份、不把 Schema 通过当成资格通过。

- `context_types.go`：完整 ContextManifest、RoleView、SnapshotRef，以及本地绑定选项。
- `context_semantics.go`：恰好三角色、独立经验/人格 ID、UTF-16 排序唯一性、精确种类闭包、无重复或孤儿 descriptor、不可变版本和公共有效期交集。
- `context_bindings.go`：独立调用方选择、Domain/Persona 精确 ID/角色/版本及声明区间、逐份描述符/原始字节校验，保留全部跨工件未核验依赖。
- `context_validate.go` / `context_read.go`：不可变结果、C0 身份与原始 canonical 字节 Adopt 回读、独立 Envelope 限额。

`ContextSelection` 固定 case/round、Charter 声明、Domain 发布版本、共同事实/偏好/索引、三角色 Persona/经验/Allowed Context、admission policy 和 `(scope,epoch)`。角色视图扩大或交换经验即使仍有完整 Schema/闭包，也会与该独立选择冲突。调用方须独立取得选择；直接复制待验证正文不证明归属或批准。

`ContextCharterBinding` **只是外部声明**，当前显式接受 schema_version=2.0 的选择上下文，检查其精确 `chtr_` ID、声明版本、领域 ID、声明有效期和 horizon。它不是 ValidatedCharter；本切片没有生产 Charter 解析器，原始 Charter 字节、完整 Schema/语义、主体确认、disclosure 约束及消费者 v2 兼容能力始终为 `UNVERIFIABLE_REFERENCE`。不能用这份声明证明 Charter 已批准，也不能把声明中的政策当作权限。没有选择上下文仍可检查本地闭包，但其跨工件选择状态不完整。

Domain/Persona 必须来自已有不可变验证结果。所选 Persona 的 `persona_id` 必须匹配角色，且其实际检查过的领域上下文必须匹配 Manifest。仅填写领域 ID、没有领域原件的 Persona 验证结果不能被提升为完整关系。两类工件的待核验依赖全部向上传递，组合后再检查同一外部 namespace/object/version 的摘要冲突。

`ContextSnapshot` 的独立 descriptor 必须逐字段相同；可选 `RawBytes` 按原始字节 SHA-256 比对，不使用 C0 工件域，也不规范化外部原文。nil 表示缺原件，非 nil 的空字节有明确空串摘要；摘要正确不证明快照内容完备、受控空经验/空偏好语义、角色归属或来源真实性。snapshot 的摘要与 source_ref 原件摘要不被无依据地当作同一种字节包。

已知 Manifest、快照、Domain、Persona 和 Charter 声明区间取共同交集，拒绝空交集及冻结前已到期的依赖。可选 `UsageWindow` 的整个区间必须落在已知交集和 Charter horizon 内；未提供时明确保留使用时有效性依赖。验证器不读取本机时钟，不把固定 Fixture 日期解释为当前有效，不取代外部可信时间、撤销或 freshness 核验。

默认正文资源上限为 262144 bytes / depth 24 / members 4096 / string bytes 16384，正数选项可覆盖工程限额；Envelope 分别派生 bytes+1024、depth+1、members+5、string 至少 69，支持独立更严格上限并防加法溢出。正文在 Envelope 中仍单独受限。原始快照另有每份 262144 / 总计 1048576 bytes 的默认工程上限，采用剩余预算扣减避免总和溢出。没有截断材料路径。

结果的 `Body()`、`CanonicalBytes()`、`PendingDependencies()` 均为独立副本，`Status()` 返回值。资格始终 `UNVERIFIABLE_REFERENCE`，实际读取始终 `NOT_RUN`；`RequireCompleteQualification=true` 始终失败关闭。`RequireCompleteBindings=true` 只要求全部本地选择、Domain/Persona 关系、descriptor 和原始快照齐全，仍不认证 Charter 或权限。

| 状态 | 证明范围 |
| --- | --- |
| `Structure=VALID_OFFLINE` | 冻结 Schema、本地语义和精确描述符闭包通过 |
| `Bindings=CHECKED_OFFLINE` | 调用方精确选择与已验证 Domain/Persona 关系一致；没有资格结论 |
| `Bindings=PARTIAL_OFFLINE/UNVERIFIABLE_REFERENCE` | 缺选择或已检查的 Domain/Persona 关系 |
| `SnapshotBytes=CHECKED_OFFLINE` | 所有独立 descriptor 和原始字节摘要匹配；没有来源真实性结论 |
| `SnapshotBytes=PARTIAL_OFFLINE` | 至少一份 descriptor/原始字节缺失 |
| `Qualification=UNVERIFIABLE_REFERENCE` | Charter、签发者、发布、访问/披露、偏好确认、作用域/撤销及唯一轮次登记未认证 |
| `ActualReads=NOT_RUN` | 未运行模型、会话、注入或读取记录 |

共享改动只有外部引用遍历的识别条件：按 `namespace` 识别已通过 Schema 的 ExternalRef，而不是看到 `content_digest` 就停止。这样 SnapshotRef 自己的摘要不会遮蔽内部 source/qualification policy；原 B1/B2 的 ExternalRef 结果不变。

## C. RED→GREEN 与独立 Golden

测试先写，API 缺失的编译失败单列，不冒充语义失败。可编译的仅 C0/Schema/身份实现实际放行了 **51 个** Schema 仍合法的语义变异；补齐闭包/角色选择/引用版本/有效期/字节绑定后转绿。复用 C0/Schema 已拒绝的 12 类反例属于原有行为回归，不算新增语义 RED。

存储入口的初始可编译实现实际放行 5 个反例：无预期 ID、两种非 canonical 表示、错误 Envelope digest 和嵌套正文重编码；增加原始字节 Adopt/摘要检查后转绿。独立补充的 Persona 领域证据反例也观察到先放行、后以 `UNVERIFIABLE_REFERENCE` 失败关闭。

Schema 漂移命令的初始空比较实现实际放行 9 个有效漂移反例，再加入比较后转绿；Token 漂移测试最初操作不存在的 maxLength 导致测试错误，已改为真实 pattern 变异并重跑。这项测试错误不计入漂移 RED 证据。

临时 RED 日志位于 `/tmp/magi-c1c1-{api-red,semantic-red,read-red,persona-domain-red,schema-red}.log`，不是持久发布工件。可复现证据为提交的具名测试和最终 Head CI。

两份全量新 Fixture 的 canonical bytes / digest / `ctxm_` ID 由独立 Python C0 参考先计算，Go 再核对；未从 Go 生成期望值。完整视图含 12 份 descriptor，空 Allowed Context 视图含 6 份；三角色经验均显式独立固定。Fixture 和原始快照均标记 synthetic/offline，不代表真实批准或权限。

| Fixture | Canonical bytes | ArtifactID |
| --- | ---: | --- |
| 完整三角色视图 | 10003 | `ctxm_bbac8ae597c5e4949584bf78b6e6bcaf71d106023d340a9a083c83dfcefc915b` |
| 空 Allowed Context 视图 | 5806 | `ctxm_a089bcedc94172201354c103fc84d12bbe65ea3945ee895066c063940cecf46a` |

`check-context-schema-parity.py` 比较 ContextManifest 的 14 份传递定义，精确列出 #39 已有的四处 Token→FixedVersion 收紧：Manifest.source_dependency_index_version、SnapshotRef.version、ExternalRef.version、SubjectRef.identity_source_version。它校验 FixedVersion 的完整别名规则；不做通用忽略或盲目替换。测试命令对输入侧及镜像侧共 10 个刻意漂移做拒绝验证。原冻结 Schema 不改写。

## D. 验证

本地全量 Go、race、vet、专项和下列全部契约检查均成功，gofmt 无输出，diff 检查无错误。最终 Head CI 状态在 PR 中单独记录。以下是实际运行的检查命令：

```sh
cd backend
GOCACHE=/tmp/magi-c1c1-go-cache go test ./...
GOCACHE=/tmp/magi-c1c1-go-cache go test -race ./...
GOCACHE=/tmp/magi-c1c1-go-cache go vet ./...
GOCACHE=/tmp/magi-c1c1-go-cache go test ./domain/inputartifact -count=1 -json
python3 domain/canonical/testdata/verify_golden.py domain/canonical/testdata/golden_vectors.json
python3 domain/inputartifact/testdata/verify_domain_golden.py
python3 domain/inputartifact/testdata/verify_persona_golden.py
python3 domain/inputartifact/testdata/verify_context_golden.py
GOCACHE=/tmp/magi-c1c1-go-cache go run ../scripts/check-input-artifact-contract.go
GOCACHE=/tmp/magi-c1c1-go-cache python3 ../scripts/check-decision-artifact-v2.py
python3 ../scripts/check-context-schema-parity.py
python3 ../scripts/test-context-schema-parity.py
gofmt -l .
cd ..
git diff --check
```

本轮 inputartifact 包共 671 个通过事件、7 个跳过事件（含子测试）；其中 Context 为 12 个顶层通过、112 个通过事件、2 个边界跳过。旧 C0/C0-R、Domain/Persona、C1-A、F36-01、Decision v2 的检查保持存在；v2 仍为 9 份工件 Golden、1 份风险输入 Golden、7 正例、88 负例、28 版本差分。

本地没有配置 `MAGI_TEST_MYSQL_DSN`；全量 Go 成功不能证明真实 MySQL 迁移、writer cutover 或新 Evidence Closure 事务。CI 的既有 MySQL Jobs 测试的是既有数据库契约，也不能变成尚未实现的 C1-C2 Evidence Closure 证据。

## E. Findings 与证据边界

1. **独立选择不可由快照 ID 名字推断。** 本地禁止经验继承；完整交换或扩大视图还须与外部选择比对。外部选择的真实性没有在本切片认证。
2. **Schema 仍合法的语义错误必须拒绝。** 闭包、排序、同 ID 异内容、错误 kind、错误 index、交叉角色/工件绑定和使用区间单独测试，不靠坏 ID 掩盖。
3. **同一引用版本不能有两种摘要。** Manifest、Domain、Persona 的组合依赖再次检查；缺资格不掩盖局部可判定矛盾。
4. **绑定和资格分开。** 完整字节/本地关系通过仍保留 Charter 原件/批准、签发资格、数据访问/披露、人格发布、偏好认证、scope 当前 epoch、唯一轮次登记和可信时序依赖。
5. **完整视图不等于已读取。** 没有 Agent Loop、provider session、可信回执、ReadSetJournal 或 Evidence Closure；没有模型人格行为实验。

## F. 放行与后续

交付目标是独立 Draft PR；独立源码审阅、最终 Head CI、Ready 和合并分别判断。#40 及父 Epic/路线图/CAS-9 不因创建 PR 或本地绿色关闭。

明确状态：

- **NOT RUN**：真实 Agent 注入、模型实际读取/三角色交付、Journal/ReadSet/Evidence Closure、数据库关闭事务、撤销失效传播、人格行为和 Rev4 运行时实验。
- **BLOCKED**：没有 C2/C3 权威实现的 Charter 批准、真实偏好/来源/访问/披露资格、治理根/身份凭据、独立见证、安全外发、唯一轮次登记和共同顺序提交。

后续 C1-C2 可复用这里的不可变允许视图身份；正式集成仍依赖独立审阅的 `VerifyAuthority` / `CommitIfCurrent`、credential/epoch fencing。当前代码没有生产签发入口或可以把离线通过升级成授权的选项。
