import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, fireEvent, waitFor } from '@testing-library/react';
import { MemoryRouter } from 'react-router-dom';
import Memory from '../Memory';

const mockSearch = vi.fn();

vi.mock('@/api/client', () => ({
  api: {
    searchMemory: (...a: unknown[]) => mockSearch(...a),
  },
}));

beforeEach(() => {
  vi.restoreAllMocks();
});

describe('Memory page', () => {
  it('searches and renders historical decisions', async () => {
    mockSearch.mockResolvedValue({
      results: [
        { CaseID: 'case-1', QuestionSummary: 'Should we keep the stack?', ContextSummary: 'scale concerns', Resolution: 'approve', Outcome: { Status: 'resolved', Learned: 'keep' }, ProjectionVersion: 2 },
      ],
    });

    const { getByPlaceholderText, getByText, findByText } = render(
      <MemoryRouter><Memory /></MemoryRouter>,
    );
    fireEvent.change(getByPlaceholderText(/Search historical decisions/), { target: { value: 'stack' } });
    fireEvent.click(getByText('Search'));

    await waitFor(() => expect(mockSearch).toHaveBeenCalledWith('stack'));
    await findByText('Should we keep the stack?');
    expect(document.body.textContent ?? '').toContain('learned: keep');
  });
});
