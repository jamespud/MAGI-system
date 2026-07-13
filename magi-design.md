一份可以作为最终目标架构的 **完整 MAGI 系统设计**：把三智能体、Commander、手写 Agent Loop、Evidence Ledger、Claim Graph、投票、共识、辩论、反思、Memory/RAG、Tracing、Evaluation，以及 Coze Studio 的复用边界全部一次性定义清楚。

下面给出我建议冻结的**完整目标架构**。实现时仍然可以分阶段，但架构本身不再按阶段拆分。

---

# 一、系统定位

MAGI 不是「三个不同 Prompt 的 Coze Bot」，也不是「三个 LLM 节点组成的 Workflow」。

它应该被定义为：

> **一个构建于 Coze Studio 基础设施之上的、证据驱动的多智能体决策系统。三个具有不同目标函数、证据标准、风险倾向和工具权限的独立 Agent，通过调查、主张、投票、辩论、反思与复议形成最终决议，并保存完整、可审计、可回放的决策链。**

完整逻辑模型：

```text
User
  │
  ▼
Decision Case
  │
  ▼
Commander ───────► Context Builder ◄────── Memory / Historical Cases
  │                       │
  │                       ▼
  │               Decision Task Package
  │                       │
  ▼                       ▼
Workflow Orchestrator / Deterministic FSM
  │
  ├──────────────┬────────────────┬────────────────┐
  ▼              ▼                ▼                │
MELCHIOR       BALTHASAR         CASPER            │
科学/逻辑       安全/保护          机会/直觉          │
  │              │                │                │
  ▼              ▼                ▼                │
Agent Loop     Agent Loop       Agent Loop          │
  │              │                │                │
  ▼              ▼                ▼                │
Tool Runtime   Tool Runtime     Tool Runtime         │
  │              │                │                │
  └──────────────┴────────────────┘                │
                  │                                │
                  ▼                                │
        Evidence Ledger + Claim Graph              │
                  │                                │
                  ▼                                │
             Evidence Gate                         │
                  │                                │
                  ▼                                │
          Independent Voting                       │
                  │                                │
                  ▼                                │
            Consensus Engine                       │
                  │                                │
        ┌─────────┴──────────┐                     │
        ▼                    ▼                     │
    Consensus             Conflict                 │
        │                    │                     │
        │                    ▼                     │
        │              Debate Engine               │
        │                    │                     │
        │                    ▼                     │
        │            Reflection / Reconsider       │
        │                    │                     │
        │                    ▼                     │
        │                 Re-vote ─────────────────┘
        │
        ▼
Final Resolution
        │
        ├────► Decision Report
        ├────► Trace / Audit
        ├────► Evaluation
        └────► Decision Memory / RAG Projection
```

---

# 二、四层完整架构

整个系统明确分成四层。

| 层                                | 职责                                        | 策略 |
| -------------------------------- | ----------------------------------------- | -- |
| MAGI Application Layer           | 用户交互、案件管理、决策展示、实时状态                       | 新建 |
| MAGI Orchestration Layer         | 多 Agent 编排、状态机、投票、辩论、共识                   | 新建 |
| MAGI Agent Runtime Layer         | Agent Loop、Tool Calling、证据、Claim、结构校验     | 新建 |
| Coze Studio Infrastructure Layer | LLM、Plugin、Knowledge、Workflow Tool、DB、消息等 | 复用 |

关键依赖方向必须保持：

```text
Application
     ↓
Orchestration
     ↓
Agent Runtime
     ↓
Port / Adapter
     ↓
Coze Infrastructure
```

禁止反向依赖：

```text
Coze Plugin ─────X─────► Magi Agent
Coze Knowledge ──X─────► Consensus Engine
Agent Runtime ───X─────► Application UI
```

MAGI 应通过 Adapter 使用 Coze，而不是把 Coze 的领域模型传播到 MAGI 核心。

---

# 三、核心领域模型

我建议完整系统至少拥有以下 14 个一等领域实体：

```text
DecisionCase
MagiConfig
DecisionTask
AgentRun
AgentState
ToolCallRecord
EvidenceRecord
Claim
EvidenceSummary
Vote
DebateRound
Reflection
Resolution
CaseMemoryProjection
```

它们之间的关系：

```text
DecisionCase
  │
  ├── 1:N ──► AgentRun
  │               │
  │               ├── N:1 ──► MagiConfig
  │               │
  │               ├── 1:N ──► ToolCallRecord
  │               │                 │
  │               │                 └── 1:N ──► EvidenceRecord
  │               │
  │               ├── 1:N ──► Claim
  │               │
  │               ├── 1:N ──► EvidenceSummary
  │               │
  │               └── 1:N ──► Vote
  │
  ├── 1:N ──► DebateRound
  │               │
  │               └── 1:N ──► Reflection
  │
  ├── 1:1 ──► Resolution
  │
  └── 1:1 ──► CaseMemoryProjection
```

---

# 四、Decision Case：整个系统的聚合根

所有运行围绕一个 `DecisionCase`：

```go
type DecisionCase struct {
    ID          string
    UserID      int64

    Question    string
    Context     string
    Constraints []Constraint

    Status      CaseStatus
    CurrentPhase CasePhase

    MaxDebateRounds int
    Deadline        *time.Time

    CreatedAt time.Time
    UpdatedAt time.Time
}
```

状态：

```text
DRAFT
  ↓
NORMALIZING
  ↓
CONTEXT_BUILDING
  ↓
INVESTIGATING
  ↓
VOTING
  ↓
CONSENSUS_CHECK
  │
  ├──► RESOLVING
  │
  └──► DEBATING
           ↓
       REFLECTING
           ↓
        REVOTING
           ↓
      CONSENSUS_CHECK
  ↓
RESOLVED
  ↓
MEMORY_INDEXED
```

异常终态：

```text
FAILED
CANCELLED
TIMED_OUT
INSUFFICIENT_EVIDENCE
DEADLOCKED
```

---

# 五、MagiConfig：人格不是 Prompt，而是执行策略

完整配置：

```go
type MagiConfig struct {
    ID      string
    Code    MagiCode
    Name    string

    Persona          PersonaDefinition
    ObjectiveFunction ObjectiveFunction
    RiskPolicy       RiskPolicy

    EvidenceStandard EvidenceStandard

    ModelRef    ModelRef
    ToolBindings []ToolBinding

    LoopPolicy       LoopPolicy
    ReflectionPolicy ReflectionPolicy

    Version int64
}
```

三个 Agent：

### Melchior

```text
Objective:
- Correctness
- Empirical validity
- Efficiency
- Technical feasibility

Evidence preference:
- Quantitative data
- Benchmark
- Experiment
- Code execution
- Technical documentation

Risk tendency:
- Evidence-calibrated
```

### Balthasar

```text
Objective:
- Safety
- Stability
- Reversibility
- Maintainability
- Ethical impact

Evidence preference:
- Failure history
- Risk analysis
- Compliance
- Worst-case scenario
- Operational evidence

Risk tendency:
- Conservative
```

### Casper

```text
Objective:
- Opportunity
- Innovation
- User experience
- Strategic upside
- Timing advantage

Evidence preference:
- Emerging trends
- User signals
- Competitive intelligence
- Market changes
- Opportunity cost

Risk tendency:
- Aggressive
```

因此真正的差异来自五个维度：

```text
Persona
+ Objective Function
+ Evidence Standard
+ Risk Policy
+ Tool Permission
```

而不是只依赖 System Prompt。

---

# 六、Commander 的职责边界

Commander 是 LLM，但不能成为上帝 Agent。

它只负责四项语言任务：

```text
1. Normalize Decision
2. Build Decision Task
3. Identify Decision Dimensions
4. Generate Final Human-readable Report
```

Commander **不负责**：

```text
× 数票
× 判断共识
× 修改 Agent 投票
× 绕过 Evidence Gate
× 修改 Evidence Reliability
× 决定状态转移
× 判断 Agent 是否拥有工具权限
```

这些全部属于 deterministic code。

输入：

```json
{
  "question": "是否应该把 Java 后端重构成 Rust？",
  "context": "...",
  "constraints": ["团队只有两名 Rust 工程师"]
}
```

输出 `DecisionTask`：

```go
type DecisionTask struct {
    CanonicalQuestion string

    DecisionType DecisionType

    Background string

    Constraints []Constraint

    Dimensions []DecisionDimension

    InformationNeeds []InformationNeed

    SuccessCriteria []Criterion

    Unknowns []string
}
```

---

# 七、Context Builder 与 Memory

Context Builder 统一构造 Agent 开始工作前的上下文：

```text
Decision Case
    +
Commander Normalized Task
    +
User Constraints
    +
Historical Similar Cases
    +
Relevant Knowledge
    +
Current Debate Context（复议轮才有）
    ↓
AgentContext
```

结构：

```go
type AgentContext struct {
    CaseID string

    Task DecisionTask

    Constraints []Constraint

    HistoricalCases []HistoricalCase

    KnowledgeContext []KnowledgeChunk

    DebateContext *DebateContext

    PreviousRun *PreviousAgentState
}
```

Memory 分三层。

### Working Memory

当前 Agent Loop：

```text
[]schema.Message
Tool Results
Current Evidence IDs
Current Claims
Step Count
Token Budget
```

### Case Memory

当前案件：

```text
Evidence Ledger
Claim Graph
Votes
Debates
Reflections
Resolution
```

存 PostgreSQL，属于 Canonical Source of Truth。

### Long-Term Decision Memory

案件结束后生成：

```go
type CaseMemoryProjection struct {
    CaseID string

    QuestionSummary string
    ContextSummary string

    KeyEvidence []MemoryEvidence
    KeyClaims   []MemoryClaim

    Votes      []MemoryVote
    Resolution string
    Outcome    *CaseOutcome
}
```

然后：

```text
CaseMemoryProjection
        ↓
crossknowledge.Store()
        ↓
Embedding / Index
        ↓
未来案件 Retrieve()
```

因此：

> PostgreSQL 回答「实际发生了什么」，RAG 回答「以前是否遇到过类似问题」。

---

# 八、Agent Runtime

三个 MAGI 共用同一个 Runtime：

```go
type MagiRuntime interface {
    Run(
        ctx context.Context,
        config MagiConfig,
        context AgentContext,
    ) (*AgentResult, error)

    Reconsider(
        ctx context.Context,
        config MagiConfig,
        previous AgentResult,
        debate DebateContext,
    ) (*AgentResult, error)
}
```

内部完整循环：

```text
START
  ↓
Build System Prompt
  ↓
Build Context
  ↓
Call LLM
  ↓
Parse Response
  │
  ├── TOOL_CALL
  │       ↓
  │   Permission Check
  │       ↓
  │   Schema Validation
  │       ↓
  │   Execute Tool
  │       ↓
  │   Record ToolCall
  │       ↓
  │   Evidence Adapter
  │       ↓
  │   Evidence Ledger
  │       ↓
  │   Append Tool Result
  │       └────────────────► Call LLM
  │
  ├── CLAIM_SUBMISSION
  │       ↓
  │   Validate Claim
  │       ↓
  │   Bind Evidence IDs
  │       ↓
  │   Update Claim Graph
  │       └────────────────► Call LLM
  │
  ├── READY_TO_VOTE
  │       ↓
  │   Generate Evidence Summary
  │       ↓
  │   Evidence Gate
  │       │
  │       ├── FAIL
  │       │     ↓
  │       │ Gate Feedback
  │       │     └──────────► Call LLM
  │       │
  │       └── PASS
  │             ↓
  │       Structured Vote
  │             ↓
  │       Vote Validation
  │             ↓
  │            END
  │
  └── INVALID
          ↓
     Structured Error Feedback
          └────────────────────► Call LLM
```

终止条件必须由 Runtime 强制：

```text
Valid Vote
Max Steps
Timeout
Context Cancellation
Token Budget Exceeded
Repeated Validation Failure
Repeated Tool Failure
Evidence Gate Failure Limit
```

---

# 九、统一 Validation Architecture

完整系统统一使用：

> **JSON Schema / OpenAPI Schema = Runtime Validation IR**

架构：

```text
                    Runtime Validator
                           ▲
                           │
             JSON Schema / OpenAPI Schema
                           ▲
               ┌───────────┴───────────┐
               │                       │
        Coze Plugin Tool          Local Go Tool
               │                       │
        Existing OpenAPI          Go Struct T
               │                       │
               │                Schema Generator
               │                       │
               └───────────┬───────────┘
                           ▼
                     Validate JSON
                           ↓
                     Typed Unmarshal
                           ↓
                        Execute
```

它不仅验证 Tool Args，还验证所有 LLM 结构化产物：

```text
DecisionTask
EvidenceSummary
ClaimSubmission
Vote
Reflection
FinalReportData
```

所以建议定义统一接口：

```go
type SchemaValidator interface {
    Validate(
        ctx context.Context,
        schema SchemaDefinition,
        payload []byte,
    ) error
}
```

---

# 十、Tool Runtime 与 Coze 复用

MAGI 不直接依赖具体 Coze 实现，而通过 Port：

```go
type ToolRegistry interface {
    ListTools(
        ctx context.Context,
        bindings []ToolBinding,
    ) ([]AgentTool, error)
}

type ToolExecutor interface {
    Execute(
        ctx context.Context,
        request ToolExecutionRequest,
    ) (*ToolExecutionResult, error)
}
```

Adapter：

```text
Magi Tool Port
      │
      ├── CozePluginAdapter
      │       ↓
      │ crossplugin.DefaultSVC()
      │       ↓
      │ ExecuteTool()
      │
      ├── LocalToolAdapter
      │       ↓
      │ Eino InvokableTool
      │
      ├── KnowledgeToolAdapter
      │       ↓
      │ crossknowledge.Retrieve()
      │
      ├── WorkflowToolAdapter
      │       ↓
      │ Existing Coze Workflow
      │
      └── CodeRunnerAdapter
              ↓
          Coze Sandbox
```

每个 Magi 只看到自己被授权的 Tool Definition。

---

# 十一、Evidence Ledger 完整设计

这里建议采用三层模型：

```text
Tool Result
    ↓
EvidenceRecord
    ↓ supports
Claim
    ↓
Reasoning / Vote
```

### EvidenceRecord

表示**观察到的证据**：

```go
type EvidenceRecord struct {
    ID string

    CaseID     string
    AgentRunID string
    ToolCallID string

    SourceType EvidenceSourceType
    SourceURI  *string

    RawContent string
    Observation string

    Reliability ReliabilityScore

    CollectedBy MagiCode

    CreatedAt time.Time
}
```

### Claim

表示 Agent 对证据提出的命题：

```go
type Claim struct {
    ID string

    CaseID     string
    AgentRunID string

    Statement string

    Supports []string

    Contradicts []string

    Status ClaimStatus
}
```

### Claim Graph

```text
EV-001 ──supports────► CL-001
EV-002 ──supports────► CL-001

EV-003 ──supports────► CL-002

CL-001 ◄──contradicts──► CL-002
```

这样辩论的对象不是整篇自然语言，而是：

```text
Claim
Evidence
Counter-Claim
Counter-Evidence
```

这会成为整个 MAGI 系统最有价值的数据结构之一。

---

# 十二、Evidence Adapter 与 Reliability

每次工具返回：

```text
ToolExecutionResult
       ↓
Evidence Adapter Registry
       ↓
Structured Evidence Candidate
       ↓
Evidence Validator
       ↓
Evidence Ledger
```

接口：

```go
type EvidenceAdapter interface {
    Supports(tool ToolDefinition) bool

    Extract(
        ctx context.Context,
        result ToolExecutionResult,
    ) ([]EvidenceCandidate, error)
}
```

优先级：

```text
1. Native Structured Evidence Adapter
2. Tool-specific Parser
3. Generic LLM Extractor
4. Raw Observation Fallback
```

即：

> 能确定性解析绝不使用 LLM；实在无法解析才使用 LLM Extraction。

Reliability 不建议直接让 LLM 输出一个 `0.82`。

采用：

```text
Base Source Reliability
       +
Directness Modifier
       +
Recency Modifier
       +
Corroboration Modifier
       +
Extraction Confidence
       ↓
Final Reliability
```

例如：

```go
type ReliabilityScore struct {
    Base          float64
    Directness    float64
    Recency       float64
    Corroboration float64
    Extraction    float64

    Final float64
}
```

注意：这些分数是**决策启发式**，不应该被宣传为客观真理。

---

# 十三、Evidence Gate

每个 MAGI 有自己的 `EvidenceStandard`：

```go
type EvidenceStandard struct {
    MinEvidenceCount int

    RequiredTypes []EvidenceTypeRequirement

    MinReliability float64

    RequireOwnCollected bool

    RequiredClaimCount int

    CustomRules []EvidenceRule
}
```

例如 Melchior：

```text
至少 3 个 Evidence
至少 1 个 Quantitative Evidence
至少 1 个 Primary/Technical Source
至少 2 个 Claim
每个核心 Utility Dimension 至少有 1 个 EV-ID 支持
```

Balthasar：

```text
必须存在 Worst-case Claim
必须存在 Reversibility Assessment
至少一个 Operational/Risk Evidence
```

Casper：

```text
必须存在 Opportunity Cost Claim
必须存在 Time-window Assessment
至少一个 Trend/User/Market Evidence
```

Gate 完全由确定性代码执行。

---

# 十四、Vote 完整模型

```go
type Vote struct {
    ID string

    CaseID     string
    AgentRunID string
    Round      int

    Decision VoteDecision
    Confidence float64

    UtilityScores []UtilityDimensionScore

    KeyClaimIDs []string
    EvidenceIDs []string

    ReasoningSummary string

    Conditions []DecisionCondition

    CreatedAt time.Time
}
```

其中：

```go
type UtilityDimensionScore struct {
    DimensionCode string

    Score float64

    EvidenceIDs []string
    ClaimIDs    []string

    Reasoning string
}
```

三个 MAGI 使用不同的维度：

```text
Melchior:
- correctness
- efficiency
- feasibility

Balthasar:
- safety
- reversibility
- maintainability

Casper:
- opportunity
- innovation
- user_value
```

允许：

```text
APPROVE
REJECT
ABSTAIN
CONDITIONAL_APPROVE
```

我建议保留 `CONDITIONAL_APPROVE`，因为复杂决策中二元 approve/reject 太粗糙。

---

# 十五、Consensus Engine

Consensus Engine 必须是纯 Go deterministic domain service。

```go
type ConsensusEngine interface {
    Evaluate(
        ctx context.Context,
        votes []Vote,
        policy ConsensusPolicy,
    ) ConsensusResult
}
```

规则示例：

```text
3 APPROVE
→ STRONG_APPROVAL

3 REJECT
→ STRONG_REJECTION

2 APPROVE + 1 REJECT
→ MAJORITY_APPROVAL_WITH_DISSENT

2 REJECT + 1 APPROVE
→ MAJORITY_REJECTION_WITH_DISSENT

存在 ABSTAIN
→ 根据 quorum policy 判断

3 个不同结果
→ DEADLOCK

CONDITIONAL_APPROVE
→ Evaluate Conditions
```

但有一点非常重要：

> **2:1 不应该直接结束。**

如果目标是复刻 MAGI 的价值冲突，那么首次 2:1 应进入 Debate/Reflection。只有复议后的 2:1 才可以根据策略结束。

---

# 十六、Debate Engine

Debate 不是让三个 Agent 无限聊天。

应该是结构化辩论协议：

```text
Round N
   ↓
Find Disagreement
   ↓
Identify Conflicting Claims
   ↓
Build Debate Packet
   ↓
Send to Relevant Agents
   ↓
Each Agent:
   - Concede
   - Rebut
   - Request More Evidence
   - Maintain Position
   ↓
Optional Tool Investigation
   ↓
Reflection
   ↓
Re-vote
```

`DebatePacket`：

```go
type DebatePacket struct {
    Round int

    MajorityVotes []Vote
    MinorityVotes []Vote

    ConflictingClaims []ClaimConflict

    SharedEvidence []EvidenceRecord

    Questions []DebateQuestion
}
```

不要强制只让少数派反思。

**多数派也可能错。**

因此三个 Agent 都应该收到 Debate Packet，只是：

```text
Minority:
解释为什么坚持或改变

Majority:
回应少数派最强反驳

All:
允许调用工具获取新证据
```

---

# 十七、Reflection

Reflection 输出：

```go
type Reflection struct {
    AgentRunID string
    Round      int

    PreviousVoteID string

    PositionChange PositionChange

    AcceptedClaims []string
    RejectedClaims []string

    NewEvidenceIDs []string

    Reasoning string

    ReadyToRevote bool
}
```

`PositionChange`：

```text
MAINTAIN
STRENGTHEN
WEAKEN
CHANGE
ABSTAIN
```

关键规则：

> 改票不能只写「看了多数派意见后我改变了想法」。

必须满足至少一个：

```text
引用新的 EV-ID
接受一个原先拒绝的 Claim
指出自己原先某个 Claim 被反证
重新评估某个 Utility Dimension
```

否则 Reflection 无效。

---

# 十八、Workflow Orchestrator 完整状态机

顶层状态机：

```text
CASE_CREATED
      ↓
NORMALIZING
      ↓
BUILDING_CONTEXT
      ↓
RETRIEVING_MEMORY
      ↓
DISPATCHING
      ↓
INVESTIGATING
      ↓
EVIDENCE_GATING
      ↓
COLLECTING_VOTES
      ↓
CHECKING_CONSENSUS
      │
      ├──── strong consensus ────► RESOLVING
      │
      └──── conflict ────────────► BUILDING_DEBATE
                                          ↓
                                      DEBATING
                                          ↓
                                      REFLECTING
                                          ↓
                                      REVOTING
                                          ↓
                                  CHECKING_CONSENSUS
                                          │
                              ┌───────────┴───────────┐
                              ▼                       ▼
                          RESOLVING               next round
                              │                       │
                              │                 max round?
                              │                       │
                              │                  DEADLOCKED
                              ▼
                       GENERATING_REPORT
                              ↓
                       SAVING_MEMORY
                              ↓
                         EVALUATING
                              ↓
                          COMPLETED
```

Orchestrator 负责：

```text
状态转移
并发 Fan-out
Fan-in 等待
Timeout
Retry
Checkpoint
Resume
Cancellation
Round Limit
Failure Policy
```

但不负责语言推理。

---

# 十九、并发模型

三个 Agent 应并发：

```go
errgroup.WithContext(ctx)
```

概念：

```text
                  Orchestrator
                       │
           ┌───────────┼───────────┐
           ▼           ▼           ▼
       Melchior    Balthasar     Casper
           │           │           │
        isolated    isolated    isolated
         state        state        state
           │           │           │
           └───────────┼───────────┘
                       ▼
                    Fan-in
```

必须隔离：

```text
Messages
Step Count
Tool Calls
Working Memory
Temporary Claims
Token Budget
```

共享：

```text
DecisionCase
Historical Memory Snapshot
Evidence Ledger（追加写）
Published Claims
Debate Context
```

共享账本要明确并发安全和事务边界。

---

# 二十、Trace、Audit 与 Replay

建议所有运行产生统一事件：

```go
type MagiEvent struct {
    ID string

    CaseID string
    RunID  string

    AgentCode *MagiCode

    Type EventType

    Payload json.RawMessage

    Timestamp time.Time
}
```

事件类型：

```text
CASE_CREATED
TASK_NORMALIZED
MEMORY_RETRIEVED

AGENT_STARTED
MODEL_REQUESTED
MODEL_RESPONDED

TOOL_CALL_REQUESTED
TOOL_CALL_VALIDATED
TOOL_CALL_STARTED
TOOL_CALL_COMPLETED
TOOL_CALL_FAILED

EVIDENCE_CREATED
CLAIM_CREATED
CLAIM_CONTRADICTION_DECLARED

EVIDENCE_GATE_PASSED
EVIDENCE_GATE_FAILED

VOTE_SUBMITTED
CONSENSUS_EVALUATED

DEBATE_STARTED
REFLECTION_SUBMITTED
REVOTE_SUBMITTED

RESOLUTION_CREATED
MEMORY_INDEXED

CASE_COMPLETED
CASE_FAILED
```

前端 SSE 只订阅这些事件。

这比直接把内部 callback 强绑定前端更干净：

```text
Domain Event
    ├──► Event Store
    ├──► SSE
    ├──► Trace UI
    ├──► Audit
    └──► Replay
```

---

# 二十一、Evaluation

Evaluation 至少分五类。

| 类别        | 指标                           |
| --------- | ---------------------------- |
| Tool      | 成功率、参数校验失败率、平均调用数            |
| Evidence  | EV 引用覆盖率、无来源证据率、证据重复率        |
| Agent     | 投票稳定性、门禁失败次数、无效 Reflection 率 |
| Consensus | 首轮共识率、复议改票率、Deadlock 率       |
| System    | 延迟、Token、成本、失败率              |

还应该加入一个关键指标：

> **Counterfactual Stability：同一个 Case 重跑 N 次，最终结果稳定程度如何？**

因为多 Agent 系统最大的问题之一就是非确定性。

---

# 二十二、推荐代码目录

结合 Coze Studio，我建议新增：

```text
backend/domain/magi/
├── entity/
│   ├── case.go
│   ├── config.go
│   ├── task.go
│   ├── agent_run.go
│   ├── evidence.go
│   ├── claim.go
│   ├── vote.go
│   ├── debate.go
│   ├── reflection.go
│   ├── resolution.go
│   └── event.go
│
├── runtime/
│   ├── runtime.go
│   ├── agent_loop.go
│   ├── state.go
│   ├── response_parser.go
│   ├── termination.go
│   └── checkpoint.go
│
├── orchestration/
│   ├── orchestrator.go
│   ├── state_machine.go
│   ├── dispatcher.go
│   └── failure_policy.go
│
├── evidence/
│   ├── ledger.go
│   ├── adapter.go
│   ├── extractor.go
│   ├── reliability.go
│   └── gate.go
│
├── claim/
│   ├── service.go
│   ├── graph.go
│   └── conflict.go
│
├── consensus/
│   ├── engine.go
│   └── policy.go
│
├── debate/
│   ├── engine.go
│   ├── packet.go
│   └── reflection.go
│
├── memory/
│   ├── service.go
│   ├── context_builder.go
│   └── projection.go
│
├── validation/
│   ├── validator.go
│   ├── schema.go
│   └── structured_output.go
│
├── port/
│   ├── model.go
│   ├── tool.go
│   ├── knowledge.go
│   ├── repository.go
│   └── event.go
│
└── service/
    ├── commander.go
    ├── case_service.go
    ├── report.go
    └── evaluation.go
```

Coze Adapter：

```text
backend/application/magi/
├── model_adapter.go
├── plugin_adapter.go
├── knowledge_adapter.go
├── workflow_adapter.go
├── coderunner_adapter.go
├── event_publisher.go
└── service.go
```

我会刻意让 `domain/magi` 不直接 import：

```text
domain/plugin
domain/knowledge
domain/workflow
bizpkg/llm/modelbuilder
```

全部通过 Port/Adapter 隔离。这样未来即使 Coze Studio 内部重构，MAGI 核心领域也不会跟着碎裂。

---

# 二十三、数据库完整设计

建议至少新增：

```text
magi_config
decision_case
decision_task
magi_agent_run
magi_agent_checkpoint
magi_tool_call
evidence_record
claim
claim_evidence_relation
claim_relation
evidence_summary
magi_vote
vote_utility_score
debate_round
reflection
resolution
case_memory_projection
magi_event
magi_evaluation
```

不建议把所有内容都塞进 JSON 字段。核心查询字段关系化：

```text
Case
Agent Run
Evidence
Claim
Vote
Resolution
```

而复杂快照可以 JSON：

```text
Agent Checkpoint
Structured LLM Output
Debate Packet Snapshot
Memory Projection
Event Payload
Evaluation Details
```

---

# 二十四、完整 Coze Studio 复用边界

最终边界建议冻结为：

| Coze 能力              | MAGI 策略                                 |
| -------------------- | --------------------------------------- |
| Model Builder        | 直接复用                                    |
| ToolCallingChatModel | 直接复用                                    |
| Plugin Registry      | 直接复用                                    |
| ExecuteTool          | 直接复用                                    |
| Knowledge Retrieve   | 直接复用                                    |
| Knowledge Store      | 直接复用                                    |
| Variables            | 复用                                      |
| Workflow             | 作为 Agent 可调用 Tool，不作为 MAGI Orchestrator |
| CodeRunner           | Adapter 复用                              |
| Conversation Message | 不作为 Evidence Ledger                     |
| RunRecord            | 不作为 MAGI Trace 主存储                      |
| Eino ReAct           | 不用于 MAGI 核心 Agent Loop                  |
| Eino Compose         | 不用于顶层 MAGI FSM                          |
| SSE Infrastructure   | 尽可能复用传输层                                |
| Auth/User Context    | 直接复用                                    |

因此最终形态不是「修改 Coze 的 Agent」。

而是：

> **Coze Studio 提供模型、工具、知识、沙盒、认证和基础设施；MAGI 提供自己的 Agent Runtime、Evidence System、Claim Graph、Consensus Protocol、Debate Protocol、Memory Model 和 Decision UX。**

---

# 二十五、我对完整设计的三个关键建议

第一，**Evidence Ledger 应该成为真正的系统脊柱**。Tool Result 进入 Evidence，Evidence 支撑 Claim，Claim 支撑 Utility Score 和 Vote，冲突发生在 Claim Graph，Debate 针对冲突 Claim，最终 Resolution 引用 Vote、Claim 和 Evidence，整个链路因此可审计。

第二，**不要把认知多样性仅建立在 Prompt 上**。完整的差异公式应该是：

```text
Cognitive Diversity
    =
Persona
+ Objective Function
+ Evidence Standard
+ Risk Policy
+ Tool Permission
+ Model Diversity（可选）
+ Memory Interpretation
```

第三，**LLM 负责语义，代码负责规则**：

```text
LLM:
理解问题
提出 Claim
解释 Evidence
生成 Reflection
撰写报告

Deterministic Code:
权限
Schema 校验
EV-ID 真实性
Evidence Gate
Utility Dimension 合法性
投票计数
共识判断
状态转换
循环限制
Timeout
```

这是我建议作为最终目标冻结的**完整 MAGI 架构**。实现顺序可以分阶段，但领域模型、模块边界、证据链、状态机和 Coze 复用边界都应以这套完整架构为目标，不再为某个 MVP 临时设计一套过渡架构。
