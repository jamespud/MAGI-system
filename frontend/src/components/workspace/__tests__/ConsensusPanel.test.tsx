import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render } from '@testing-library/react';
import type { AgentSnapshot } from '@/types/agent';
import type { Case } from '@/types/case';
import type { Stance } from '@/lib/stance';

let _case: Case | null = null;
let _agents: Record<string, AgentSnapshot | null> = { melchior: null, balthasar: null, casper: null };

vi.mock('@/stores', () => ({
  useCaseStore: (sel: (s: { case: Case | null }) => unknown) => sel({ case: _case }),
  useAgentStore: (sel: (s: { agents: Record<string, AgentSnapshot | null> }) => unknown) => sel({ agents: _agents }),
}));

import ConsensusPanel from '../ConsensusPanel';

const setCase = (c: Case | null) => { _case = c; };
const setAgents = (a: Record<string, AgentSnapshot | null>) => { _agents = a; };

const baseCase = (overrides: Partial<Case> = {}): Case => ({
  id: 'c1', question: 'q', background: '', constraints: [], status: 'RESOLVED',
  round: 1, consensus: { approve: 2, reject: 1, abstain: 0, majority: 'Approve', needReflection: false },
  confidence: 80, finalDecision: 'approve', createdAt: 't', updatedAt: 't', ...overrides,
});

const votedAgent = (id: string, stance: Stance, confidence: number): AgentSnapshot => ({
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
      melchior: votedAgent('melchior', 'reject', 85),
      balthasar: votedAgent('balthasar', 'abstain', 50),
      casper: votedAgent('casper', 'reject', 70),
    });

    const { getByText, getAllByText } = render(<ConsensusPanel />);
    expect(getByText('0 : 2')).toBeDefined();
    expect(getAllByText('Deadlocked').length).toBeGreaterThanOrEqual(1);
  });

  it('counts conditional_approve as neutral (not approve nor reject)', () => {
    setCase(baseCase({ status: 'DEADLOCKED', consensus: null, confidence: 0 }));
    setAgents({
      melchior: votedAgent('melchior', 'approve', 80),
      balthasar: votedAgent('balthasar', 'conditional_approve', 60),
      casper: votedAgent('casper', 'reject', 70),
    });
    const { getByText } = render(<ConsensusPanel />);
    expect(getByText('1 : 1')).toBeDefined();
  });

  it('shows Pending when no votes and no consensus', () => {
    setCase(baseCase({ status: 'DRAFT', consensus: null, confidence: 0 }));
    setAgents({ melchior: null, balthasar: null, casper: null });
    const { getAllByText } = render(<ConsensusPanel />);
    expect(getAllByText('Pending').length).toBeGreaterThanOrEqual(1);
  });

  it('lights the correct timeline step for INVESTIGATING', () => {
    setCase(baseCase({ status: 'INVESTIGATING', consensus: null, confidence: 0 }));
    setAgents({ melchior: null, balthasar: null, casper: null });
    const { container } = render(<ConsensusPanel />);
    // Round 1 dot should be lit (accent bg), Debate dot should be dim
    const dots = container.querySelectorAll('.rounded-full');
    expect(dots[0].className).toContain('bg-accent');
    expect(dots[1].className).toContain('bg-border-dim');
  });

  it('lights all steps for RESOLVED', () => {
    setCase(baseCase({ status: 'RESOLVED' }));
    const { container } = render(<ConsensusPanel />);
    const dots = container.querySelectorAll('.rounded-full');
    for (const dot of dots) {
      expect(dot.className).toContain('bg-accent');
    }
  });
});
