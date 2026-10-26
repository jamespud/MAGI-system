# M1-00：ROLLOUT-LAB-E2E-01 证据入口

> 对应 Issue [#54](https://github.com/jamespud/MAGI-system/issues/54)。
>
> **M1-00 是测试基础设施，不是 M1 验收器。它不能授予 `PASS_M1`。**
> 独立 `M1-GATE` 必须另行读取持久化原件、Provider 证据、Journal、Candidate、Ballot 和 DecisionRecord，并重新计算关键身份和状态。

## 1. 入口

```bash
cd backend
GOCACHE=/tmp/magi-m1-go-cache go run ./cmd/m1-e2e -mode real
GOCACHE=/tmp/magi-m1-go-cache go run ./cmd/m1-e2e -mode synthetic
GOCACHE=/tmp/magi-m1-go-cache go run ./cmd/m1-e2e -mode real -strict
```

Makefile 等价入口：

```bash
cd backend
make m1-e2e
M1_MODE=synthetic make m1-e2e
```

输出为稳定 JSON，包含：

- 固定场景 `ROLLOUT-LAB-E2E-01`；
- `REAL` / `SYNTHETIC` 模式；
- 每个阶段的 `NOT_RUN`、`BLOCKED`、`DIAGNOSTIC_ONLY`、`FAIL` 或 `EVIDENCE_READY`；
- 稳定 blocker code；
- 精确 artifact slot 和实际 artifact ref（若阶段执行器提供）；
- 负例定义；
- 确定性 trace；
- `m1_verdict=NOT_EVALUATED`。

`-strict` 只在所有 M1-00 证据为 `EVIDENCE_READY` 时成功；当前默认能力全部缺失，因此严格模式会以退出码 2 失败，并先输出完整证据 JSON。

## 2. 固定输入

fixture 位于：

```text
backend/internal/m1e2e/testdata/rollout_lab_e2e_01.json
```

固定内容包括：

- Requester 的 ≥10% P95 延迟目标、停止权、恢复可验证和禁止扩权约束；
- Melchior / Balthasar / Casper 的 v1.0 PersonaSpec、最低调查和 6 次 / 6,000 token 预算；
- C0–C3 四个候选，其中 C0 是唯一 no-action，C2/C3 只能作为分析候选，R0 不得执行受保护写入；
- ROLLOUT-LAB 受控材料描述与有效期；
- 三份 Journal admission、三份 delivery receipt、Candidate、三份 ReadSet、三份确认、三份 SealedBallot、BallotSet、DecisionRecord 和 TrustedReport 的精确 artifact slot；
- 六个必需负例入口：缺第三回执、材料过期、上下文撤销、重复票据、见证不可用、Provider 结果未知；
- 两个补充控制负例：旧 Generation 重放、R0 受保护发送尝试。输出通过 `requirement` 明确区分 `REQUIRED` 与 `SUPPLEMENTAL`，补充控制不能替代六个必需项。
- 固定目标/C0-C3 语义、22 个 artifact slot 的类型/前缀/Persona/阶段以及每个阶段要求的 artifact 清单均独立冻结；同时删除 slot 与阶段要求会被拒绝。

## 3. 模式隔离

### `REAL`

- 不把任何 synthetic evidence 当作真实证据；
- 缺失能力返回 `BLOCKED`；
- synthetic evidence 返回 `FAIL: SYNTHETIC_EVIDENCE_IN_REAL_MODE`；
- non-real evidence 返回 `FAIL: NON_REAL_EVIDENCE_IN_REAL_MODE`；
- 不允许 stage executor 返回 `PASS_M1`；否则 runner 返回 `ErrPassM1Forbidden`。

### `SYNTHETIC`

- 可用于检查场景结构、状态机和失败路径；
- 所有 synthetic 执行最多为 `DIAGNOSTIC_ONLY`；
- 永远不产生 `PASS_M1`；
- 不能替代真实 Provider、Journal、authority 或独立验收。

## 4. 当前阻塞

当前 runner 在 `REAL` 模式显示：

| 阶段 | 当前 blocker |
| --- | --- |
| `CHARTER_CONTEXT` | `CAPABILITY_UNAVAILABLE:charter_context` |
| `JOURNAL_ADMISSION` | `CAPABILITY_UNAVAILABLE:journal_admission_v2` |
| `PERSONA_INVESTIGATION` | `CAPABILITY_UNAVAILABLE:persona_runtime` |
| `EVIDENCE_CLOSURE` | `CAPABILITY_UNAVAILABLE:evidence_closure` |
| `SEALED_BALLOTS` | `CAPABILITY_UNAVAILABLE:sealed_ballots` |
| `DECISION_RECORD` | `CAPABILITY_UNAVAILABLE:decision_record` |
| `TRUSTED_REPORT` | `CAPABILITY_UNAVAILABLE:trusted_report` |
| `R0_EGRESS_CHECK` | `CAPABILITY_UNAVAILABLE:r0_egress_probe` |

`M1_GATE` 始终是 `NOT_RUN`，并标注为独立验收边界。

这些 blocker 是设计上的真实缺口，不是测试失败，也不是 M1 通过。

## 5. 后续接入

后续 PR 只需注册对应 `StageExecutor`，并确保：

- `REAL` 模式提供 `EvidenceReal`；
- 每个 required artifact slot 都有精确 ID、digest 和 source；
- artifact ID 必须匹配该 slot 的 `id_prefix + "_" + 64 lowercase hex` 结构；M1-00 只检查结构，不替代 M1-GATE 的原件真实性验证；
- 失败路径返回明确 `FAIL` 或 `BLOCKED`；
- 不修改 runner 的 `PASS_M1` 禁止规则；
- 不由 M1-00 自己判定 M1 通过。

## 6. 非目标

- 不实现 Persona、Journal、authority、Provider、Evidence Closure、ballot、decision、report 或 Egress；
- 不连接真实 Provider；
- 不激活生产能力；
- 不创建 `PASS_M1`；
- 不替代独立 M1-GATE。

## 7. A-F 证据

### A. 基线与范围

- 起点 main：`cbfe7668346cc2af1c7435601f19fd77915dd797`（2026-10-25 CI 6/6 success）。
- 分支：`codex/m1-00-e2e-skeleton`。
- 本切片只修改 `backend/internal/m1e2e/`、`backend/cmd/m1-e2e/`、`backend/Makefile` 和本文件；没有修改生产 authority、Journal、Provider、Egress 或 M1-GATE。

### B. 需求映射

| Issue #54 要求 | 实现证据 |
| --- | --- |
| 固定三个 Persona、C0-C3、R0 目标和 artifact/负例槽位 | `scenario.go` 的冻结目标、C0-C3、22 个 artifact slot、每阶段 artifact 清单与 6+2 负例分类 |
| REAL / SYNTHETIC 隔离 | `runner.go` 的 `classifyEvidence` 与 `runner_test.go` 正反测试 |
| 禁止 `PASS_M1` | `ErrPassM1Forbidden` 与 executor 负例 |
| 每阶段 `BLOCKED` / `NOT_RUN` / `FAIL` / `EVIDENCE_READY` | `RunResult`、稳定 blocker 和 CLI 测试 |
| 确定性 JSON 与可用 CLI | 重复 CLI 输出逐字节比较、`TestRunnerResultIsDeterministicAndKeepsArtifactSlots` |

### C. Review RED → GREEN

在修复前提交 `3e7328b` 上，用同一语义探针观察到：

```text
SEMANTIC RED: required delivery_receipt_casper can be removed from both inventories
SEMANTIC RED: frozen goal and C2 semantics can be changed
SEMANTIC RED: malformed artifact ID was accepted with status EVIDENCE_READY
```

修复后：

```text
cd backend
GOCACHE=/tmp/magi-m1-go-cache go test ./cmd/m1-e2e ./internal/m1e2e -count=1 -v
PASS

GOCACHE=/tmp/magi-m1-go-cache go test -race ./cmd/m1-e2e ./internal/m1e2e -count=1
PASS
```

### D. 全量回归与静态检查

```text
GOCACHE=/tmp/magi-m1-go-cache go test ./... -count=1
PASS

GOCACHE=/tmp/magi-m1-go-cache go vet ./...
PASS（无输出）

gofmt -l backend/internal/m1e2e backend/cmd/m1-e2e
无输出

git diff --check
无输出
```

### E. 真实/合成隔离观测

```text
go run ./cmd/m1-e2e -mode real
status=BLOCKED m1_verdict=NOT_EVALUATED blockers=CAPABILITY_UNAVAILABLE:*

go run ./cmd/m1-e2e -mode synthetic
status=BLOCKED m1_verdict=NOT_EVALUATED
```

默认 CLI 没有注入 capability，因此 REAL 模式只显示当前缺口；测试中的 ready executor 仅证明 runner 状态机，不证明真实业务能力。两次 REAL CLI 输出逐字节一致。

### F. 未验证 / 仍阻塞

- 未连接真实 Provider、C2、Journal 或 Egress；没有 MySQL 依赖。
- 没有执行独立 M1-GATE，未生成 `PASS_M1`。
- `M1_GATE` 始终为 `NOT_RUN`，这是独立验收边界。
- artifact digest/source 的真实性与外部原件核验仍由 M1-GATE 负责；M1-00 只校验结构和槽位。
