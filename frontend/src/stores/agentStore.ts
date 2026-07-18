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
