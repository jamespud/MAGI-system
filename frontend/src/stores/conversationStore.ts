import { create } from 'zustand';
import {
  api,
  type ApiConversation,
  type ApiConversationMessage,
} from '@/api/client';

export interface ConversationSummary {
  id: string;
  title: string;
  updatedAt: string;
}

export interface ConversationTurn {
  id: string;
  role: 'user' | 'assistant';
  content: string;
  caseId?: string;
  createdAt: string;
}

interface ConversationState {
  conversations: ConversationSummary[];
  current: ConversationSummary | null;
  messages: ConversationTurn[];
  loading: boolean;
  sending: boolean;
  error: string | null;
  fetchConversations: () => Promise<void>;
  openConversation: (id: string) => Promise<void>;
  ask: (message: string, conversationId?: string, background?: string) => Promise<string>;
  deleteConversation: (id: string) => Promise<void>;
  resetConversation: () => void;
}

function summaryFromApi(c: ApiConversation): ConversationSummary {
  return { id: c.id, title: c.title, updatedAt: c.updated_at };
}

function turnFromApi(m: ApiConversationMessage): ConversationTurn {
  return {
    id: m.id,
    role: m.role,
    content: m.content,
    caseId: m.case_id,
    createdAt: m.created_at,
  };
}

function upsert(list: ConversationSummary[], item: ConversationSummary): ConversationSummary[] {
  const next = list.filter((c) => c.id !== item.id);
  return [item, ...next].sort((a, b) => b.updatedAt.localeCompare(a.updatedAt));
}

export const useConversationStore = create<ConversationState>((set) => ({
  conversations: [],
  current: null,
  messages: [],
  loading: false,
  sending: false,
  error: null,

  fetchConversations: async () => {
    set({ loading: true, error: null });
    try {
      const result = await api.listConversations();
      set({ conversations: result.conversations.map(summaryFromApi), loading: false });
    } catch (e) {
      set({ error: (e as Error).message, loading: false });
    }
  },

  openConversation: async (id) => {
    set({ loading: true, error: null });
    try {
      const detail = await api.getConversation(id);
      set({
        current: summaryFromApi(detail.conversation),
        messages: detail.messages.map(turnFromApi),
        loading: false,
      });
    } catch (e) {
      set({ error: (e as Error).message, loading: false });
    }
  },

  ask: async (message, conversationId, background) => {
    set({ sending: true, error: null });
    try {
      const result = await api.askAssistant(message, conversationId, background);
      const detail = await api.getConversation(result.conversation_id);
      const summary = summaryFromApi(detail.conversation);
      set((s) => ({
        conversations: upsert(s.conversations, summary),
        current: summary,
        messages: detail.messages.map(turnFromApi),
        sending: false,
      }));
      return result.conversation_id;
    } catch (e) {
      set({ sending: false, error: (e as Error).message });
      throw e;
    }
  },

  deleteConversation: async (id) => {
    set({ error: null });
    try {
      await api.deleteConversation(id);
      set((s) => ({
        conversations: s.conversations.filter((c) => c.id !== id),
        current: s.current?.id === id ? null : s.current,
        messages: s.current?.id === id ? [] : s.messages,
      }));
    } catch (e) {
      set({ error: (e as Error).message });
    }
  },

  resetConversation: () => set({ current: null, messages: [], error: null }),
}));
