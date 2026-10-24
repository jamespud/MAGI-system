# C1-C2A：ReadSetSnapshot v2 离线语义消费者（#42）

## A. 基线、威胁模型与不变量

2026-10-24（Asia/Shanghai），起点为已合并 #41 的 `main=737ee9381ec0fdc57d26e605d557a844bdefd3d1`。工作分支 `codex/c1-c2a-readset-v2`，初始工作区干净；父执行器已核对当时开放 PR 为 0。这里的交付是离线消费者，不是 Journal、提供方交付、可信关闭事务或生产签发器。

依据 #42、Rev4 §§2、6–13、17、19，input-artifact contract §§5–6.5，冻结 Decision v2 Schema 与 C0 注册。既有 Rev4、Schema、Context/Persona/Domain/Decision Golden 及 Legacy 路径均保持原字节。新增 `readset_schema.json` 是冻结 Decision v2 Schema 的逐字节镜像；不修改原件。

攻击者可提交合法 JSON/Schema，重新计算 C0 身份，重排/替换事件、角色、代次、来源、祖先、关闭前缀、外部声明或其引用，并制造巨大嵌套/集合。摘要正确、`COMMITTED`、`APPLIED_VERIFIED`、`delivery_settled=true` 或复制正文生成一份“选择”都不证明权威来源。

必须保持的不变量：

1. ReadSet v2 使用独立 `read_set_snapshot` 域及 `rset_` 全摘要；原始存储正文须已经 canonical，Envelope 不能修复正文。
2. JournalKey、角色视图、索引版本、worker generation 和各 `(scope,epoch)` 精确绑定；generation 不冒充轮次或撤销代次。
3. 有限本地序列恰好为 `1..len(events)`；权威序号仅作为严格递增声明。登记 delivery ID 的重试保留同 key、同完整事件；不能在序列中追加冲突重放。
4. 每个事件的 source/parent 集与图完全一致；节点排序唯一，祖先齐全、无环、无额外节点。RAW/EMPTY_RESULT 无父，DERIVED 非空父；派生标签不能扩大 Manifest 角色访问上限。
5. 所有祖先的准入声明都先于首次派生曝光，所有已知有效期覆盖实际声明时间。cutoff 后只重读关闭前事件及其完整祖先闭包中的精确版本；不把新索引声明当作已登记旧材料。
6. Candidate → 外部 closure/completeness → ReadSet 只向既有工件引用；拒绝依赖中的未来 `rset_` 引用和跨记录种类复用同一外部身份。
7. 离线结构与绑定检查不能产生实际读取、来源真实性、签发资格、Journal 完整性或 C2/C3 成功。

## B. 实现与使用边界

入口位于 `backend/domain/inputartifact/readset_*.go`：

```go
ValidateReadSetSnapshot(raw, expectedID, opts)
ReadReadSetSnapshot(raw, expectedID, opts)
ReadReadSetSnapshotEnvelope(raw, expectedID, opts)
```

验证次序为 C0 严格有界解析 → 精确冻结 v2 Schema → 序列/图/时间/cutoff → 独立声明与已验证 Context → Candidate/原始外部记录字节 → 依赖一致性 → `rset_` 身份。失败返回零值和带 Kind/Path 的错误。回读需要非空 expected ID；比较原始字节与本次有界 C0 结果，不使用 C0 `Adopt` 的较小默认 member 限额隐式拒绝合法大型正文。512 事件正例覆盖这条路径。

`ReadSetSelection` 是外部精确选择；`ContextManifest` 必须来自既有不可变 validated 类型。Manifest 的 case/round/角色/spec/experience/preference/index/context scope+epoch 均精确比较；其全部 pending dependencies 原样向上传递。Context 的部分 Persona/domain 或 snapshot 证据保持 `PARTIAL_OFFLINE`，严格本地绑定模式失败关闭。完整本地正例通过真实重新验证、重新寻址 Persona/Context 与后代构建，没有伪造 validated 状态。

`Sources` 和独立 `SourceDependencyIndexVersion` 是外部不可变索引声明。每个节点需要精确 descriptor、严格早于 closure cutoff 的准入权威序号声明和 Manifest snapshot membership；先按 SourceEntryID 核对已知 Candidate/全部 Manifest snapshot 的身份、精确内容和角色，再考虑调用方 membership。DERIVED 的每个祖先分别检查。调用者供应的 membership 本身没有来源认证能力。Candidate 来源另以精确 C0 ID、版本和原始 canonical 字节 SHA-256 绑定。权限、producer、方法、验证政策引用固定版本并列入未核验依赖；没有网络/隐式 latest 解析。

`Candidate` 原件按冻结 Schema、C0 ID、case/round/Manifest/三角色视图/共享包/receipt refs/数量核对。`Materials` 用 `MaterialKey(ref)` 索引，必须保留独立原始字节 SHA-256；原件不会重编码后验摘要。可用的三角色 receipt、EvidenceClosureRecord、CompletenessRecord 再按冻结对应类型解析并交叉核对。缺原件是部分证据，存在但内容矛盾是错误。closure 的各角色非零水位由 Schema 约束，本人水位精确分割 cutoff 前缀；completeness 保留完整事件版本和结算时序。记录中的合成成功字段始终不受信。closure 的 confirmation refs 等尚未认证外部依赖明确保留 pending，未实现 confirmation 提供方核验。

默认工程资源上限：正文 1048576 bytes / depth 32 / members 65536 / string bytes 16384；事件 512、节点 2048、依赖路径深度 64；每份外部原件 1048576 bytes，总量 4194304 bytes（含 Candidate），materials 最多 4096。外部 metadata 每字符串另限 16384 bytes，并在拼接 key 前检查。正数选项覆盖对应工程限额，冻结 Schema 的数组上限仍不放宽。原件采用剩余预算扣减；Envelope 采用饱和加法独立派生 bytes+1024/depth+1/members+5，正文仍单独受限。校验不截断事件或依赖。

结果 `Body()`、`CanonicalBytes()`、`PendingDependencies()` 均返回独立副本，Status 返回值：

| 维度 | 可能结果和含义 |
| --- | --- |
| Structure | `VALID_OFFLINE`：精确 v2 形状及本地序列/DAG/截止语义一致 |
| Bindings | `CHECKED_OFFLINE` 或 `PARTIAL_OFFLINE`：仅供应的本地声明/Context/原件的一致性 |
| ContextBindings | 保留 Context 检查的部分状态；未提升其资格 |
| Completeness / Qualification | 始终 `UNVERIFIABLE_REFERENCE` |
| ActualReads / Authority | 始终 `NOT_RUN` |

`RequireCompleteBindings` 拒绝缺独立选择/Context/索引/登记事件/Candidate/必需记录字节及部分 Context。它不能消除尚未认证的外部依赖。`RequireCompleteQualification` 或 `RequireActualReadProof` 始终失败关闭，包括全部自洽合成记录的正例。

## C. RED→GREEN 与 Golden

先写期望 API 测试，缺 API 的编译失败单列。独立可编译基线探针使用原冻结 v2 Schema 和 C0，实际接受七个重新寻址的合法形状语义反例：事件 gap、外来 generation、错误目标角色、RAW 隐藏父、错误高水位、错误 Context epoch、新材料在 cutoff 后首次出现。它们不是 Schema 拒绝，也不是编译错误。相同语义在新消费者中拒绝。

后续自审另获得三个真实可编译 RED：派生祖先晚于曝光准入、来源引用未来 ReadSet、巨大外部 metadata；分别补整条祖先曝光检查、禁止未来引用和拼接前限额后 GREEN。测试夹具最初 round 未与 Context 对齐、512 事件测试辅助 canonicalize 沿用 C0 默认 4096 members，是测试错误；已修正并重新跑正反例，不计入语义 RED。

新 Python 脚本独立读取全量正文并计算 canonical bytes/digest/`rset_`，不调用 Go。Go 对照新冻结 vector，既有 Decision v2 向量未重写：7419 bytes，`rset_d2183668ea2296cff06cc4b8882a2bdd7cf518ba6d771f16a195619ec47e8c06`。它同时核对供应 Candidate C0 ID 和外部记录原始摘要；这些 Fixture 全部 synthetic/offline。

具名验收测试：

| 测试 | 主要义务 |
| --- | --- |
| `TestReadSetFullBodyGolden` | Python/Go 全正文、身份、部分 Context 和 pending 传播 |
| `TestReadSetSemanticMutations` | Schema 仍合法且重新寻址的选择、序列、generation/scope、source/DAG/cutoff、索引/共享包反例 |
| `TestReadSetExternalRecordMutations` | 外部原件重新计算原始摘要后的 closure/receipt/completeness 语义反例；settlement=false/零水位是 Schema 拒绝，单独识别 |
| `TestReadSetLegalRereadAndDerivedClosure` | 合法 cutoff 后重读、合法派生链、外来嵌套来源和深度拒绝 |
| `TestReadSetNestedAdmissionPrecedesExposure` | 晚到祖先的真实 RED→GREEN |
| `TestReadSetNoFutureContentReference` | 未来 ReadSet 引用的真实 RED→GREEN |
| `TestReadSetCompleteLocalBindingsAndMissingInputs` | 真实完整本地正例，逐项删除八类输入失败关闭 |
| `TestReadSetOfflineGateAndMissingDependencies` | 两个严格资格/实际读取门、部分 Context 和全缺输入状态 |
| `TestReadSetReadbackLimitsAndCopies` | expected ID、原始 canonical、Envelope、limits、原件总预算、结果复制 |
| `TestReadSetEventCountBoundary` | 512 事件完整正文/回读成功，513 拒绝 |
| `TestReadSetEnvelopeOverflowSafe` | max-int 工程限额派生不溢出 |
| `TestReadSetExternalMetadataResourceLimit` | 外部 metadata 真实 RED→GREEN |
| `TestReadSetVersions` / `TestReadSetSchemaMirror` | 固定非 SemVer 版本、mutable aliases、冻结镜像 |

CI 新增 full-body Python Golden、逐字节 Schema parity 和六个独立漂移反例；原 C0/C1/Decision 检查步骤保留。

## D. 实测命令与证据边界

命令均从 `backend/` 运行，Go 使用可写 `/tmp` 缓存。临时日志目录 `/tmp/magi-joint-stage-43/a-logs/` 不作为持久发布工件；提交中的具名测试和实际 PR head CI 才可长期复现。

| 命令 | 本地观察 |
| --- | --- |
| `GOCACHE=/tmp/magi-c2a-go-build go test ./domain/inputartifact ./domain/canonical` | 起点基线 PASS |
| `GOCACHE=/tmp/magi-c1c1-go-cache go test ./...` | 实现后完整 Go PASS；真实 MySQL 条件套件没有配置 DSN |
| `GOCACHE=/tmp/magi-c1c1-go-cache go test -race ./...` | 完整 race PASS |
| `GOCACHE=/tmp/magi-c1c1-go-cache go vet ./...` | PASS，无输出 |
| `gofmt -l domain/inputartifact/readset_*.go`、`git diff --check` | 无输出 |
| `go test ./domain/inputartifact -run ReadSet -count=1 -v` | 全部新验收 PASS；含 512 事件和完整本地模式 |
| `python3 domain/canonical/testdata/verify_golden.py domain/canonical/testdata/golden_vectors.json` | 原 C0 向量 PASS |
| `python3 domain/inputartifact/testdata/verify_{domain,persona,context,readset}_golden.py`（分别运行） | 全部原件与新增 full-body Golden PASS |
| `python3 ../scripts/check-context-schema-parity.py`、`test-context-schema-parity.py` | 原 Context parity/反例 PASS |
| `python3 ../scripts/check-readset-schema-parity.py`、`test-readset-schema-parity.py` | 新精确镜像及六个漂移反例 PASS |
| `go run ../scripts/check-input-artifact-contract.go` | C1-A/C0-R 结构与身份兼容 PASS |
| `python3 ../scripts/check-decision-artifact-v2.py` | 原 9 Python/Go identities、7 正边界、88 named negatives、risk-input Golden、28 version parity PASS |

本轮不会声称本地测试等于 PR head 的 6/6 CI。实际 Draft PR head、独立 review、CI job 和 Ready/merge 决定由协调执行器补录，旧 main/旧 PR job 不冒充本 head。

## E. 自审与未运行项目

| 发现 | 级别 | 处理 |
| --- | --- | --- |
| 只核对直接来源会漏掉晚到祖先 | P1 | 真实 RED 后按整条 DAG 的首次曝光检查准入声明；GREEN |
| source ref 可隐藏未来 rset 依赖 | P1 | 真实 RED 后拒绝所有依赖中的未来 ReadSet 身份；GREEN |
| 外部 metadata 未受原件 bytes 预算约束 | P2 | 真实 RED 后在 refKey 拼接前设字符串硬上限；GREEN |
| 默认 C0 Adopt member 数低于新正文上限 | P2 | 回读比较本次有界 C0 原始字节；512 事件正例覆盖 |
| 合成 COMMITTED/settled 容易误报 authority | P1 风险边界 | 无成功能力；Completeness/Qualification 不可核验、ActualReads/Authority NOT_RUN，严格门拒绝 |

| 验收项 | 状态 | 原因 |
| --- | --- | --- |
| 实际 provider 日志对比、可信 SESSION APPLY、模型真实读取 | NOT RUN | 没有 provider/session 实现 |
| append-only Journal、持久 worker fencing、未结算注入、原子 EVIDENCE_CLOSED | BLOCKED | #44 C1-C2B 及后续关闭集成 |
| 来源/派生链认证、实际权限、在线撤销与竞争发送 | BLOCKED | C2/C3 与受控材料准入、签名/治理根 |
| C2 凭据、独立 witness、生产签发、SealedBallot | NOT RUN / BLOCKED | 本 Issue 不授予这些能力 |
| Rev4 人格行为、k=1..4 裁决、Epic/CAS-9 总体验收 | NOT RUN | 超出离线消费者切片 |
| 新 PR actual-head 6/6 CI、独立 review、Ready/merge | 协调执行器待办 | 本地不预先宣告通过或允许 merge |

### E.1 独立审查修正（基于 `4ca407299ce7b27a9dab981805ca5a7499a235c5`）

独立审查初始结论为 **CHANGES REQUIRED**，发现以下四项离线语义缺陷；没有生产 authority 越权结论。临时完整审查和五个原始诊断位于 `/tmp/magi-joint-stage-43/review-a.md`、`review-a-probes_test.go`。修正限制在两个消费者文件、新增回归测试和本文，不修改冻结 Schema/Golden 或生产权限。

| 审查项 | 修正与覆盖文件 | 精确测试义务 |
| --- | --- | --- |
| A-1 / P1 | `readset_bindings.go` 先识别 `SourceEntryID` 的已知 Candidate/Manifest snapshot，拒绝伪造 membership、外来角色、已知 descriptor 矛盾；不受 RAW/DERIVED 或是否为祖先影响。未知来源依旧未认证 | `TestReadSetReviewCandidateMembershipBypass`、`TestReadSetReviewForeignSnapshotMembershipBypass`、`TestReadSetReviewKnownIdentityIgnoresFakeLabels`（direct/ancestor/derived_identity）、`TestReadSetReviewKnownIdentityMembership` 的正确身份正例与仅改 label 反例 |
| A-2 / P2 | `readset_semantics.go` 从 cutoff 前事件的完整精确祖先闭包构造 admitted 集；允许已准入祖先首次直接重读，不从新索引声明制造 admission | `TestReadSetReviewKnownAncestorReread` 正例、`TestReadSetReviewAncestorRereadNewVersionRejected` 在 `CUTOFF` 拒绝；对齐原冻结 Python `post-cutoff-admitted-parent-without-own-read-event` 正边界 |
| A-3 / P2 | 本地事件禁止 `authority_sequence == cutoff`；关闭前缀严格 `< cutoff`、重读严格 `> cutoff`；索引 admission 同样严格 `< cutoff` | `TestReadSetReviewEventUsesClosureAuthoritySequence`、`TestReadSetReviewAdmissionReservesClosureSequence` |
| A-4 / P2 | `readset_bindings.go` 检查前缀 `injected_at <= committed_at`，关闭后重读 `committed_at <= injected_at`；只是声明时间一致性，不是真实可信时间 | `TestReadSetReviewClosureBeforeClosedEvent`、`TestReadSetReviewClosureTimeline` 的 prefix_after_close/reread_before_close 反例，以及 equal_prefix_time/equal_reread_time 正边界 |

新测试均在 `readset_review_test.go`，使用真实完整 Context 和严格本地模式。来源变异同步更新 event/index/delivery/completeness，时间变异重新计算原始记录摘要和 ReadSet ID，明确检查 Schema 合法；扩展负例还断言内层 `BINDING $.sources` 或 `BINDING $.closure.committed_at`，避免仅以外层引用不匹配通过。

实际 RED：在未改生产代码时执行 `GOCACHE=/tmp/magi-c1c1-go-cache go test ./domain/inputartifact -run '^TestReadSetReview' -count=1 -v`，原五项全部失败（`review-integration-red.log`）；扩展 direct/ancestor/DERIVED、纯 membership 和前后关闭时间用同一命令获得额外语义失败（`review-expanded-red.log`）。own_snapshot 内容冲突和 admission 等于 cutoff 的回归原已被其他一致性/首次曝光检查拒绝，未算新增语义 RED。修正后相同命令全部 PASS（`review-green.log`）；完整 ReadSet 命令 `go test ./domain/inputartifact -run ReadSet -count=1 -v` 通过（`review-final-readset-green.log`）。

祖先重读正例把 completeness `recorded_at` 同步设为 `10.500Z`，以覆盖真正的合法重读；原诊断的重读为 `10.500Z`、完整性记录仍为 `10.000Z`，在消除原 CUTOFF 缺陷后会触发正确的 settlement 时序拒绝。这个夹具修正没有放宽生产检查。

本轮修正后重新执行完整 `go test ./...`、`go test -race ./...`、`go vet ./...`，以及 D 节全部原有/新 Golden 和 Schema 检查。日志分别为 `review-full-go-test.log`、`review-full-race.log`、`review-vet.log`、`review-contract-regressions.log`；均 PASS。`gofmt -l .` 和 `git diff --check` 无输出，冻结原件对基线/初始实现的 diff 为空。实际修正 head、范围 diff 和命令退出状态另录本机交接报告；独立复审、actual-head CI、Ready/merge 继续由协调执行器单独决定。

## F. 后续依赖 DAG 与发布门

`C0-R + Domain/Persona + Context v1 + 冻结 Decision v2 → 本 #42 离线消费者 → #44 Journal/受控准入/真实读曝光 → 三角色终审交付与可信关闭 → 权威 ReadSet/SealedBallot`。C2 治理根/VerifyAuthority 和 C3 共同顺序/条件提交并行提供权限边界；#45 是共享接口设计，不是这些机制的运行证据。

本分支只应进入独立 Draft PR 和源代码复核。离线切片验收、Ready 和 merge 是独立决定；没有生产 issuer 能力、没有 Epic/CAS-9 关闭或真实读取证明。
