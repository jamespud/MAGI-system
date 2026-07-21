import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render } from '@testing-library/react';
import type { AgentSnapshot } from '@/types/agent';
import type { Case } from '@/types/case';

let _case: Case | null = null;
let _agents: Record<string, AgentSnapshot | null> = { melchior: null, balthasar: null, casper: null };

vi.mock('@/stores', () => ({
  useCaseStore: (sel: (s: { case: Case | null }) => unknown) => sel({ case: _case }),
  useAgentStore: (sel: (s: { agents: Record<string, AgentSnapshot | null> }) => unknown) => sel({ agents: _agents }),
}));

// import AFTER vi.mock so it sees the mock
import ConsensusPanel from '../ConsensusPanel';

const setCase = (c: Case | null) => { _case = c; };
const setAgents = (a: Record<string, AgentSnapshot | null>) => { _agents = a; };

const baseCase = (overrides: Partial<Case> = {}): Case => ({
  id: 'c1', question: 'q', background: '', constraints: [], status: 'RESOLVED',
  round: 1, consensus: { approve: 2, reject: 1, abstain: 0, majority: 'Approve', needReflection: false },
  confidence: 80, finalDecision: 'approve', createdAt: 't', updatedAt: 't', ...overrides,
});

const votedAgent = (id: string, stance: 'Approve' | 'Reject' | 'Abstain', confidence: number): AgentSnapshot => ({
  agentId: id as AgentSnapshot['agentId'], status: 'completed', step: 0, maxSteps: 12, thought: '',
  toolCalls: [], evidence: [], claims: [], vote: { stance, confidence, reasoning: 'r' },
});

describe('ConsensusPanel', () => {
  beforeEach(() => {
    setCase(null);
    setAgents({ melchior: null, balthasar: null, casper: null });
  });

  it('derives vote counts from agents when consensus is null (deadlocked)', () => {
    setCase(baseCase({ status: 'DEADLOCKED', consensus: null, confidence: 0 }));
    setAgents({
      melchior: votedAgent('melchior', 'Reject', 85),
      balthasar: votedAgent('balthasar', 'Abstain', 50),
      casper: votedAgent('casper', 'Reject', 70),
    });

    const { getByText, getAllByText } = render(<ConsensusPanel />);
    // Derived approve:reject = 0 : 2
    expect(getByText('0 : 2')).toBeDefined();
    // Deadlocked status surfaced (badge + majority column)
    expect(getAllByText('Deadlocked').length).toBeGreaterThanOrEqual(1);
  });

  it('shows Pending when no votes and no consensus', () => {
    setCase(baseCase({ status: 'DRAFT', consensus: null, confidence: 0 }));
    setAgents({ melchior: null, balthasar: null, casper: null });
    const { getAllByText } = render(<ConsensusPanel />);
    expect(getAllByText('Pending').length).toBeGreaterThanOrEqual(1);
  });
});
