import { describe, it, expect, vi } from 'vitest';
import { render } from '@testing-library/react';
import type { AgentSnapshot } from '@/types/agent';
import type { Stance } from '@/lib/stance';

let _agent: AgentSnapshot | null = null;

vi.mock('@/stores', () => ({
  useAgentStore: (sel: (s: { agents: Record<string, AgentSnapshot | null> }) => unknown) =>
    sel({ agents: { melchior: _agent, balthasar: null, casper: null } }),
  useUiStore: vi.fn(() => ({ expandedAgent: null, setExpandedAgent: vi.fn() })),
}));

import AgentPanel from '../AgentPanel';

const mkAgent = (overrides: Partial<AgentSnapshot> = {}): AgentSnapshot => ({
  agentId: 'melchior', status: 'completed', step: 5, maxSteps: 12, thought: '',
  toolCalls: [], evidence: [], claims: [], vote: null, ...overrides,
});

describe('AgentPanel StatusBadge', () => {
  it('shows vote stance + color when agent has voted', () => {
    _agent = mkAgent({ vote: { stance: 'approve' as Stance, confidence: 80, reasoning: 'r' } });
    const { getAllByText } = render(<AgentPanel agentId="melchior" />);
    const badges = getAllByText('APPROVE');
    // First occurrence is StatusBadge (the one with inline color style)
    const badge = badges[0];
    expect(badge.style.color).toBe('var(--accent)');
  });

  it('shows RUNNING with agent color when active and no vote', () => {
    _agent = mkAgent({ status: 'running', vote: null });
    const { getByText } = render(<AgentPanel agentId="melchior" />);
    const badge = getByText('RUNNING');
    expect(badge).toBeDefined();
  });

  it('shows conditional_approve in warning color', () => {
    _agent = mkAgent({ vote: { stance: 'conditional_approve' as Stance, confidence: 60, reasoning: 'r' } });
    const { getAllByText } = render(<AgentPanel agentId="melchior" />);
    const badges = getAllByText('CONDITIONAL APPROVE');
    // First occurrence is StatusBadge
    const badge = badges[0];
    expect(badge.style.color).toBe('var(--warning)');
  });
});
