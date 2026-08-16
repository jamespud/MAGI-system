import { create } from 'zustand';
import type { Case, CaseStatus, CaseSummary } from '@/types/case';
import { api, type ApiCaseResponse } from '@/api/client';

function mapToSummary(c: ApiCaseResponse): CaseSummary {
  return {
    id: c.id,
    question: c.question,
    parentCaseId: c.parent_case_id ?? '',
    status: c.status as CaseStatus,
    round: c.round,
    createdAt: c.created_at,
    pinned: c.pinned ?? false,
    archived: c.archived ?? false,
  };
}

function mapToCase(c: ApiCaseResponse): Case {
  return {
    id: c.id,
    question: c.question,
    background: c.background,
    constraints: (c.constraints || []).map(ct => ({ label: ct.label, value: ct.value })),
    parentCaseId: c.parent_case_id ?? '',
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
    dissent: (c.dissent || []).map((d) => ({
      agentCode: d.agent_code,
      decision: d.decision,
      reasoning: d.reasoning ?? '',
      evidenceIds: d.evidence_ids ?? [],
      claimIds: d.claim_ids ?? [],
      conditions: d.conditions ?? [],
    })),
    finalDecision: c.final_decision ?? '',
    pinned: c.pinned ?? false,
    archived: c.archived ?? false,
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
  total: number;
  page: number;
  hasMore: boolean;
  loading: boolean;
  error: string | null;
  loadCase: (c: Case) => void;
  loadCaseList: (list: CaseSummary[]) => void;
  updateCaseStatus: (caseId: string, status: Case['status'], round: number) => void;
  updateConsensus: (consensus: Case['consensus'], confidence: number) => void;
  setLoading: (loading: boolean) => void;
  setError: (error: string | null) => void;
  fetchCases: (opts?: { reset?: boolean }) => Promise<void>;
  loadMoreCases: () => Promise<void>;
  updateCaseFlags: (caseId: string, patch: { pinned?: boolean; archived?: boolean }) => Promise<void>;
  deleteCase: (caseId: string) => Promise<void>;
  fetchCase: (id: string, opts?: { silent?: boolean }) => Promise<void>;
  createCase: (question: string, background?: string) => Promise<ApiCaseResponse>;
  forkCase: (id: string) => Promise<ApiCaseResponse>;
  runCase: (id: string) => Promise<void>;
  cancelCase: (id: string) => Promise<void>;
}

const PAGE_SIZE = 20;

export const useCaseStore = create<CaseState>((set, get) => ({
  case: null,
  cases: [],
  total: 0,
  page: 0,
  hasMore: false,
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

  fetchCases: async (opts?: { reset?: boolean }) => {
    const reset = opts?.reset ?? true;
    set({ loading: true, error: null });
    try {
      const r = await api.getCasesPaged(1, PAGE_SIZE);
      set({
        cases: r.cases.map(mapToSummary),
        total: r.total ?? r.cases.length,
        page: 1,
        hasMore: (r.total ?? r.cases.length) > r.cases.length,
        loading: false,
      });
    } catch (e) {
      set({ error: (e as Error).message, loading: false });
    }
    void reset;
  },

  loadMoreCases: async () => {
    const { page, cases } = get();
    try {
      const next = page + 1;
      const r = await api.getCasesPaged(next, PAGE_SIZE);
      const merged = [...cases];
      const seen = new Set(merged.map((c) => c.id));
      for (const c of r.cases) {
        if (!seen.has(c.id)) {
          merged.push(mapToSummary(c));
          seen.add(c.id);
        }
      }
      set({
        cases: merged,
        total: r.total ?? merged.length,
        page: next,
        hasMore: (r.total ?? merged.length) > merged.length,
      });
    } catch (e) {
      set({ error: (e as Error).message });
    }
  },

  updateCaseFlags: async (caseId, patch) => {
    const updated = await api.patchCase(caseId, patch);
    set((s) => ({
      case: s.case && s.case.id === caseId
        ? { ...s.case, pinned: updated.pinned ?? s.case.pinned, archived: updated.archived ?? s.case.archived }
        : s.case,
      cases: s.cases.map((x) =>
        x.id === caseId
          ? { ...x, pinned: updated.pinned ?? x.pinned, archived: updated.archived ?? x.archived }
          : x,
      ),
    }));
  },

  deleteCase: async (caseId) => {
    await api.deleteCase(caseId);
    set((s) => ({
      case: s.case && s.case.id === caseId ? null : s.case,
      cases: s.cases.filter((x) => x.id !== caseId),
      total: Math.max(0, s.total - 1),
      hasMore: s.total - 1 > s.cases.length - 1,
    }));
  },

  fetchCase: async (id: string, opts?: { silent?: boolean }) => {
    if (!opts?.silent) set({ loading: true, error: null });
    try {
      const c = await api.getCase(id);
      set((s) => ({
        case: mapToCase(c),
        cases: upsertSummary(s.cases, mapToSummary(c)),
        loading: false,
      }));
    } catch (e) {
      if (!opts?.silent) set({ error: (e as Error).message, loading: false });
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

  forkCase: async (id: string) => {
    set({ loading: true, error: null });
    try {
      const c = await api.forkCase(id);
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
