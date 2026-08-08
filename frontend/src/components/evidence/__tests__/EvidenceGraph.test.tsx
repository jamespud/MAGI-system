import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, waitFor } from '@testing-library/react';

const { mockSelect, state } = vi.hoisted(() => ({
  mockSelect: vi.fn(),
  state: { agents: {} as Record<string, unknown> },
}));

vi.mock('react-router-dom', () => ({ useParams: () => ({ caseId: 'c1' }) }));
vi.mock('@/stores', () => ({
  useUiStore: Object.assign(vi.fn(() => ({})), { getState: () => ({ select: mockSelect }) }),
  useAgentStore: (sel: (s: { agents: Record<string, unknown> }) => unknown) => sel({ agents: state.agents }),
}));

import EvidenceGraph from '../EvidenceGraph';

const agent = (over: Record<string, unknown> = {}) => ({
  agentId: 'melchior',
  status: 'completed' as const,
  step: 0,
  maxSteps: 12,
  thought: '',
  toolCalls: [],
  evidence: [],
  claims: [],
  vote: null,
  ...over,
});

describe('EvidenceGraph', () => {
  beforeEach(() => {
    mockSelect.mockReset();
    state.agents = {};
  });

  it('renders zoom controls + 100% when evidence exists', async () => {
    state.agents = {
      melchior: agent({
        evidence: [{ id: 'EV-1', source: 's', reliability: 0.8 }],
        claims: [{ id: 'CL-1', text: 'claim', supports: ['EV-1'], contradicts: [] }],
        vote: { stance: 'approve', confidence: 80, reasoning: 'r' },
      }),
    };
    const { getByLabelText, getByText } = render(<EvidenceGraph />);
    await waitFor(() => expect(getByLabelText('Zoom in')).toBeInTheDocument());
    expect(getByLabelText('Zoom out')).toBeInTheDocument();
    expect(getByLabelText('Reset zoom')).toBeInTheDocument();
    expect(getByText('100%')).toBeInTheDocument();
  });

  it('hides zoom controls and shows empty state when no evidence', async () => {
    state.agents = { melchior: null, balthasar: null, casper: null };
    const { queryByLabelText, getByText } = render(<EvidenceGraph />);
    await waitFor(() => expect(getByText(/No evidence yet/)).toBeInTheDocument());
    expect(queryByLabelText('Zoom in')).toBeNull();
  });

  it('hides orphan evidence not referenced by any claim', async () => {
    state.agents = {
      melchior: agent({
        evidence: [
          { id: 'EV-1', source: 's', reliability: 0.8 },
          { id: 'EV-2', source: 's', reliability: 0.8 },
        ],
        claims: [{ id: 'CL-1', text: 'claim', supports: ['EV-2'], contradicts: [] }],
        vote: { stance: 'approve', confidence: 80, reasoning: 'r' },
      }),
    };
    const { container } = render(<EvidenceGraph />);
    await waitFor(() => {
      const lines = container.querySelectorAll('line[stroke]');
      expect(lines.length).toBe(2);
    });
    expect(container.textContent).not.toContain('EV-1');
  });

  it('renders one vote node per agent with the latest vote', async () => {
    state.agents = {
      melchior: agent({
        evidence: [{ id: 'EV-1', source: 's', reliability: 0.8 }],
        claims: [{ id: 'CL-1', text: 'claim', supports: ['EV-1'], contradicts: [] }],
        vote: { stance: 'approve', confidence: 80, reasoning: 'r' },
      }),
    };
    const { container } = render(<EvidenceGraph />);
    await waitFor(() => {
      const lines = container.querySelectorAll('line[stroke]');
      expect(lines.length).toBe(2);
    });
    expect(container.textContent).toContain('approve');
  });

  it('does not double-link referenced evidence (links via claim, not vote)', async () => {
    state.agents = {
      melchior: agent({
        evidence: [{ id: 'EV-1', source: 's', reliability: 0.8 }],
        claims: [{ id: 'CL-1', text: 'claim', supports: ['EV-1'], contradicts: [] }],
        vote: { stance: 'approve', confidence: 80, reasoning: 'r' },
      }),
    };
    const { container } = render(<EvidenceGraph />);
    await waitFor(() => {
      const lines = container.querySelectorAll('line[stroke]');
      expect(lines.length).toBe(2);
    });
  });
});
