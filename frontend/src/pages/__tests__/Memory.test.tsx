import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, fireEvent, waitFor } from '@testing-library/react';
import { MemoryRouter } from 'react-router-dom';
import Memory from '../Memory';

const mockSearch = vi.fn();
const mockUpdate = vi.fn();
const mockDelete = vi.fn();

vi.mock('@/api/client', () => ({
  api: {
    searchMemory: (...a: unknown[]) => mockSearch(...a),
    updateMemory: (...a: unknown[]) => mockUpdate(...a),
    deleteMemory: (...a: unknown[]) => mockDelete(...a),
  },
}));

const memory = {
  CaseID: 'case-1',
  QuestionSummary: 'Should we keep the stack?',
  ContextSummary: 'scale concerns',
  Resolution: 'approve',
  Annotation: '',
  Tags: [],
  Outcome: { Status: 'resolved', Learned: 'keep' },
  ProjectionVersion: 2,
};

beforeEach(() => {
  vi.restoreAllMocks();
});

async function searchMemories() {
  mockSearch.mockResolvedValue({ results: [{ ...memory }] });
  const view = render(<MemoryRouter><Memory /></MemoryRouter>);
  fireEvent.change(view.getByPlaceholderText(/Search historical decisions/), { target: { value: 'stack' } });
  fireEvent.click(view.getByText('Search'));
  await waitFor(() => expect(mockSearch).toHaveBeenCalledWith('stack'));
  await view.findByText(memory.QuestionSummary);
  return view;
}

describe('Memory page', () => {
  it('searches and renders historical decisions', async () => {
    await searchMemories();
    expect(document.body.textContent ?? '').toContain('learned: keep');
  });

  it('annotates and tags an existing memory', async () => {
    const { getByText, getByLabelText } = await searchMemories();
    fireEvent.click(getByText('Edit'));
    fireEvent.change(getByLabelText(/Annotation/), { target: { value: 'verified by SRE' } });
    fireEvent.change(getByLabelText(/Tags \(comma-separated\)/), { target: { value: 'ops, database' } });
    mockUpdate.mockResolvedValue({ ...memory, Annotation: 'verified by SRE', Tags: ['ops', 'database'] });
    fireEvent.click(getByText('Save'));

    await waitFor(() => expect(mockUpdate).toHaveBeenCalledWith('case-1', {
      question_summary: memory.QuestionSummary,
      context_summary: memory.ContextSummary,
      resolution: memory.Resolution,
      learned: memory.Outcome.Learned,
      annotation: 'verified by SRE',
      tags: ['ops', 'database'],
    }));
    await waitFor(() => expect(document.body.textContent ?? '').toContain('verified by SRE'));
    await waitFor(() => expect(document.body.textContent ?? '').toContain('database'));
  });

  it('deletes a memory after confirmation', async () => {
    window.confirm = vi.fn(() => true);
    mockDelete.mockResolvedValue(undefined);
    const { getByText, queryByText } = await searchMemories();
    fireEvent.click(getByText('Delete'));

    await waitFor(() => expect(mockDelete).toHaveBeenCalledWith('case-1'));
    await waitFor(() => expect(queryByText(memory.QuestionSummary)).toBeNull());
  });
});
