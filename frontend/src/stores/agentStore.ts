import { create } from 'zustand';
import type { AgentId, AgentSnapshot, AgentStatus, AgentVote } from '@/types/agent';
import type { ApiAgentSnapshot } from '@/api/client';

interface AgentState {
  agents: Record<AgentId, AgentSnapshot | null>;
  loadAgents: (agents: Record<AgentId, AgentSnapshot>) => void;
  loadAgentsFromApi: (snap: Record<string, ApiAgentSnapshot>) => void;
  patchAgent: (id: AgentId, patch: Partial<AgentSnapshot>) => void;
  updateAgentStatus: (id: AgentId, status: AgentStatus) => void;
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
    case 'timed_out': return 'error';
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

  loadAgents: (agents) => set({ agents }),

  loadAgentsFromApi: (snap) => {
    const agents = { ...empty } as Record<AgentId, AgentSnapshot | null>;
    for (const [k, v] of Object.entries(snap)) {
      const id = k as AgentId;
      const vote: AgentVote | null = v.vote
        ? { stance: v.vote.stance as AgentVote['stance'], confidence: v.vote.confidence, reasoning: v.vote.reasoning }
        : null;
      agents[id] = {
        agentId: id,
        status: apiStatusToAgentStatus(v.status),
        step: v.step ?? 0,
        maxSteps: 12,
        thought: '',
        toolCalls: (v.tool_calls ?? []).map((tc) => ({
          name: tc.tool_name,
          params: tc.arguments ? safeParseArgs(tc.arguments) : {},
          result: tc.result || null,
          timestamp: '',
        })),
        evidence: (v.evidence ?? []).map((e) => ({ id: e.id, source: e.source, reliability: e.reliability })),
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

  resetAgents: () => set({ agents: empty }),
}));
