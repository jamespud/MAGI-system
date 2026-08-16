import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, findByText } from '@testing-library/react';
import { MemoryRouter } from 'react-router-dom';
import Settings from '../Settings';

const mockGetVersion = vi.fn();
const mockGetReady = vi.fn();

vi.mock('@/api/client', async (importOriginal) => {
  const actual = await importOriginal<typeof import('@/api/client')>();
  return {
    ...actual,
    api: {
      ...actual.api,
      getVersion: (...a: unknown[]) => mockGetVersion(...a),
      getReady: (...a: unknown[]) => mockGetReady(...a),
    },
  };
});

beforeEach(() => {
  vi.restoreAllMocks();
});

describe('Settings page', () => {
  it('renders version and readiness', async () => {
    mockGetVersion.mockResolvedValue({ version: '2.0.0' });
    mockGetReady.mockResolvedValue({ status: 'ready' });

    const { container } = render(<MemoryRouter><Settings /></MemoryRouter>);
    await findByText(container, '2.0.0');
    expect(container.textContent ?? '').toContain('ready');
  });
});
