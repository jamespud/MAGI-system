# MAGI Frontend Design Spec

> **Status:** Approved
> **Created:** 2026-07-18
> **Reference:** `magi-front.md` (user-authored 20-section design vision)

## Overview

Build the MAGI Decision Operating System frontend — a React SPA that presents a multi-agent decision engine through an Evangelion-inspired dashboard interface. The core metaphor: **everything is a Decision Case**, not a conversation.

## Global Constraints

- React 18+ with Vite + TypeScript
- All code under `frontend/` in the monorepo
- Tailwind CSS 4 for layout; custom CSS for EVA-themed animations (scanlines, pulse-glow, status-blink)
- Mock data via MSW (Mock Service Worker) — backend is not required for frontend development
- Makefile targets: `make fe` (build), `make fe_dev` (dev server), `make fe_test`, `make fe_lint`
- Tests: Vitest + React Testing Library for components and stores
- CSS variable-driven theming — no hardcoded colors in components
- Chinese-ready UI copy (labels, status text) with English code identifiers

---

## Architecture

### 1. Project Structure

```
frontend/
├── src/
│   ├── components/
│   │   ├── layout/           # AppShell, TopNav, LeftNav, RightInspector, BottomTimeline
│   │   ├── workspace/        # CaseHeader, DecisionInput, AgentPanel, AgentTrio, ConsensusPanel
│   │   ├── evidence/         # EvidenceGraph (D3), EvidenceNode
│   │   ├── shared/           # GlowBorder, ScanLine, PulseDot, StatusBadge, MonoText
│   │   └── ui/               # Button, Card, Badge, Divider
│   ├── stores/               # Zustand: caseStore, agentStore, eventStore, uiStore
│   ├── mock/                 # MSW handlers + mock data factory
│   ├── hooks/                # useSSE, useAgentStream, useTimeline
│   ├── pages/                # DecisionWorkspace, Memory, Replay, Evaluation, Dataset, Tools, Settings
│   ├── styles/               # globals.css, tokens.css, animations.css
│   ├── App.tsx
│   ├── main.tsx
│   └── router.tsx
├── public/
├── package.json
├── tsconfig.json
├── vite.config.ts
├── tailwind.config.ts
├── vitest.config.ts
└── index.html
```

### 2. Four-Panel Layout

Fixed, non-scrolling shell. Only the Main Workspace scrolls internally.

```
┌────────────────────────────────────────────────────────────────────────────┐
│ TopNav (h-12, fixed)                                                       │
├──────────────┬──────────────────────────────────────────────┬──────────────┤
│ LeftNav      │ Main Workspace (<Outlet/>)                   │ RightInspector│
│ (w-56,       │                                              │ (w-80,        │
│  scroll-y)   │ DecisionWorkspace                            │  scroll-y)    │
│              │   ├── CaseHeader                             │               │
│              │   ├── AgentTrio (3x AgentPanel, accordion)    │               │
│              │   ├── ConsensusPanel                         │               │
│              │   └── EvidencePanel (D3 Knowledge Graph)     │               │
├──────────────┴──────────────────────────────────────────────┴──────────────┤
│ BottomTimeline (h-48, collapsible, filterable)                              │
└────────────────────────────────────────────────────────────────────────────┘
```

### 3. Top Navigation

Left: MAGI logo + 7 primary nav items (Decision, Memory, Replay, Evaluation, Dataset, Tools, Settings).
Right: System status bar — Model name, Token count, Cost, Latency, System Status LED, User avatar.

No menus. No dropdowns. Flat, always-visible navigation with highlighted active state.

### 4. Left Navigation (Decision Center)

Case list organized by status sections: Running, Recent, Pinned, Completed, Archived.
Below a divider: Templates, Benchmark, Dataset, History.
Clicking a case navigates to `/:caseId` and loads the Decision Workspace.

### 5. Decision Workspace (Core Page)

Route: `/case/:caseId`

#### 5a. CaseHeader

Top of workspace. Displays: Decision question, Status badge (INVESTIGATING → DEBATING → RESOLVED), Round number, Creation time, Current consensus (2:1), Confidence (81%), and action buttons (Run, Pause, Replay, Export, Delete).

#### 5b. AgentTrio

Three `AgentPanel` components displayed side-by-side, never as tabs. Accordion interaction: clicking an agent expands it (flex-grow from 0.33 → 0.6), others contract (0.33 → 0.2). This preserves the parallel cognition view while allowing deeper inspection.

Each `AgentPanel` displays:
- **Header:** Agent name, status badge, step counter (e.g., "Step 6/12"), progress bar
- **Collapsed view:** Tool call count, evidence count, claims count, vote status
- **Expanded view:** Thought trace, tool call list with parameters, evidence items (EV-001, EV-002...), claims list, reasoning paragraph

Agent identity colors (CSS variables):
- Melchior (scientist): `--melchior: #4A9EFF` — cold blue
- Balthasar (protector): `--balthasar: #E5A93D` — amber yellow
- Casper (innovator): `--casper: #C77DFF` — magenta/purple

#### 5c. ConsensusPanel

Below the AgentTrio. Shows:
- Current vote tally (e.g., "2 : 1"), majority/minority stance
- Confidence score (0-100%)
- "Need Reflection?" indicator (YES/NO)
- Mini consensus timeline: Round1 → Debate → Reflection → Round2 → Resolved (vertical flow)

#### 5d. EvidencePanel (Knowledge Graph)

Below Consensus. A D3.js force-directed graph where:
- Evidence nodes (EV-001, EV-004...) connect to Claims
- Claims connect to Agent Votes
- Clicking a node selects it → RightInspector shows full details
- Edges show relationship type (supports / contradicts)

### 6. Right Inspector

Always visible (w-80, ~320px). Context-aware detail panel:
- Click an Evidence node → shows source, URL, observation, reliability score (0.87), collected by
- Click a Vote → shows stance, confidence, utility dimensions (Correctness, Efficiency, Risk)
- Click an Agent → shows full agent state dump
- Click a Timeline event → shows raw event JSON

Behaves like VSCode's inspector: single content area, no tabs, content switches on selection.

### 7. Bottom Timeline

Height: ~192px (h-48), collapsible. Always-present event stream similar to IDE console.
Each event entry: timestamp | event type icon | agent avatar (if applicable) | message.
Filter buttons: Tool | Agent | Evidence | Vote — toggle which event types are visible.
New events from SSE stream append with slide-up animation.

### 8. Other Pages (Placeholder for Phase 1)

Memory, Replay, Evaluation, Dataset, Tools, Settings — each renders a page shell with title and "Coming soon" placeholder content. Full routing structure is functional, content is deferred.

---

## State Management

### Stores (Zustand)

**caseStore:**
```ts
interface CaseState {
  case: Case | null;
  cases: CaseSummary[];
  loading: boolean;
  // Actions: loadCase(id), loadCaseList(), updateFromEvent(event)
}
```

**agentStore:**
```ts
interface AgentState {
  agents: Record<'melchior' | 'balthasar' | 'casper', AgentSnapshot>;
  // AgentSnapshot = { status, step, maxSteps, toolCalls[], evidence[], claims[], vote, thought }
  // Actions: patchAgent(id, partial), resetAgents()
}
```

**eventStore:**
```ts
interface EventState {
  events: MagiEvent[];
  filters: { tool: boolean; agent: boolean; evidence: boolean; vote: boolean };
  // Actions: pushEvent(event), clearEvents(), toggleFilter(type)
}
```

**uiStore:**
```ts
interface UIState {
  selected: { type: 'evidence' | 'vote' | 'agent' | 'event'; id: string } | null;
  expandedAgent: 'melchior' | 'balthasar' | 'casper' | null;
  sidebarCollapsed: boolean;
  timelineCollapsed: boolean;
  timelineHeight: number;
  // Actions: select(obj), clearSelection(), toggleSidebar(), setTimelineHeight(h)
}
```

### Data Flow

```
SSE stream (/api/v1/cases/:id/stream)
  → parse MagiEvent JSON
  → eventStore.pushEvent(event)   // Timeline append
  → agentStore.patchAgent(...)     // Agent state incremental update
  → caseStore.updateFromEvent(...) // Case status transitions
  → uiStore.select(...) auto-selects significant events
```

In Phase 1, SSE is mocked via MSW handlers that replay pre-built event sequences with realistic timing.

---

## Design System

### Color Tokens

```css
:root {
  --bg-base:       #0B0F14;
  --bg-elevated:   #131820;
  --bg-raised:     #1A2130;
  --bg-overlay:    #202B3B;

  --melchior:      #4A9EFF;
  --balthasar:     #E5A93D;
  --casper:        #C77DFF;

  --accent:        #2DD4BF;
  --warning:       #F59E0B;
  --error:         #EF4444;

  --text-primary:  #F1F5F9;
  --text-secondary:#94A3B8;
  --text-muted:    #64748B;

  --border-dim:    #1E293B;
  --border-glow:   rgba(74, 158, 255, 0.15);
  --border-active: rgba(74, 158, 255, 0.3);
}
```

### Typography

- **UI body:** IBM Plex Sans (weights 400, 500, 600)
- **Monospace:** JetBrains Mono (weights 400, 500) — for status labels, IDs, code, timestamps, event data
- **Font loading:** `@font-face` with `font-display: swap`

### Animations

| Name | Duration | Description |
|------|----------|-------------|
| `scanline` | 8s infinite | Translucent line sweeps top-to-bottom across panels |
| `pulse-glow` | 2s infinite | Border glow breathe for running agents |
| `status-blink` | 1.4s infinite | Dot blink for active status indicators |
| `slide-up` | 0.3s ease-out | Timeline entries enter from below |
| `fade-in` | 0.15s | Inspector content transition |

All animations via CSS `@keyframes`. Agent status drives animation state through CSS custom properties (e.g., `style={{ '--agent-status': 'running' }}`) rather than JS toggling classes.

---

## Dependencies

| Package | Purpose |
|---------|---------|
| react, react-dom | UI framework |
| react-router-dom | Client-side routing |
| zustand | State management |
| d3 (force, selection, scale) | Evidence/Claim knowledge graph |
| lucide-react | Icon set (control-room aesthetic) |
| date-fns | Timestamp formatting |
| msw | Mock API for development |
| tailwindcss, @tailwindcss/vite | Utility-first CSS |
| vitest, @testing-library/react, jsdom | Testing |
| @types/react, @types/d3 | TypeScript types |

---

## Makefile Additions

```makefile
FRONTEND_DIR := ./frontend
FRONTEND_STATIC := ./bin/resources/static

fe:
	@echo "Building frontend..."
	@cd $(FRONTEND_DIR) && npm ci && npm run build
	@rm -rf $(FRONTEND_STATIC)
	@mkdir -p $(FRONTEND_STATIC)
	@cp -r $(FRONTEND_DIR)/dist/* $(FRONTEND_STATIC)/
	@echo "Frontend built → $(FRONTEND_STATIC)"

fe_dev:
	@echo "Starting frontend dev server..."
	@cd $(FRONTEND_DIR) && npm run dev

fe_test:
	@echo "Running frontend tests..."
	@cd $(FRONTEND_DIR) && npm run test

fe_lint:
	@echo "Linting frontend..."
	@cd $(FRONTEND_DIR) && npm run lint
```

Vite dev config includes proxy: `/api` → `http://localhost:8080` (Go backend).

---

## Test Strategy

- **Component tests:** Vitest + RTL, focus on AgentPanel (accordion interaction, status display), BottomTimeline (filter, event rendering), RightInspector (context switching)
- **Store tests:** Pure function tests for each Zustand store (state transitions, action correctness)
- **Mock data:** Shared test fixtures in `src/mock/` produce consistent fake Cases, Agents, Events, Evidence, Claims
- **Visual regression:** Deferred to Phase 2 (Storybook + Chromatic)
- **E2E:** Deferred to Phase 2 (Playwright)

---

## Phase 1 Scope (This Implementation)

**In scope:**
- Complete 4-panel layout (TopNav, LeftNav, Main, RightInspector, BottomTimeline)
- Decision Workspace: CaseHeader, AgentTrio (3 AgentPanels with accordion), ConsensusPanel, EvidencePanel (D3 graph)
- Right Inspector with context switching
- Bottom Timeline with filter
- Routing for all 7 modules (placeholder pages for Memory, Replay, Evaluation, Dataset, Tools, Settings)
- Mock data via MSW for a complete sample case
- Makefile frontend commands
- Design system (tokens, typography, animations)

**Out of scope:**
- Real SSE connection (mock only)
- Real API calls to backend
- Claim Graph page (separate page in Decision Center)
- Replay playback controls
- Evaluation benchmark runner
- Mobile responsive layout
- Settings page configuration forms
- E2E tests
- Visual regression tests
