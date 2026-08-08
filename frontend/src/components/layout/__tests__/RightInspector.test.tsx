import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render } from '@testing-library/react';

const mocks = vi.hoisted(() => ({
  clearSelection: vi.fn(),
  getEvidence: vi.fn(),
  state: {
    selected: null as unknown,
    agents: {} as Record<string, unknown>,
    events: [] as unknown[],
  },
}));

vi.mock('react-router-dom', () => ({ useParams: () => ({ caseId: 'c1' }) }));
vi.mock('@/api/client', () => ({ api: { getEvidence: mocks.getEvidence } }));
vi.mock('@/stores', () => ({
  useUiStore: Object.assign(vi.fn((sel: (s: { selected: unknown }) => unknown) => sel({ selected: mocks.state.selected })), {
    getState: () => ({ clearSelection: mocks.clearSelection }),
  }),
  useAgentStore: (sel: (s: { agents: Record<string, unknown> }) => unknown) => sel({ agents: mocks.state.agents }),
  useEventStore: (sel: (s: { events: unknown[] }) => unknown) => sel({ events: mocks.state.events }),
}));

import RightInspector from '../RightInspector';

const melchior = {
  agentId: 'melchior',
  status: 'completed',
  step: 5,
  maxSteps: 12,
  thought: 'checked the tape',
  toolCalls: [
    {
      id: 'tc1',
      name: 'mcp_stock_mcp_get_quote',
      params: { symbol: 'NVDA' },
      result: '{"close":223.96}',
      error: undefined,
      durationMs: 8,
      timestamp: '',
    },
    {
      id: 'tc2',
      name: 'mcp_stock_mcp_get_news',
      params: { symbol: 'NVDA' },
      result: null,
      error: 'tool failed',
      durationMs: undefined,
      timestamp: '',
    },
  ],
  evidence: [{ id: 'EV-1', source: 'mcp', reliability: 0.78, observation: 'NVDA closed at 223.96' }],
  claims: [{ id: 'CL-1', text: 'NVDA is richly valued', supports: ['EV-1'], contradicts: [] }],
  vote: { stance: 'reject', confidence: 87, reasoning: 'too concentrated', dimensions: { safety: 20 } },
};

function agentsWith(mel: unknown) {
  return { melchior: mel, balthasar: null, casper: null };
}

describe('RightInspector item detail', () => {
  beforeEach(() => {
    mocks.clearSelection.mockReset();
    mocks.getEvidence.mockResolvedValue([
      { id: 'EV-1', source: 'mcp', url: 'https://example.com/q', observation: 'NVDA closed at 223.96', reliability: 0.78, collected_by: 'melchior', timestamp: '2026-08-08T00:00:00Z' },
    ]);
    mocks.state.events = [];
  });

  it('renders a single tool call detail', async () => {
    mocks.state.selected = { type: 'tool_call', id: 'tc1', data: { agentId: 'melchior' } };
    mocks.state.agents = agentsWith(melchior);
    const { getByText, container } = render(<RightInspector />);
    expect(getByText('mcp_stock_mcp_get_quote')).toBeDefined();
    expect(getByText('Arguments')).toBeDefined();
    expect(container.textContent).toContain('"symbol": "NVDA"');
    expect(getByText('8ms')).toBeDefined();
    expect(getByText('{"close":223.96}')).toBeDefined();
  });

  it('shows the error for a failed tool call', async () => {
    mocks.state.selected = { type: 'tool_call', id: 'tc2', data: { agentId: 'melchior' } };
    mocks.state.agents = agentsWith(melchior);
    const { getByText } = render(<RightInspector />);
    expect(getByText('tool failed')).toBeDefined();
  });

  it('shows claim detail for a claim selection', async () => {
    mocks.state.selected = { type: 'claim', id: 'CL-1', data: { agentId: 'melchior' } };
    mocks.state.agents = agentsWith(melchior);
    const { getByText } = render(<RightInspector />);
    expect(getByText('Statement')).toBeDefined();
    expect(getByText('NVDA is richly valued')).toBeDefined();
    expect(getByText('Supports')).toBeDefined();
    expect(getByText('EV-1')).toBeDefined();
  });

  it('shows evidence detail for an evidence selection', async () => {
    mocks.state.selected = { type: 'evidence', id: 'EV-1', data: { agentId: 'melchior' } };
    mocks.state.agents = agentsWith(melchior);
    const { getByText } = render(<RightInspector />);
    expect(getByText('NVDA closed at 223.96')).toBeDefined();
    expect(getByText('mcp')).toBeDefined();
    expect(getByText('0.78')).toBeDefined();
  });

  it('resolves vote selection by vote-<agent> id', async () => {
    mocks.state.selected = { type: 'vote', id: 'vote-melchior', data: { agentId: 'melchior' } };
    mocks.state.agents = agentsWith(melchior);
    const { getByText } = render(<RightInspector />);
    expect(getByText('REJECT')).toBeDefined();
    expect(getByText('too concentrated')).toBeDefined();
  });

  it('shows an empty hint when nothing is selected', async () => {
    mocks.state.selected = null;
    mocks.state.agents = agentsWith(melchior);
    const { getByText } = render(<RightInspector />);
    expect(getByText('Select an object to inspect')).toBeDefined();
  });
});
