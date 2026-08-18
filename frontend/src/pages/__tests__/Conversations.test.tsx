import { describe, expect, it, vi, beforeEach } from 'vitest';
import { fireEvent, render, screen, waitFor } from '@testing-library/react';
import { MemoryRouter, Route, Routes } from 'react-router-dom';
import Conversations from '../Conversations';

const mocks = vi.hoisted(() => ({
  list: vi.fn(),
  get: vi.fn(),
  ask: vi.fn(),
  remove: vi.fn(),
}));

vi.mock('@/api/client', () => ({
  api: {
    listConversations: mocks.list,
    getConversation: mocks.get,
    askAssistant: mocks.ask,
    deleteConversation: mocks.remove,
  },
}));

const conversation = {
  id: 'conv-001',
  title: 'Should we migrate?',
  created_at: '2026-08-18T01:00:00Z',
  updated_at: '2026-08-18T02:00:00Z',
};

beforeEach(() => {
  vi.clearAllMocks();
});

describe('Conversations page', () => {
  it('renders a thread and linked decision cases', async () => {
    mocks.list.mockResolvedValue({ conversations: [conversation] });
    mocks.get.mockResolvedValue({
      conversation,
      messages: [
        { id: 'm1', role: 'user', content: 'Should we migrate?', created_at: '2026-08-18T01:00:01Z' },
        { id: 'm2', role: 'assistant', content: 'Decision case case-001 created.', case_id: 'case-001', created_at: '2026-08-18T01:00:02Z' },
      ],
    });

    render(
      <MemoryRouter initialEntries={['/conversations/conv-001']}>
        <Routes>
          <Route path="/conversations/:conversationId" element={<Conversations />} />
          <Route path="/case/:caseId" element={<div>case workspace</div>} />
        </Routes>
      </MemoryRouter>,
    );

    expect((await screen.findAllByText('Should we migrate?')).length).toBeGreaterThan(0);
    expect(screen.getByText('Open decision case')).toHaveAttribute('href', '/case/case-001');
  });

  it('sends a follow-up with the active conversation id', async () => {
    mocks.list.mockResolvedValue({ conversations: [] });
    mocks.get.mockResolvedValue({ conversation, messages: [] });
    mocks.ask.mockResolvedValue({ id: 'case-002', conversation_id: 'conv-001' });

    render(
      <MemoryRouter initialEntries={['/conversations/conv-001']}>
        <Routes>
          <Route path="/conversations/:conversationId" element={<Conversations />} />
        </Routes>
      </MemoryRouter>,
    );

    await screen.findByText('Ask a question to start a persistent decision thread.');
    fireEvent.change(screen.getByPlaceholderText('Ask a follow-up question…'), { target: { value: 'What about rollback?' } });
    fireEvent.submit(screen.getByPlaceholderText('Ask a follow-up question…').closest('form')!);

    await waitFor(() => expect(mocks.ask).toHaveBeenCalledWith('What about rollback?', 'conv-001', undefined));
  });
});
