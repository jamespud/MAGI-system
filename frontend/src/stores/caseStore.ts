import { create } from 'zustand';
import type { Case, CaseStatus, CaseSummary } from '@/types/case';
import { api, type ApiCaseResponse } from '@/api/client';

function mapToSummary(c: ApiCaseResponse): CaseSummary {
  return {
    id: c.id,
    question: c.question,
    status: c.status as CaseStatus,
    round: c.round,
    createdAt: c.created_at,
    pinned: false,
  };
}

interface CaseState {
  case: Case | null;
  cases: CaseSummary[];
  loading: boolean;
  error: string | null;
  loadCase: (c: Case) => void;
  loadCaseList: (list: CaseSummary[]) => void;
  updateCaseStatus: (status: Case['status'], round: number) => void;
  updateConsensus: (consensus: Case['consensus'], confidence: number) => void;
  setLoading: (loading: boolean) => void;
  setError: (error: string | null) => void;
  fetchCases: () => Promise<void>;
}

export const useCaseStore = create<CaseState>((set) => ({
  case: null,
  cases: [],
  loading: false,
  error: null,

  loadCase: (c) => set({ case: c, loading: false }),

  loadCaseList: (list) => set({ cases: list }),

  updateCaseStatus: (status, round) =>
    set((s) => ({ case: s.case ? { ...s.case, status, round } : null })),

  updateConsensus: (consensus, confidence) =>
    set((s) => ({ case: s.case ? { ...s.case, consensus, confidence } : null })),

  setLoading: (loading) => set({ loading }),

  setError: (error) => set({ error }),

  fetchCases: async () => {
    set({ loading: true, error: null });
    try {
      const list = await api.getCases();
      set({ cases: list.map(mapToSummary), loading: false });
    } catch (e) {
      set({ error: (e as Error).message, loading: false });
    }
  },
}));
