import { describe, expect, it, vi } from 'vitest';
import { fireEvent, render, screen, waitFor } from '@testing-library/react';

const { mockList, mockApprove, mockReject } = vi.hoisted(() => ({
  mockList: vi.fn(),
  mockApprove: vi.fn(),
  mockReject: vi.fn(),
}));

vi.mock('@/api/client', () => ({
  api: {
    listApprovals: mockList,
    approveApproval: mockApprove,
    rejectApproval: mockReject,
  },
}));

import Approvals from '@/pages/Approvals';

const approval = {
  id: 'appr-1',
  case_id: 'case-1',
  run_id: 'run-1',
  agent_code: 'balthasar',
  tool_name: 'code_runner',
  arguments: '{"code":"print(1)"}',
  status: 'pending',
  requested_at: '2026-08-09T12:00:00Z',
};

describe('Approvals', () => {
  it('lists pending approvals and approves', async () => {
    mockList.mockResolvedValue({ approvals: [approval] });
    mockApprove.mockResolvedValue({ ...approval, status: 'approved', decided_by: 'admin' });
    render(<Approvals />);
    await screen.findByText(/code_runner/);
    fireEvent.click(screen.getByText('Approve'));
    await waitFor(() => expect(mockApprove).toHaveBeenCalledWith('appr-1'));
  });

  it('rejects an approval', async () => {
    mockList.mockResolvedValue({ approvals: [approval] });
    mockReject.mockResolvedValue({ ...approval, status: 'rejected', decided_by: 'admin' });
    render(<Approvals />);
    await screen.findByText(/code_runner/);
    fireEvent.click(screen.getByText('Reject'));
    await waitFor(() => expect(mockReject).toHaveBeenCalledWith('appr-1'));
  });
});
