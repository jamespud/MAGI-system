# MAGI Frontend Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build the MAGI Decision Operating System frontend — a React SPA with Evangelion-inspired dashboard, four-panel layout, three parallel agent panels, D3 knowledge graph, and event timeline.

**Architecture:** React 18 + Vite + TypeScript SPA under `frontend/`. Zustand for state (4 stores), Tailwind CSS 4 + custom CSS variables for theming, D3.js for evidence graph, MSW for mock data. Fixed 4-panel shell with scrollable workspace. Mock-driven development — no backend required.

**Tech Stack:** React 18, Vite 6, TypeScript 5, Tailwind CSS 4, Zustand 5, D3 7, React Router 7, MSW 2, Lucide React, date-fns, Vitest + React Testing Library

## Global Constraints

- React 18+ with Vite + TypeScript
- All code under `frontend/` in the monorepo
- Tailwind CSS 4 for layout; custom CSS for EVA-themed animations (scanlines, pulse-glow, status-blink)
- Mock data via MSW (Mock Service Worker) — backend is not required for frontend development
- Makefile targets: `make fe` (build), `make fe_dev` (dev server), `make fe_test`, `make fe_lint`
- Tests: Vitest + React Testing Library for components and stores
- CSS variable-driven theming — no hardcoded colors in components
- Chinese-ready UI copy with English code identifiers
- Commit message format: `feat(frontend): <description>` with conventional commits

---

### Task 1: Scaffold Vite project and install dependencies

**Files:**
- Create: `frontend/package.json`
- Create: `frontend/index.html`
- Create: `frontend/vite.config.ts`
- Create: `frontend/tsconfig.json`
- Create: `frontend/tsconfig.app.json`
- Create: `frontend/tsconfig.node.json`
- Create: `frontend/tailwind.config.ts`
- Create: `frontend/vitest.config.ts`
- Create: `frontend/src/main.tsx`
- Create: `frontend/src/App.tsx`
- Create: `frontend/src/styles/globals.css`
- Create: `frontend/.gitignore`

**Interfaces:**
- Produces: npm project ready with `npm run dev`, `npm run build`, `npm run test`

- [ ] **Step 1: Create frontend directory and package.json**

```bash
mkdir -p frontend/src/{components/{layout,workspace,evidence,shared,ui},stores,mock,hooks,pages,styles} frontend/public
```

```json
// frontend/package.json
{
  "name": "magi-frontend",
  "private": true,
  "version": "0.1.0",
  "type": "module",
  "scripts": {
    "dev": "vite",
    "build": "tsc -b && vite build",
    "preview": "vite preview",
    "test": "vitest run",
    "test:watch": "vitest",
    "lint": "eslint . --ext ts,tsx"
  },
  "dependencies": {
    "react": "^18.3.1",
    "react-dom": "^18.3.1",
    "react-router-dom": "^7.1.0",
    "zustand": "^5.0.0",
    "d3": "^7.9.0",
    "lucide-react": "^0.468.0",
    "date-fns": "^4.1.0"
  },
  "devDependencies": {
    "@types/react": "^18.3.0",
    "@types/react-dom": "^18.3.0",
    "@types/d3": "^7.4.0",
    "@vitejs/plugin-react": "^4.3.0",
    "vite": "^6.0.0",
    "typescript": "~5.6.0",
    "tailwindcss": "^4.0.0",
    "@tailwindcss/vite": "^4.0.0",
    "vitest": "^2.1.0",
    "@testing-library/react": "^16.1.0",
    "@testing-library/jest-dom": "^6.6.0",
    "jsdom": "^25.0.0",
    "msw": "^2.6.0",
    "eslint": "^9.0.0"
  },
  "msw": {
    "workerDirectory": ["public"]
  }
}
```

- [ ] **Step 2: Run npm install**

```bash
cd frontend && npm install
```

- [ ] **Step 3: Create index.html**

```html
<!-- frontend/index.html -->
<!DOCTYPE html>
<html lang="zh-CN">
  <head>
    <meta charset="UTF-8" />
    <meta name="viewport" content="width=device-width, initial-scale=1.0" />
    <link rel="icon" type="image/svg+xml" href="/magi.svg" />
    <title>MAGI — Decision Operating System</title>
  </head>
  <body class="bg-base text-primary antialiased">
    <div id="root"></div>
    <script type="module" src="/src/main.tsx"></script>
  </body>
</html>
```

- [ ] **Step 4: Create vite.config.ts**

```ts
// frontend/vite.config.ts
import { defineConfig } from 'vite';
import react from '@vitejs/plugin-react';
import tailwindcss from '@tailwindcss/vite';

export default defineConfig({
  plugins: [react(), tailwindcss()],
  server: {
    port: 5173,
    proxy: {
      '/api': {
        target: 'http://localhost:8080',
        changeOrigin: true,
      },
    },
  },
});
```

- [ ] **Step 5: Create tsconfig files**

```json
// frontend/tsconfig.json
{
  "files": [],
  "references": [
    { "path": "./tsconfig.app.json" },
    { "path": "./tsconfig.node.json" }
  ]
}
```

```json
// frontend/tsconfig.app.json
{
  "compilerOptions": {
    "target": "ES2020",
    "useDefineForClassFields": true,
    "lib": ["ES2020", "DOM", "DOM.Iterable"],
    "module": "ESNext",
    "skipLibCheck": true,
    "moduleResolution": "bundler",
    "allowImportingTsExtensions": true,
    "isolatedModules": true,
    "moduleDetection": "force",
    "noEmit": true,
    "jsx": "react-jsx",
    "strict": true,
    "noUnusedLocals": true,
    "noUnusedParameters": true,
    "noFallthroughCasesInSwitch": true,
    "forceConsistentCasingInFileNames": true,
    "baseUrl": ".",
    "paths": {
      "@/*": ["./src/*"]
    }
  },
  "include": ["src"]
}
```

```json
// frontend/tsconfig.node.json
{
  "compilerOptions": {
    "target": "ES2022",
    "lib": ["ES2023"],
    "module": "ESNext",
    "skipLibCheck": true,
    "moduleResolution": "bundler",
    "allowImportingTsExtensions": true,
    "isolatedModules": true,
    "moduleDetection": "force",
    "noEmit": true,
    "strict": true,
    "noUnusedLocals": true,
    "noUnusedParameters": true,
    "noFallthroughCasesInSwitch": true,
    "forceConsistentCasingInFileNames": true
  },
  "include": ["vite.config.ts", "vitest.config.ts", "tailwind.config.ts"]
}
```

- [ ] **Step 6: Create vitest.config.ts**

```ts
// frontend/vitest.config.ts
import { defineConfig } from 'vitest/config';
import react from '@vitejs/plugin-react';
import path from 'path';

export default defineConfig({
  plugins: [react()],
  test: {
    environment: 'jsdom',
    globals: true,
    setupFiles: ['./src/test-setup.ts'],
  },
  resolve: {
    alias: {
      '@': path.resolve(__dirname, './src'),
    },
  },
});
```

- [ ] **Step 7: Create test setup file**

```ts
// frontend/src/test-setup.ts
import '@testing-library/jest-dom/vitest';
```

- [ ] **Step 8: Create tailwind.config.ts**

```ts
// frontend/tailwind.config.ts
import type { Config } from 'tailwindcss';

export default {
  content: ['./index.html', './src/**/*.{ts,tsx}'],
  theme: {
    extend: {
      colors: {
        base: 'var(--bg-base)',
        elevated: 'var(--bg-elevated)',
        raised: 'var(--bg-raised)',
        overlay: 'var(--bg-overlay)',
        melchior: 'var(--melchior)',
        balthasar: 'var(--balthasar)',
        casper: 'var(--casper)',
        accent: 'var(--accent)',
        'text-primary': 'var(--text-primary)',
        'text-secondary': 'var(--text-secondary)',
        'text-muted': 'var(--text-muted)',
        'border-dim': 'var(--border-dim)',
      },
      fontFamily: {
        sans: ['IBM Plex Sans', 'system-ui', 'sans-serif'],
        mono: ['JetBrains Mono', 'monospace'],
      },
    },
  },
  plugins: [],
} satisfies Config;
```

- [ ] **Step 9: Create entry files (minimal)**

```tsx
// frontend/src/main.tsx
import React from 'react';
import ReactDOM from 'react-dom/client';
import App from './App';
import './styles/globals.css';

ReactDOM.createRoot(document.getElementById('root')!).render(
  <React.StrictMode>
    <App />
  </React.StrictMode>,
);
```

```tsx
// frontend/src/App.tsx
export default function App() {
  return (
    <div className="flex h-screen items-center justify-center bg-base text-text-primary">
      <span className="font-mono text-accent">MAGI System Initializing...</span>
    </div>
  );
}
```

- [ ] **Step 10: Create frontend .gitignore**

```
# frontend/.gitignore
node_modules/
dist/
.vite/
```

- [ ] **Step 11: Verify dev server starts**

```bash
cd frontend && npx vite --host 0.0.0.0 &
sleep 3
curl -s http://localhost:5173 | head -5
# Expected: HTML with "MAGI — Decision Operating System"
kill %1 2>/dev/null
```

- [ ] **Step 12: Commit**

```bash
cd /home/spud/proj/magi
git add frontend/package.json frontend/package-lock.json frontend/index.html \
  frontend/vite.config.ts frontend/tsconfig.json frontend/tsconfig.app.json \
  frontend/tsconfig.node.json frontend/tailwind.config.ts frontend/vitest.config.ts \
  frontend/src/main.tsx frontend/src/App.tsx frontend/src/test-setup.ts \
  frontend/src/styles/globals.css frontend/.gitignore
git commit -m "feat(frontend): scaffold Vite React TypeScript project with deps"
```

---

### Task 2: Add Makefile frontend commands

**Files:**
- Modify: `Makefile` (append frontend targets)
- Modify: `Makefile` (update `help` target)

**Interfaces:**
- Produces: `make fe`, `make fe_dev`, `make fe_test`, `make fe_lint`

- [ ] **Step 1: Add frontend variables and targets to Makefile**

```makefile
# In Makefile, add after existing variables (before .PHONY or first target):

FRONTEND_DIR := ./frontend
FRONTEND_STATIC := ./bin/resources/static
```

```makefile
# Add these targets after existing targets:

fe:
	@echo "Building frontend..."
	@cd $(FRONTEND_DIR) && npm ci && npm run build
	@rm -rf $(FRONTEND_STATIC)
	@mkdir -p $(FRONTEND_STATIC)
	@cp -r $(FRONTEND_DIR)/dist/* $(FRONTEND_STATIC)/
	@echo "Frontend built and copied to $(FRONTEND_STATIC)"

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

```makefile
# Update the .PHONY line at the top to include new targets:
# Change from:
# .PHONY: debug server middleware sync_db test build_server clean help
# To:
# .PHONY: debug server middleware sync_db test build_server clean help fe fe_dev fe_test fe_lint
```

- [ ] **Step 2: Update help target to document new commands**

Add these lines to the help target output:
```makefile
	@echo ""
	@echo "Frontend:"
	@echo "  make fe                Build the frontend"
	@echo "  make fe_dev            Start frontend dev server (HMR)"
	@echo "  make fe_test           Run frontend tests"
	@echo "  make fe_lint           Lint frontend code"
```

- [ ] **Step 3: Verify help shows new commands**

```bash
make help | grep -A 5 "Frontend:"
# Expected: Shows fe, fe_dev, fe_test, fe_lint
```

- [ ] **Step 4: Commit**

```bash
git add Makefile
git commit -m "feat(build): add frontend Makefile targets (fe, fe_dev, fe_test, fe_lint)"
```

---

### Task 3: Design tokens, global styles, fonts, and animations

**Files:**
- Create: `frontend/src/styles/tokens.css`
- Create: `frontend/src/styles/animations.css`
- Modify: `frontend/src/styles/globals.css`
- Modify: `frontend/index.html` (add font preload links)

**Interfaces:**
- Produces: All CSS custom properties, @keyframes, font imports available globally
- Consumed by: Every component

- [ ] **Step 1: Create tokens.css**

```css
/* frontend/src/styles/tokens.css */
:root {
  /* Background layers */
  --bg-base:       #0B0F14;
  --bg-elevated:   #131820;
  --bg-raised:     #1A2130;
  --bg-overlay:    #202B3B;

  /* Agent identity colors */
  --melchior:      #4A9EFF;
  --balthasar:     #E5A93D;
  --casper:        #C77DFF;

  /* Semantic colors */
  --accent:        #2DD4BF;
  --warning:       #F59E0B;
  --error:         #EF4444;
  --success:       #22C55E;

  /* Text hierarchy */
  --text-primary:  #F1F5F9;
  --text-secondary:#94A3B8;
  --text-muted:    #64748B;

  /* Borders */
  --border-dim:    #1E293B;
  --border-glow:   rgba(74, 158, 255, 0.15);
  --border-active: rgba(74, 158, 255, 0.3);

  /* Shadows */
  --shadow-glow-melchior: 0 0 12px rgba(74, 158, 255, 0.15);
  --shadow-glow-balthasar: 0 0 12px rgba(229, 169, 61, 0.15);
  --shadow-glow-casper:    0 0 12px rgba(199, 125, 255, 0.15);
}
```

- [ ] **Step 2: Create animations.css**

```css
/* frontend/src/styles/animations.css */
@keyframes scanline {
  0%   { transform: translateY(-100%); }
  100% { transform: translateY(400%); }
}

@keyframes pulse-glow {
  0%, 100% { opacity: 0.4; }
  50%      { opacity: 1.0; }
}

@keyframes status-blink {
  0%, 100% { opacity: 1; }
  50%      { opacity: 0.2; }
}

@keyframes slide-up {
  from { opacity: 0; transform: translateY(12px); }
  to   { opacity: 1; transform: translateY(0); }
}

@keyframes fade-in {
  from { opacity: 0; }
  to   { opacity: 1; }
}

@keyframes spin-slow {
  from { transform: rotate(0deg); }
  to   { transform: rotate(360deg); }
}

.animate-scanline  { animation: scanline 8s linear infinite; }
.animate-pulse-glow { animation: pulse-glow 2s ease-in-out infinite; }
.animate-status-blink { animation: status-blink 1.4s ease-in-out infinite; }
.animate-slide-up  { animation: slide-up 0.3s ease-out forwards; }
.animate-fade-in   { animation: fade-in 0.15s ease-out forwards; }
.animate-spin-slow { animation: spin-slow 3s linear infinite; }
```

- [ ] **Step 3: Update globals.css to import tokens and animations**

```css
/* frontend/src/styles/globals.css */
@import './tokens.css';
@import './animations.css';
@import "tailwindcss";

@theme {
  --color-base: var(--bg-base);
  --color-elevated: var(--bg-elevated);
  --color-raised: var(--bg-raised);
  --color-overlay: var(--bg-overlay);
  --color-melchior: var(--melchior);
  --color-balthasar: var(--balthasar);
  --color-casper: var(--casper);
  --color-accent: var(--accent);
  --color-warning: var(--warning);
  --color-error: var(--error);
  --color-success: var(--success);
  --color-text-primary: var(--text-primary);
  --color-text-secondary: var(--text-secondary);
  --color-text-muted: var(--text-muted);
  --color-border-dim: var(--border-dim);
  --font-sans: 'IBM Plex Sans', system-ui, sans-serif;
  --font-mono: 'JetBrains Mono', monospace;
}

* {
  box-sizing: border-box;
}

html, body, #root {
  height: 100%;
  margin: 0;
  padding: 0;
  background-color: var(--bg-base);
  color: var(--text-primary);
  font-family: var(--font-sans);
}

::-webkit-scrollbar {
  width: 6px;
  height: 6px;
}
::-webkit-scrollbar-track {
  background: var(--bg-base);
}
::-webkit-scrollbar-thumb {
  background: var(--border-dim);
  border-radius: 3px;
}
::-webkit-scrollbar-thumb:hover {
  background: var(--text-muted);
}
```

- [ ] **Step 4: Add font preload to index.html**

Insert before `</head>` in `frontend/index.html`:
```html
    <link rel="preconnect" href="https://fonts.googleapis.com" />
    <link rel="preconnect" href="https://fonts.gstatic.com" crossorigin />
    <link
      href="https://fonts.googleapis.com/css2?family=IBM+Plex+Sans:wght@400;500;600&family=JetBrains+Mono:wght@400;500&display=swap"
      rel="stylesheet"
    />
```

- [ ] **Step 5: Create MAGI favicon SVG**

```svg
<!-- frontend/public/magi.svg -->
<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 32 32">
  <rect width="32" height="32" rx="4" fill="#0B0F14"/>
  <text x="16" y="23" text-anchor="middle" font-family="monospace" font-size="20" font-weight="bold" fill="#2DD4BF">M</text>
</svg>
```

- [ ] **Step 6: Verify compilation**

```bash
cd frontend && npx tsc --noEmit 2>&1 | head -10
# Expected: no errors (or only unused-variable warnings from scaffold)
```

- [ ] **Step 7: Commit**

```bash
git add frontend/src/styles/tokens.css frontend/src/styles/animations.css \
  frontend/src/styles/globals.css frontend/index.html frontend/public/magi.svg
git commit -m "feat(frontend): add design tokens, animations, fonts, and global styles"
```

---

### Task 4: Type definitions

**Files:**
- Create: `frontend/src/types/case.ts`
- Create: `frontend/src/types/agent.ts`
- Create: `frontend/src/types/evidence.ts`
- Create: `frontend/src/types/event.ts`
- Create: `frontend/src/types/index.ts` (barrel)

**Interfaces:**
- Produces: All TypeScript types consumed by stores and components
- Produces: `Case`, `CaseSummary`, `CaseStatus`, `AgentId`, `AgentSnapshot`, `EvidenceRecord`, `Claim`, `Vote`, `MagiEvent`, `EventFilter`

- [ ] **Step 1: Create case types**

```ts
// frontend/src/types/case.ts
export type CaseStatus =
  | 'DRAFT'
  | 'NORMALIZING'
  | 'CONTEXT_BUILDING'
  | 'RETRIEVING_MEMORY'
  | 'INVESTIGATING'
  | 'EVIDENCE_GATING'
  | 'COLLECTING_VOTES'
  | 'CONSENSUS_CHECK'
  | 'DEBATING'
  | 'REFLECTING'
  | 'REVOTING'
  | 'RESOLVING'
  | 'GENERATING_REPORT'
  | 'SAVING_MEMORY'
  | 'EVALUATING'
  | 'RESOLVED';

// Human-readable status labels for UI
export const CASE_STATUS_LABELS: Record<CaseStatus, string> = {
  DRAFT: 'Draft',
  NORMALIZING: 'Normalizing',
  CONTEXT_BUILDING: 'Building Context',
  RETRIEVING_MEMORY: 'Retrieving Memory',
  INVESTIGATING: 'Investigating',
  EVIDENCE_GATING: 'Evidence Gate',
  COLLECTING_VOTES: 'Collecting Votes',
  CONSENSUS_CHECK: 'Consensus Check',
  DEBATING: 'Debating',
  REFLECTING: 'Reflecting',
  REVOTING: 'Re-voting',
  RESOLVING: 'Resolving',
  GENERATING_REPORT: 'Generating Report',
  SAVING_MEMORY: 'Saving Memory',
  EVALUATING: 'Evaluating',
  RESOLVED: 'Resolved',
};

export interface Case {
  id: string;
  question: string;
  background: string;
  constraints: Constraint[];
  status: CaseStatus;
  round: number;
  consensus: ConsensusState | null;
  confidence: number;
  createdAt: string;
  updatedAt: string;
}

export interface CaseSummary {
  id: string;
  question: string;
  status: CaseStatus;
  round: number;
  createdAt: string;
  pinned: boolean;
}

export interface Constraint {
  label: string;
  value: string;
}

export interface ConsensusState {
  approve: number;
  reject: number;
  abstain: number;
  majority: 'Approve' | 'Reject' | 'Tie';
  needReflection: boolean;
}
```

- [ ] **Step 2: Create agent types**

```ts
// frontend/src/types/agent.ts

export type AgentId = 'melchior' | 'balthasar' | 'casper';

export const AGENT_NAMES: Record<AgentId, string> = {
  melchior: 'MELCHIOR',
  balthasar: 'BALTHASAR',
  casper: 'CASPER',
};

export const AGENT_ROLES: Record<AgentId, string> = {
  melchior: 'Scientist — Logic & Analysis',
  balthasar: 'Protector — Risk & Safety',
  casper: 'Innovator — Opportunity & Vision',
};

export const AGENT_COLORS: Record<AgentId, string> = {
  melchior: 'var(--melchior)',
  balthasar: 'var(--balthasar)',
  casper: 'var(--casper)',
};

export type AgentStatus = 'idle' | 'running' | 'waiting' | 'completed' | 'error';

export interface ToolCall {
  name: string;
  params: Record<string, string>;
  result: string | null;
  timestamp: string;
}

export interface EvidenceRef {
  id: string;
  source: string;
  reliability: number;
}

export interface ClaimRef {
  id: string;
  text: string;
  supports: string[];
  contradicts: string[];
}

export interface AgentVote {
  stance: 'Approve' | 'Reject' | 'Abstain';
  confidence: number;
  reasoning: string;
  dimensions?: Record<string, number>;
}

export interface AgentSnapshot {
  agentId: AgentId;
  status: AgentStatus;
  step: number;
  maxSteps: number;
  thought: string;
  toolCalls: ToolCall[];
  evidence: EvidenceRef[];
  claims: ClaimRef[];
  vote: AgentVote | null;
}
```

- [ ] **Step 3: Create evidence types**

```ts
// frontend/src/types/evidence.ts
import type { AgentId } from './agent';

export interface EvidenceRecord {
  id: string;
  source: string;
  url: string;
  observation: string;
  reliability: number;
  collectedBy: AgentId;
  timestamp: string;
}

export interface EvidenceNode {
  id: string;
  label: string;
  type: 'evidence' | 'claim' | 'vote';
  evidence?: EvidenceRecord;
  claim?: ClaimNode;
  vote?: VoteNode;
}

export interface ClaimNode {
  id: string;
  text: string;
  agentId: AgentId;
}

export interface VoteNode {
  id: string;
  stance: string;
  agentId: AgentId;
}

export interface EvidenceEdge {
  source: string;
  target: string;
  type: 'supports' | 'contradicts';
}
```

- [ ] **Step 4: Create event types**

```ts
// frontend/src/types/event.ts
import type { AgentId } from './agent';

export type EventType = 'TOOL_CALL' | 'AGENT_STEP' | 'EVIDENCE_CREATED' | 'VOTE_SUBMITTED' | 'CONSENSUS_CHANGED' | 'ROUND_START' | 'DEBATE_START' | 'REFLECTION' | 'RESOLVED' | 'ERROR';

export const EVENT_TYPE_LABELS: Record<EventType, string> = {
  TOOL_CALL: 'Tool Call',
  AGENT_STEP: 'Agent Step',
  EVIDENCE_CREATED: 'Evidence Created',
  VOTE_SUBMITTED: 'Vote Submitted',
  CONSENSUS_CHANGED: 'Consensus Changed',
  ROUND_START: 'Round Start',
  DEBATE_START: 'Debate Start',
  REFLECTION: 'Reflection',
  RESOLVED: 'Resolved',
  ERROR: 'Error',
};

export interface MagiEvent {
  id: string;
  type: EventType;
  timestamp: string;
  agentId?: AgentId;
  message: string;
  data?: Record<string, unknown>;
}

export interface EventFilter {
  tool: boolean;
  agent: boolean;
  evidence: boolean;
  vote: boolean;
}
```

- [ ] **Step 5: Create barrel export**

```ts
// frontend/src/types/index.ts
export * from './case';
export * from './agent';
export * from './evidence';
export * from './event';
```

- [ ] **Step 6: Commit**

```bash
git add frontend/src/types/
git commit -m "feat(frontend): add TypeScript type definitions for Case, Agent, Evidence, Event"
```

---

### Task 5: Mock data factory and MSW handlers

**Files:**
- Create: `frontend/src/mock/data.ts`
- Create: `frontend/src/mock/handlers.ts`
- Create: `frontend/src/mock/browser.ts`

**Interfaces:**
- Produces: `createMockCase()`, `createMockAgents()`, `createMockEvents()`, `createMockEvidence()`
- Produces: MSW `handlers` array, `startWorker()` function
- Consumed by: `main.tsx` (worker start), store tests, component tests

- [ ] **Step 1: Create mock data factory**

```ts
// frontend/src/mock/data.ts
import type { Case, CaseSummary, CaseStatus } from '@/types/case';
import type { AgentId, AgentSnapshot, AgentStatus } from '@/types/agent';
import type { EvidenceRecord } from '@/types/evidence';
import type { MagiEvent, EventType } from '@/types/event';

const SAMPLE_CASE_ID = 'case-001';

export function createMockCase(): Case {
  return {
    id: SAMPLE_CASE_ID,
    question: 'Should we migrate the Java backend to Rust?',
    background: 'Our backend currently runs on Java 17 with Spring Boot. The team has been evaluating Rust for performance improvements and memory safety. We need to decide whether to rewrite the core services in Rust or continue with the current Java stack.',
    constraints: [
      { label: 'Budget', value: '3 months engineering time' },
      { label: 'Deadline', value: 'Q4 2026' },
      { label: 'Team Size', value: '5 engineers' },
      { label: 'Priority', value: 'High' },
    ],
    status: 'INVESTIGATING' as CaseStatus,
    round: 1,
    consensus: null,
    confidence: 0,
    createdAt: '2026-07-18T10:00:00Z',
    updatedAt: '2026-07-18T10:05:00Z',
  };
}

export function createMockCaseList(): CaseSummary[] {
  return [
    { id: 'case-001', question: 'Should we migrate the Java backend to Rust?', status: 'RESOLVED', round: 2, createdAt: '2026-07-18T10:00:00Z', pinned: true },
    { id: 'case-002', question: 'Which cloud provider for ML workloads?', status: 'DEBATING', round: 1, createdAt: '2026-07-17T14:00:00Z', pinned: false },
    { id: 'case-003', question: 'Monorepo vs polyrepo for team of 15?', status: 'RESOLVED', round: 1, createdAt: '2026-07-16T09:00:00Z', pinned: false },
    { id: 'case-004', question: 'Should we adopt event sourcing?', status: 'INVESTIGATING', round: 1, createdAt: '2026-07-15T11:00:00Z', pinned: false },
    { id: 'case-005', question: 'GraphQL or REST for public API?', status: 'RESOLVED', round: 2, createdAt: '2026-07-14T08:00:00Z', pinned: false },
  ];
}

export function createMockAgents(): Record<AgentId, AgentSnapshot> {
  const base: Record<AgentId, AgentSnapshot> = {
    melchior: {
      agentId: 'melchior',
      status: 'completed' as AgentStatus,
      step: 8,
      maxSteps: 12,
      thought: '',
      toolCalls: [],
      evidence: [],
      claims: [],
      vote: null,
    },
    balthasar: {
      agentId: 'balthasar',
      status: 'completed' as AgentStatus,
      step: 10,
      maxSteps: 12,
      thought: '',
      toolCalls: [],
      evidence: [],
      claims: [],
      vote: null,
    },
    casper: {
      agentId: 'casper',
      status: 'running' as AgentStatus,
      step: 6,
      maxSteps: 12,
      thought: '',
      toolCalls: [],
      evidence: [],
      claims: [],
      vote: null,
    },
  };

  base.melchior.thought = 'Analyzing the migration question from a scientific perspective. Rust offers memory safety guarantees that eliminate entire classes of bugs. Benchmark data from comparable migrations shows 30-40% latency reduction. However, the team has 0 Rust experience — the learning curve is real.';
  base.melchior.toolCalls = [
    { name: 'web_search', params: { query: 'Java Spring Boot vs Rust Actix performance benchmarks 2025' }, result: 'Found 23 benchmarks. Average latency reduction: 38%. Memory usage: 4x lower.', timestamp: '2026-07-18T10:01:00Z' },
    { name: 'web_search', params: { query: 'Java to Rust migration case study enterprise' }, result: '3 major case studies found: Discord, Dropbox, Figma. All reported positive outcomes.', timestamp: '2026-07-18T10:02:00Z' },
    { name: 'web_search', params: { query: 'Rust developer availability market survey 2026' }, result: 'Rust developer pool growing 40% YoY. Still ~15% of Java pool.', timestamp: '2026-07-18T10:03:00Z' },
  ];
  base.melchior.evidence = [
    { id: 'EV-001', source: 'Web Search', reliability: 0.91 },
    { id: 'EV-002', source: 'Web Search', reliability: 0.85 },
    { id: 'EV-003', source: 'Web Search', reliability: 0.78 },
  ];
  base.melchior.claims = [
    { id: 'CL-001', text: 'Rust reduces backend latency by 30-40% compared to Java', supports: ['CL-003'], contradicts: [] },
    { id: 'CL-002', text: 'Rust memory safety eliminates null pointer and concurrency bugs', supports: ['CL-003'], contradicts: [] },
  ];
  base.melchior.vote = { stance: 'Approve', confidence: 78, reasoning: 'Performance gains are substantial and well-documented. The migration is technically sound.', dimensions: { Correctness: 92, Efficiency: 88, Risk: 45 } };

  base.balthasar.thought = 'Risk assessment underway. The team has no Rust experience — this is the primary risk factor. Migration cost estimates from comparable projects range from 6-12 months. Current Java system is stable. What is the failure mode if the migration goes over budget?';
  base.balthasar.toolCalls = [
    { name: 'web_search', params: { query: 'failed Java to Rust migration postmortem' }, result: 'Found 7 postmortems. Common failure mode: timeline underestimation, skill gap.', timestamp: '2026-07-18T10:01:00Z' },
    { name: 'web_search', params: { query: 'Rust migration cost overrun statistics' }, result: 'Average overrun: 40% of initial estimate. Top cause: learning curve.', timestamp: '2026-07-18T10:02:00Z' },
  ];
  base.balthasar.evidence = [
    { id: 'EV-004', source: 'Web Search', reliability: 0.82 },
    { id: 'EV-005', source: 'Web Search', reliability: 0.88 },
  ];
  base.balthasar.claims = [
    { id: 'CL-003', text: 'Migration is feasible and beneficial with proper planning', supports: [], contradicts: ['CL-004'] },
    { id: 'CL-004', text: 'Team skill gap makes the migration too risky within budget', supports: [], contradicts: ['CL-003'] },
  ];
  base.balthasar.vote = { stance: 'Reject', confidence: 72, reasoning: 'Risk of timeline overrun and team skill gap outweigh benefits. Recommend incremental Rust adoption instead of full migration.', dimensions: { Correctness: 70, Efficiency: 45, Risk: 85 } };

  base.casper.thought = 'Exploring opportunity space. Rust opens up WASM edge computing and embedded systems — entirely new product possibilities. What if we use this migration to also modernize the architecture toward event-driven? Could this be a competitive advantage?';
  base.casper.toolCalls = [
    { name: 'web_search', params: { query: 'Rust WASM edge computing production use cases 2026' }, result: 'Cloudflare Workers, Fastly Compute@Edge, and AWS Lambda all support Rust. Growing ecosystem.', timestamp: '2026-07-18T10:04:00Z' },
  ];
  base.casper.evidence = [
    { id: 'EV-006', source: 'Web Search', reliability: 0.80 },
  ];
  base.casper.claims = [
    { id: 'CL-005', text: 'Rust migration enables WASM edge computing expansion', supports: ['CL-003'], contradicts: [] },
  ];
  base.casper.vote = null;

  return base;
}

export function createMockEvidence(): EvidenceRecord[] {
  return [
    { id: 'EV-001', source: 'Web Search', url: 'https://benchmarks.example.com/java-vs-rust-2025', observation: 'Rust Actix-web outperformed Spring Boot by 38% avg latency. Memory footprint was 4x smaller. Tests conducted on 32-core ARM64 machines.', reliability: 0.91, collectedBy: 'melchior', timestamp: '2026-07-18T10:01:30Z' },
    { id: 'EV-002', source: 'Web Search', url: 'https://case-studies.example.com/discord-rust-migration', observation: 'Discord migrated Go→Rust for their media service. 10x latency improvement. Go was already faster than Java, so Java→Rust may see even bigger gains.', reliability: 0.85, collectedBy: 'melchior', timestamp: '2026-07-18T10:02:30Z' },
    { id: 'EV-003', source: 'Web Search', url: 'https://survey.example.com/rust-devs-2026', observation: 'Rust developer pool has grown 40% year-over-year. Still approximately 15% the size of the Java developer pool.', reliability: 0.78, collectedBy: 'melchior', timestamp: '2026-07-18T10:03:30Z' },
    { id: 'EV-004', source: 'Web Search', url: 'https://postmortems.example.com/rust-migration-failures', observation: '7 failed migration case studies analyzed. Primary failure modes: timeline underestimation (71%), insufficient Rust training (57%), attempting full rewrite instead of incremental (43%).', reliability: 0.82, collectedBy: 'balthasar', timestamp: '2026-07-18T10:01:45Z' },
    { id: 'EV-005', source: 'Web Search', url: 'https://reports.example.com/migration-cost-overruns', observation: 'Average Rust migration overrun was 40% above initial estimates. Teams with prior systems programming experience reported 15% overrun vs 60% for teams without.', reliability: 0.88, collectedBy: 'balthasar', timestamp: '2026-07-18T10:02:45Z' },
    { id: 'EV-006', source: 'Web Search', url: 'https://edge-computing.example.com/rust-wasm-2026', observation: 'Cloudflare Workers, Fastly, and AWS Lambda all support Rust for edge functions. WASM-based edge compute market growing at 65% CAGR.', reliability: 0.80, collectedBy: 'casper', timestamp: '2026-07-18T10:04:30Z' },
    { id: 'EV-007', source: 'Web Search', url: 'https://security.example.com/rust-cve-analysis', observation: 'Rust codebases have 70% fewer memory-safety CVEs compared to equivalent Java codebases. Borrow checker eliminates use-after-free, double-free, and data races at compile time.', reliability: 0.94, collectedBy: 'melchior', timestamp: '2026-07-18T10:05:00Z' },
    { id: 'EV-008', source: 'Web Search', url: 'https://surveys.example.com/rust-productivity', observation: 'Developer productivity in Rust reaches parity with Java after approximately 3-6 months of learning. Initial 2-month period shows 50% productivity dip.', reliability: 0.87, collectedBy: 'balthasar', timestamp: '2026-07-18T10:06:00Z' },
  ];
}

export function createMockEvents(): MagiEvent[] {
  return [
    { id: 'evt-001', type: 'ROUND_START', timestamp: '2026-07-18T10:00:00Z', message: 'Round 1 started' },
    { id: 'evt-002', type: 'TOOL_CALL', timestamp: '2026-07-18T10:01:00Z', agentId: 'melchior', message: 'Melchior called web_search: Java vs Rust benchmarks', data: { tool: 'web_search' } },
    { id: 'evt-003', type: 'TOOL_CALL', timestamp: '2026-07-18T10:01:00Z', agentId: 'balthasar', message: 'Balthasar called web_search: migration failure postmortems', data: { tool: 'web_search' } },
    { id: 'evt-004', type: 'EVIDENCE_CREATED', timestamp: '2026-07-18T10:01:30Z', agentId: 'melchior', message: 'EV-001 created by Melchior (reliability: 0.91)', data: { evidenceId: 'EV-001' } },
    { id: 'evt-005', type: 'EVIDENCE_CREATED', timestamp: '2026-07-18T10:01:45Z', agentId: 'balthasar', message: 'EV-004 created by Balthasar (reliability: 0.82)', data: { evidenceId: 'EV-004' } },
    { id: 'evt-006', type: 'TOOL_CALL', timestamp: '2026-07-18T10:02:00Z', agentId: 'melchior', message: 'Melchior called web_search: Rust migration case studies', data: { tool: 'web_search' } },
    { id: 'evt-007', type: 'TOOL_CALL', timestamp: '2026-07-18T10:02:00Z', agentId: 'balthasar', message: 'Balthasar called web_search: cost overrun statistics', data: { tool: 'web_search' } },
    { id: 'evt-008', type: 'EVIDENCE_CREATED', timestamp: '2026-07-18T10:02:30Z', agentId: 'melchior', message: 'EV-002 created by Melchior (reliability: 0.85)', data: { evidenceId: 'EV-002' } },
    { id: 'evt-009', type: 'EVIDENCE_CREATED', timestamp: '2026-07-18T10:02:45Z', agentId: 'balthasar', message: 'EV-005 created by Balthasar (reliability: 0.88)', data: { evidenceId: 'EV-005' } },
    { id: 'evt-010', type: 'VOTE_SUBMITTED', timestamp: '2026-07-18T10:05:00Z', agentId: 'melchior', message: 'Melchior voted APPROVE (confidence: 78%)', data: { stance: 'Approve', confidence: 78 } },
    { id: 'evt-011', type: 'VOTE_SUBMITTED', timestamp: '2026-07-18T10:05:30Z', agentId: 'balthasar', message: 'Balthasar voted REJECT (confidence: 72%)', data: { stance: 'Reject', confidence: 72 } },
    { id: 'evt-012', type: 'CONSENSUS_CHANGED', timestamp: '2026-07-18T10:05:30Z', message: 'Consensus: 1:1 (Casper pending)', data: { approve: 1, reject: 1 } },
    { id: 'evt-013', type: 'DEBATE_START', timestamp: '2026-07-18T10:06:00Z', message: 'Debate initiated between Melchior and Balthasar', data: { claims: ['CL-003', 'CL-004'] } },
    { id: 'evt-014', type: 'AGENT_STEP', timestamp: '2026-07-18T10:06:30Z', agentId: 'casper', message: 'Casper step 5: searching for WASM edge computing use cases', data: { step: 5 } },
    { id: 'evt-015', type: 'EVIDENCE_CREATED', timestamp: '2026-07-18T10:04:30Z', agentId: 'casper', message: 'EV-006 created by Casper (reliability: 0.80)', data: { evidenceId: 'EV-006' } },
  ];
}
```

- [ ] **Step 2: Create MSW handlers**

```ts
// frontend/src/mock/handlers.ts
import { http, HttpResponse } from 'msw';
import { createMockCase, createMockCaseList } from './data';

const CASE = createMockCase();
const CASE_LIST = createMockCaseList();

export const handlers = [
  // GET /api/v1/cases
  http.get('/api/v1/cases', () => {
    return HttpResponse.json({ cases: CASE_LIST });
  }),

  // GET /api/v1/cases/:id
  http.get('/api/v1/cases/:id', ({ params }) => {
    if (params.id === CASE.id) {
      return HttpResponse.json(CASE);
    }
    return HttpResponse.json({ error: 'Case not found' }, { status: 404 });
  }),

  // POST /api/v1/cases
  http.post('/api/v1/cases', async ({ request }) => {
    const body = await request.json();
    return HttpResponse.json({ ...CASE, ...(body as object), id: 'case-new', status: 'DRAFT' }, { status: 201 });
  }),

  // GET /api/v1/cases/:id/stream (SSE — mock as a simple event endpoint for Phase 1)
  http.get('/api/v1/cases/:id/stream', () => {
    return HttpResponse.json({ message: 'SSE endpoint — mock not yet connected' }, { status: 200 });
  }),
];
```

- [ ] **Step 3: Create MSW browser worker setup**

```ts
// frontend/src/mock/browser.ts
import { setupWorker } from 'msw/browser';
import { handlers } from './handlers';

export const worker = setupWorker(...handlers);

export async function startWorker(): Promise<void> {
  if (import.meta.env.DEV) {
    await worker.start({
      onUnhandledRequest: 'bypass',
    });
    console.log('[MSW] Mock Service Worker started');
  }
}
```

- [ ] **Step 4: Generate MSW service worker file**

```bash
cd frontend && npx msw init public/ --save
```

- [ ] **Step 5: Commit**

```bash
git add frontend/src/mock/ frontend/public/mockServiceWorker.js
git commit -m "feat(frontend): add mock data factory and MSW handlers"
```

---

### Task 6: Zustand stores (caseStore, agentStore, eventStore, uiStore)

**Files:**
- Create: `frontend/src/stores/caseStore.ts`
- Create: `frontend/src/stores/agentStore.ts`
- Create: `frontend/src/stores/eventStore.ts`
- Create: `frontend/src/stores/uiStore.ts`
- Create: `frontend/src/stores/index.ts` (barrel)
- Create: `frontend/src/stores/__tests__/caseStore.test.ts`
- Create: `frontend/src/stores/__tests__/agentStore.test.ts`
- Create: `frontend/src/stores/__tests__/eventStore.test.ts`
- Create: `frontend/src/stores/__tests__/uiStore.test.ts`

**Interfaces:**
- Consumes: `Case`, `CaseSummary`, `AgentSnapshot`, `MagiEvent`, `EventFilter` types from Task 4; mock data from Task 5
- Produces: `useCaseStore`, `useAgentStore`, `useEventStore`, `useUiStore` hooks

- [ ] **Step 1: Create caseStore**

```ts
// frontend/src/stores/caseStore.ts
import { create } from 'zustand';
import type { Case, CaseSummary } from '@/types/case';

interface CaseState {
  case: Case | null;
  cases: CaseSummary[];
  loading: boolean;
  error: string | null;
  loadCase: (c: Case) => void;
  loadCaseList: (list: CaseSummary[]) => void;
  updateCaseStatus: (status: Case['status'], round: number) => void;
  updateConsensus: (consensus: Case['consensus'], confidence: number) => void;
  setLoading: (loading: boolean) => void;
  setError: (error: string | null) => void;
}

export const useCaseStore = create<CaseState>((set) => ({
  case: null,
  cases: [],
  loading: false,
  error: null,

  loadCase: (c) => set({ case: c, loading: false }),

  loadCaseList: (list) => set({ cases: list }),

  updateCaseStatus: (status, round) =>
    set((s) => ({ case: s.case ? { ...s.case, status, round } : null })),

  updateConsensus: (consensus, confidence) =>
    set((s) => ({ case: s.case ? { ...s.case, consensus, confidence } : null })),

  setLoading: (loading) => set({ loading }),

  setError: (error) => set({ error }),
}));
```

- [ ] **Step 2: Create agentStore**

```ts
// frontend/src/stores/agentStore.ts
import { create } from 'zustand';
import type { AgentId, AgentSnapshot, AgentStatus } from '@/types/agent';

interface AgentState {
  agents: Record<AgentId, AgentSnapshot | null>;
  loadAgents: (agents: Record<AgentId, AgentSnapshot>) => void;
  patchAgent: (id: AgentId, patch: Partial<AgentSnapshot>) => void;
  updateAgentStatus: (id: AgentId, status: AgentStatus) => void;
  resetAgents: () => void;
}

const empty: Record<AgentId, AgentSnapshot | null> = {
  melchior: null,
  balthasar: null,
  casper: null,
};

export const useAgentStore = create<AgentState>((set) => ({
  agents: empty,

  loadAgents: (agents) => set({ agents }),

  patchAgent: (id, patch) =>
    set((s) => ({
      agents: {
        ...s.agents,
        [id]: s.agents[id] ? { ...s.agents[id]!, ...patch } : null,
      },
    })),

  updateAgentStatus: (id, status) =>
    set((s) => ({
      agents: {
        ...s.agents,
        [id]: s.agents[id] ? { ...s.agents[id]!, status } : null,
      },
    })),

  resetAgents: () => set({ agents: empty }),
}));
```

- [ ] **Step 3: Create eventStore**

```ts
// frontend/src/stores/eventStore.ts
import { create } from 'zustand';
import type { MagiEvent, EventFilter } from '@/types/event';

const defaultFilters: EventFilter = { tool: true, agent: true, evidence: true, vote: true };

interface EventState {
  events: MagiEvent[];
  filters: EventFilter;
  pushEvent: (event: MagiEvent) => void;
  loadEvents: (events: MagiEvent[]) => void;
  clearEvents: () => void;
  toggleFilter: (key: keyof EventFilter) => void;
}

export const useEventStore = create<EventState>((set) => ({
  events: [],
  filters: { ...defaultFilters },

  pushEvent: (event) =>
    set((s) => ({ events: [...s.events, event] })),

  loadEvents: (events) => set({ events }),

  clearEvents: () => set({ events: [] }),

  toggleFilter: (key) =>
    set((s) => ({
      filters: { ...s.filters, [key]: !s.filters[key] },
    })),
}));
```

- [ ] **Step 4: Create uiStore**

```ts
// frontend/src/stores/uiStore.ts
import { create } from 'zustand';
import type { AgentId } from '@/types/agent';

export type SelectionType = 'evidence' | 'vote' | 'agent' | 'event';

interface Selection {
  type: SelectionType;
  id: string;
  data?: Record<string, unknown>;
}

interface UIState {
  selected: Selection | null;
  expandedAgent: AgentId | null;
  sidebarCollapsed: boolean;
  timelineCollapsed: boolean;
  timelineHeight: number;
  select: (sel: Selection) => void;
  clearSelection: () => void;
  setExpandedAgent: (id: AgentId | null) => void;
  toggleSidebar: () => void;
  toggleTimeline: () => void;
  setTimelineHeight: (h: number) => void;
}

export const useUiStore = create<UIState>((set) => ({
  selected: null,
  expandedAgent: null,
  sidebarCollapsed: false,
  timelineCollapsed: false,
  timelineHeight: 192,

  select: (sel) => set({ selected: sel }),
  clearSelection: () => set({ selected: null }),
  setExpandedAgent: (id) => set({ expandedAgent: id }),
  toggleSidebar: () => set((s) => ({ sidebarCollapsed: !s.sidebarCollapsed })),
  toggleTimeline: () => set((s) => ({ timelineCollapsed: !s.timelineCollapsed })),
  setTimelineHeight: (h) => set({ timelineHeight: h }),
}));
```

- [ ] **Step 5: Create barrel export**

```ts
// frontend/src/stores/index.ts
export { useCaseStore } from './caseStore';
export { useAgentStore } from './agentStore';
export { useEventStore } from './eventStore';
export { useUiStore } from './uiStore';
export type { SelectionType } from './uiStore';
```

- [ ] **Step 6: Write caseStore test**

```ts
// frontend/src/stores/__tests__/caseStore.test.ts
import { describe, it, expect, beforeEach } from 'vitest';
import { useCaseStore } from '../caseStore';
import type { Case } from '@/types/case';

const mockCase: Case = {
  id: 'test-1',
  question: 'Test question?',
  background: 'Test background',
  constraints: [],
  status: 'DRAFT',
  round: 1,
  consensus: null,
  confidence: 0,
  createdAt: '2026-01-01T00:00:00Z',
  updatedAt: '2026-01-01T00:00:00Z',
};

describe('caseStore', () => {
  beforeEach(() => {
    useCaseStore.setState({
      case: null,
      cases: [],
      loading: false,
      error: null,
    });
  });

  it('loads a case', () => {
    useCaseStore.getState().loadCase(mockCase);
    expect(useCaseStore.getState().case?.id).toBe('test-1');
    expect(useCaseStore.getState().loading).toBe(false);
  });

  it('loads case list', () => {
    useCaseStore.getState().loadCaseList([
      { id: '1', question: 'Q1', status: 'DRAFT', round: 1, createdAt: '', pinned: false },
    ]);
    expect(useCaseStore.getState().cases).toHaveLength(1);
  });

  it('updates case status', () => {
    useCaseStore.getState().loadCase(mockCase);
    useCaseStore.getState().updateCaseStatus('INVESTIGATING', 1);
    expect(useCaseStore.getState().case?.status).toBe('INVESTIGATING');
  });

  it('updates consensus', () => {
    useCaseStore.getState().loadCase(mockCase);
    const consensus = { approve: 2, reject: 1, abstain: 0, majority: 'Approve' as const, needReflection: false };
    useCaseStore.getState().updateConsensus(consensus, 81);
    expect(useCaseStore.getState().case?.consensus?.approve).toBe(2);
    expect(useCaseStore.getState().case?.confidence).toBe(81);
  });
});
```

- [ ] **Step 7: Write agentStore test**

```ts
// frontend/src/stores/__tests__/agentStore.test.ts
import { describe, it, expect, beforeEach } from 'vitest';
import { useAgentStore } from '../agentStore';

describe('agentStore', () => {
  beforeEach(() => {
    useAgentStore.getState().resetAgents();
  });

  it('starts with null agents', () => {
    const { agents } = useAgentStore.getState();
    expect(agents.melchior).toBeNull();
    expect(agents.balthasar).toBeNull();
    expect(agents.casper).toBeNull();
  });

  it('patches agent partial data', () => {
    useAgentStore.getState().patchAgent('melchior', { step: 5, thought: 'Testing...' });
    const m = useAgentStore.getState().agents.melchior;
    expect(m?.step).toBe(5);
    expect(m?.thought).toBe('Testing...');
  });

  it('loads all agents at once', () => {
    const mock = {
      melchior: { agentId: 'melchior' as const, status: 'running' as const, step: 1, maxSteps: 10, thought: '', toolCalls: [], evidence: [], claims: [], vote: null },
      balthasar: { agentId: 'balthasar' as const, status: 'idle' as const, step: 0, maxSteps: 10, thought: '', toolCalls: [], evidence: [], claims: [], vote: null },
      casper: { agentId: 'casper' as const, status: 'idle' as const, step: 0, maxSteps: 10, thought: '', toolCalls: [], evidence: [], claims: [], vote: null },
    };
    useAgentStore.getState().loadAgents(mock);
    expect(useAgentStore.getState().agents.melchior?.status).toBe('running');
  });
});
```

- [ ] **Step 8: Write eventStore test**

```ts
// frontend/src/stores/__tests__/eventStore.test.ts
import { describe, it, expect, beforeEach } from 'vitest';
import { useEventStore } from '../eventStore';
import type { MagiEvent } from '@/types/event';

const ev: MagiEvent = { id: 'e1', type: 'TOOL_CALL', timestamp: '', message: 'Test event' };

describe('eventStore', () => {
  beforeEach(() => {
    useEventStore.setState({ events: [], filters: { tool: true, agent: true, evidence: true, vote: true } });
  });

  it('pushes events', () => {
    useEventStore.getState().pushEvent(ev);
    expect(useEventStore.getState().events).toHaveLength(1);
  });

  it('toggles filter', () => {
    useEventStore.getState().toggleFilter('tool');
    expect(useEventStore.getState().filters.tool).toBe(false);
    useEventStore.getState().toggleFilter('tool');
    expect(useEventStore.getState().filters.tool).toBe(true);
  });

  it('clears events', () => {
    useEventStore.getState().pushEvent(ev);
    useEventStore.getState().clearEvents();
    expect(useEventStore.getState().events).toHaveLength(0);
  });
});
```

- [ ] **Step 9: Write uiStore test**

```ts
// frontend/src/stores/__tests__/uiStore.test.ts
import { describe, it, expect, beforeEach } from 'vitest';
import { useUiStore } from '../uiStore';

describe('uiStore', () => {
  beforeEach(() => {
    useUiStore.setState({ selected: null, expandedAgent: null, sidebarCollapsed: false, timelineCollapsed: false, timelineHeight: 192 });
  });

  it('selects an object', () => {
    useUiStore.getState().select({ type: 'evidence', id: 'EV-001' });
    expect(useUiStore.getState().selected?.id).toBe('EV-001');
  });

  it('clears selection', () => {
    useUiStore.getState().select({ type: 'evidence', id: 'EV-001' });
    useUiStore.getState().clearSelection();
    expect(useUiStore.getState().selected).toBeNull();
  });

  it('toggles sidebar', () => {
    useUiStore.getState().toggleSidebar();
    expect(useUiStore.getState().sidebarCollapsed).toBe(true);
  });
});
```

- [ ] **Step 10: Run tests**

```bash
cd frontend && npx vitest run --reporter=verbose
# Expected: 11 tests passed (4 + 3 + 3 + 1 from the 4 test files)
```

- [ ] **Step 11: Commit**

```bash
git add frontend/src/stores/
git commit -m "feat(frontend): add Zustand stores (case, agent, event, ui) with tests"
```

---

### Task 7: Shared UI components (GlowBorder, ScanLine, PulseDot, StatusBadge, MonoText)

**Files:**
- Create: `frontend/src/components/shared/GlowBorder.tsx`
- Create: `frontend/src/components/shared/ScanLine.tsx`
- Create: `frontend/src/components/shared/PulseDot.tsx`
- Create: `frontend/src/components/shared/StatusBadge.tsx`
- Create: `frontend/src/components/shared/MonoText.tsx`
- Create: `frontend/src/components/shared/AgentAvatar.tsx`
- Create: `frontend/src/components/shared/index.ts` (barrel)
- Create: `frontend/src/components/ui/Button.tsx`
- Create: `frontend/src/components/ui/Card.tsx`
- Create: `frontend/src/components/ui/index.ts`

**Interfaces:**
- Produces: Shared visual components used across all layout and workspace components
- Consumed by: Layout shell (Task 8), workspace components (Tasks 10-14)

- [ ] **Step 1: Create GlowBorder**

```tsx
// frontend/src/components/shared/GlowBorder.tsx
import type { ReactNode } from 'react';

interface GlowBorderProps {
  children: ReactNode;
  color?: string;
  className?: string;
  active?: boolean;
}

export default function GlowBorder({ children, color = 'var(--border-glow)', className = '', active = false }: GlowBorderProps) {
  return (
    <div
      className={`relative rounded-lg border ${active ? 'border-[var(--border-active)]' : 'border-border-dim'} ${className}`}
      style={{
        boxShadow: active ? `0 0 12px ${color}` : 'none',
        borderColor: active ? 'var(--border-active)' : 'var(--border-dim)',
      }}
    >
      {children}
    </div>
  );
}
```

- [ ] **Step 2: Create ScanLine**

```tsx
// frontend/src/components/shared/ScanLine.tsx
interface ScanLineProps {
  className?: string;
  height?: number;
}

export default function ScanLine({ className = '', height = 2 }: ScanLineProps) {
  return (
    <div
      className={`pointer-events-none absolute inset-x-0 animate-scanline ${className}`}
      style={{
        height: `${height}px`,
        background: 'linear-gradient(180deg, transparent 0%, rgba(74, 158, 255, 0.06) 50%, transparent 100%)',
      }}
    />
  );
}
```

- [ ] **Step 3: Create PulseDot**

```tsx
// frontend/src/components/shared/PulseDot.tsx
interface PulseDotProps {
  color?: string;
  size?: number;
  className?: string;
}

export default function PulseDot({ color = 'var(--accent)', size = 8, className = '' }: PulseDotProps) {
  return (
    <span
      className={`inline-block rounded-full animate-pulse-glow ${className}`}
      style={{ width: size, height: size, backgroundColor: color, boxShadow: `0 0 6px ${color}` }}
    />
  );
}
```

- [ ] **Step 4: Create StatusBadge**

```tsx
// frontend/src/components/shared/StatusBadge.tsx
interface StatusBadgeProps {
  status: string;
  color?: string;
  className?: string;
}

export default function StatusBadge({ status, color, className = '' }: StatusBadgeProps) {
  return (
    <span
      className={`inline-flex items-center gap-1.5 rounded-full px-2.5 py-0.5 font-mono text-xs font-medium tracking-wider uppercase ${className}`}
      style={{
        color: color || 'var(--text-secondary)',
        backgroundColor: color ? `${color}15` : 'var(--bg-raised)',
        border: `1px solid ${color ? `${color}30` : 'var(--border-dim)'}`,
      }}
    >
      <span
        className="inline-block rounded-full"
        style={{ width: 6, height: 6, backgroundColor: color || 'var(--text-muted)' }}
      />
      {status}
    </span>
  );
}
```

- [ ] **Step 5: Create MonoText**

```tsx
// frontend/src/components/shared/MonoText.tsx
import type { ReactNode } from 'react';

interface MonoTextProps {
  children: ReactNode;
  className?: string;
  muted?: boolean;
  size?: 'sm' | 'md' | 'lg';
}

const sizes = { sm: 'text-xs', md: 'text-sm', lg: 'text-base' };

export default function MonoText({ children, className = '', muted = false, size = 'md' }: MonoTextProps) {
  return (
    <span
      className={`font-mono ${sizes[size]} ${muted ? 'text-text-muted' : 'text-text-secondary'} ${className}`}
    >
      {children}
    </span>
  );
}
```

- [ ] **Step 6: Create AgentAvatar**

```tsx
// frontend/src/components/shared/AgentAvatar.tsx
import type { AgentId } from '@/types/agent';
import { AGENT_NAMES } from '@/types/agent';

interface AgentAvatarProps {
  agentId: AgentId;
  size?: number;
}

export default function AgentAvatar({ agentId, size = 24 }: AgentAvatarProps) {
  const colors: Record<AgentId, string> = {
    melchior: 'var(--melchior)',
    balthasar: 'var(--balthasar)',
    casper: 'var(--casper)',
  };

  return (
    <span
      className="inline-flex items-center justify-center rounded-full font-mono text-xs font-bold"
      style={{
        width: size,
        height: size,
        backgroundColor: `${colors[agentId]}20`,
        color: colors[agentId],
        border: `1px solid ${colors[agentId]}40`,
      }}
    >
      {AGENT_NAMES[agentId][0]}
    </span>
  );
}
```

- [ ] **Step 7: Create Button**

```tsx
// frontend/src/components/ui/Button.tsx
import type { ReactNode, ButtonHTMLAttributes } from 'react';

interface ButtonProps extends ButtonHTMLAttributes<HTMLButtonElement> {
  children: ReactNode;
  variant?: 'default' | 'ghost' | 'accent';
  size?: 'sm' | 'md';
}

export default function Button({ children, variant = 'default', size = 'md', className = '', ...props }: ButtonProps) {
  const base = 'inline-flex items-center justify-center gap-1.5 rounded-md font-mono text-xs font-medium tracking-wider uppercase transition-colors cursor-pointer disabled:opacity-40 disabled:cursor-not-allowed';
  const variants = {
    default: 'bg-raised text-text-secondary border border-border-dim hover:bg-overlay hover:text-text-primary',
    ghost: 'text-text-muted hover:text-text-primary hover:bg-raised',
    accent: 'text-accent border border-[var(--accent)] hover:bg-[var(--accent)]/10',
  };
  const sizes = { sm: 'px-2 py-1 text-[10px]', md: 'px-3 py-1.5' };

  return (
    <button className={`${base} ${variants[variant]} ${sizes[size]} ${className}`} {...props}>
      {children}
    </button>
  );
}
```

- [ ] **Step 8: Create Card**

```tsx
// frontend/src/components/ui/Card.tsx
import type { ReactNode } from 'react';

interface CardProps {
  children: ReactNode;
  className?: string;
  padded?: boolean;
}

export default function Card({ children, className = '', padded = true }: CardProps) {
  return (
    <div className={`rounded-lg border border-border-dim bg-elevated ${padded ? 'p-4' : ''} ${className}`}>
      {children}
    </div>
  );
}
```

- [ ] **Step 9: Create barrel exports**

```tsx
// frontend/src/components/shared/index.ts
export { default as GlowBorder } from './GlowBorder';
export { default as ScanLine } from './ScanLine';
export { default as PulseDot } from './PulseDot';
export { default as StatusBadge } from './StatusBadge';
export { default as MonoText } from './MonoText';
export { default as AgentAvatar } from './AgentAvatar';
```

```tsx
// frontend/src/components/ui/index.ts
export { default as Button } from './Button';
export { default as Card } from './Card';
```

- [ ] **Step 10: Commit**

```bash
git add frontend/src/components/shared/ frontend/src/components/ui/
git commit -m "feat(frontend): add shared UI components (GlowBorder, ScanLine, PulseDot, StatusBadge, MonoText, AgentAvatar, Button, Card)"
```

---

### Task 8: Layout shell (TopNav, LeftNav, AppShell)

**Files:**
- Create: `frontend/src/components/layout/TopNav.tsx`
- Create: `frontend/src/components/layout/LeftNav.tsx`
- Create: `frontend/src/components/layout/AppShell.tsx`
- Create: `frontend/src/components/layout/index.ts` (barrel)

**Interfaces:**
- Produces: `AppShell` component (shell for all pages: TopNav + LeftNav + `<Outlet/>` + empty placeholder for RightInspector/BottomTimeline)
- Consumes: Router `<Outlet/>` (defined in Task 9)

- [ ] **Step 1: Create TopNav**

```tsx
// frontend/src/components/layout/TopNav.tsx
import { NavLink } from 'react-router-dom';
import { Activity, Database, Play, BarChart3, Wrench, Settings, Layers } from 'lucide-react';
import { PulseDot, MonoText } from '@/components/shared';

const NAV_ITEMS = [
  { to: '/', icon: Activity, label: 'Decision' },
  { to: '/memory', icon: Layers, label: 'Memory' },
  { to: '/replay', icon: Play, label: 'Replay' },
  { to: '/evaluation', icon: BarChart3, label: 'Evaluation' },
  { to: '/dataset', icon: Database, label: 'Dataset' },
  { to: '/tools', icon: Wrench, label: 'Tools' },
  { to: '/settings', icon: Settings, label: 'Settings' },
];

export default function TopNav() {
  return (
    <header className="flex h-12 items-center justify-between border-b border-border-dim bg-base px-4 shrink-0">
      <div className="flex items-center gap-6">
        {/* Logo */}
        <NavLink to="/" className="flex items-center gap-2">
          <span className="font-mono text-lg font-bold text-accent tracking-widest">MAGI</span>
        </NavLink>

        {/* Nav Items */}
        <nav className="flex items-center gap-1">
          {NAV_ITEMS.map(({ to, icon: Icon, label }) => (
            <NavLink
              key={to}
              to={to}
              className={({ isActive }) =>
                `flex items-center gap-1.5 rounded-md px-3 py-1.5 font-mono text-xs font-medium tracking-wider uppercase transition-colors ${
                  isActive
                    ? 'bg-accent/10 text-accent'
                    : 'text-text-muted hover:text-text-secondary hover:bg-raised'
                }`
              }
            >
              <Icon size={14} />
              {label}
            </NavLink>
          ))}
        </nav>
      </div>

      {/* System Status Bar */}
      <div className="flex items-center gap-4 font-mono text-xs text-text-muted">
        <div className="flex items-center gap-1.5">
          <MonoText size="sm">Claude Opus 4</MonoText>
        </div>
        <MonoText size="sm" muted>$0.14</MonoText>
        <MonoText size="sm" muted>1450 Tokens</MonoText>
        <div className="flex items-center gap-1.5">
          <PulseDot color="var(--accent)" size={6} />
          <MonoText size="sm" muted>Running</MonoText>
        </div>
        <div className="flex items-center gap-1.5 text-text-muted">
          <span className="inline-block w-1.5 h-1.5 rounded-full bg-success" />
          <MonoText size="sm" muted>Connected</MonoText>
        </div>
      </div>
    </header>
  );
}
```

- [ ] **Step 2: Create LeftNav**

```tsx
// frontend/src/components/layout/LeftNav.tsx
import { NavLink } from 'react-router-dom';
import { Pin, Play, CheckCircle, Archive, Clock, FileText, FlaskConical, Database, History } from 'lucide-react';
import { MonoText } from '@/components/shared';

interface CaseItem {
  id: string;
  question: string;
  status: string;
  pinned?: boolean;
}

interface LeftNavProps {
  cases?: CaseItem[];
}

const SECTIONS = [
  { title: 'Pinned', icon: Pin, filter: (c: CaseItem) => c.pinned },
  { title: 'Running', icon: Play, filter: (c: CaseItem) => c.status === 'INVESTIGATING' || c.status === 'DEBATING' },
  { title: 'Completed', icon: CheckCircle, filter: (c: CaseItem) => c.status === 'RESOLVED' },
  { title: 'Archived', icon: Archive, filter: () => false },
];

const FOOTER_ITEMS = [
  { to: '/templates', icon: FileText, label: 'Templates' },
  { to: '/benchmark', icon: FlaskConical, label: 'Benchmark' },
  { to: '/dataset', icon: Database, label: 'Dataset' },
  { to: '/history', icon: History, label: 'History' },
];

export default function LeftNav({ cases = [] }: LeftNavProps) {
  return (
    <aside className="flex w-56 shrink-0 flex-col border-r border-border-dim bg-base">
      {/* Header */}
      <div className="border-b border-border-dim px-4 py-3">
        <h2 className="font-mono text-xs font-semibold text-text-secondary uppercase tracking-wider">Decision Center</h2>
      </div>

      {/* Case List by Section */}
      <div className="flex-1 overflow-y-auto">
        {SECTIONS.map(({ title, icon: Icon, filter }) => {
          const filtered = cases.filter(filter);
          if (filtered.length === 0 && title !== 'Pinned' && title !== 'Archived') return null;
          return (
            <div key={title} className="border-b border-border-dim last:border-b-0">
              <div className="flex items-center gap-2 px-4 py-2">
                <Icon size={12} className="text-text-muted" />
                <MonoText size="sm" muted>{title}</MonoText>
              </div>
              {filtered.map((c) => (
                <NavLink
                  key={c.id}
                  to={`/case/${c.id}`}
                  className={({ isActive }) =>
                    `block px-6 py-1.5 text-sm transition-colors truncate ${
                      isActive ? 'bg-accent/10 text-accent border-r-2 border-accent' : 'text-text-secondary hover:bg-raised hover:text-text-primary'
                    }`
                  }
                >
                  {c.question.length > 28 ? c.question.slice(0, 28) + '...' : c.question}
                </NavLink>
              ))}
              {filtered.length === 0 && (
                <p className="px-6 py-1.5 text-xs text-text-muted italic">No cases</p>
              )}
            </div>
          );
        })}
      </div>

      {/* Footer */}
      <div className="border-t border-border-dim py-2">
        {FOOTER_ITEMS.map(({ to, icon: Icon, label }) => (
          <NavLink
            key={to}
            to={to}
            className="flex items-center gap-2 px-4 py-1.5 text-xs text-text-muted hover:text-text-secondary hover:bg-raised transition-colors"
          >
            <Icon size={12} />
            {label}
          </NavLink>
        ))}
      </div>
    </aside>
  );
}
```

- [ ] **Step 3: Create AppShell**

```tsx
// frontend/src/components/layout/AppShell.tsx
import { Outlet } from 'react-router-dom';
import TopNav from './TopNav';
import LeftNav from './LeftNav';
import { useCaseStore } from '@/stores';

export default function AppShell() {
  const cases = useCaseStore((s) => s.cases);

  return (
    <div className="flex h-screen flex-col bg-base">
      <TopNav />
      <div className="flex flex-1 overflow-hidden">
        <LeftNav cases={cases} />
        <main className="flex-1 overflow-y-auto bg-base">
          <Outlet />
        </main>
      </div>
    </div>
  );
}
```

- [ ] **Step 4: Create barrel export**

```tsx
// frontend/src/components/layout/index.ts
export { default as TopNav } from './TopNav';
export { default as LeftNav } from './LeftNav';
export { default as AppShell } from './AppShell';
```

- [ ] **Step 5: Commit**

```bash
git add frontend/src/components/layout/
git commit -m "feat(frontend): add layout shell (TopNav, LeftNav, AppShell)"
```

---

### Task 9: Router setup and placeholder pages

**Files:**
- Create: `frontend/src/router.tsx`
- Create: `frontend/src/pages/DecisionWorkspace.tsx` (minimal stub)
- Create: `frontend/src/pages/PlaceholderPage.tsx`
- Create: `frontend/src/pages/index.ts`
- Modify: `frontend/src/App.tsx`
- Modify: `frontend/src/main.tsx` (add MSW start + Router)

**Interfaces:**
- Produces: Full client-side routing, all 7 module routes, placeholder pages for deferred content

- [ ] **Step 1: Create router**

```tsx
// frontend/src/router.tsx
import { createBrowserRouter } from 'react-router-dom';
import { AppShell } from '@/components/layout';
import DecisionWorkspace from '@/pages/DecisionWorkspace';
import PlaceholderPage from '@/pages/PlaceholderPage';

export const router = createBrowserRouter([
  {
    path: '/',
    element: <AppShell />,
    children: [
      { index: true, element: <DecisionWorkspace /> },
      { path: 'case/:caseId', element: <DecisionWorkspace /> },
      { path: 'memory', element: <PlaceholderPage title="Memory" /> },
      { path: 'replay', element: <PlaceholderPage title="Replay" /> },
      { path: 'evaluation', element: <PlaceholderPage title="Evaluation" /> },
      { path: 'dataset', element: <PlaceholderPage title="Dataset" /> },
      { path: 'tools', element: <PlaceholderPage title="Tools" /> },
      { path: 'settings', element: <PlaceholderPage title="Settings" /> },
      { path: 'templates', element: <PlaceholderPage title="Templates" /> },
      { path: 'benchmark', element: <PlaceholderPage title="Benchmark" /> },
      { path: 'history', element: <PlaceholderPage title="History" /> },
    ],
  },
]);
```

- [ ] **Step 2: Create PlaceholderPage**

```tsx
// frontend/src/pages/PlaceholderPage.tsx
import { Card } from '@/components/ui';

interface PlaceholderPageProps {
  title: string;
}

export default function PlaceholderPage({ title }: PlaceholderPageProps) {
  return (
    <div className="flex h-full items-center justify-center p-8">
      <Card className="text-center max-w-md">
        <h1 className="font-mono text-xl font-bold text-text-primary mb-2">{title}</h1>
        <p className="text-sm text-text-muted font-mono">Coming soon</p>
      </Card>
    </div>
  );
}
```

- [ ] **Step 3: Create minimal DecisionWorkspace stub**

```tsx
// frontend/src/pages/DecisionWorkspace.tsx
import { Card } from '@/components/ui';

export default function DecisionWorkspace() {
  return (
    <div className="p-6">
      <Card className="text-center py-12">
        <h1 className="font-mono text-xl font-bold text-text-primary mb-2">Decision Workspace</h1>
        <p className="text-sm text-text-muted font-mono">Agent panels and evidence graph loading...</p>
      </Card>
    </div>
  );
}
```

- [ ] **Step 4: Create pages barrel**

```tsx
// frontend/src/pages/index.ts
export { default as DecisionWorkspace } from './DecisionWorkspace';
export { default as PlaceholderPage } from './PlaceholderPage';
```

- [ ] **Step 5: Update App.tsx**

```tsx
// frontend/src/App.tsx
import { RouterProvider } from 'react-router-dom';
import { router } from './router';

export default function App() {
  return <RouterProvider router={router} />;
}
```

- [ ] **Step 6: Update main.tsx with MSW**

```tsx
// frontend/src/main.tsx
import React from 'react';
import ReactDOM from 'react-dom/client';
import App from './App';
import { startWorker } from './mock/browser';
import './styles/globals.css';

async function init() {
  await startWorker();

  ReactDOM.createRoot(document.getElementById('root')!).render(
    <React.StrictMode>
      <App />
    </React.StrictMode>,
  );
}

init();
```

- [ ] **Step 7: Verify app loads**

```bash
cd frontend && npx vite build 2>&1 | tail -5
# Expected: "✓ built in ..." with no errors
```

- [ ] **Step 8: Commit**

```bash
git add frontend/src/router.tsx frontend/src/pages/ frontend/src/App.tsx frontend/src/main.tsx
git commit -m "feat(frontend): add router setup and placeholder pages for all modules"
```

---

### Task 10: CaseHeader component

**Files:**
- Create: `frontend/src/components/workspace/CaseHeader.tsx`
- Create: `frontend/src/components/workspace/__tests__/CaseHeader.test.tsx`

**Interfaces:**
- Consumes: `useCaseStore` from Task 6, `Case` type from Task 4
- Produces: `CaseHeader` component

- [ ] **Step 1: Create CaseHeader**

```tsx
// frontend/src/components/workspace/CaseHeader.tsx
import { Play, Pause, RotateCw, Download, Trash2 } from 'lucide-react';
import { useCaseStore } from '@/stores';
import { StatusBadge, MonoText } from '@/components/shared';
import { Button } from '@/components/ui';
import { CASE_STATUS_LABELS } from '@/types/case';
import { formatDistanceToNow } from 'date-fns';

const STATUS_COLORS: Record<string, string> = {
  INVESTIGATING: 'var(--melchior)',
  DEBATING: 'var(--warning)',
  RESOLVING: 'var(--warning)',
  RESOLVED: 'var(--accent)',
  DRAFT: 'var(--text-muted)',
};

export default function CaseHeader() {
  const c = useCaseStore((s) => s.case);

  if (!c) return null;

  const consensus = c.consensus
    ? `${c.consensus.approve} : ${c.consensus.reject}`
    : '— : —';

  const createdAgo = formatDistanceToNow(new Date(c.createdAt), { addSuffix: true });

  return (
    <div className="flex items-start justify-between border-b border-border-dim px-6 py-4 bg-elevated">
      <div className="flex-1 min-w-0">
        {/* Title */}
        <h1 className="text-lg font-semibold text-text-primary mb-3 font-sans">
          {c.question}
        </h1>

        {/* Meta row */}
        <div className="flex items-center gap-4 flex-wrap">
          <StatusBadge
            status={CASE_STATUS_LABELS[c.status]}
            color={STATUS_COLORS[c.status] || 'var(--text-muted)'}
          />

          <div className="flex items-center gap-1.5">
            <MonoText size="sm" muted>Round</MonoText>
            <MonoText size="sm">{c.round}</MonoText>
          </div>

          <div className="flex items-center gap-1.5">
            <MonoText size="sm" muted>Created</MonoText>
            <MonoText size="sm">{createdAgo}</MonoText>
          </div>

          <div className="flex items-center gap-1.5">
            <MonoText size="sm" muted>Consensus</MonoText>
            <span className="font-mono text-sm font-semibold text-text-primary">{consensus}</span>
          </div>

          <div className="flex items-center gap-1.5">
            <MonoText size="sm" muted>Confidence</MonoText>
            <span className="font-mono text-sm font-semibold text-accent">{c.confidence}%</span>
          </div>
        </div>
      </div>

      {/* Actions */}
      <div className="flex items-center gap-1.5 ml-4">
        <Button variant="accent" size="sm">
          <Play size={12} /> Run
        </Button>
        <Button size="sm">
          <Pause size={12} /> Pause
        </Button>
        <Button size="sm">
          <RotateCw size={12} /> Replay
        </Button>
        <Button size="sm">
          <Download size={12} /> Export
        </Button>
        <Button variant="ghost" size="sm">
          <Trash2 size={12} />
        </Button>
      </div>
    </div>
  );
}
```

- [ ] **Step 2: Write component test**

```tsx
// frontend/src/components/workspace/__tests__/CaseHeader.test.tsx
import { describe, it, expect, beforeEach } from 'vitest';
import { render, screen } from '@testing-library/react';
import { MemoryRouter } from 'react-router-dom';
import CaseHeader from '../CaseHeader';
import { useCaseStore } from '@/stores';
import type { Case } from '@/types/case';

const mockCase: Case = {
  id: 'test-1',
  question: 'Should we migrate?',
  background: '',
  constraints: [],
  status: 'INVESTIGATING',
  round: 2,
  consensus: { approve: 2, reject: 1, abstain: 0, majority: 'Approve', needReflection: false },
  confidence: 81,
  createdAt: new Date().toISOString(),
  updatedAt: new Date().toISOString(),
};

function renderWithRouter() {
  return render(
    <MemoryRouter>
      <CaseHeader />
    </MemoryRouter>
  );
}

describe('CaseHeader', () => {
  beforeEach(() => {
    useCaseStore.setState({ case: mockCase, cases: [], loading: false, error: null });
  });

  it('renders the question', () => {
    renderWithRouter();
    expect(screen.getByText('Should we migrate?')).toBeInTheDocument();
  });

  it('displays consensus score', () => {
    renderWithRouter();
    expect(screen.getByText('2 : 1')).toBeInTheDocument();
  });

  it('displays confidence', () => {
    renderWithRouter();
    expect(screen.getByText('81%')).toBeInTheDocument();
  });

  it('renders action buttons', () => {
    renderWithRouter();
    expect(screen.getByText('Run')).toBeInTheDocument();
    expect(screen.getByText('Replay')).toBeInTheDocument();
  });

  it('returns null when no case', () => {
    useCaseStore.setState({ case: null });
    const { container } = renderWithRouter();
    expect(container.innerHTML).toBe('');
  });
});
```

- [ ] **Step 3: Run tests**

```bash
cd frontend && npx vitest run --reporter=verbose 2>&1 | tail -20
# Expected: All tests pass
```

- [ ] **Step 4: Commit**

```bash
git add frontend/src/components/workspace/CaseHeader.tsx frontend/src/components/workspace/__tests__/
git commit -m "feat(frontend): add CaseHeader component with status, consensus, and actions"
```

---

### Task 11: AgentPanel and AgentTrio (accordion)

**Files:**
- Create: `frontend/src/components/workspace/AgentPanel.tsx`
- Create: `frontend/src/components/workspace/AgentTrio.tsx`
- Create: `frontend/src/components/workspace/__tests__/AgentPanel.test.tsx`

**Interfaces:**
- Consumes: `useAgentStore`, `useUiStore` from Task 6; `AgentId`, `AgentSnapshot` types from Task 4; shared components from Task 7
- Produces: `AgentPanel`, `AgentTrio` components

- [ ] **Step 1: Create AgentPanel**

```tsx
// frontend/src/components/workspace/AgentPanel.tsx
import type { AgentId } from '@/types/agent';
import { AGENT_NAMES, AGENT_ROLES, AGENT_COLORS } from '@/types/agent';
import { useAgentStore, useUiStore } from '@/stores';
import { GlowBorder, ScanLine, StatusBadge, MonoText } from '@/components/shared';
import { Card } from '@/components/ui';
import { Wrench, FileSearch, Lightbulb, Vote } from 'lucide-react';

interface AgentPanelProps {
  agentId: AgentId;
}

export default function AgentPanel({ agentId }: AgentPanelProps) {
  const agent = useAgentStore((s) => s.agents[agentId]);
  const expandedAgent = useUiStore((s) => s.expandedAgent);
  const setExpandedAgent = useUiStore((s) => s.setExpandedAgent);
  const color = AGENT_COLORS[agentId];
  const isExpanded = expandedAgent === agentId;

  const handleClick = () => {
    setExpandedAgent(isExpanded ? null : agentId);
  };

  if (!agent) {
    return (
      <Card className="flex-1 min-w-0">
        <div className="text-center py-8">
          <MonoText muted>{AGENT_NAMES[agentId]}</MonoText>
          <div className="mt-2">
            <span className="font-mono text-xs text-text-muted">Waiting for agent data...</span>
          </div>
        </div>
      </Card>
    );
  }

  const isRunning = agent.status === 'running';

  return (
    <GlowBorder
      color={color}
      active={isRunning}
      className={`flex-1 min-w-0 cursor-pointer transition-all duration-300 overflow-hidden relative ${isExpanded ? 'flex-[2]' : 'flex-1'}`}
    >
      <div onClick={handleClick} className="h-full">
        {isRunning && <ScanLine />}

        {/* Header */}
        <div className="px-4 pt-3 pb-2 border-b border-border-dim">
          <div className="flex items-center justify-between mb-1">
            <h3 className="font-mono text-sm font-bold tracking-wider" style={{ color }}>
              {AGENT_NAMES[agentId]}
            </h3>
            <StatusBadge
              status={agent.status.toUpperCase()}
              color={isRunning ? color : undefined}
            />
          </div>
          <p className="text-xs text-text-muted mb-2">{AGENT_ROLES[agentId]}</p>

          {/* Progress bar */}
          <div className="flex items-center gap-2 mb-1">
            <div className="flex-1 h-1 bg-border-dim rounded-full overflow-hidden">
              <div
                className="h-full rounded-full transition-all duration-500"
                style={{
                  width: `${(agent.step / agent.maxSteps) * 100}%`,
                  backgroundColor: color,
                }}
              />
            </div>
            <MonoText size="sm">{agent.step}/{agent.maxSteps}</MonoText>
          </div>
        </div>

        {/* Summary (always visible) */}
        <div className="grid grid-cols-2 gap-2 px-4 py-3">
          <div className="flex items-center gap-1.5">
            <Wrench size={12} className="text-text-muted" />
            <MonoText size="sm">{agent.toolCalls.length} Tools</MonoText>
          </div>
          <div className="flex items-center gap-1.5">
            <FileSearch size={12} className="text-text-muted" />
            <MonoText size="sm">{agent.evidence.length} Evidence</MonoText>
          </div>
          <div className="flex items-center gap-1.5">
            <Lightbulb size={12} className="text-text-muted" />
            <MonoText size="sm">{agent.claims.length} Claims</MonoText>
          </div>
          <div className="flex items-center gap-1.5">
            <Vote size={12} className="text-text-muted" />
            <MonoText size="sm">{agent.vote?.stance || 'Pending'}</MonoText>
          </div>
        </div>

        {/* Expanded Detail */}
        {isExpanded && (
          <div className="px-4 pb-4 space-y-3 animate-fade-in border-t border-border-dim pt-3">
            {/* Thought */}
            {agent.thought && (
              <div>
                <MonoText size="sm" muted>Thought</MonoText>
                <p className="text-xs text-text-secondary mt-1 leading-relaxed">{agent.thought}</p>
              </div>
            )}

            {/* Tool Calls */}
            {agent.toolCalls.length > 0 && (
              <div>
                <MonoText size="sm" muted>Tool Calls</MonoText>
                <div className="mt-1 space-y-1.5">
                  {agent.toolCalls.map((tc, i) => (
                    <div key={i} className="bg-raised rounded p-2 text-xs">
                      <div className="font-mono" style={{ color }}>{tc.name}({JSON.stringify(tc.params)})</div>
                      {tc.result && <div className="text-text-muted mt-0.5 truncate">{tc.result}</div>}
                    </div>
                  ))}
                </div>
              </div>
            )}

            {/* Evidence */}
            {agent.evidence.length > 0 && (
              <div>
                <MonoText size="sm" muted>Evidence</MonoText>
                <div className="flex flex-wrap gap-1 mt-1">
                  {agent.evidence.map((ev) => (
                    <span
                      key={ev.id}
                      className="font-mono text-[10px] px-1.5 py-0.5 rounded bg-raised text-text-secondary border border-border-dim cursor-pointer hover:border-[var(--border-active)]"
                    >
                      {ev.id}
                    </span>
                  ))}
                </div>
              </div>
            )}

            {/* Claims */}
            {agent.claims.length > 0 && (
              <div>
                <MonoText size="sm" muted>Claims</MonoText>
                <ul className="mt-1 space-y-1">
                  {agent.claims.map((cl) => (
                    <li key={cl.id} className="text-xs text-text-secondary pl-2 border-l-2" style={{ borderColor: `${color}60` }}>
                      {cl.text}
                    </li>
                  ))}
                </ul>
              </div>
            )}

            {/* Vote */}
            {agent.vote && (
              <div>
                <MonoText size="sm" muted>Vote</MonoText>
                <div className="mt-1 p-2 rounded bg-raised">
                  <div className="flex items-center gap-2 mb-1">
                    <span className={`font-mono text-xs font-bold ${agent.vote.stance === 'Approve' ? 'text-accent' : 'text-error'}`}>
                      {agent.vote.stance.toUpperCase()}
                    </span>
                    <MonoText size="sm" muted>{agent.vote.confidence}% confidence</MonoText>
                  </div>
                  <p className="text-xs text-text-secondary leading-relaxed">{agent.vote.reasoning}</p>
                </div>
              </div>
            )}
          </div>
        )}
      </div>
    </GlowBorder>
  );
}
```

- [ ] **Step 2: Create AgentTrio**

```tsx
// frontend/src/components/workspace/AgentTrio.tsx
import AgentPanel from './AgentPanel';

export default function AgentTrio() {
  return (
    <div className="flex gap-3 p-4">
      <AgentPanel agentId="melchior" />
      <AgentPanel agentId="balthasar" />
      <AgentPanel agentId="casper" />
    </div>
  );
}
```

- [ ] **Step 3: Write AgentPanel test**

```tsx
// frontend/src/components/workspace/__tests__/AgentPanel.test.tsx
import { describe, it, expect, beforeEach } from 'vitest';
import { render, screen, fireEvent } from '@testing-library/react';
import AgentPanel from '../AgentPanel';
import { useAgentStore, useUiStore } from '@/stores';

describe('AgentPanel', () => {
  beforeEach(() => {
    useAgentStore.getState().resetAgents();
    useUiStore.setState({ selected: null, expandedAgent: null, sidebarCollapsed: false, timelineCollapsed: false, timelineHeight: 192 });
  });

  it('shows agent name', () => {
    useAgentStore.getState().patchAgent('melchior', {
      agentId: 'melchior',
      status: 'idle',
      step: 0,
      maxSteps: 12,
      thought: '',
      toolCalls: [],
      evidence: [],
      claims: [],
      vote: null,
    });
    render(<AgentPanel agentId="melchior" />);
    expect(screen.getByText('MELCHIOR')).toBeInTheDocument();
  });

  it('shows waiting state when no agent data', () => {
    render(<AgentPanel agentId="melchior" />);
    expect(screen.getByText('Waiting for agent data...')).toBeInTheDocument();
  });

  it('expands on click', () => {
    useAgentStore.getState().patchAgent('balthasar', {
      agentId: 'balthasar',
      status: 'running',
      step: 5,
      maxSteps: 12,
      thought: 'Risk assessment...',
      toolCalls: [{ name: 'search', params: {}, result: 'found', timestamp: '' }],
      evidence: [{ id: 'EV-001', source: 'web', reliability: 0.8 }],
      claims: [],
      vote: null,
    });
    render(<AgentPanel agentId="balthasar" />);
    fireEvent.click(screen.getByText('BALTHASAR'));
    expect(useUiStore.getState().expandedAgent).toBe('balthasar');
  });
});
```

- [ ] **Step 4: Run tests**

```bash
cd frontend && npx vitest run --reporter=verbose 2>&1 | tail -5
# Expected: all tests pass
```

- [ ] **Step 5: Commit**

```bash
git add frontend/src/components/workspace/AgentPanel.tsx frontend/src/components/workspace/AgentTrio.tsx frontend/src/components/workspace/__tests__/AgentPanel.test.tsx
git commit -m "feat(frontend): add AgentPanel and AgentTrio with accordion interaction"
```

---

### Task 12: ConsensusPanel

**Files:**
- Create: `frontend/src/components/workspace/ConsensusPanel.tsx`
- Create: `frontend/src/components/workspace/__tests__/ConsensusPanel.test.tsx`

**Interfaces:**
- Consumes: `useCaseStore`, `useAgentStore` from Task 6
- Produces: `ConsensusPanel` component

- [ ] **Step 1: Create ConsensusPanel**

```tsx
// frontend/src/components/workspace/ConsensusPanel.tsx
import { useCaseStore, useAgentStore } from '@/stores';
import { Card } from '@/components/ui';
import { MonoText } from '@/components/shared';
import { Check, X, Minus } from 'lucide-react';

export default function ConsensusPanel() {
  const { consensus, confidence } = useCaseStore((s) => ({
    consensus: s.case?.consensus,
    confidence: s.case?.confidence,
  }));
  const agents = useAgentStore((s) => s.agents);

  // Count votes from agent states
  const votes = {
    melchior: agents.melchior?.vote,
    balthasar: agents.balthasar?.vote,
    casper: agents.casper?.vote,
  };

  const renderVoteIcon = (stance: string | undefined) => {
    if (stance === 'Approve') return <Check size={14} className="text-accent" />;
    if (stance === 'Reject') return <X size={14} className="text-error" />;
    return <Minus size={14} className="text-text-muted" />;
  };

  const timelineSteps = ['Round 1', 'Debate', 'Reflection', 'Round 2', 'Resolved'];
  const currentStep = 0; // Phase 1: static

  return (
    <Card className="mx-4 mb-4">
      <h3 className="font-mono text-xs font-semibold text-text-secondary uppercase tracking-wider mb-3">Consensus</h3>

      <div className="grid grid-cols-4 gap-4">
        {/* Vote Tally */}
        <div>
          <MonoText size="sm" muted>Current</MonoText>
          <div className="mt-1">
            <span className="font-mono text-2xl font-bold text-text-primary">
              {consensus ? `${consensus.approve} : ${consensus.reject}` : '— : —'}
            </span>
          </div>
        </div>

        {/* Majority */}
        <div>
          <MonoText size="sm" muted>Majority</MonoText>
          <div className="mt-1">
            <span className="font-mono text-sm font-semibold" style={{ color: consensus?.majority === 'Approve' ? 'var(--accent)' : 'var(--error)' }}>
              {consensus?.majority || 'Pending'}
            </span>
          </div>
        </div>

        {/* Confidence */}
        <div>
          <MonoText size="sm" muted>Confidence</MonoText>
          <div className="mt-1">
            <span className="font-mono text-sm font-semibold text-accent">{confidence}%</span>
          </div>
        </div>

        {/* Need Reflection */}
        <div>
          <MonoText size="sm" muted>Need Reflection</MonoText>
          <div className="mt-1">
            <span className={`font-mono text-sm font-semibold ${consensus?.needReflection ? 'text-warning' : 'text-accent'}`}>
              {consensus?.needReflection ? 'YES' : 'NO'}
            </span>
          </div>
        </div>
      </div>

      {/* Agent Vote Strip */}
      <div className="flex gap-4 mt-4 pt-4 border-t border-border-dim">
        {(['melchior', 'balthasar', 'casper'] as const).map((id) => {
          const agent = agents[id];
          const v = agent?.vote;
          return (
            <div key={id} className="flex-1 flex items-center gap-2 p-2 rounded bg-raised">
              {renderVoteIcon(v?.stance)}
              <div className="flex-1 min-w-0">
                <div className="text-xs font-mono text-text-secondary">{id.toUpperCase()}</div>
                {v ? (
                  <div className="text-[10px] text-text-muted truncate">{v.stance} ({v.confidence}%)</div>
                ) : (
                  <div className="text-[10px] text-text-muted">Pending</div>
                )}
              </div>
            </div>
          );
        })}
      </div>

      {/* Mini Consensus Timeline */}
      <div className="flex items-center gap-2 mt-4 pt-4 border-t border-border-dim">
        {timelineSteps.map((step, i) => (
          <div key={step} className="flex items-center gap-2">
            <div className={`flex items-center gap-1.5 font-mono text-[10px] ${i <= currentStep ? 'text-accent' : 'text-text-muted'}`}>
              <span
                className={`inline-block w-2 h-2 rounded-full ${i <= currentStep ? 'bg-accent' : 'bg-border-dim'}`}
              />
              {step}
            </div>
            {i < timelineSteps.length - 1 && (
              <span className="w-3 h-px bg-border-dim" />
            )}
          </div>
        ))}
      </div>
    </Card>
  );
}
```

- [ ] **Step 2: Write test**

```tsx
// frontend/src/components/workspace/__tests__/ConsensusPanel.test.tsx
import { describe, it, expect, beforeEach } from 'vitest';
import { render, screen } from '@testing-library/react';
import ConsensusPanel from '../ConsensusPanel';
import { useCaseStore, useAgentStore } from '@/stores';

describe('ConsensusPanel', () => {
  beforeEach(() => {
    useAgentStore.getState().resetAgents();
    const mockCase = {
      id: '1', question: '?', background: '', constraints: [],
      status: 'COLLECTING_VOTES' as const, round: 1,
      consensus: { approve: 2, reject: 1, abstain: 0, majority: 'Approve' as const, needReflection: true },
      confidence: 81, createdAt: '', updatedAt: '',
    };
    useCaseStore.setState({ case: mockCase, cases: [], loading: false, error: null });
  });

  it('shows vote tally', () => {
    render(<ConsensusPanel />);
    expect(screen.getByText('2 : 1')).toBeInTheDocument();
  });

  it('shows need reflection', () => {
    render(<ConsensusPanel />);
    expect(screen.getByText('YES')).toBeInTheDocument();
  });

  it('shows consensus timeline steps', () => {
    render(<ConsensusPanel />);
    expect(screen.getByText('Round 1')).toBeInTheDocument();
    expect(screen.getByText('Resolved')).toBeInTheDocument();
  });
});
```

- [ ] **Step 3: Run tests**

```bash
cd frontend && npx vitest run --reporter=verbose 2>&1 | tail -5
# Expected: all tests pass
```

- [ ] **Step 4: Commit**

```bash
git add frontend/src/components/workspace/ConsensusPanel.tsx frontend/src/components/workspace/__tests__/ConsensusPanel.test.tsx
git commit -m "feat(frontend): add ConsensusPanel with vote tally, agent strip, and timeline"
```

---

### Task 13: BottomTimeline

**Files:**
- Create: `frontend/src/components/layout/BottomTimeline.tsx`
- Create: `frontend/src/components/layout/__tests__/BottomTimeline.test.tsx`

**Interfaces:**
- Consumes: `useEventStore`, `useUiStore` from Task 6; `MagiEvent`, `EventFilter` types from Task 4; shared components from Task 7
- Produces: `BottomTimeline` component

- [ ] **Step 1: Create BottomTimeline**

```tsx
// frontend/src/components/layout/BottomTimeline.tsx
import { useEventStore, useUiStore } from '@/stores';
import { MonoText, AgentAvatar } from '@/components/shared';
import { Wrench, User, FileSearch, Vote, ChevronUp, ChevronDown } from 'lucide-react';
import { format } from 'date-fns';
import type { EventType } from '@/types/event';

const EVENT_ICONS: Record<EventType, React.ReactNode> = {
  TOOL_CALL: <Wrench size={12} />,
  AGENT_STEP: <User size={12} />,
  EVIDENCE_CREATED: <FileSearch size={12} />,
  VOTE_SUBMITTED: <Vote size={12} />,
  CONSENSUS_CHANGED: <Vote size={12} />,
  ROUND_START: <FileSearch size={12} />,
  DEBATE_START: <User size={12} />,
  REFLECTION: <User size={12} />,
  RESOLVED: <Vote size={12} />,
  ERROR: <FileSearch size={12} />,
};

const FILTER_KEYS: { key: keyof EventFilter; label: string }[] = [
  { key: 'tool', label: 'Tool' },
  { key: 'agent', label: 'Agent' },
  { key: 'evidence', label: 'Evidence' },
  { key: 'vote', label: 'Vote' },
];

const EVENT_TO_FILTER: Record<EventType, keyof EventFilter> = {
  TOOL_CALL: 'tool',
  AGENT_STEP: 'agent',
  EVIDENCE_CREATED: 'evidence',
  VOTE_SUBMITTED: 'vote',
  CONSENSUS_CHANGED: 'vote',
  ROUND_START: 'agent',
  DEBATE_START: 'agent',
  REFLECTION: 'agent',
  RESOLVED: 'vote',
  ERROR: 'agent',
};

export default function BottomTimeline() {
  const events = useEventStore((s) => s.events);
  const filters = useEventStore((s) => s.filters);
  const toggleFilter = useEventStore((s) => s.toggleFilter);
  const timelineCollapsed = useUiStore((s) => s.timelineCollapsed);
  const toggleTimeline = useUiStore((s) => s.toggleTimeline);
  const timelineHeight = useUiStore((s) => s.timelineHeight);

  const filteredEvents = events.filter((e) => filters[EVENT_TO_FILTER[e.type]]);

  return (
    <div
      className="shrink-0 border-t border-border-dim bg-base flex flex-col"
      style={{ height: timelineCollapsed ? 32 : timelineHeight }}
    >
      {/* Header */}
      <div className="flex items-center justify-between px-4 h-8 border-b border-border-dim">
        <div className="flex items-center gap-3">
          <button onClick={toggleTimeline} className="text-text-muted hover:text-text-primary cursor-pointer">
            {timelineCollapsed ? <ChevronUp size={14} /> : <ChevronDown size={14} />}
          </button>
          <span className="font-mono text-[10px] text-text-muted uppercase tracking-wider">Timeline</span>
          <span className="font-mono text-[10px] text-text-muted">{events.length} events</span>
        </div>

        {!timelineCollapsed && (
          <div className="flex items-center gap-1">
            {FILTER_KEYS.map(({ key, label }) => (
              <button
                key={key}
                onClick={() => toggleFilter(key)}
                className={`px-2 py-0.5 rounded font-mono text-[10px] transition-colors cursor-pointer ${
                  filters[key]
                    ? 'bg-elevated text-text-primary border border-border-dim'
                    : 'text-text-muted hover:text-text-secondary'
                }`}
              >
                {label}
              </button>
            ))}
          </div>
        )}
      </div>

      {/* Event List */}
      {!timelineCollapsed && (
        <div className="flex-1 overflow-y-auto font-mono text-xs">
          {filteredEvents.map((event, i) => (
            <div
              key={event.id}
              className="flex items-center gap-2 px-4 py-1 border-b border-border-dim animate-slide-up hover:bg-raised cursor-pointer"
              style={{ animationDelay: `${i * 20}ms` }}
            >
              <MonoText size="sm" muted>
                {format(new Date(event.timestamp), 'HH:mm')}
              </MonoText>
              <span className="text-text-muted">{EVENT_ICONS[event.type]}</span>
              {event.agentId && <AgentAvatar agentId={event.agentId} size={16} />}
              <span className="text-text-secondary truncate">{event.message}</span>
            </div>
          ))}
          {filteredEvents.length === 0 && (
            <div className="px-4 py-2 text-text-muted text-xs italic">
              No events matching current filters
            </div>
          )}
        </div>
      )}
    </div>
  );
}
```

- [ ] **Step 2: Write test**

```tsx
// frontend/src/components/layout/__tests__/BottomTimeline.test.tsx
import { describe, it, expect, beforeEach } from 'vitest';
import { render, screen, fireEvent } from '@testing-library/react';
import BottomTimeline from '../BottomTimeline';
import { useEventStore, useUiStore } from '@/stores';
import type { MagiEvent } from '@/types/event';

const mockEvents: MagiEvent[] = [
  { id: 'e1', type: 'TOOL_CALL', timestamp: '2026-01-01T10:00:00Z', agentId: 'melchior', message: 'Test tool call' },
  { id: 'e2', type: 'VOTE_SUBMITTED', timestamp: '2026-01-01T10:01:00Z', agentId: 'melchior', message: 'Voted Approve' },
];

describe('BottomTimeline', () => {
  beforeEach(() => {
    useEventStore.setState({
      events: mockEvents,
      filters: { tool: true, agent: true, evidence: true, vote: true },
    });
    useUiStore.setState({
      selected: null, expandedAgent: null,
      sidebarCollapsed: false, timelineCollapsed: false, timelineHeight: 192,
    });
  });

  it('renders events', () => {
    render(<BottomTimeline />);
    expect(screen.getByText('Test tool call')).toBeInTheDocument();
    expect(screen.getByText('Voted Approve')).toBeInTheDocument();
  });

  it('shows event count', () => {
    render(<BottomTimeline />);
    expect(screen.getByText('2 events')).toBeInTheDocument();
  });

  it('filters events by type', () => {
    render(<BottomTimeline />);
    // Toggle off 'tool' filter
    const toolBtn = screen.getAllByText('Tool')[0];
    fireEvent.click(toolBtn);
    expect(screen.queryByText('Test tool call')).not.toBeInTheDocument();
    expect(screen.getByText('Voted Approve')).toBeInTheDocument();
  });
});
```

- [ ] **Step 3: Run tests**

```bash
cd frontend && npx vitest run --reporter=verbose 2>&1 | tail -5
```

- [ ] **Step 4: Commit**

```bash
git add frontend/src/components/layout/BottomTimeline.tsx frontend/src/components/layout/__tests__/
git commit -m "feat(frontend): add BottomTimeline with event filters and animations"
```

---

### Task 14: RightInspector

**Files:**
- Create: `frontend/src/components/layout/RightInspector.tsx`
- Create: `frontend/src/components/layout/__tests__/RightInspector.test.tsx`

**Interfaces:**
- Consumes: `useUiStore`, `useAgentStore`, `useEventStore` from Task 6; mock evidence data from Task 5
- Produces: `RightInspector` component

- [ ] **Step 1: Create RightInspector**

```tsx
// frontend/src/components/layout/RightInspector.tsx
import { useUiStore, useAgentStore, useEventStore } from '@/stores';
import { MonoText, AgentAvatar } from '@/components/shared';
import { Card } from '@/components/ui';
import { Globe, Link2, Shield, User, Clock, FileText } from 'lucide-react';
import { createMockEvidence } from '@/mock/data';

const MOCK_EVIDENCE_MAP = new Map(createMockEvidence().map((e) => [e.id, e]));

export default function RightInspector() {
  const selected = useUiStore((s) => s.selected);
  const agents = useAgentStore((s) => s.agents);
  const events = useEventStore((s) => s.events);

  if (!selected) {
    return (
      <aside className="w-80 shrink-0 border-l border-border-dim bg-base p-4 flex items-center justify-center">
        <div className="text-center">
          <FileText size={24} className="text-text-muted mx-auto mb-2" />
          <MonoText muted>Inspector</MonoText>
          <p className="text-xs text-text-muted mt-1">Select an object to inspect</p>
        </div>
      </aside>
    );
  }

  const renderEvidenceDetail = () => {
    const ev = MOCK_EVIDENCE_MAP.get(selected.id);
    if (!ev) return <MonoText muted>Evidence {selected.id} not found</MonoText>;
    return (
      <div className="space-y-4">
        <div>
          <MonoText size="sm" muted>Source</MonoText>
          <div className="flex items-center gap-1.5 mt-1">
            <Globe size={14} className="text-text-muted" />
            <span className="text-sm text-text-primary">{ev.source}</span>
          </div>
        </div>

        {ev.url && (
          <div>
            <MonoText size="sm" muted>URL</MonoText>
            <div className="flex items-center gap-1.5 mt-1">
              <Link2 size={14} className="text-text-muted" />
              <span className="text-xs text-accent truncate block">{ev.url}</span>
            </div>
          </div>
        )}

        <div>
          <MonoText size="sm" muted>Observation</MonoText>
          <p className="text-sm text-text-secondary mt-1 leading-relaxed">{ev.observation}</p>
        </div>

        <div className="flex items-center gap-4">
          <div>
            <MonoText size="sm" muted>Reliability</MonoText>
            <div className="flex items-center gap-1.5 mt-1">
              <Shield size={14} className="text-accent" />
              <span className="font-mono text-sm font-semibold text-accent">{ev.reliability.toFixed(2)}</span>
            </div>
          </div>
          <div>
            <MonoText size="sm" muted>Collected By</MonoText>
            <div className="flex items-center gap-1.5 mt-1">
              <AgentAvatar agentId={ev.collectedBy} size={18} />
              <span className="text-sm text-text-secondary">{ev.collectedBy}</span>
            </div>
          </div>
        </div>

        <div>
          <MonoText size="sm" muted>Timestamp</MonoText>
          <div className="flex items-center gap-1.5 mt-1">
            <Clock size={14} className="text-text-muted" />
            <span className="font-mono text-xs text-text-muted">{ev.timestamp}</span>
          </div>
        </div>
      </div>
    );
  };

  const renderVoteDetail = () => {
    const agentVotes = Object.entries(agents)
      .filter(([, a]) => a?.vote?.stance)
      .map(([id, a]) => ({ agentId: id, ...a!.vote! }));

    const vote = agentVotes.find((v) => v.agentId === selected.id) ||
      agentVotes[0];

    if (!vote) return <MonoText muted>No vote data available</MonoText>;

    return (
      <div className="space-y-4">
        <div>
          <MonoText size="sm" muted>Stance</MonoText>
          <div className="mt-1">
            <span className={`font-mono text-base font-bold ${vote.stance === 'Approve' ? 'text-accent' : 'text-error'}`}>
              {vote.stance.toUpperCase()}
            </span>
          </div>
        </div>

        <div>
          <MonoText size="sm" muted>Confidence</MonoText>
          <div className="mt-1">
            <span className="font-mono text-lg font-semibold text-text-primary">{vote.confidence}%</span>
          </div>
        </div>

        {vote.dimensions && (
          <div>
            <MonoText size="sm" muted>Utility Dimensions</MonoText>
            <div className="mt-2 space-y-2">
              {Object.entries(vote.dimensions).map(([dim, val]) => (
                <div key={dim}>
                  <div className="flex justify-between text-xs mb-1">
                    <span className="text-text-secondary">{dim}</span>
                    <span className="font-mono text-text-primary">{val}</span>
                  </div>
                  <div className="h-1 bg-border-dim rounded-full overflow-hidden">
                    <div className="h-full rounded-full bg-accent" style={{ width: `${val}%` }} />
                  </div>
                </div>
              ))}
            </div>
          </div>
        )}

        <div>
          <MonoText size="sm" muted>Reasoning</MonoText>
          <p className="text-sm text-text-secondary mt-1 leading-relaxed">{vote.reasoning}</p>
        </div>
      </div>
    );
  };

  const renderAgentDetail = () => {
    const agent = agents[selected.id as keyof typeof agents];
    if (!agent) return <MonoText muted>No agent data</MonoText>;

    return (
      <div className="space-y-3">
        <div>
          <MonoText size="sm" muted>Status</MonoText>
          <div className="mt-1 flex items-center gap-2">
            <span className={`inline-block w-2 h-2 rounded-full ${agent.status === 'running' ? 'bg-accent animate-status-blink' : 'bg-text-muted'}`} />
            <span className="text-sm text-text-primary">{agent.status}</span>
          </div>
        </div>
        <div>
          <MonoText size="sm" muted>Progress</MonoText>
          <div className="mt-1">
            <span className="font-mono text-sm text-text-primary">Step {agent.step} / {agent.maxSteps}</span>
          </div>
        </div>
        <div>
          <MonoText size="sm" muted>Tools Called</MonoText>
          <div className="mt-1">
            <span className="font-mono text-sm text-text-primary">{agent.toolCalls.length}</span>
          </div>
        </div>
        <div>
          <MonoText size="sm" muted>Evidence Collected</MonoText>
          <div className="mt-1">
            <span className="font-mono text-sm text-text-primary">{agent.evidence.length}</span>
          </div>
        </div>
        <div>
          <MonoText size="sm" muted>Claims Submitted</MonoText>
          <div className="mt-1">
            <span className="font-mono text-sm text-text-primary">{agent.claims.length}</span>
          </div>
        </div>
        {agent.thought && (
          <div>
            <MonoText size="sm" muted>Thought</MonoText>
            <p className="text-xs text-text-secondary mt-1 leading-relaxed">{agent.thought}</p>
          </div>
        )}
      </div>
    );
  };

  const renderEventDetail = () => {
    const event = events.find((e) => e.id === selected.id);
    if (!event) return <MonoText muted>Event not found</MonoText>;

    return (
      <div className="space-y-4">
        <div>
          <MonoText size="sm" muted>Type</MonoText>
          <div className="mt-1">
            <span className="text-sm text-text-primary">{event.type}</span>
          </div>
        </div>
        <div>
          <MonoText size="sm" muted>Timestamp</MonoText>
          <div className="mt-1">
            <span className="font-mono text-xs text-text-muted">{event.timestamp}</span>
          </div>
        </div>
        {event.agentId && (
          <div>
            <MonoText size="sm" muted>Agent</MonoText>
            <div className="flex items-center gap-1.5 mt-1">
              <AgentAvatar agentId={event.agentId} size={18} />
              <span className="text-sm text-text-secondary">{event.agentId}</span>
            </div>
          </div>
        )}
        <div>
          <MonoText size="sm" muted>Message</MonoText>
          <p className="text-sm text-text-secondary mt-1">{event.message}</p>
        </div>
        {event.data && (
          <div>
            <MonoText size="sm" muted>Data</MonoText>
            <pre className="text-xs text-text-muted mt-1 p-2 rounded bg-raised overflow-x-auto font-mono">
              {JSON.stringify(event.data, null, 2)}
            </pre>
          </div>
        )}
      </div>
    );
  };

  const renderContent = () => {
    switch (selected.type) {
      case 'evidence': return renderEvidenceDetail();
      case 'vote': return renderVoteDetail();
      case 'agent': return renderAgentDetail();
      case 'event': return renderEventDetail();
      default: return <MonoText muted>Unknown selection type</MonoText>;
    }
  };

  const titleMap = { evidence: 'Evidence', vote: 'Vote', agent: 'Agent', event: 'Event' };

  return (
    <aside className="w-80 shrink-0 border-l border-border-dim bg-base overflow-y-auto">
      <div className="sticky top-0 bg-base border-b border-border-dim px-4 py-3">
        <div className="flex items-center justify-between">
          <h3 className="font-mono text-xs font-semibold text-text-secondary uppercase tracking-wider">
            {titleMap[selected.type]}
          </h3>
          <button onClick={() => useUiStore.getState().clearSelection()} className="text-text-muted hover:text-text-primary font-mono text-[10px] cursor-pointer">
            ✕
          </button>
        </div>
        <div className="mt-1">
          <MonoText size="sm" muted>{selected.id}</MonoText>
        </div>
      </div>
      <div className="p-4 animate-fade-in">
        {renderContent()}
      </div>
    </aside>
  );
}
```

- [ ] **Step 2: Write test**

```tsx
// frontend/src/components/layout/__tests__/RightInspector.test.tsx
import { describe, it, expect, beforeEach } from 'vitest';
import { render, screen } from '@testing-library/react';
import RightInspector from '../RightInspector';
import { useUiStore, useAgentStore, useEventStore } from '@/stores';

describe('RightInspector', () => {
  beforeEach(() => {
    useUiStore.setState({ selected: null, expandedAgent: null, sidebarCollapsed: false, timelineCollapsed: false, timelineHeight: 192 });
    useAgentStore.getState().resetAgents();
    useEventStore.setState({ events: [], filters: { tool: true, agent: true, evidence: true, vote: true } });
  });

  it('shows placeholder when nothing selected', () => {
    render(<RightInspector />);
    expect(screen.getByText('Select an object to inspect')).toBeInTheDocument();
  });

  it('shows evidence details', () => {
    useUiStore.getState().select({ type: 'evidence', id: 'EV-001' });
    render(<RightInspector />);
    expect(screen.getByText('Evidence')).toBeInTheDocument();
    expect(screen.getByText('EV-001')).toBeInTheDocument();
  });

  it('shows vote details', () => {
    useAgentStore.getState().patchAgent('melchior', {
      agentId: 'melchior', status: 'completed', step: 10, maxSteps: 12,
      thought: '', toolCalls: [], evidence: [], claims: [],
      vote: { stance: 'Approve', confidence: 78, reasoning: 'Good idea.', dimensions: { Correctness: 92, Efficiency: 88 } },
    });
    useUiStore.getState().select({ type: 'vote', id: 'melchior' });
    render(<RightInspector />);
    expect(screen.getByText('APPROVE')).toBeInTheDocument();
  });
});
```

- [ ] **Step 3: Run tests**

```bash
cd frontend && npx vitest run --reporter=verbose 2>&1 | tail -5
```

- [ ] **Step 4: Commit**

```bash
git add frontend/src/components/layout/RightInspector.tsx frontend/src/components/layout/__tests__/RightInspector.test.tsx
git commit -m "feat(frontend): add RightInspector with context-aware detail views"
```

---

### Task 15: EvidencePanel (D3 Knowledge Graph)

**Files:**
- Create: `frontend/src/components/evidence/EvidenceGraph.tsx`
- Create: `frontend/src/components/evidence/index.ts`

**Interfaces:**
- Consumes: `useUiStore` from Task 6; `EvidenceNode`, `EvidenceEdge` types from Task 4; mock evidence data from Task 5
- Produces: `EvidenceGraph` D3 SVG component

- [ ] **Step 1: Create EvidenceGraph (D3 force-directed)**

```tsx
// frontend/src/components/evidence/EvidenceGraph.tsx
import { useEffect, useRef } from 'react';
import * as d3 from 'd3';
import { useUiStore } from '@/stores';
import { createMockEvidence, createMockAgents } from '@/mock/data';
import { Card } from '@/components/ui';
import { MonoText } from '@/components/shared';

interface GraphNode extends d3.SimulationNodeDatum {
  id: string;
  label: string;
  type: 'evidence' | 'claim' | 'vote';
  color: string;
}

interface GraphLink extends d3.SimulationLinkDatum<GraphNode> {
  type: 'supports' | 'contradicts';
}

export default function EvidenceGraph() {
  const svgRef = useRef<SVGSVGElement>(null);
  const select = useUiStore((s) => s.select);

  useEffect(() => {
    const evidence = createMockEvidence();
    const agents = createMockAgents();

    const nodes: GraphNode[] = [
      // Evidence nodes
      ...evidence.map((e) => ({
        id: e.id,
        label: e.id,
        type: 'evidence' as const,
        color: e.collectedBy === 'melchior' ? 'var(--melchior)'
             : e.collectedBy === 'balthasar' ? 'var(--balthasar)'
             : 'var(--casper)',
      })),
      // Claim nodes
      ...Object.values(agents).flatMap((a) =>
        (a.claims || []).map((c) => ({
          id: c.id,
          label: c.text.slice(0, 30) + (c.text.length > 30 ? '...' : ''),
          type: 'claim' as const,
          color: a.agentId === 'melchior' ? 'var(--melchior)'
               : a.agentId === 'balthasar' ? 'var(--balthasar)'
               : 'var(--casper)',
        }))
      ),
      // Vote nodes
      ...Object.entries(agents)
        .filter(([, a]) => a.vote)
        .map(([id, a]) => ({
          id: `vote-${id}`,
          label: `${id}: ${a.vote!.stance}`,
          type: 'vote' as const,
          color: a.vote!.stance === 'Approve' ? 'var(--accent)' : 'var(--error)',
        })),
    ];

    const links: GraphLink[] = [
      // Evidence → Claim connections
      { source: 'EV-001', target: 'CL-001', type: 'supports' },
      { source: 'EV-002', target: 'CL-001', type: 'supports' },
      { source: 'EV-003', target: 'CL-002', type: 'supports' },
      { source: 'EV-004', target: 'CL-004', type: 'supports' },
      { source: 'EV-005', target: 'CL-004', type: 'supports' },
      { source: 'EV-006', target: 'CL-005', type: 'supports' },
      { source: 'EV-007', target: 'CL-002', type: 'supports' },
      { source: 'EV-008', target: 'CL-004', type: 'supports' },
      // Claim → Claim connections
      { source: 'CL-001', target: 'CL-003', type: 'supports' },
      { source: 'CL-002', target: 'CL-003', type: 'supports' },
      { source: 'CL-003', target: 'CL-004', type: 'contradicts' },
      { source: 'CL-005', target: 'CL-003', type: 'supports' },
      // Claim → Vote connections
      { source: 'CL-001', target: 'vote-melchior', type: 'supports' },
      { source: 'CL-004', target: 'vote-balthasar', type: 'supports' },
    ];

    const svg = d3.select(svgRef.current);
    svg.selectAll('*').remove();

    const width = svgRef.current?.clientWidth || 800;
    const height = 320;

    svg.attr('viewBox', `0 0 ${width} ${height}`);

    const simulation = d3.forceSimulation<GraphNode>(nodes)
      .force('link', d3.forceLink<GraphNode, GraphLink>(links).id((d) => d.id).distance(80))
      .force('charge', d3.forceManyBody().strength(-200))
      .force('center', d3.forceCenter(width / 2, height / 2))
      .force('collision', d3.forceCollide(20));

    // Links
    const link = svg.append('g')
      .selectAll('line')
      .data(links)
      .join('line')
      .attr('stroke', (d) => d.type === 'contradicts' ? 'var(--error)' : 'var(--border-dim)')
      .attr('stroke-width', 1.5)
      .attr('stroke-dasharray', (d) => d.type === 'contradicts' ? '4 2' : 'none')
      .attr('opacity', 0.6);

    // Nodes
    const node = svg.append('g')
      .selectAll('g')
      .data(nodes)
      .join('g')
      .style('cursor', 'pointer')
      .on('click', (_e, d) => {
        const typeMap: Record<string, 'evidence' | 'claim' | 'vote'> = {
          evidence: 'evidence',
          claim: 'evidence',
          vote: 'vote',
        };
        select({ type: typeMap[d.type], id: d.id });
      });

    // Node circles
    node.append('circle')
      .attr('r', (d) => d.type === 'evidence' ? 8 : d.type === 'vote' ? 12 : 10)
      .attr('fill', (d) => d.type === 'evidence' ? 'var(--bg-raised)' : d.color + '20')
      .attr('stroke', (d) => d.color)
      .attr('stroke-width', (d) => d.type === 'vote' ? 2 : 1.5);

    // Node labels
    node.append('text')
      .text((d) => d.label)
      .attr('x', 14)
      .attr('y', 4)
      .attr('fill', 'var(--text-secondary)')
      .attr('font-size', '9px')
      .attr('font-family', 'JetBrains Mono, monospace');

    // Drag behavior
    node.call(d3.drag<SVGGElement, GraphNode>()
      .on('start', (event, d) => {
        if (!event.active) simulation.alphaTarget(0.3).restart();
        d.fx = d.x;
        d.fy = d.y;
      })
      .on('drag', (event, d) => {
        d.fx = event.x;
        d.fy = event.y;
      })
      .on('end', (event, d) => {
        if (!event.active) simulation.alphaTarget(0);
        d.fx = null;
        d.fy = null;
      })
    );

    simulation.on('tick', () => {
      link
        .attr('x1', (d) => (d.source as GraphNode).x!)
        .attr('y1', (d) => (d.source as GraphNode).y!)
        .attr('x2', (d) => (d.target as GraphNode).x!)
        .attr('y2', (d) => (d.target as GraphNode).y!);

      node.attr('transform', (d) => `translate(${d.x},${d.y})`);
    });

    return () => {
      simulation.stop();
    };
  }, [select]);

  return (
    <Card className="mx-4 mb-4" padded={false}>
      <div className="px-4 pt-3 pb-2 border-b border-border-dim">
        <h3 className="font-mono text-xs font-semibold text-text-secondary uppercase tracking-wider">Evidence Graph</h3>
        <div className="flex items-center gap-3 mt-1">
          <div className="flex items-center gap-1">
            <span className="inline-block w-2 h-2 rounded-full bg-border-dim" />
            <MonoText size="sm" muted>Supports</MonoText>
          </div>
          <div className="flex items-center gap-1">
            <span className="inline-block w-2 h-0.5 bg-error" style={{ width: 8 }} />
            <MonoText size="sm" muted>Contradicts</MonoText>
          </div>
        </div>
      </div>
      <svg ref={svgRef} className="w-full cursor-grab active:cursor-grabbing" style={{ height: 320 }} />
    </Card>
  );
}
```

- [ ] **Step 2: Create barrel export**

```tsx
// frontend/src/components/evidence/index.ts
export { default as EvidenceGraph } from './EvidenceGraph';
```

- [ ] **Step 3: Verify compilation**

```bash
cd frontend && npx tsc --noEmit 2>&1 | head -20
# Expected: no errors
```

- [ ] **Step 4: Commit**

```bash
git add frontend/src/components/evidence/
git commit -m "feat(frontend): add EvidenceGraph D3 force-directed knowledge graph"
```

---

### Task 16: DecisionWorkspace page integration

**Files:**
- Modify: `frontend/src/pages/DecisionWorkspace.tsx`
- Modify: `frontend/src/components/layout/AppShell.tsx` (add RightInspector + BottomTimeline)

**Interfaces:**
- Consumes: All workspace components (CaseHeader, AgentTrio, ConsensusPanel, EvidenceGraph), layout components (RightInspector, BottomTimeline), stores (caseStore, agentStore, eventStore, uiStore), mock data
- Produces: Complete DecisionWorkspace page

- [ ] **Step 1: Update DecisionWorkspace to load mock data and compose components**

```tsx
// frontend/src/pages/DecisionWorkspace.tsx
import { useEffect } from 'react';
import { useParams } from 'react-router-dom';
import { useCaseStore, useAgentStore, useEventStore } from '@/stores';
import { createMockCase, createMockAgents, createMockEvents } from '@/mock/data';
import { CaseHeader } from '@/components/workspace/CaseHeader';
import { AgentTrio } from '@/components/workspace/AgentTrio';
import { ConsensusPanel } from '@/components/workspace/ConsensusPanel';
import { EvidenceGraph } from '@/components/evidence';

export default function DecisionWorkspace() {
  const { caseId } = useParams<{ caseId: string }>();
  const loadCase = useCaseStore((s) => s.loadCase);
  const loadAgents = useAgentStore((s) => s.loadAgents);
  const loadEvents = useEventStore((s) => s.loadEvents);
  const currentCase = useCaseStore((s) => s.case);

  useEffect(() => {
    // Load mock data on mount
    const caseData = createMockCase();
    const agents = createMockAgents();
    const events = createMockEvents();

    loadCase(caseData);
    loadAgents(agents);
    loadEvents(events);
  }, [caseId, loadCase, loadAgents, loadEvents]);

  if (!currentCase) {
    return (
      <div className="flex h-full items-center justify-center">
        <span className="font-mono text-text-muted animate-pulse-glow">Loading case data...</span>
      </div>
    );
  }

  return (
    <div className="h-full overflow-y-auto">
      <CaseHeader />
      <AgentTrio />
      <ConsensusPanel />
      <EvidenceGraph />
    </div>
  );
}
```

- [ ] **Step 2: Update AppShell to include RightInspector and BottomTimeline**

```tsx
// frontend/src/components/layout/AppShell.tsx
import { Outlet } from 'react-router-dom';
import TopNav from './TopNav';
import LeftNav from './LeftNav';
import RightInspector from './RightInspector';
import BottomTimeline from './BottomTimeline';
import { useCaseStore } from '@/stores';

export default function AppShell() {
  const cases = useCaseStore((s) => s.cases);

  return (
    <div className="flex h-screen flex-col bg-base">
      <TopNav />
      <div className="flex flex-1 overflow-hidden">
        <LeftNav cases={cases} />
        <main className="flex-1 overflow-y-auto bg-base">
          <Outlet />
        </main>
        <RightInspector />
      </div>
      <BottomTimeline />
    </div>
  );
}
```

- [ ] **Step 3: Create workspace barrel and fix imports**

Create barrel file if not existing:
```tsx
// frontend/src/components/workspace/index.ts
export { default as CaseHeader } from './CaseHeader';
export { default as AgentPanel } from './AgentPanel';
export { default as AgentTrio } from './AgentTrio';
export { default as ConsensusPanel } from './ConsensusPanel';
```

- [ ] **Step 4: Fix the import paths in DecisionWorkspace to use barrel**

The DecisionWorkspace above already uses direct paths — that's fine. No change needed.

- [ ] **Step 5: Verify build**

```bash
cd frontend && npm run build 2>&1 | tail -10
# Expected: "✓ built in ..." with no errors
```

- [ ] **Step 6: Commit**

```bash
git add frontend/src/pages/DecisionWorkspace.tsx frontend/src/components/layout/AppShell.tsx frontend/src/components/workspace/index.ts
git commit -m "feat(frontend): integrate DecisionWorkspace with mock data, RightInspector, and BottomTimeline"
```

---

### Task 17: Wire LeftNav with case list, add DecisionInput stub, polish

**Files:**
- Modify: `frontend/src/pages/DecisionWorkspace.tsx` (load case list)
- Create: `frontend/src/components/workspace/DecisionInput.tsx`

**Interfaces:**
- Consumes: caseStore.loadCaseList, mock case list
- Produces: Working LeftNav case list, minimal DecisionInput component

- [ ] **Step 1: Create DecisionInput stub**

```tsx
// frontend/src/components/workspace/DecisionInput.tsx
import { useCaseStore } from '@/stores';
import { Card } from '@/components/ui';
import { MonoText } from '@/components/shared';
import { FileQuestion, BookOpen, Wrench } from 'lucide-react';

export default function DecisionInput() {
  const c = useCaseStore((s) => s.case);

  // Show only when case is in DRAFT or early states
  if (!c || c.status !== 'DRAFT') return null;

  return (
    <Card className="mx-4 mb-4">
      <h3 className="font-mono text-xs font-semibold text-text-secondary uppercase tracking-wider mb-3">Decision Input</h3>

      {/* Question */}
      <div className="mb-3">
        <div className="flex items-center gap-1.5 mb-1">
          <FileQuestion size={14} className="text-text-muted" />
          <MonoText size="sm" muted>Decision Question</MonoText>
        </div>
        <p className="text-sm text-text-primary">{c.question}</p>
      </div>

      {/* Background */}
      <div className="mb-3">
        <div className="flex items-center gap-1.5 mb-1">
          <BookOpen size={14} className="text-text-muted" />
          <MonoText size="sm" muted>Background</MonoText>
        </div>
        <p className="text-sm text-text-secondary leading-relaxed">{c.background}</p>
      </div>

      {/* Constraints */}
      {c.constraints.length > 0 && (
        <div>
          <div className="flex items-center gap-1.5 mb-1">
            <Wrench size={14} className="text-text-muted" />
            <MonoText size="sm" muted>Constraints</MonoText>
          </div>
          <div className="grid grid-cols-2 gap-2">
            {c.constraints.map((ct, i) => (
              <div key={i} className="bg-raised rounded p-2">
                <MonoText size="sm" muted>{ct.label}</MonoText>
                <div className="text-sm text-text-primary mt-0.5">{ct.value}</div>
              </div>
            ))}
          </div>
        </div>
      )}
    </Card>
  );
}
```

- [ ] **Step 2: Update DecisionWorkspace to load case list and include DecisionInput**

Add `loadCaseList` call and import `createMockCaseList`:

```tsx
// In DecisionWorkspace.tsx, update the useEffect:
import { createMockCase, createMockAgents, createMockEvents, createMockCaseList } from '@/mock/data';
// ... and DecisionInput import
import DecisionInput from '@/components/workspace/DecisionInput';

// In useEffect, add:
const caseList = createMockCaseList();
loadCaseList(caseList);

// In the component JSX, add before AgentTrio:
<DecisionInput />
```

Full updated useEffect:
```tsx
useEffect(() => {
  const caseData = createMockCase();
  const agents = createMockAgents();
  const events = createMockEvents();
  const caseList = createMockCaseList();

  loadCase(caseData);
  loadAgents(agents);
  loadEvents(events);
  loadCaseList(caseList);
}, [caseId, loadCase, loadAgents, loadEvents, loadCaseList]);
```

And update `loadCaseList` extraction:
```tsx
const loadCaseList = useCaseStore((s) => s.loadCaseList);
```

- [ ] **Step 3: Update workspace barrel**

```tsx
// Add to frontend/src/components/workspace/index.ts
export { default as DecisionInput } from './DecisionInput';
```

- [ ] **Step 4: Final build verification**

```bash
cd frontend && npm run build 2>&1
# Expected: clean build with no errors
```

- [ ] **Step 5: Run all tests**

```bash
cd frontend && npx vitest run --reporter=verbose 2>&1
# Expected: all tests pass
```

- [ ] **Step 6: Commit**

```bash
git add frontend/src/pages/DecisionWorkspace.tsx frontend/src/components/workspace/DecisionInput.tsx frontend/src/components/workspace/index.ts
git commit -m "feat(frontend): wire LeftNav case list, add DecisionInput component"
```

---

### Task 18: Final verification and dev server smoke test

- [ ] **Step 1: Full test suite**

```bash
cd frontend && npx vitest run --reporter=verbose
# Expected: all tests pass (approximately 20+ tests across all stores and components)
```

- [ ] **Step 2: TypeScript check**

```bash
cd frontend && npx tsc --noEmit
# Expected: no errors
```

- [ ] **Step 3: Production build**

```bash
make fe
# Expected: builds successfully, copies dist/ to bin/resources/static/
```

- [ ] **Step 4: Dev server smoke test**

```bash
# Start dev server in background
cd frontend && npx vite --host 0.0.0.0 &
sleep 3
# Verify index.html loads
curl -s http://localhost:5173 | grep -o 'MAGI — Decision Operating System'
# Expected: "MAGI — Decision Operating System"
# Stop dev server
kill %1 2>/dev/null
```

- [ ] **Step 5: Commit any final changes**

```bash
git status
# If there are any modified files from polish, commit them
git add -A && git commit -m "chore(frontend): final integration polish and verification"
```
