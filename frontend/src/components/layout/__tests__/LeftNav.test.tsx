import { describe, it, expect, beforeEach, vi } from 'vitest';
import { render } from '@testing-library/react';
import { MemoryRouter } from 'react-router-dom';
import LeftNav from '../LeftNav';

const { mockFetchCases } = vi.hoisted(() => ({
  mockFetchCases: vi.fn(),
}));

vi.mock('@/stores', () => {
  const storeHook = vi.fn(() => []);
  Object.assign(storeHook, {
    getState: () => ({
      fetchCases: mockFetchCases,
    }),
  });
  return {
    useCaseStore: storeHook,
    useAgentStore: vi.fn(() => ({})),
    useEventStore: vi.fn(() => ({})),
  };
});

describe('LeftNav', () => {
  beforeEach(() => {
    mockFetchCases.mockReset();
  });

  it('calls fetchCases on mount', () => {
    render(
      <MemoryRouter>
        <LeftNav cases={[]} />
      </MemoryRouter>
    );

    expect(mockFetchCases).toHaveBeenCalledOnce();
  });

  it('renders Decision Center heading', () => {
    const { getByText } = render(
      <MemoryRouter>
        <LeftNav cases={[]} />
      </MemoryRouter>
    );

    expect(getByText('Decision Center')).toBeDefined();
  });
});
