import { describe, it, expect, beforeEach } from 'vitest';
import { useAgentStore } from '../agentStore';
import type { AgentSnapshot } from '@/types/agent';

const mockSnapshot = (overrides: Partial<AgentSnapshot> = {}): AgentSnapshot => ({
  agentId: 'melchior' as const,
  status: 'running' as const,
  step: 1,
  maxSteps: 10,
  thought: '',
  toolCalls: [],
  evidence: [],
  claims: [],
  vote: null,
  ...overrides,
});

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
    const agents = {
      melchior: mockSnapshot({ agentId: 'melchior' }),
      balthasar: mockSnapshot({ agentId: 'balthasar', status: 'idle' }),
      casper: mockSnapshot({ agentId: 'casper', status: 'idle' }),
    };
    useAgentStore.getState().loadAgents(agents);
    expect(useAgentStore.getState().agents.melchior?.status).toBe('running');
  });

  it('resets all agents', () => {
    const agents = {
      melchior: mockSnapshot({ agentId: 'melchior' }),
      balthasar: mockSnapshot({ agentId: 'balthasar', status: 'idle' }),
      casper: mockSnapshot({ agentId: 'casper', status: 'idle' }),
    };
    useAgentStore.getState().loadAgents(agents);
    useAgentStore.getState().resetAgents();
    expect(useAgentStore.getState().agents.melchior).toBeNull();
  });
});
