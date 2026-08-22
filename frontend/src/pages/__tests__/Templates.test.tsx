import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, fireEvent, waitFor } from '@testing-library/react';
import Templates from '../Templates';

const mocks = {
  list: vi.fn(),
  create: vi.fn(),
  setEnabled: vi.fn(),
  runNow: vi.fn(),
  remove: vi.fn(),
};

vi.mock('@/api/client', () => ({
  api: {
    listRecurring: (...a: unknown[]) => mocks.list(...a),
    createRecurring: (...a: unknown[]) => mocks.create(...a),
    setRecurringEnabled: (...a: unknown[]) => mocks.setEnabled(...a),
    runRecurringNow: (...a: unknown[]) => mocks.runNow(...a),
    deleteRecurring: (...a: unknown[]) => mocks.remove(...a),
  },
}));

beforeEach(() => {
  vi.restoreAllMocks();
  mocks.list.mockResolvedValue([
    { id: 'rc1', name: 'daily review', question: 'keep the stack?', background: '', interval_seconds: 3600, enabled: true, created_at: 'x' },
  ]);
});

describe('Templates page', () => {
  it('creates a template with optional background', async () => {
    mocks.create.mockResolvedValue({ id: 'rc3', name: 'bg', question: 'q?', background: 'ctx', interval_seconds: 3600, enabled: true, created_at: 'x' });
    mocks.list.mockResolvedValue([{ id: 'rc1', name: 'daily review', question: 'keep the stack?', background: '', interval_seconds: 3600, enabled: true, created_at: 'x' }]);

    const { getByPlaceholderText, getByText, findByText } = render(<Templates />);
    await findByText('daily review');
    fireEvent.change(getByPlaceholderText('Template name'), { target: { value: 'bg' } });
    fireEvent.change(getByPlaceholderText('Decision question'), { target: { value: 'q?' } });
    fireEvent.change(getByPlaceholderText('Background / hints (optional)'), { target: { value: 'ctx' } });
    fireEvent.click(getByText('Create template'));

    await waitFor(() => expect(mocks.create).toHaveBeenCalledWith('bg', 'q?', 'ctx', 3600));
  });

  it('creates a template and renders the list', async () => {
    mocks.create.mockResolvedValue({ id: 'rc2', name: 'weekly', question: 'expand?', background: '', interval_seconds: 604800, enabled: true, created_at: 'x' });
    mocks.list.mockResolvedValue([
      { id: 'rc1', name: 'daily review', question: 'keep the stack?', background: '', interval_seconds: 3600, enabled: true, created_at: 'x' },
      { id: 'rc2', name: 'weekly', question: 'expand?', background: '', interval_seconds: 604800, enabled: true, created_at: 'x' },
    ]);

    const { getByPlaceholderText, getByText, findByText } = render(<Templates />);
    await findByText('daily review');
    await findByText('weekly');

    fireEvent.change(getByPlaceholderText('Template name'), { target: { value: 'weekly' } });
    fireEvent.change(getByPlaceholderText('Decision question'), { target: { value: 'expand?' } });
    fireEvent.click(getByText('Create template'));

    await waitFor(() => expect(mocks.create).toHaveBeenCalledWith('weekly', 'expand?', undefined, 3600));
  });
});
