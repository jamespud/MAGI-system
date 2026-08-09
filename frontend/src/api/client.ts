// API response types (snake_case as returned by backend)
interface ApiConsensus {
  approve: number;
  reject: number;
  abstain: number;
  majority: string;
  need_reflection: boolean;
}

interface ApiCaseResponse {
  id: string;
  question: string;
  background: string;
  constraints: { label: string; value: string }[];
  parent_case_id?: string;
  status: string;
  consensus: ApiConsensus | null;
  dissent?: ApiDissent[];
  final_decision: string;
  confidence: number;
  round: number;
  created_at: string;
  updated_at: string;
}

interface ApiCaseListResponse {
  cases: ApiCaseResponse[];
}

interface ApiRunResponse {
  id: string;
  status: string;
}

interface ApiToolCall {
  tool_call_id: string;
  tool_name: string;
  arguments: string;
  result: string;
  err?: string;
  evidence_id?: string;
  duration_ms: number;
}

interface ApiAgentSnapshot {
  agent_code: string;
  status: string;
  round: number;
  step: number;
  tool_calls: ApiToolCall[];
  evidence: ApiEvidence[];
  claims: ApiClaim[];
  vote?: ApiVote;
}

interface ApiEvidence {
  id: string;
  source: string;
  url?: string;
  observation: string;
  reliability: number;
  collected_by: string;
  timestamp: string;
}

interface ApiClaim {
  id: string;
  text: string;
  supports: string[];
  contradicts: string[];
  created_by: string;
}

interface ApiVote {
  id: string;
  agent_code: string;
  stance: string;
  confidence: number;
  reasoning: string;
  round: number;
}

interface ApiEvent {
  id: string;
  type: string;
  agent_code?: string;
  run_id?: string;
  message: string;
  payload?: Record<string, unknown>;
  timestamp: string;
}

const BASE_URL = '/api/v1';

async function request<T>(path: string, options?: RequestInit): Promise<T> {
  const res = await fetch(`${BASE_URL}${path}`, {
    headers: { 'Content-Type': 'application/json' },
    ...options,
  });
  if (!res.ok) {
    const err = await res.json().catch(() => ({ error: res.statusText }));
    throw new Error(err.error || `HTTP ${res.status}`);
  }
  return res.json();
}

export const api = {
  getCases: async (): Promise<ApiCaseResponse[]> => {
    const r = await request<ApiCaseListResponse | ApiCaseResponse[]>('/cases');
    return Array.isArray(r) ? r : r.cases;
  },

  getCase: (id: string) => request<ApiCaseResponse>(`/cases/${id}`),

  createCase: (question: string, background?: string) =>
    request<ApiCaseResponse>('/cases', {
      method: 'POST',
      body: JSON.stringify({ question, background }),
    }),

  runCase: (id: string) => request<ApiRunResponse>(`/cases/${id}/run`, { method: 'POST' }),

  forkCase: (id: string) => request<ApiCaseResponse>(`/cases/${id}/fork`, { method: 'POST' }),

  cancelCase: (id: string) => request<ApiRunResponse>(`/cases/${id}/cancel`, { method: 'POST' }),

  getAgents: (id: string) => request<Record<string, ApiAgentSnapshot>>(`/cases/${id}/agents`),

  getEvidence: (id: string) => request<ApiEvidence[]>(`/cases/${id}/evidence`),

  getClaims: (id: string) => request<ApiClaim[]>(`/cases/${id}/claims`),

  getVotes: (id: string) => request<ApiVote[]>(`/cases/${id}/votes`),

  getEvents: (id: string) => request<ApiEvent[]>(`/cases/${id}/events`),

  listDatasets: () => request<{ datasets: ApiDataset[] }>('/datasets'),

  createDataset: (name: string, description?: string) =>
    request<ApiDataset>('/datasets', { method: 'POST', body: JSON.stringify({ name, description }) }),

  addDatasetItems: (id: string, items: { question: string; expected_decision: string }[]) =>
    request<{ added: number }>(`/datasets/${id}/items`, { method: 'POST', body: JSON.stringify({ items }) }),

  startDatasetRun: (id: string, runs?: number, threshold?: number) =>
    request<ApiBenchmarkRun>(`/datasets/${id}/runs${runs ? `?runs=${runs}${threshold ? `&threshold=${threshold}` : ''}` : ''}`, { method: 'POST' }),

  getBenchmarkRun: (runId: string) => request<ApiBenchmarkDetail>(`/benchmarks/${runId}`),

  getDatasetRuns: (id: string) => request<ApiBenchmarkRun[]>(`/datasets/${id}/runs`),

  searchMemory: (query: string, limit = 20) =>
    request<{ results: ApiMemoryProjection[] }>(`/memory?q=${encodeURIComponent(query)}&limit=${limit}`),

  evaluateCase: (id: string) => request<ApiEvaluation>(`/evaluation/${id}`, { method: 'POST' }),

  judgeCase: (id: string) => request<ApiJudgeResult>(`/evaluation/${id}/judge`, { method: 'POST' }),

  listRecurring: () => request<ApiRecurring[]>(`/recurring`),

  createRecurring: (name: string, question: string, intervalSeconds: number) =>
    request<ApiRecurring>(`/recurring`, {
      method: 'POST',
      body: JSON.stringify({ name, question, interval_seconds: intervalSeconds }),
    }),

  setRecurringEnabled: (id: string, enabled: boolean) =>
    request<unknown>(`/recurring/${id}`, { method: 'PATCH', body: JSON.stringify({ enabled }) }),

  runRecurringNow: (id: string) => request<{ id: string; status: string }>(`/recurring/${id}/run`, { method: 'POST' }),

  listTools: () => request<{ name: string; desc: string }[]>(`/tools`),

  listApprovals: (caseId?: string) =>
    request<{ approvals: ApiApproval[] }>(`/approvals${caseId ? `?case_id=${encodeURIComponent(caseId)}` : ''}`),

  approveApproval: (id: string, reason?: string) =>
    request<ApiApproval>(`/approvals/${id}/approve`, { method: 'POST', body: JSON.stringify({ reason }) }),

  rejectApproval: (id: string, reason?: string) =>
    request<ApiApproval>(`/approvals/${id}/reject`, { method: 'POST', body: JSON.stringify({ reason }) }),

  getVersion: () => request<{ version: string }>(`/version`),

  getReady: () => request<{ status: string }>(`/ready`),

  benchmarkCases: (caseIds: string[]) =>
    request<Record<string, ApiEvaluation>>(`/benchmark`, {
      method: 'POST',
      body: JSON.stringify({ case_ids: caseIds }),
    }),

  deleteRecurring: async (id: string) => {
    const res = await fetch(`${BASE_URL}/recurring/${id}`, {
      method: 'DELETE',
      headers: { 'Content-Type': 'application/json' },
    });
    if (!res.ok) {
      const err = await res.json().catch(() => ({ error: res.statusText }));
      throw new Error(err.error || `HTTP ${res.status}`);
    }
  },
};

interface ApiRecurring {
  id: string;
  name: string;
  question: string;
  background: string;
  interval_seconds: number;
  enabled: boolean;
  last_run_at?: string;
  created_at: string;
}

interface ApiEvaluation {
  tool_success_rate: number;
  gate_failures: number;
  total_tokens: number;
  first_round_consensus: boolean;
  consensus_round: number;
}

interface ApiMemoryProjection {
  CaseID: string;
  QuestionSummary: string;
  ContextSummary: string;
  Resolution: string;
  Outcome?: { Status: string; Learned: string } | null;
  ProjectionVersion: number;
}

interface ApiDataset {
  id: string;
  name: string;
  description: string;
  item_count: number;
  created_at: string;
}

interface ApiBenchmarkRun {
  id: string;
  dataset_id: string;
  status: string;
  total: number;
  matched: number;
  accuracy: number;
  weighted_accuracy: number;
  runs_per_item?: number;
  stability?: number;
  regression_failed?: boolean;
  failure_reason?: string;
  started_at: string;
  completed_at?: string;
}

interface ApiBenchmarkItemResult {
  id: string;
  case_id: string;
  expected_decision: string;
  actual_decision: string;
  matched: boolean;
  score: number;
  runs?: number;
  consistency?: number;
  decisions?: string[];
  error?: string;
  feedback?: string;
  feedback_at?: string;
}

interface ApiBenchmarkDetail {
  run: ApiBenchmarkRun;
  results: ApiBenchmarkItemResult[];
}

interface ApiDissent {
  agent_code: string;
  decision: string;
  reasoning?: string;
  evidence_ids?: string[];
  claim_ids?: string[];
  conditions?: string[];
}

interface ApiJudgeResult {
  case_id: string;
  report_quality: number;
  evidence_consistency: number;
  reflection_validity: number;
  overall: number;
  rationale?: string;
  model_name?: string;
  created_at: string;
}

interface ApiApproval {
  id: string;
  case_id: string;
  run_id?: string;
  agent_code?: string;
  tool_name: string;
  arguments?: string;
  status: string;
  reason?: string;
  decided_by?: string;
  requested_at?: string;
  decided_at?: string;
}

export type {
  ApiCaseResponse,
  ApiDataset,
  ApiBenchmarkRun,
  ApiBenchmarkDetail,
  ApiMemoryProjection,
  ApiEvaluation,
  ApiRecurring,
  ApiConsensus,
  ApiRunResponse,
  ApiAgentSnapshot,
  ApiToolCall,
  ApiEvidence,
  ApiClaim,
  ApiVote,
  ApiEvent,
  ApiApproval,
  ApiDissent,
  ApiJudgeResult,
};
