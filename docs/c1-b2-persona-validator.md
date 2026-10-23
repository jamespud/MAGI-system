# C1-B2：PersonaSpec v1.0 离线语义验证器

## A. 基线

2026-10-23（Asia/Shanghai）执行前重新 fetch：`HEAD` 与 `origin/main` 均为 `cee6eae4f14e35d979185d2d439f328227c08553`。PR #36 已合并，GitHub 开放 PR 为 0；#4、#8 为 OPEN。分支为 `codex/c1-b2-persona-validator`，已有独立 worktree，无初始工作区改动。

Linear CAS-9 的 `In Progress` 来自用户本轮核实。执行环境没有 Linear 连接器或可用认证页面，作者未独立读取该状态；未修改 Linear 或 Issues。

依据：Rev4 §3、§5–8、§17.1–17.5，C1-A §2、§4、§9，现有 Schema、C0 与 B1。原 Rev4、Schema、C0/C0-R Golden、DomainProfile Golden 和 Legacy 字节均未修改。

## B. 实现

代码位于 `backend/domain/inputartifact/`：

- `persona_types.go`：完整 PersonaSpec、Investigation、Allocation、PersonaExtension、PersonaCompatibility 与选项/状态类型。
- `persona_baseline.go`：不可被调用方覆盖的三个正式 v1.0 基线、原始集合排序/唯一性、有序字段、固定 2/2/1/1 调用桶与增量扩展检查。
- `persona_domain.go`：精确 DomainProfile 绑定、逐条规则存在性和断言类型、声明有效期交集、预算交集及明确的未核验依赖。
- `persona_validate.go`、`persona_read.go`：严格验证、不可变结果、Adopt 回读与独立 Envelope/正文资源上限。

入口：

```go
ValidatePersonaSpec(raw []byte, expectedID string, opts PersonaValidationOptions) (ValidatedPersonaSpec, error)
ReadPersonaSpec(raw []byte, expectedID string, opts PersonaValidationOptions) (ValidatedPersonaSpec, error)
ReadPersonaSpecEnvelope(raw []byte, expectedID string, opts PersonaValidationOptions) (ValidatedPersonaSpec, error)
```

处理顺序固定为 C0 严格解析 → 现有 C1 Schema → 本地基线语义 → 领域依赖/资源检查 → C0 persona_spec 身份派生/比对。回读先用正文 Limits 预检，再 Adopt 原始 canonical 字节；不剥 fence、不修复存储表示、不把 JSON Unmarshal 当作可信入口。

`ValidatedPersonaSpec` 返回 `Body()`、`CanonicalBytes()`、`Digest()`、`ArtifactID()`、`PendingDependencies()`、`Status()`、`DomainProfileID()` 和 `EffectiveBudget()`。正文、字节、依赖、嵌套调查列表和费用对象均不向外暴露内部可变引用。零值不具有身份或通过状态。

Schema 已冻结 spec/schema_version=1.0、输出字段清单、6 次/6000 token 与转移策略；语义层逐个核对九组正式代码。代码集合检查原始 UTF-16 排序/唯一性并与完整正式集合比较；仅对内置预期集合排序，从不修复输入。核心新增义务必须放在独立的 `domain_extensions`，不能通过改核心列表、描述、兼容版本或扩展同名 ID 覆盖原义务。

选项中的 `DomainProfile` 仅接受 B1 不可变验证结果，并须同时给出匹配的 `DomainProfileID`。无领域扩展的 Persona 也保留所核对的领域 ID。领域选择属于验证上下文；Persona 内容身份仍只由完整 Persona 正文派生，消费者不能把一次验证结果移用到另一个领域。后续决策绑定不在本 PR 实现。

状态分别记录：

| 状态字段 | 含义 |
| --- | --- |
| `Baseline=VALID_OFFLINE` | 正式机器码/结构/本地规则通过；没有执行调查 |
| `DomainRelations=UNVERIFIABLE_REFERENCE` | 没有提供对应 DomainProfile，跨工件关系未核验 |
| `DomainRelations=CHECKED_OFFLINE` | 精确 ID、规则存在性、已知调查代码的声明断言类型和声明有效期相交已检查；实际证据适用性仍未核验 |
| `DomainRelations=PARTIAL_OFFLINE` | 领域引用已检查，但自定义扩展调查代码没有冻结的本地断言类型映射，其含义/方法绑定仍未核验 |
| `Qualification=UNVERIFIABLE_REFERENCE` | 签发、维护、扩展批准、身份、连续性、发布和撤销没有获得外部资格证明 |
| `Behavior=NOT_RUN` | PER-01..03 与 Rev4 §17.5 均未实测 |

`RequireDomain=true` 在缺失领域时失败关闭；`RequireCompleteQualification=true` 始终失败关闭，因为本切片没有资格解析器或治理根。任何外部引用，包括看起来声称“已通过”的行为记录，都只生成待核验依赖。

`AdditionalBudgets` 应由消费者提供所有适用的 Charter/工程上限。验证器将其与所选 Domain 和 Persona v1.0 上限取交集，含角色/任务交集、精确货币维度检查；不存在 Domain=12 导致 Persona 超过 6 的路径。`EffectiveBudget` 的 bool 为 false 时表示缺少预算上下文，不能当作无限额度。没有领域时只能计算已提供上限，不能声称完整领域预算已检查。

若直接完成最低调查所需调用数超过组合上限，产生 `RUNTIME_BUDGET_REVIEW_REQUIRED`。这不是调用次数预测，也不判定合格独立记录已存在；后续运行时必须证明合法替代或弃权/暂缓。本 PR 没有执行调查、计数、投票或外发。

共享改动仅抽取原有外部引用遍历和 Envelope 元数据解析；B1 返回行为、错误及默认资源计算保持原样。C0 算法没有新增实现。

## C. RED→GREEN

测试使用真实验证器和独立 Fixture，无 Mock 投票：

| 回归组 | RED 现象 | GREEN 条件 |
| --- | --- | --- |
| 三份独立正式基线 | 仅有 Schema 时，删除/替换关键代码、乱序、错误 baseline_id、错误调用桶仍被接受 | `TestMelchiorBaseline`、`TestBalthasarBaseline`、`TestCasperBaseline` 对九组字段逐项删除/替换/重复/乱序，全部拒绝，合法原件通过 |
| 领域及预算 | 缺领域时 RequireDomain 放行、缺失/错误类型规则放行、组合预算结果缺失 | 精确领域 ID/规则逐项核对、预算交集与资源不足依赖通过 |
| 存储表示 | 普通 canonicalize 把非 canonical 存储字节修复后接受 | 原始字节预检 + Adopt 拒绝；正文恰达上限的合法包装仍可回读 |
| Envelope 摘要 | 三角色错误包装 digest 被接受 | 回读正文后逐项比对 digest，拒绝全部反例 |
| 跨工件引用冲突 | Persona/Domain 分别合法，但同一外部对象版本有不同 digest，仍组合成功 | 组合依赖一致性检查拒绝，并返回零身份 |
| 额外费用上限 | 费用交集有结果但未保留计费映射依赖 | `UNVERIFIABLE_BILLING_MAPPING` 明确保留 |

初次新增 API 的缺失编译错误单独属于入口缺失，不将它冒充语义 RED。上述语义/回读/摘要反例均在能编译的中间实现上实际运行失败，再由实现转绿。

原始 RED 日志保存在本次执行环境的 `/tmp/magi-c1b2-red-*.log`；这些临时日志不是可持久发布的验收工件。永久证据是随 PR 提交的测试及最终 Head 的 CI。

三份完整 Fixture、canonical UTF-8 字节和 Golden 元数据均在 `testdata/persona_*`。Python 参考程序直接读取 C1-A §4.2 检查全部正式基线，并使用既有独立 Python C0 实现计算期望值，不运行 Go 编码器。每角色冻结九类核心字段变异后的摘要/ID和拒绝结果，以及所有 Persona 外部引用。合成引用不代表真实治理批准。

| 人格 | Canonical bytes | pspc ArtifactID |
| --- | ---: | --- |
| MELCHIOR | 3490 | `pspc_6cb82968cbc4b922fd78bec825ccb8c24cee342a650ee5fcb0d152d99b8ab87e` |
| BALTHASAR | 3536 | `pspc_fca8ae6f97e645262eb454fd5ab869cfd2e2ba1d6f38539000890d46dd081721` |
| CASPER | 3504 | `pspc_92847d5e37906e013fe6b0701a0af6c54ecfd12a36c390b296f539dbd03393a2` |

三份 Fixture 含附加可用性调查义务，其自定义机器码的方法/类型注册没有外部证明，因此 Golden 如实冻结 `PARTIAL_OFFLINE`，而非把它提升为已完成扩展资格认证。

## D. 验证

最终代码上的下列本地检查均已通过；`gofmt -l .` 无输出，`git diff --check` 无错误：

```text
cd backend
go test ./...
go test -race ./...
go vet ./...
go run ../scripts/check-input-artifact-contract.go
python3 domain/canonical/testdata/verify_golden.py domain/canonical/testdata/golden_vectors.json
python3 domain/inputartifact/testdata/verify_domain_golden.py
python3 domain/inputartifact/testdata/verify_persona_golden.py
cd ..
gofmt -l .
git diff --check
```

C0/C0-R、Schema 镜像、C1-A 正负例/身份变异/类型分离、D-04 预算边界、B1/Persona Golden、Legacy 摘要及 F36-01 原回归均由上述检查覆盖。CI Backend gate 新增独立 Persona Python 检查；其他既有 Job 不删减。

本地没有 `MAGI_TEST_MYSQL_DSN`。具名 MySQL 核验运行记录 129 个跳过事件；同次筛选中其他测试通过不算真实 MySQL 证据。真实 MySQL 迁移和 writer-cutover 执行须以最终 Head 的独立 CI Jobs 为准，不能从本地 `go test` 绿色推断数据库验证通过。

显式边界测试：`TestPersonaBehaviorIntegration` 为 NOT RUN；`TestPersonaQualificationIntegration` 为 BLOCKED。原 B1 的三个集成边界也保持跳过。测试/CI 绿色不改变这些状态。

## E. Findings

1. **跨工件相同外部版本的摘要矛盾已修复。** 自审和独立只读审阅均复现同一 P2；组合后再次检查外部引用身份/摘要一致性。资格未核验不掩盖本地可判定的矛盾。
2. **调查代码到断言类型是本地保守映射。** EFFECT_BASELINE/RECOVERY_VERIFICATION 只接受 FACT；FEASIBILITY_COUNTEREVIDENCE/FAILURE_IMPACT 可接受 FACT 或 PREDICTION；AUTHENTICATED_GOAL_PREFERENCE 只接受 USER_PREFERENCE；CONTROL_CONSENT_BURDEN 接受 EXTERNAL_AUTHORITY 或 FACT，不拿普通偏好推测当作权限链或实际负担证明。每个候选规则都必须匹配，不能混入一个弱规则靠另一条正确规则放行。这不证明预测质量、同意范围或实际恢复成功。
3. **实际适用条件不能靠现有 Schema 证明。** Domain 规则只有声明类型、来源类别、方法引用、TTL 和失效事件，没有本轮具体环境/资源/证据记录。每项调查保留 `UNVERIFIABLE_APPLICABILITY`；自定义扩展另保留 `UNVERIFIABLE_REQUIREMENT_BINDING`。B1 的全部待核验依赖也向上传递。没有暗中补成 VERIFIED 或已发布领域。
4. **description 只有解释意义。** 正式规范意义来自机器码，不从自由文字推导较弱条件；更改文字不删除调查义务，也不证明调查质量。即使文字声称无需调查，也不会更改代码定义。消费者不得把自由描述作为覆盖正式基线的运行时指令；自然语言忠实性评估与模型遵循均未执行。
5. **外部版本历史和预算上下文不自证。** 相同基线下核心改动被本地拒绝，正文或 version 改动改变完整 C0 身份；跨发布连续性、已注册版本、紧急撤销和批准者资格仍待外部记录。额外预算的供应完整性属于消费者集成义务，不新增 Charter 解析或授权路径。

## F. 放行

本交付仅供独立 Draft PR 审阅。三人格正式机器基线可确定性离线核验、派生不可变身份并严格回读；任何缺失/未知关系都有独立状态或失败关闭入口。

不能据此批准真实人格发布、宣称 PER-01..03 或复合干预通过、完成 CAS-9、关闭 Issues、放行 C2/C3 或启用运行时/最终外发。ContextManifest、ReadSetJournal、Decision Artifact v2、AuthorityProof、治理根、发布登记和真实行为实验均在本 PR 之外。
