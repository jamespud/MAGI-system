# #38：Decision Artifact v2 的 A–F 交付记录

> 启动 2026-10-23，交付 2026-10-24（Asia/Shanghai）；Draft 供独立审阅。本报告是作者证据清单，不授予 Ready/merge 或 CAS-9 验收。

## A. 基线与范围

启动时 `main=37ec45997a62e4e2829f69e7384ed6f1a6c4447e`，`gh pr list --state open`
返回 `[]`；当前 linked worktree 干净、detached HEAD。#38 的 D2-01..08 为范围。
本 PR 新增版本化契约、机器结构、合成 fixtures、静态检查及 CI 入口；保留 v1/C0/C0-R/
C1-A/DomainProfile/PersonaSpec 既有身份。没有新协议生产 Go/DB/会话实现。

## B. v1 → v2 与身份

完整差异见 [契约 §1.1](decision-artifact-v2-contract.md#11-v1--v2-完整字段差异)。
显式 Domain、Manifest、case/round、共同包、三回执、本人视图、ReadSet ID/digest 和 scoped epochs；
自身 ID/digest 与后置批准/确认/closure 均不塞进被其引用的正文。
新正文 schema_version=2.0 继续用 C0 原类型域及前缀，固定九份独立 Python 字节/摘要/ID Golden，
再由现有 Go C0 校验。既有 v1/C0/C1 期望值、Schema 和生产者不改写。合成 Golden 是内容身份探针，
不是获批工件、ROLLOUT-LAB 有效配置或发布资格证明。

## C. RED → GREEN 与反例

真实 RED：初版工具仅调用现有 C0 + JSON Schema，正例通过；
`charter-domain-mismatch` 经重新寻址后仍被接受，进程以 1 退出：

```text
PASS positive: ACCEPT
AssertionError: charter-domain-mismatch: expected DOMAIN, got ACCEPT
```

补入离线绑定检查后通过；初次提交 `9fe9c2c` 的 5 个正向边界、78 个命名负例、9 份 Python/Go Golden
通过。没有把环境错误算作 RED；只声明上述实际观察到的单项 Schema-only RED，
不声称每个后来补入的负例都逐项经历过 RED。
首次默认 Go/npm 缓存因 sandbox 只读失败（EROFS），改用 `/tmp` 缓存后重跑。
Schema 闭合、重新寻址后语义拒绝、未重哈希身份篡改分别检查，不以任意失败当通过。
命名反例清单是 [negative-cases.json](contracts/decision-artifacts-v2/negative-cases.json)。
附加正向边界包括同键同 ID 重试、cutoff 后已准入节点重读、30000 ms 新鲜性等号。

## D. 实际命令与 CI

初次交付本地命令均成功（Go 使用 `GOCACHE=/tmp/magi-v2-go-build`，npm 安装缓存在 `/tmp`）；
下表记录 `9fe9c2c` 的结果，后续 F39 修订验证单列于 §E.1：

| 命令 | 实际结果 / 证明范围 |
| --- | --- |
| `python3 scripts/check-decision-artifact-v2.py` | 5 正向边界、78 命名负例、9 Python/Go 完整正文身份；仅静态 |
| backend: `go test -race ./...` | 通过；无 MySQL DSN 的数据库专属测试可跳过，不冒充 MySQL 验证 |
| backend: `go vet ./...`；`gofmt -l .`；新 Go 脚本 gofmt | 通过 / 无未格式化文件 |
| backend: `python3 domain/canonical/testdata/verify_golden.py domain/canonical/testdata/golden_vectors.json` | 0 failures，既有 C0/C0-R 字节与身份回归 |
| backend: `python3 domain/inputartifact/testdata/verify_domain_golden.py` | 通过，含 6 权威字段变更；资格仍 UNVERIFIABLE_REFERENCE |
| backend: `python3 domain/inputartifact/testdata/verify_persona_golden.py` | 三角色各 9 核心变更拒绝、旧 bytes/digest/ID 一致；行为 NOT RUN |
| backend: `go run ../scripts/check-input-artifact-contract.go` | 5 正例、46 负例、5 身份/Adopt、14 D-04、6 不支持 ReadSet 准入边界通过 |
| frontend: `npm ci --cache /tmp/magi-v2-npm-cache`；`npm run lint`；`npx tsc -b`；`npm test`；`npm run build` | 全部退出 0；32 文件 / 188 测试通过；原有 Dataset hook lint、React act、bundle size 警告保留 |
| `python3 scripts/test-decision-cutover.py`；五个 shell 脚本 `bash -n` | 3 测试与语法通过 |
| decision-cutover / backup / restore `--dry-run` | 通过，restore 使用 `/tmp` 合成 bundle，未改真实服务 |
| `git diff --check`；既有 Golden 路径差异核对 | 通过；原有 Golden 文件无修改 |

新增门禁接入 Backend quality gate，原有六个 CI jobs 均保留。远端 **exact-HEAD CI**
在创建 Draft 后核对，以 PR checks 的当前 `headRefOid` / workflow run 为准，实际结果写入
PR 描述，避免把旧 Head 的成功带到新提交。本文不提前声明远端成功。
所有 MySQL/session/C2-C3 **v2** 故障测试均 NOT RUN / BLOCKED；既有数据库 CI
即使通过，也只证明旧实现。

## E. P0 / P1 / P2 与独立审阅

本次独立只读 reviewer：未发现 P0/P1，判断适合提交 Draft 供进一步独立审阅，
不授权 Ready/merge。以下 P2 已修正并加入静态证据：

| 审阅发现 | 修正 |
| --- | --- |
| 先存 Charter 治理批准被错误要求等于后续执行 proof | 分开治理批准声明/作用域，正例使用不同 proof；仍各自验当前资格 |
| Manifest 视图/ReadSet 依赖期限未检查 | 正例修正期限，提交时检查所有描述符和节点，过期负例拒绝 |
| Candidate 确认上下文未核版本 | 确认序号/会话/请求/精确 Candidate ID、原始摘要和版本绑定 |
| 派生父节点被错误要求有独立读取事件 | 准入完整祖先闭包，增加仅经闭包准入父节点的合法重读正例 |
| 风险分类缺少显式来源 / C1-A 最小记录字段 | Candidate 后向引用先存 RiskClassificationRecord，补齐属性、证据、命中规则、版本和期限；过期/缺来源负例 |

作者另修正 Candidate 上下文 SourceNode 的 raw_content_digest：使用原始字节 SHA-256，
与 C0 类型域摘要分开。全部外置记录仍为合成声明，不能当签名或发布资格证明。
初次独立 reviewer 对当时最后一项修正再次复核：当时提出的 P2 已闭合，无剩余 blocker；
独立运行通过 9 份身份、5 个正向边界、78 个负例。该判断仅支持 Draft 审阅，
不授权 Ready/merge，也不证明运行时集成。

### E.1 PR #39 独立审阅后修订（2026-10-24）

后续源码审阅提出 F39-01/F39-02 两项 P2；初次 CI 成功没有覆盖这两个语义缺口。
先在原 Head `9fe9c2c` 实际运行五个新增变体，分别修改 proposal、conditions、
跨工件一致的 SnapshotRef.version、ExternalRef.version、依赖索引版本，并合法重新寻址
全部后继。五例的 Go Schema/C0/ID/Adopt 均通过，旧语义检查也全部错误接受；
回归断言进程退出 1，得到如下 RED（不是环境失败）：

```text
F39-01-stale-risk-after-proposal-change: expected RISK_INPUT, got ACCEPT
F39-01-stale-risk-after-conditions-change: expected RISK_INPUT, got ACCEPT
F39-02-consistent-snapshot-version-latest: expected SCHEMA, got ACCEPT
F39-02-consistent-external-ref-version-latest: expected SCHEMA, got ACCEPT
F39-02-consistent-dependency-index-latest: expected SCHEMA, got ACCEPT
5/5 regression expectations failed
```

| 发现 | 修订与区分能力 |
| --- | --- |
| F39-01：分类没有绑定精确候选输入 | RiskClassificationRecord 显式升为 2.1；新增 magi-risk-input/1 域分隔摘要，覆盖候选内容、domain、规则和输入属性/证据，排除后继 Snapshot ID 与分类输出。离线消费者重算；重寻址不自动刷新分类摘要。两个旧分类变体按 RISK_INPUT 拒绝，合法重新分类的变更提案接受。 |
| F39-02：普通引用版本接受可变别名 | Schema FixedVersion 与 Python 语义检查复用 C1-B1 的完整别名分段规则；28 项矩阵与冻结 Go 公共消费者差分。三个一致引用变体按 SCHEMA 拒绝；非 SemVer 固定编码正例接受。 |

修订后的完整门禁通过 **7 正向边界、88 命名负例、9 完整工件 Python/Go Golden**，
另有 **1 风险输入 Python/Go Golden、28 版本规则 Go/Python/Schema 差分用例**。
增加的其余五例覆盖分类属性、规则、证据、任意摘要和旧记录版本；
不声称这五例也各自经历了修复前 RED。
新风险记录引用导致七份下游 Draft Golden 改变；Charter、Manifest 两份不变，
原始 Draft 向量保留于 `9fe9c2c`。既有 C0/C0-R/C1-A/Domain/Persona 路径逐项核对无改动。

本轮重新运行 v2 门禁、既有 C0/Domain/Persona Python Golden、C1-A 结构检查、
backend `go test -race ./...`、`go vet ./...`、新 Go 脚本 vet/gofmt 与 `git diff --check`。
远端最终 Head 的六个 CI jobs 结果在 PR 描述中以精确 SHA/run 记录。
本轮不把初次本地 frontend/运维验证冒充为再次本地运行；这些由最终 Head CI 再验。

独立只读 reviewer 针对本轮差异复核后未发现可行动问题，判定 F39-01/F39-02
在离线契约范围内通过；独立运行完整门禁退出 0，并重新加载原 Head 的语义检查器，
确认原五个回归均 ACCEPT。复核确认 proposal/conditions 变体保留分类原件不变，
同时重新寻址 Candidate、三个 ReadSet 和三个 Ballot；摘要无后继引用环，
版本差分直接复用冻结 Go 入口。该结论只支持继续 Draft 独立审阅。

## F. 发布边界

本交付只能审阅/合并契约与离线门禁，不启用正式生产者。
C1-C 实际注入/read-set、C2 治理根/权限/epoch、C3 顺序/出口尚未实现，见契约 §6。
必须另行实现并运行：三真实交付、所有关闭写步骤故障、并发唯一键、锁后时钟/epoch/generation、
回复丢失恢复、撤销传播、终票独立 submit/aggregate/adjudicate、最终 witness/保护发送。
完整 k=1..4 / 4800 轮验收未运行。Epic/#38 的独立审阅验收与 CAS-9 不因 CI 绿灯自动完成。
