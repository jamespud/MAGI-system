import { create } from 'zustand';
import type { Case, CaseSummary } from '@/types/case';

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
}));
