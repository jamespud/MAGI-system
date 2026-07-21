# Case Page UI Fixes — Design

- Date: 2026-07-21
- Status: Approved
- Scope: Frontend only (`frontend/src/`)

## Problem

Three issues on the case workspace page (`DecisionWorkspace` + `LeftNav` + `EvidenceGraph`):

1. **Completed 分栏无分页** — `LeftNav` 的 Completed 分区直接渲染全部 RESOLVED 案例，案例多时挤占整个侧边栏，只能靠整栏滚动，无法快速翻阅。
2. **Evidence Graph 太小** — 图固定高度 `320px`，节点多（27 claims + evidence + votes）时挤在一起看不清，且无缩放能力。
3. **APPROVE 显示为红色** — agent 最终投票颜色错误。

### 颜色 bug 根因

后端 `backend/domain/entity/vote.go:48-51` 发送**小写** stance 常量：`approve` / `reject` / `abstain` / `conditional_approve`。DTO `server/dto/dto.go:304` 直接 `string(v.Decision)` 透传，API 返回小写。

前端却用**大写**比较，导致所有正向判定落到红色分支：

| 文件 | 代码 | 结果 |
|---|---|---|
| `AgentPanel.tsx:149` | `agent.vote.stance === 'Approve' ? 'text-accent' : 'text-error'` | approve → 红 |
| `RightInspector.tsx:105` | `vote.stance === 'Approve' ? 'text-accent' : 'text-error'` | approve → 红 |
| `ConsensusPanel.tsx:14-16,27-29` | `=== 'Approve' / 'Reject' / 'Abstain'` | 派生票数全 0、图标全 Minus |
| `EvidenceGraph.tsx:46` | `v.stance === 'approve' ? ...` | 唯一正确（小写） |

`agentStore.test.ts:96` 断言 `toBe('approve')`（小写透传），证实运行时数据为小写。`AgentVote.stance` 类型声明 `'Approve' \| 'Reject' \| 'Abstain'`（大写）与运行时不符——类型在说谎。另有第 4 种 stance `conditional_approve` 前端完全未处理。

调色板（`tokens.css`）：`--accent:#2DD4BF`（青，正向意图色）、`--success:#22C55E`（绿，未使用）、`--error:#EF4444`（红）、`--warning:#F59E0B`（琥珀）。

## Decisions (approved)

- Graph：缩放按钮 + 加高（320→520px）+ 滚轮缩放 + reset。
- Approve 颜色：Teal（`--accent`）。reject 保持红、abstain 保持灰、`conditional_approve` 用 `--warning` 琥珀。
- Completed 分栏：仅 Completed 分区，10/页，`‹ 1/3 ›` 箭头按钮 + 滚轮翻页。

## Design

### 1. Completed 分栏分页

**新建** `src/components/layout/PaginatedSection.tsx`：

```
interface PaginatedSectionProps {
  title: string;
  icon: React.ComponentType<{ size?: number; className?: string }>;
  items: CaseSummary[];
  pageSize?: number; // default 10
}
```

- 状态：`page`（1-based）。`maxPage = max(1, ceil(items.length / pageSize))`。
- 渲染当前页 `items.slice((page-1)*pageSize, page*pageSize)`，沿用现有 `NavLink` 样式与 28 字截断逻辑。
- `page` 钳制：当 `items` 变化（新案例 resolve）导致 `page > maxPage` 时，渲染前 `Math.min(page, maxPage)`；用 `useEffect` 同步 state。
- 仅当 `maxPage > 1` 时渲染底栏：`‹  page/maxPage  ›`。
  - 左右箭头用 lucide `ChevronLeft` / `ChevronRight`。
  - 首页禁用 `‹`，末页禁用 `›`（`disabled` 样式 + `cursor-not-allowed`）。
  - 空列表仍显示 "No cases"（沿用原行为）。
- **滚轮翻页**：`useRef` 持有列表容器，`useEffect` 中 `el.addEventListener('wheel', handler, { passive: false })`。
  - `e.deltaY > 0` → 下一页；`e.deltaY < 0` → 上一页。
  - `e.preventDefault()` 阻止侧边栏整栏滚动。
  - 仅 `maxPage > 1` 时注册监听；cleanup 时 `removeEventListener`。

**修改** `src/components/layout/LeftNav.tsx`：
- `SECTIONS` 中 `Completed` 项改用 `<PaginatedSection title="Completed" icon={CheckCircle} items={filtered} />`。
- 其余分区（Pinned / Running / Archived）保持现有内联渲染不变。
- 从 `PaginatedSection` 导出默认，加入 `layout/index.ts` barrel。

### 2. Evidence Graph 缩放

**修改** `src/components/evidence/EvidenceGraph.tsx`：

- 高度常量 `HEIGHT = 520`（替换原 320 的三处：viewBox、空状态 div `style={{ height }}`、svg `style={{ height }}`）。
- 引入 `d3.zoom()`：
  - 渲染时把 `link` 与 `node` 两个 `<g>` 包进一个 `<g class="zoom-layer">`（或直接对内容 `<g>` 应用 transform）。
  - `const zoom = d3.zoom<SVGSVGElement, unknown>().scaleExtent([0.3, 3]).on('zoom', (e) => layer.attr('transform', e.transform))`。
  - `svg.call(zoom)`。
  - 滚轮缩放与空白处拖拽平移由 `d3.zoom` 自动提供；节点上的 `d3.drag` 仍单独工作（拖节点 vs 拖画布共存）。
- `zoomRef = useRef<d3.ZoomBehavior<SVGSVGElement, unknown> | null>(null)`，`svgRef` 已有；effect 内赋值。
- `const [zoomPct, setZoomPct] = useState(100)`，在 `zoom` 的 `on('zoom')` 回调里 `setZoomPct(Math.round(e.transform.k * 100))`。
- 卡片头右上角加 3 个按钮（lucide `ZoomIn` / `ZoomOut` / `Maximize2`）+ 缩放比文字：
  - `+` → `zoomRef.current && d3.select(svgRef.current).transition().call(zoomRef.current.scaleBy, 1.3)`
  - `−` → `...scaleBy, 1/1.3`
  - `reset` → `...call(zoomRef.current.transform, d3.zoomIdentity)`
  - 按钮 disabled 边界：`scaleExtent` 已限制，按钮不额外禁用（d3 会钳制）。
- 头部布局调整：标题 + 图例在左，缩放控件在右（`flex items-center justify-between`）。
- 拖拽 (`d3.drag`) 与缩放互不干扰：drag 绑定在 node `<g>`，zoom 绑定在 svg。保留现有 drag 逻辑。
- 注意：`svg.selectAll('*').remove()` 后重建，zoom 行为需重新 `svg.call(zoom)`（每次 effect 重建都在 clean svg 后重新 attach）。

### 3. 颜色修复（集中 helper）

**新建** `src/lib/stance.ts`：

```ts
export type Stance = 'approve' | 'reject' | 'abstain' | 'conditional_approve';

export function normalizeStance(s: string | undefined | null): Stance {
  const n = (s ?? '').toLowerCase();
  if (n === 'approve' || n === 'reject' || n === 'abstain' || n === 'conditional_approve') return n;
  return 'abstain'; // 未知值兜底为中性
}

export function stanceColor(s: string | undefined | null): string {
  const n = normalizeStance(s);
  if (n === 'approve') return 'var(--accent)';
  if (n === 'reject') return 'var(--error)';
  if (n === 'conditional_approve') return 'var(--warning)';
  return 'var(--text-muted)'; // abstain / unknown
}

export function stanceLabel(s: string | undefined | null): string {
  const n = normalizeStance(s);
  return n === 'conditional_approve' ? 'CONDITIONAL APPROVE' : n.toUpperCase();
}
```

**修改** `src/types/agent.ts`：
- `AgentVote.stance: Stance`（从 `src/lib/stance` 导入；小写联合，含 `conditional_approve`）。

**修改** `src/stores/agentStore.ts`：
- `loadAgentsFromApi` 中 `stance: normalizeStance(v.vote.stance)`（替换原 `as AgentVote['stance']` 强转）。
- `AgentVote` 字面量构造处（如 `patchAgent` 默认）无需改。
- 现有测试 `agentStore.test.ts:96` 断言 `toBe('approve')` 继续通过。

**替换 4 处手写比较**：

- `AgentPanel.tsx:88` — `{agent.vote?.stance || 'Pending'}` → `{agent.vote ? stanceLabel(agent.vote.stance) : 'Pending'}`。
- `AgentPanel.tsx:149-150` — className 三元 → `style={{ color: stanceColor(agent.vote.stance) }}`，文本 `{stanceLabel(agent.vote.stance)}`。
- `RightInspector.tsx:105-106` — 同上：`style={{ color: stanceColor(vote.stance) }}` + `{stanceLabel(vote.stance)}`。
- `ConsensusPanel.tsx:13-17` `renderVoteIcon` — 用 `normalizeStance`：approve→`Check`(`text-accent`)、reject→`X`(`text-error`)、conditional_approve→`Check`(`text-warning`)、abstain→`Minus`(`text-text-muted`)。
- `ConsensusPanel.tsx:24-31` 派生计数 — `normalizeStance(s)` 判定。**`conditional_approve` 既不计入 approve 也不计入 reject**（保持中立，与"有条件赞成"语义一致）；`Current` 显示仍为 `${approve} : ${reject}`，不新增列（YAGNI）。conditional_approve 仅在单投票展示处（AgentPanel / RightInspector / 图节点）以琥珀色正确渲染。
- `ConsensusPanel.tsx:68` majorityLabel 颜色 — `normalizeStance` 判定。
- `EvidenceGraph.tsx:46` — `color: stanceColor(v.stance)`。

**mock 数据**：`src/mock/data.ts` 用大写 `'Approve'/'Reject'`。核对是否仍被引用：
- 若 MSW 已停用且 `data.ts` 仅作参考 → 同步改小写以保持一致（或留 `data.reference.ts` 不动）。
- `ConsensusPanel.test.tsx:26-28` 喂大写 `'Approve'/'Reject'/'Abstain'` → `normalizeStance` 兼容大写，测试继续通过；如需统一可改小写（非必须）。

## Testing

- **`src/lib/stance.test.ts`**（新增）：`normalizeStance` 大小写/空/未知值；`stanceColor` 四种 stance 映射；`stanceLabel` 含 `conditional_approve`。
- **`src/components/layout/__tests__/PaginatedSection.test.tsx`**（新增）：
  - 12 项 → 2 页，默认显示前 10。
  - 点 `›` 翻到第 2 页，`›` 禁用；点 `‹` 回第 1 页，`‹` 禁用。
  - ≤10 项不显示翻页栏。
  - 滚轮 `deltaY>0` 触发下一页（fireEvent `wheel`）。
- **`EvidenceGraph`**：现无测试。补轻量测试：缩放按钮（+/−/reset）存在；`stanceColor` 已在 lib 测试覆盖。
- **`agentStore.test.ts`**：保持 `toBe('approve')` 断言。
- **`ConsensusPanel.test.tsx`**：现有用例（大写 stance）继续通过；补一个 `conditional_approve` 用例。

## Out of Scope

- 不改后端 stance 取值（小写已正确）。
- 不改其它分区（Pinned/Running/Archived）的分页。
- 不引入新依赖（d3 已在）。
- 不改 `BottomTimeline` 的 stance 显示（`voted ${d.stance}` 文本展示，小写可接受）。

## Files Touched

- 新增：`src/lib/stance.ts`、`src/lib/stance.test.ts`、`src/components/layout/PaginatedSection.tsx`、`src/components/layout/__tests__/PaginatedSection.test.tsx`
- 修改：`src/components/layout/LeftNav.tsx`、`src/components/layout/index.ts`、`src/components/evidence/EvidenceGraph.tsx`、`src/components/workspace/AgentPanel.tsx`、`src/components/workspace/ConsensusPanel.tsx`、`src/components/layout/RightInspector.tsx`、`src/types/agent.ts`、`src/stores/agentStore.ts`
- 测试：`src/components/workspace/__tests__/ConsensusPanel.test.tsx`（补 conditional_approve）
- 可选：`src/mock/data.ts`（stance 小写，若仍引用）
