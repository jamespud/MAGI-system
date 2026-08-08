我认为，**MAGI 的前端不能按照传统 AI Chat 产品（ChatGPT、Coze、Dify）来设计，而应该按照「决策操作系统（Decision Operating System）」来设计。**

---

# 一、设计理念（Design Philosophy）

整个产品只有一个核心：

> **Everything is a Decision Case.**

不是 Chat。

不是 Conversation。

不是 Session。

所有内容都围绕一个 Decision Case 展开。

例如：

```
Decision Center
    ├── Case
    ├── Evidence
    ├── Debate
    ├── Vote
    ├── Timeline
    ├── Memory
    └── Report
```

因此导航也围绕 Decision，而不是 Conversation。

---

# 二、整体布局

建议采用四区布局。

```
┌────────────────────────────────────────────────────────────────────────────┐
│ Top Navigation                                                             │
├──────────────┬──────────────────────────────────────────────┬──────────────┤
│              │                                              │              │
│              │                                              │              │
│ Left         │                Main Workspace                │ Right Panel  │
│ Navigation   │                                              │              │
│              │                                              │              │
│              │                                              │              │
├──────────────┴──────────────────────────────────────────────┴──────────────┤
│ Bottom Timeline                                                            │
└────────────────────────────────────────────────────────────────────────────┘
```

四块分别承担不同职责。

---

# 三、顶部导航（Global Navigation）

顶部不是菜单栏。

而是整个系统状态。

```
MAGI

──────────────────────────────────────────────────────────

Decision

Memory

Evaluation

Replay

Dataset

Settings

──────────────────────────────────────────────────────────

Model

Token

Cost

Latency

System Status

User
```

右侧永远显示：

```
Claude 4

$0.14

1450 Tokens

Running

Connected
```

方便观察整个系统。

---

# 四、左侧导航

左侧永远管理 Decision。

```
Decision Center

Recent Cases

Pinned

Running

Completed

Archived

---------------------

Templates

Benchmark

Dataset

History
```

点击：

```
Java → Rust Migration
```

进入：

```
Decision Workspace
```

整个页面切换。

---

# 五、Decision Workspace

这是整个系统最重要的一页。

布局建议：

```
┌────────────────────────────────────────────────────────────┐
│ Case Header                                                │
├────────────────────────────────────────────────────────────┤
│                                                            │
│ Decision Question                                          │
│                                                            │
├────────────────────────────────────────────────────────────┤
│                                                            │
│ Three MAGI Agents                                          │
│                                                            │
├────────────────────────────────────────────────────────────┤
│                                                            │
│ Consensus                                                   │
│                                                            │
├────────────────────────────────────────────────────────────┤
│                                                            │
│ Evidence                                                    │
│                                                            │
└────────────────────────────────────────────────────────────┘
```

这不是聊天。

而是 Dashboard。

---

# 六、Case Header

顶部展示：

```
Should We Rewrite Backend to Rust?

Status

Investigating

Round 2

Created

5 min ago

Consensus

2 : 1

Confidence

81%
```

右侧：

```
Run

Pause

Replay

Export

Delete
```

---

# 七、Decision Input

不是聊天输入框。

而是案件编辑器。

```
Decision Question

Should we migrate Java backend to Rust?

------------------------------------------

Background

......

------------------------------------------

Constraints

Budget

Deadline

Team Size

Priority
```

更像 Jira Issue。

---

# 八、三个 MAGI Panel（整个系统核心）

中间必须占据最大面积。

```
┌──────────────┬──────────────┬──────────────┐
│ MELCHIOR     │ BALTHASAR    │ CASPER       │
├──────────────┼──────────────┼──────────────┤
│              │              │              │
│ Thinking...  │ Searching... │ Voting...    │
│              │              │              │
│ Tool Calls   │ Tool Calls   │ Tool Calls   │
│              │              │              │
│ Evidence     │ Evidence     │ Evidence     │
│              │              │              │
│ Vote         │ Vote         │ Vote         │
│              │              │              │
└──────────────┴──────────────┴──────────────┘
```

三个 Agent 永远并列。

不能切 Tab。

因为：

MAGI 的核心就是：

**Parallel Cognition**

用户应该能看到：

三个 Agent 同时思考。

---

# 九、Agent Card

例如：

```
MELCHIOR

Status

Investigating

██████████████

Step 6 / 12

Tool

Web Search

Memory

Retrieved

Evidence

12

Claims

4

Vote

Pending
```

点击展开。

---

展开：

```
Thought

......

Tool Calls

Search()

Read()

Code()

Evidence

EV-001

EV-002

EV-003

Claims

......

Reasoning

......
```

---

# 十、Consensus Panel

三个 Agent 下方。

```
Consensus

Current

2 : 1

Majority

Approve

Minority

Reject

Confidence

81%

Need Reflection

YES
```

下面：

```
Consensus Timeline

Round1

↓

Debate

↓

Reflection

↓

Round2

↓

Resolved
```

---

# 十一、Evidence Panel

Evidence 不应该是 Table。

应该是：

Knowledge Graph。

```
EV001

↓

Claim

↓

Vote

↓

Resolution
```

例如：

```
EV001

Benchmark

↓

Performance improves 32%

↓

Melchior

Approve

──────────────

EV004

Migration Risk

↓

Balthasar

Reject
```

点击节点：

右侧显示全文。

---

# 十二、Claim Graph

建议单独一页。

```
Claim

Migration is feasible

↑

Evidence

↓

Counter Claim

Migration cost is high

↓

Evidence
```

这样：

Debate

实际上就是：

Claim Graph

不断扩展。

---

# 十三、Timeline

底部建议一直存在。

类似 IDE Console。

```
14:01

Commander Started

14:01

Melchior Tool Call

14:02

Evidence Created

14:03

Vote Submitted

14:04

Debate Started

14:05

Reflection

14:06

Resolved
```

支持：

```
Filter

Tool

Agent

Evidence

Vote
```

---

# 十四、右侧 Inspector

右边永远是：

Inspector。

点击任何对象：

显示详情。

例如：

点击：

```
EV001
```

右侧：

```
Evidence

Source

Web Search

URL

......

Observation

......

Reliability

0.87

Collected By

Melchior
```

点击：

Vote：

```
Vote

Approve

Confidence

81%

Utility

Correctness

92

Efficiency

76
```

类似 VSCode。

---

# 十五、Replay 页面

Replay 不只是 Timeline。

而是真正回放。

```
▶

Step 1

↓

Step 2

↓

Step 3
```

整个 Workspace：

实时恢复。

可以：

```
Play

Pause

1x

2x

Jump
```

调试 Agent。

---

# 十六、Memory 页面

展示：

历史案件。

```
Decision Memory

Migration

Risk

Investment

Architecture
```

每个：

```
Case

Resolution

Outcome

Similarity
```

点击：

恢复整个案件。

---

# 十七、Evaluation 页面

专门做 Benchmark。

```
Run Benchmark

↓

10 Cases

↓

3 Models

↓

Accuracy

↓

Cost

↓

Latency
```

图表：

```
Accuracy

██████

Claude

██████████

Gemini

███████

GPT
```

---

# 十八、Settings

这里只配置：

```
Models

Tool

Knowledge

Prompt

Policy

Theme
```

不放业务内容。

---

# 十九、移动端

移动端不建议完整复刻桌面。

而是：

Case First。

```
Decision

↓

Three Cards

↓

Timeline

↓

Evidence

↓

Vote
```

点击 Agent：

全屏展开。

---

# 二十、视觉风格（EVA + 专业平台融合）

这里是我认为最值得投入精力的部分。不要直接复刻电影 UI，而是提炼其设计语言，让产品既具有 EVA 的辨识度，又保持专业软件的可用性。

整体配色建议采用深色主题，背景为接近黑色的深灰（#0B0F14），配合低饱和度的青蓝色作为系统主色。三个 MAGI 使用固定身份色，但避免高饱和：

* **Melchior（科学）**：冷蓝色（Logic / Analysis）
* **Balthasar（保护）**：琥珀黄色（Risk / Safety）
* **Casper（创新）**：洋红或紫色（Opportunity / Vision）

所有运行状态通过细微的发光边框、扫描线、脉冲动画表达，而不是大面积动画。字体建议采用现代无衬线字体（如 Inter）搭配等宽字体显示日志和事件流，形成「控制中心」的专业感。

布局遵循「高信息密度 + 强空间层次」原则：左侧导航、中间工作区、右侧 Inspector、底部 Timeline 始终固定，用户可以快速建立空间记忆。所有 Evidence、Claim、Vote、Timeline 都支持互相跳转，形成完整的可追溯决策网络。

---

## 最终的信息架构

整个产品建议固定为七个一级模块：

```
MAGI

├── Decision Center
│     ├── Case Workspace
│     ├── Agent Panels
│     ├── Consensus
│     ├── Evidence
│     └── Timeline
│
├── Memory
│     ├── Historical Cases
│     ├── Knowledge
│     └── Search
│
├── Replay
│     ├── Timeline
│     ├── Event Playback
│     └── Trace
│
├── Evaluation
│     ├── Benchmark
│     ├── Comparison
│     └── Reports
│
├── Dataset
│
├── Tools
│
└── Settings
```