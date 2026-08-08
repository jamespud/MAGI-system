import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, fireEvent, waitFor } from '@testing-library/react';
import { MemoryRouter } from 'react-router-dom';
import History from '../History';

const mockGetCases = vi.fn();

vi.mock('@/api/client', () => ({
  api: { getCases: (...a: unknown[]) => mockGetCases(...a) },
}));

beforeEach(() => {
  vi.restoreAllMocks();
});

describe('History page', () => {
  it('renders cases and filters by question', async () => {
    mockGetCases.mockResolvedValue([
      { id: 'c1', question: 'Adopt Rust?', background: '', constraints: [], status: 'RESOLVED', consensus: null, final_decision: 'approve', confidence: 0, round: 1, created_at: '2026-08-01T00:00:00Z', updated_at: '2026-08-01T00:00:00Z' },
      { id: 'c2', question: 'New database?', background: '', constraints: [], status: 'DRAFT', consensus: null, final_decision: '', confidence: 0, round: 0, created_at: '2026-08-02T00:00:00Z', updated_at: '2026-08-02T00:00:00Z' },
    ]);

    const { findByText, getByPlaceholderText, queryByText } = render(
      <MemoryRouter><History /></MemoryRouter>,
    );
    await findByText('Adopt Rust?');
    await findByText('New database?');

    fireEvent.change(getByPlaceholderText(/Filter by question/), { target: { value: 'rust' } });
    await waitFor(() => expect(queryByText('New database?')).toBeNull());
    expect(queryByText('Adopt Rust?')).not.toBeNull();
  });
});
