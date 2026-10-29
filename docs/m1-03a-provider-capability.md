# M1-03A：Provider 能力验证

> 关联：[#44](https://github.com/jamespud/MAGI-system/issues/44) ·
> Stage [#43](https://github.com/jamespud/MAGI-system/issues/43)
>
> 状态：**BLOCKED / discovery-only implemented / default-off**。本切片只交付
> Provider 能力判定、只读 discovery probe 与机器可读证据。没有真实 Provider
> Adapter、material APPLY、可信回执、`drcp_`、`APPLIED_VERIFIED`、Settlement
> 或生产暴露权限。

## 0. 结论先行

当前环境只有一个 Anthropic-compatible local gateway：

```json
{"data":[],"has_more":false}
```

`GET /v1/models` 返回 HTTP 200 且 body 为合法的空列表。这只证明**当前未配置/未暴露
可验证模型目标**，不能推出底层 Provider 不支持 fence、撤销水位或 APPLY 原件。因此：

- 六项能力全部为 `NOT_RUN`；
- 综合结论为 `BLOCKED`；
- 没有调用 `/v1/messages`，`remote_model_calls_attempted=0`；
- 没有生成 `drcp_` 或 `APPLIED_VERIFIED`；
- `M1_GATE` 保持独立验收边界 `NOT_RUN`。

机器可读报告：

- 仓库内：[`docs/evidence/m1-03a/provider-capability-report.json`](evidence/m1-03a/provider-capability-report.json)
- 本次实现会话副本：`/tmp/m1-03a-provider-capability-report.json`
- 报告 SHA-256：`6f4945bdfafb738f5d0565e436672a954943b0a9ecae5c8c00ac997bf853c333`
  （其中包含本次 UTC 观测时间，后续复跑会产生新报告与 digest）。

## 1. 范围、基线与现有契约

- 基线：`main=4d969869410e64ffb3816e3d72475e0987e78e23`
- 分支：`codex/m1-03a-provider-capability`
- 只读检查了现有 `domain/port/model.go`、`adapter/model_adapter.go`、
  `domain/authoritycommit/`、`docs/authority-journal-admit-v2.md` 和 M1-00
  场景。现有运行时只有 OpenAI-compatible chat model builder；没有 Provider
  session identity、可执行 fence、撤销水位查询或独立 APPLY 原件接口。
- 本切片新增 `backend/domain/providercapability/` 与
  `backend/cmd/m1-provider-capability/`，不改 MySQL、Authority、Journal、
  Exposure Gate、生产启动器或 M1-02B-1 Worktree。

## 2. 状态语义（安全边界）

| 状态 | 判定条件 |
| --- | --- |
| `VERIFIED` | 有明确选定的真实目标；证据绑定 Provider/model/project/session/request/generation、UTC 时间、原始响应与独立查询响应的 SHA-256 和引用；含预期/实际结果、可复现步骤及能力专属原件事实（例如 fence refusal、撤销水位、APPLY receipt、恢复拒绝）。独立查询必须由不同 verifier 产生并明确 `PASS`。 |
| `UNSUPPORTED` | 有可核验的 Provider 接口契约/原始响应明确证明不支持该能力。超时、缺配置、空模型列表和普通 HTTP 错误都**不能**升级为此状态。 |
| `UNVERIFIABLE` | 有目标或部分证据，但缺少独立原件、完整身份绑定、可复现步骤或一致的能力结论。HTTP ACK、chat 响应、模型自报、gateway 普通日志不能升级。 |
| `NOT_RUN` | 没有真实目标，或该能力尚未执行测试。当前环境正是这一状态。 |

本切片的分类 API 只接受完整的 `IndependentVerification` 结构；测试中的 verifier
数据仅证明判定逻辑，不代表真实独立 verifier 已存在。缺少独立查询、能力专属原件或
`PASS` 时，最高只能保留为 `UNVERIFIABLE`。

综合 `verdict` 只有六项全部 `VERIFIED` 才可能为 `CAPABILITY_VERIFIED`；任何一项为
`NOT_RUN`、`UNSUPPORTED` 或 `UNVERIFIABLE` 都是 `BLOCKED`。本次 probe CLI 还会把
未经独立复审的能力矩阵固定为 `BLOCKED`，防止“代码判定通过”被误当验收通过。

## 3. 六项能力矩阵

| 能力 | 当前状态 | 必须证明的内容 | 当前证据/阻塞 |
| --- | --- | --- | --- |
| Session Identity | `NOT_RUN` | 会话、请求与 Generation 精确绑定 | 无模型目标；仅 200 + 空列表 |
| Enforced Fence | `NOT_RUN` | 撤销/过期后在实际暴露点强制拒绝，而非仅前置查询 | 无 Provider fence 接口或原件 |
| Revocation Watermark | `NOT_RUN` | 新撤销水位阻止旧授权继续暴露 | 无撤销查询/竞争测试 |
| Trusted APPLY Receipt | `NOT_RUN` | 独立确认材料进入指定模型会话，而非 HTTP ACK | 无独立 APPLY 原件 |
| Response Loss | `NOT_RUN` | 响应丢失后按 request/session 查询原结果，不盲发 | 无查询原件 |
| Provider Restore | `NOT_RUN` | 恢复不能绕过当前授权、Generation 与撤销状态 | 无恢复/隔离测试目标 |

## 4. Probe 行为和命令

默认只做一次 `GET /v1/models` discovery，不发送模型内容：

```bash
cd backend
GOCACHE=/tmp/magi-m1-03a-go-cache \
  go run ./cmd/m1-provider-capability \
    -provider-id anthropic-compatible-local-gateway \
    -output ../docs/evidence/m1-03a/provider-capability-report.json
```

配置来源：

- `-base-url`，否则读取 `ANTHROPIC_BASE_URL`；
- token 只从 `ANTHROPIC_AUTH_TOKEN` 环境变量读取，不接受命令行参数，避免进入
  进程列表；不写入报告或日志；
- `-model` 必须显式给出且出现在 discovery 列表中，不能按列表顺序自动选目标；
- `-strict` 在非 `CAPABILITY_VERIFIED` 时退出 2，但仍先输出报告；
- `-output` 以 `0600` 写入机器可读报告。

如果之后有真实模型目标，本 discovery 工具仍只证明“目标存在”，六项能力依旧是
`NOT_RUN`。本切片没有实现隔离 smoke 请求；普通 chat 响应即使带 response ID 和
request ID，也只能作为 `UNVERIFIABLE` 材料，不能作为 session/APPLY/fence 证据。

## 5. jadm/drcp 与 `ajc_` Admission 身份边界

- M1-00 的 `jadm`/`drcp` 是证据槽位名，不是新的 C0 artifact 类型或权威权限。
- M1-02A 的 `ajc_` 是 `JOURNAL_ADMIT_V2` 的持久 commit ID，由实际 request digest
  派生；它证明的是 Admission 提交记录，不是 Provider 已应用。
- 本切片不创建 `ajc_`、`jadm` 或 `drcp`。后续映射必须绑定真实 admission
  commit、选定 Provider/model/session/request/generation 和独立 APPLY 原件；不得
  把这三种标识互相当作回执，也不扩大为 C0 工件注册框架。
- 在相应映射通过独立审查前，`drcp_` 和 `APPLIED_VERIFIED` 必须保持不可产生。

## 6. A–F 证据

### A. 基线与范围

- `main=4d969869410e64ffb3816e3d72475e0987e78e23`；独立 Worktree
  `/tmp/magi-m1-03a-provider-capability`。
- 仅实现能力分类/报告和只读 discovery；没有 Provider 网络暴露写入、MySQL、
  Authority、Journal、Exposure Gate 或生产配置变更。
- 未改 `/tmp/magi-m1-02b1-exposure-gate`。

### B. 需求映射

| Issue #44 / 本轮约束 | 实现证据 |
| --- | --- |
| 六项能力分别标记 | `backend/domain/providercapability/providercapability.go` 的固定能力顺序与 `CapabilityResult` |
| 绑定 provider/session/request/generation/UTC/原件/查询/复现 | `Evidence`、`OriginalEvidence` 与失败关闭校验 |
| 无目标为 `NOT_RUN`，不是 `UNSUPPORTED` | `TestNoRealTargetBlocksAllCapabilitiesWithoutProducingEvidence` |
| 200/空列表不能升级 | `TestRunCLIEmptyModelsProducesBlockedReportWithoutToken` |
| ACK/chat 响应不能升级 | `TestHTTP200AndModelResponseCannotPromoteAnyCapability`、`TestRunCLINoSmokeNeverCallsMessagesWhenTargetDiscovered` |
| 缺 session/query/fence/revocation 等不能升级 | `TestMalformedOrUnboundEvidenceFailsClosed` |
| 无独立查询的 PASS 不能验证 | `TestClaimedPassWithoutIndependentVerificationCannotVerify` |
| 独立查询缺能力专属原件不能验证 | `TestCapabilitySpecificFactsAreRequiredForVerification` |
| 四种状态保持互斥 | `TestAllFourStatusesRemainDistinct` |
| 空目标/坏原件不能升级为 UNSUPPORTED | `TestUnsupportedEvidenceCannotUseEmptyModelsOrBadDigest` |
| 同键同证据幂等、异内容冲突 | `TestSameEvidenceIsIdempotentAndChangedEvidenceConflicts` |
| 不能伪造 `APPLY`/`drcp_`/`APPLIED_VERIFIED` | 报告固定安全标志为 false；代码无相关写入口 |
| 明确选择模型、避免顺序漂移 | `SelectedModelID` 必须来自 discovery 列表；`TestEvidenceForDifferentModelDoesNotVerifySelectedTarget` |

“缺 fence/撤销水位”的两项没有独立 Provider API 目标，不能构造真实 `UNVERIFIABLE`
证据；它们在该 Provider 上保持 `NOT_RUN`，这是严格语义。`UNVERIFIABLE` 只用于已有
目标或材料但原件不充分的情况。

### C. 实际 RED → GREEN

按 TDD 实际观察：

1. 新增 domain 测试后运行：
   ```text
   no non-test Go files in .../domain/providercapability
   FAIL [build failed]
   ```
   原因是生产包尚不存在。随后第一版实现又实际观察到：
   ```text
   invalid operation: err != nil (mismatched types string and untyped nil)
   cannot use err (variable of interface type error) as string value in return statement
   ```
   证明证据校验返回值尚未形成稳定契约；修正为显式失败原因后继续推进。
2. 加入初版实现后，验证测试失败：
   ```text
   session identity status = "UNVERIFIABLE", want "VERIFIED"  （历史 RED，后续安全收紧为自报 PASS 仍 UNVERIFIABLE）
   ```
   根因是证据没有绑定 model ID；补上 model 身份后 focused test 通过。
3. 收紧“显式模型选择、显式 PASS、原件齐全”后，测试再次失败：
   ```text
   status = "NOT_RUN", want "UNVERIFIABLE"
   ```
   证明未选择模型时不能拿任意证据验证；补齐显式选择与证据 outcome 后 GREEN。
4. CLI 测试在缺少 token 环境变量隔离时失败：
   ```text
   error = <nil>, want missing auth token
   ```
   测试显式清空环境变量后 GREEN；这验证了配置来源行为，不是放宽校验。
5. 最终 focused GREEN：
   ```text
   GOCACHE=/tmp/magi-m1-03a-go-cache \
     go test ./domain/providercapability ./cmd/m1-provider-capability -count=1 -v
   PASS
   ```

### D. 命令与真实结果

```bash
cd backend
GOCACHE=/tmp/magi-m1-03a-go-cache \
  go test ./domain/providercapability ./cmd/m1-provider-capability -count=1 -v
# PASS

GOCACHE=/tmp/magi-m1-03a-go-cache \
  go run ./cmd/m1-provider-capability \
    -provider-id anthropic-compatible-local-gateway \
    -output ../docs/evidence/m1-03a/provider-capability-report.json
# exit 0

python3 -m json.tool ../docs/evidence/m1-03a/provider-capability-report.json
# valid JSON; verdict=BLOCKED; m1_gate=NOT_RUN;
# all six capability status=NOT_RUN; remote_model_calls_attempted=0
```

实测 `/v1/models` 原始摘要：

```text
HTTP=200
content-type=application/json
content-length=28
body={"data":[],"has_more":false}
SHA-256=fd71b4438dea68046585a59a5e30b570dcf2dc0bf70fbaff48cbd9049f377504
UTC=2026-10-29T00:23:32Z
```

原始 header/body 仅在本次实现会话的临时文件
`/tmp/m1-03a-models.headers`、`/tmp/m1-03a-models.body`；它们是复现证据，不是
运行时依赖。未将 token、Authorization 或完整原始响应写入仓库。

### E. 未运行能力与阻塞边界

- 真实 Secret、生产权限、生产数据、受保护外发：未使用。
- Provider `/v1/messages`、真实材料 APPLY、session 创建/恢复、fence 撤销竞争、
  响应丢失查询、APPLY 原件核验：未运行。
- Exposure Gate、Settlement、Witness、Settlement→ReadSetJournal、三方
  Evidence Closure：不属于本切片，未运行。
- exact-head CI / Draft PR：**未执行**，本任务禁止 push/PR。

### F. 已知限制与后续门

- 这是可执行的证据判定骨架和只读 reconnaissance，不是 Provider Adapter。
- 分类 API 现在具备 `VERIFIED` 的失败关闭入口，但当前真实 Provider 报告仍没有任何
  `VERIFIED` 能力；测试中的 verifier/PASS 不构成真实 Provider 证据。
- 真实原件签名、Provider 独立性和测试真实性仍需独立复审；普通单元测试通过不会
  升级为生产能力。
- 当前空模型列表不能证明底层 Provider 能力缺失，也不能证明其可用；状态必须是
  `NOT_RUN/BLOCKED`。
- 要解除阻塞，必须提供隔离测试目标及可独立查询的 Provider 原件，逐项完成六项测试；
  在此之前不实现 Settlement、三人格闭环或生产暴露。
