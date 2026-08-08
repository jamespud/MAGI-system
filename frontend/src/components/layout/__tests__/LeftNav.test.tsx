import { describe, it, expect, beforeEach, vi } from 'vitest';
import { render, fireEvent } from '@testing-library/react';
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


  it('shows in-progress statuses (e.g. NORMALIZING) in the Running section', () => {
    const { getByText } = render(
      <MemoryRouter>
        <LeftNav cases={[{ id: 'c1', question: 'Active question', status: 'NORMALIZING' as const, round: 1, createdAt: 't', pinned: false }]} />
      </MemoryRouter>
    );
    expect(getByText('Running')).toBeDefined();
    expect(getByText('Active question')).toBeDefined();
  });
  it('paginates the Completed section when many cases are resolved', () => {
    const resolved = Array.from({ length: 12 }, (_, i) => ({
      id: `c-${i}`, question: `Question ${i}`, status: 'RESOLVED' as const,
      round: 1, createdAt: 't', pinned: false,
    }));
    const { getByText, queryByText, getByLabelText, getByRole } = render(
      <MemoryRouter>
        <LeftNav cases={resolved} />
      </MemoryRouter>
    );
    // Completed starts collapsed — no list visible, count badge shown
    expect(getByText('12')).toBeDefined();
    expect(queryByText('Question 0')).toBeNull();

    // Expand
    fireEvent.click(getByRole('button'));

    expect(getByText('1/2')).toBeDefined();
    expect(getByText('Question 0')).toBeDefined();
    expect(queryByText('Question 10')).toBeNull();
    fireEvent.click(getByLabelText('Next Completed page'));
    expect(getByText('Question 10')).toBeDefined();
  });

  it('paginates the Pinned section when many cases are pinned', () => {
    const pinned = Array.from({ length: 12 }, (_, i) => ({
      id: `p-${i}`, question: `Pinned ${i}`, status: 'RESOLVED' as const,
      round: 1, createdAt: 't', pinned: true,
    }));
    const { getByText, queryByText, getByLabelText } = render(
      <MemoryRouter>
        <LeftNav cases={pinned} />
      </MemoryRouter>
    );
    expect(getByText('1/2')).toBeDefined();
    expect(getByText('Pinned 0')).toBeDefined();
    expect(queryByText('Pinned 10')).toBeNull();
    fireEvent.click(getByLabelText('Next Pinned page'));
    expect(getByText('Pinned 10')).toBeDefined();
  });
});
