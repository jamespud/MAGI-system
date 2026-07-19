import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render } from '@testing-library/react';
import { MemoryRouter, Routes, Route } from 'react-router-dom';
import DecisionWorkspace from '../DecisionWorkspace';

const mockFetchCase = vi.fn();
const mockFetchCases = vi.fn();
const mockLoadAgents = vi.fn();
const mockLoadEvents = vi.fn();

vi.mock('@/stores', () => {
  const storeHook = vi.fn((selector?: (s: Record<string, unknown>) => unknown) => {
    return selector ? selector({ case: null, cases: [], loading: false, error: null }) : null;
  });
  Object.assign(storeHook, {
    getState: () => ({
      fetchCase: mockFetchCase,
      fetchCases: mockFetchCases,
      createCase: vi.fn(),
      loadCase: vi.fn(),
      loadCaseList: vi.fn(),
    }),
  });
  const agentHook = vi.fn(() => ({}));
  Object.assign(agentHook, {
    getState: () => ({ loadAgents: mockLoadAgents }),
  });
  const eventHook = vi.fn(() => ({}));
  Object.assign(eventHook, {
    getState: () => ({ loadEvents: mockLoadEvents }),
  });
  return {
    useCaseStore: storeHook,
    useAgentStore: agentHook,
    useEventStore: eventHook,
  };
});

vi.mock('@/mock/data', () => ({
  createMockAgents: () => ({}),
  createMockEvents: () => [],
}));

describe('DecisionWorkspace', () => {
  beforeEach(() => {
    mockFetchCase.mockReset();
  });

  it('shows create form when no caseId', () => {
    const { getByPlaceholderText } = render(
      <MemoryRouter initialEntries={['/']}>
        <Routes>
          <Route index element={<DecisionWorkspace />} />
          <Route path="case/:caseId" element={<DecisionWorkspace />} />
        </Routes>
      </MemoryRouter>
    );

    expect(getByPlaceholderText('What decision should MAGI analyze?')).toBeDefined();
  });

  it('calls fetchCase when caseId present', () => {
    render(
      <MemoryRouter initialEntries={['/case/case-001']}>
        <Routes>
          <Route index element={<DecisionWorkspace />} />
          <Route path="case/:caseId" element={<DecisionWorkspace />} />
        </Routes>
      </MemoryRouter>
    );

    expect(mockFetchCase).toHaveBeenCalledWith('case-001');
  });
});
