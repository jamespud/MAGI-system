import { create } from 'zustand';
import type { AgentId, AgentSnapshot, AgentStatus, AgentVote, ClaimRef, EvidenceRef, ToolCall } from '@/types/agent';
import { normalizeStance } from '@/lib/stance';
import type { ApiAgentSnapshot } from '@/api/client';

interface AgentState {
  agents: Record<AgentId, AgentSnapshot | null>;
  maxSteps: number;
  setMaxSteps: (n: number) => void;
  loadAgents: (agents: Record<AgentId, AgentSnapshot>) => void;
  loadAgentsFromApi: (snap: Record<string, ApiAgentSnapshot>) => void;
  patchAgent: (id: AgentId, patch: Partial<AgentSnapshot>) => void;
  updateAgentStatus: (id: AgentId, status: AgentStatus) => void;
  addEvidence: (id: AgentId, evidence: EvidenceRef) => void;
  addClaim: (id: AgentId, claim: ClaimRef) => void;
  upsertToolCall: (id: AgentId, tc: Partial<ToolCall> & { id: string; name: string }) => void;
  setVote: (id: AgentId, vote: AgentVote) => void;
  resetAgents: () => void;
}

const empty: Record<AgentId, AgentSnapshot | null> = {
  melchior: null,
  balthasar: null,
  casper: null,
};

function apiStatusToAgentStatus(s: string): AgentStatus {
  switch (s) {
    case 'completed': return 'completed';
    case 'failed': return 'error';
    case 'max_steps':
    case 'timed_out':
    case 'cancelled': return 'error';
    default: return 'running';
  }
}

function safeParseArgs(args: string): Record<string, string> {
  try {
    const parsed = JSON.parse(args);
    if (parsed && typeof parsed === 'object') {
      const out: Record<string, string> = {};
      for (const [k, v] of Object.entries(parsed)) out[k] = String(v);
      return out;
    }
  } catch {
    // not JSON -- fall through
  }
  return args ? { raw: args } : {};
}

export const useAgentStore = create<AgentState>((set) => ({
  agents: empty,
  maxSteps: 12,
  setMaxSteps: (n) => set({ maxSteps: n > 0 ? n : 12 }),

  loadAgents: (agents) => set({ agents }),

  loadAgentsFromApi: (snap) => {
    const agents = { ...empty } as Record<AgentId, AgentSnapshot | null>;
    for (const [k, v] of Object.entries(snap)) {
      const id = k as AgentId;
      const vote: AgentVote | null = v.vote
        ? { stance: normalizeStance(v.vote.stance), confidence: v.vote.confidence, reasoning: v.vote.reasoning }
        : null;
      agents[id] = {
        agentId: id,
        status: apiStatusToAgentStatus(v.status),
        step: v.step ?? 0,
        maxSteps: useAgentStore.getState().maxSteps,
        thought: '',
        toolCalls: (v.tool_calls ?? []).map((tc) => ({
          id: tc.tool_call_id,
          name: tc.tool_name,
          params: tc.arguments ? safeParseArgs(tc.arguments) : {},
          result: tc.result || null,
          error: tc.err || undefined,
          durationMs: tc.duration_ms > 0 ? tc.duration_ms : undefined,
          timestamp: '',
        })),
        evidence: (v.evidence ?? []).map((e) => ({
          id: e.id,
          source: e.source,
          reliability: e.reliability,
          url: e.url,
          observation: e.observation,
          timestamp: e.timestamp,
        })),
        claims: (v.claims ?? []).map((cl) => ({ id: cl.id, text: cl.text, supports: cl.supports, contradicts: cl.contradicts })),
        vote,
      };
    }
    set({ agents });
  },

  patchAgent: (id, patch) =>
    set((s) => ({
      agents: {
        ...s.agents,
        [id]: {
          agentId: id,
          status: 'idle' as AgentStatus,
          step: 0,
          maxSteps: 12,
          thought: '',
          toolCalls: [],
          evidence: [],
          claims: [],
          vote: null,
          ...s.agents[id],
          ...patch,
        },
      },
    })),

  updateAgentStatus: (id, status) =>
    set((s) => ({
      agents: {
        ...s.agents,
        [id]: s.agents[id] ? { ...s.agents[id]!, status } : null,
      },
    })),

  addEvidence: (id, evidence) =>
    set((s) => {
      const cur = s.agents[id];
      if (!cur || cur.evidence.some((e) => e.id === evidence.id)) return {};
      return { agents: { ...s.agents, [id]: { ...cur, evidence: [...cur.evidence, evidence] } } };
    }),

  addClaim: (id, claim) =>
    set((s) => {
      const cur = s.agents[id];
      if (!cur) return {};
      if (claim.id && cur.claims.some((c) => c.id === claim.id)) return {};
      return { agents: { ...s.agents, [id]: { ...cur, claims: [...cur.claims, claim] } } };
    }),

  upsertToolCall: (id, tc) =>
    set((s) => {
      const cur = s.agents[id];
      if (!cur) return {};
      const idx = cur.toolCalls.findIndex((t) => t.id === tc.id);
      const toolCalls = idx >= 0
        ? cur.toolCalls.map((t, i) => (i === idx ? { ...t, ...tc } : t))
        : [...cur.toolCalls, { params: {}, result: null, timestamp: '', ...tc }];
      return { agents: { ...s.agents, [id]: { ...cur, toolCalls } } };
    }),

  setVote: (id, vote) =>
    set((s) => {
      const cur = s.agents[id];
      if (!cur) return {};
      return { agents: { ...s.agents, [id]: { ...cur, vote } } };
    }),

  resetAgents: () => set({ agents: empty }),
}));
