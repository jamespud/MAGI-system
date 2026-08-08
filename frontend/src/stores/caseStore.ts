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

function mapToCase(c: ApiCaseResponse): Case {
  return {
    id: c.id,
    question: c.question,
    background: c.background,
    constraints: (c.constraints || []).map(ct => ({ label: ct.label, value: ct.value })),
    status: c.status as CaseStatus,
    round: c.round,
    consensus: c.consensus ? {
      approve: c.consensus.approve,
      reject: c.consensus.reject,
      abstain: c.consensus.abstain,
      majority: c.consensus.majority as 'Approve' | 'Reject' | 'Tie',
      needReflection: c.consensus.need_reflection,
    } : null,
    confidence: c.confidence,
    finalDecision: c.final_decision ?? '',
    createdAt: c.created_at,
    updatedAt: c.updated_at,
  };
}

function upsertSummary(list: CaseSummary[], c: CaseSummary): CaseSummary[] {
  const idx = list.findIndex((x) => x.id === c.id);
  if (idx === -1) return [...list, c];
  const next = [...list];
  next[idx] = c;
  return next;
}

function patchSummary(list: CaseSummary[], id: string, patch: Partial<CaseSummary>): CaseSummary[] {
  return list.map((x) => (x.id === id ? { ...x, ...patch } : x));
}

interface CaseState {
  case: Case | null;
  cases: CaseSummary[];
  loading: boolean;
  error: string | null;
  loadCase: (c: Case) => void;
  loadCaseList: (list: CaseSummary[]) => void;
  updateCaseStatus: (caseId: string, status: Case['status'], round: number) => void;
  updateConsensus: (consensus: Case['consensus'], confidence: number) => void;
  setLoading: (loading: boolean) => void;
  setError: (error: string | null) => void;
  fetchCases: () => Promise<void>;
  fetchCase: (id: string) => Promise<void>;
  createCase: (question: string, background?: string) => Promise<ApiCaseResponse>;
  runCase: (id: string) => Promise<void>;
  cancelCase: (id: string) => Promise<void>;
}

export const useCaseStore = create<CaseState>((set) => ({
  case: null,
  cases: [],
  loading: false,
  error: null,

  loadCase: (c) => set({ case: c, loading: false }),

  loadCaseList: (list) => set({ cases: list }),

  updateCaseStatus: (caseId, status, round) =>
    set((s) => ({
      case: s.case && s.case.id === caseId ? { ...s.case, status, round } : s.case,
      cases: patchSummary(s.cases, caseId, { status, round }),
    })),

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

  fetchCase: async (id: string) => {
    set({ loading: true, error: null });
    try {
      const c = await api.getCase(id);
      set((s) => ({
        case: mapToCase(c),
        cases: upsertSummary(s.cases, mapToSummary(c)),
        loading: false,
      }));
    } catch (e) {
      set({ error: (e as Error).message, loading: false });
    }
  },

  createCase: async (question: string, background?: string) => {
    set({ loading: true, error: null });
    try {
      const c = await api.createCase(question, background);
      set((s) => ({ cases: upsertSummary(s.cases, mapToSummary(c)), loading: false }));
      return c;
    } catch (e) {
      set({ error: (e as Error).message, loading: false });
      throw e;
    }
  },

  runCase: async (id: string) => {
    set({ loading: true, error: null });
    try {
      const res = await api.runCase(id);
      set((s) => ({
        case: s.case ? { ...s.case, status: res.status as Case['status'] } : null,
        cases: patchSummary(s.cases, id, { status: res.status as Case['status'] }),
        loading: false,
      }));
    } catch (e) {
      set({ error: (e as Error).message, loading: false });
      throw e;
    }
  },

  cancelCase: async (id: string) => {
    set({ error: null });
    try {
      await api.cancelCase(id);
    } catch (e) {
      set({ error: (e as Error).message });
    }
  },
}));
