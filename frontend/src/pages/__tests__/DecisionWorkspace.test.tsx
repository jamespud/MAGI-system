import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, fireEvent, waitFor } from '@testing-library/react';
import { MemoryRouter, Routes, Route } from 'react-router-dom';
import DecisionWorkspace from '../DecisionWorkspace';

const mockFetchCase = vi.fn();
const mockRunCase = vi.fn().mockResolvedValue(undefined);
const mockLoadAgentsFromApi = vi.fn();
const mockClearEvents = vi.fn();
const mockPushEvent = vi.fn();
const mockGetAgents = vi.fn().mockResolvedValue({});
const mockGetEvents = vi.fn().mockResolvedValue([]);
const mockSubscribe = vi.fn().mockReturnValue(() => {});

vi.mock('@/api/client', () => ({
  api: {
    getAgents: (...args: unknown[]) => mockGetAgents(...args),
    getEvents: (...args: unknown[]) => mockGetEvents(...args),
  },
}));

vi.mock('@/api/stream', () => ({
  subscribeCaseStream: (...args: unknown[]) => mockSubscribe(...args),
}));

vi.mock('@/api/eventMapper', () => ({
  mapBackendEvent: (e: { id: string; type: string; message: string; timestamp: string }) => ({
    id: e.id, type: 'ERROR', timestamp: e.timestamp, message: e.message,
  }),
}));

let currentCase: unknown = null;

vi.mock('@/stores', () => {
  const caseHook = vi.fn((selector?: (s: Record<string, unknown>) => unknown) => {
    const state = { case: currentCase, cases: [], loading: false, error: null };
    return selector ? selector(state) : state;
  });
  Object.assign(caseHook, {
    getState: () => ({
      fetchCase: mockFetchCase,
      runCase: mockRunCase,
      createCase: vi.fn(),
    }),
  });
  const agentHook = vi.fn(() => ({}));
  Object.assign(agentHook, { getState: () => ({ loadAgentsFromApi: mockLoadAgentsFromApi }) });
  const eventHook = vi.fn(() => ({}));
  Object.assign(eventHook, { getState: () => ({ clearEvents: mockClearEvents, pushEvent: mockPushEvent }) });
  return { useCaseStore: caseHook, useAgentStore: agentHook, useEventStore: eventHook };
});

// Stub child components to avoid rendering their internals.
vi.mock('@/components/workspace/CaseHeader', () => ({ default: () => null }));
vi.mock('@/components/workspace/AgentTrio', () => ({ default: () => null }));
vi.mock('@/components/workspace/ConsensusPanel', () => ({ default: () => null }));
vi.mock('@/components/workspace/DecisionInput', () => ({ default: () => null }));
vi.mock('@/components/evidence', () => ({ EvidenceGraph: () => null }));
vi.mock('@/components/ui', () => ({ Button: ({ children, onClick, disabled }: { children: React.ReactNode; onClick?: () => void; disabled?: boolean }) => (
  <button onClick={onClick} disabled={disabled}>{children}</button>
) }));

function renderAt(path: string) {
  return render(
    <MemoryRouter initialEntries={[path]}>
      <Routes>
        <Route index element={<DecisionWorkspace />} />
        <Route path="case/:caseId" element={<DecisionWorkspace />} />
      </Routes>
    </MemoryRouter>
  );
}

describe('DecisionWorkspace', () => {
  beforeEach(() => {
    currentCase = null;
    vi.clearAllMocks();
  });

  it('shows create form when no caseId', () => {
    const { getByPlaceholderText } = renderAt('/');
    expect(getByPlaceholderText('What decision should MAGI analyze?')).toBeDefined();
  });

  it('calls fetchCase when caseId present', () => {
    renderAt('/case/case-001');
    expect(mockFetchCase).toHaveBeenCalledWith('case-001');
  });

  it('loads agents + events + subscribes to stream when caseId present', async () => {
    renderAt('/case/case-001');
    await waitFor(() => {
      expect(mockGetAgents).toHaveBeenCalledWith('case-001');
      expect(mockGetEvents).toHaveBeenCalledWith('case-001');
      expect(mockSubscribe).toHaveBeenCalledWith('case-001', expect.any(Function));
      expect(mockClearEvents).toHaveBeenCalled();
    });
  });

  it('shows Run button that calls runCase', async () => {
    currentCase = { id: 'case-001', question: 'q', status: 'DRAFT' };
    const { getByText } = renderAt('/case/case-001');
    const btn = getByText('Run Decision');
    fireEvent.click(btn);
    await waitFor(() => expect(mockRunCase).toHaveBeenCalledWith('case-001'));
  });
});
