import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, waitFor } from '@testing-library/react';
import Audit from '../Audit';

const mockListAuditEvents = vi.fn();

vi.mock('@/api/client', () => ({
  api: {
    listAuditEvents: (...a: unknown[]) => mockListAuditEvents(...a),
  },
}));

beforeEach(() => {
  vi.restoreAllMocks();
  mockListAuditEvents.mockReset();
  mockListAuditEvents.mockResolvedValue({
    events: [
      { id: 1, user_id: 1, username: 'admin', role: 'admin', action: 'PUT', resource: '/admin/prompts/x', detail: '', status: 200, created_at: '2026-08-18T10:00:00Z' },
      { id: 2, user_id: 0, username: '', role: '', action: 'login', resource: 'oidc', detail: '', status: 302, created_at: '2026-08-18T09:00:00Z' },
    ],
    total: 2,
  });
});

describe('Audit page', () => {
  it('renders the audit trail table', async () => {
    const { findByText } = render(<Audit />);
    await waitFor(() => expect(mockListAuditEvents).toHaveBeenCalledWith(50, 0));
    await findByText('/admin/prompts/x');
    expect(document.body.textContent ?? '').toContain('admin');
    expect(document.body.textContent ?? '').toContain('login');
  });
});
