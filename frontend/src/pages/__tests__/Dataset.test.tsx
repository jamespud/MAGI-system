import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, fireEvent, waitFor } from '@testing-library/react';
import Dataset from '../Dataset';

const mockList = vi.fn();
const mockCreate = vi.fn();
const mockAddItems = vi.fn();
const mockStartRun = vi.fn();
const mockGetRun = vi.fn();

vi.mock('@/api/client', () => ({
  api: {
    listDatasets: (...a: unknown[]) => mockList(...a),
    createDataset: (...a: unknown[]) => mockCreate(...a),
    addDatasetItems: (...a: unknown[]) => mockAddItems(...a),
    startDatasetRun: (...a: unknown[]) => mockStartRun(...a),
    getBenchmarkRun: (...a: unknown[]) => mockGetRun(...a),
  },
}));

beforeEach(() => {
  vi.restoreAllMocks();
  mockList.mockResolvedValue({ datasets: [{ id: 'd1', name: 'launch-eval', description: '', item_count: 0, created_at: 'x' }] });
});

describe('Dataset page', () => {
  it('renders datasets and runs a benchmark', async () => {
    mockAddItems.mockResolvedValue({ added: 2 });
    mockStartRun.mockResolvedValue({ id: 'r1', dataset_id: 'd1', status: 'queued', total: 2, matched: 0, accuracy: 0, weighted_accuracy: 0, started_at: 'x' });
    mockGetRun.mockResolvedValue({
      run: { id: 'r1', dataset_id: 'd1', status: 'succeeded', total: 2, matched: 1, accuracy: 0.5, weighted_accuracy: 0.5, started_at: 'x', completed_at: 'y' },
      results: [{ id: 'x1', case_id: 'case-1', expected_decision: 'approve', actual_decision: 'approve', matched: true, score: 1 }],
    });

    mockList
      .mockResolvedValueOnce({ datasets: [{ id: 'd1', name: 'launch-eval', description: '', item_count: 0, created_at: 'x' }] })
      .mockResolvedValueOnce({ datasets: [{ id: 'd1', name: 'launch-eval', description: '', item_count: 2, created_at: 'x' }] });
    const { findByText, getByText } = render(<Dataset />);
    await findByText('launch-eval');

    fireEvent.click(getByText('Add demo items'));
    await waitFor(() => expect(mockAddItems).toHaveBeenCalledWith('d1', [
      { question: 'Should we adopt Rust for the core service?', expected_decision: 'approve' },
      { question: 'Should we migrate to a new database now?', expected_decision: 'reject' },
    ]));

    fireEvent.click(getByText('Run benchmark'));
    await waitFor(() => expect(mockStartRun).toHaveBeenCalledWith('d1', 1, 0));
    await waitFor(() => {
      const text = document.body.textContent ?? '';
      expect(text).toContain('succeeded');
      expect(text).toContain('accuracy 50%');
    });
  });
});
