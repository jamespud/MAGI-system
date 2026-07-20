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
        step: 0,
        maxSteps: 12,
        thought: '',
        toolCalls: [],
        evidence: [],
        claims: [],
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
