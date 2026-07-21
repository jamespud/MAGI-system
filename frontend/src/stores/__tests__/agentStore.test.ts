import { describe, it, expect, beforeEach, vi } from 'vitest';
import { useAgentStore } from '../agentStore';
import type { AgentSnapshot } from '@/types/agent';

vi.mock('@/api/client', () => ({ api: {} }));

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

describe('loadAgentsFromApi', () => {
  beforeEach(() => {
    useAgentStore.getState().resetAgents();
  });

  it('maps enriched API snapshot into AgentSnapshot arrays', () => {
    const snap = {
      melchior: {
        agent_code: 'melchior',
        status: 'completed',
        round: 1,
        step: 2,
        tool_calls: [
          { tool_call_id: 'call-1', tool_name: 'calc', arguments: '{"a":1}', result: '3', duration_ms: 5 },
        ],
        evidence: [
          { id: 'EV-m1', source: 'local', observation: 'obs', reliability: 0.9, collected_by: 'melchior', timestamp: 't' },
        ],
        claims: [
          { id: 'CL-m1', text: 'claim text', supports: [], contradicts: [], created_by: 'melchior' },
        ],
        vote: { id: 'v1', agent_code: 'melchior', stance: 'approve', confidence: 80, reasoning: 'r', round: 1 },
      },
    };
    useAgentStore.getState().loadAgentsFromApi(snap as never);

    const m = useAgentStore.getState().agents.melchior;
    expect(m?.status).toBe('completed');
    expect(m?.step).toBe(2);
    expect(m?.toolCalls).toHaveLength(1);
    expect(m?.toolCalls[0].name).toBe('calc');
    expect(m?.evidence).toHaveLength(1);
    expect(m?.evidence[0].id).toBe('EV-m1');
    expect(m?.claims).toHaveLength(1);
    expect(m?.claims[0].text).toBe('claim text');
    expect(m?.vote?.stance).toBe('approve');
  });

  it('handles empty arrays without throwing', () => {
    useAgentStore.getState().loadAgentsFromApi({
      casper: { agent_code: 'casper', status: 'running', round: 1, step: 0, tool_calls: [], evidence: [], claims: [] },
    } as never);
    const c = useAgentStore.getState().agents.casper;
    expect(c?.toolCalls).toEqual([]);
    expect(c?.evidence).toEqual([]);
    expect(c?.claims).toEqual([]);
    expect(c?.vote).toBeNull();
  });
});
