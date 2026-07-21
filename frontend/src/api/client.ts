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
  status: string;
  consensus: ApiConsensus | null;
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

  cancelCase: (id: string) => request<ApiRunResponse>(`/cases/${id}/cancel`, { method: 'POST' }),

  getAgents: (id: string) => request<Record<string, ApiAgentSnapshot>>(`/cases/${id}/agents`),

  getEvidence: (id: string) => request<ApiEvidence[]>(`/cases/${id}/evidence`),

  getClaims: (id: string) => request<ApiClaim[]>(`/cases/${id}/claims`),

  getVotes: (id: string) => request<ApiVote[]>(`/cases/${id}/votes`),

  getEvents: (id: string) => request<ApiEvent[]>(`/cases/${id}/events`),
};

export type {
  ApiCaseResponse,
  ApiConsensus,
  ApiRunResponse,
  ApiAgentSnapshot,
  ApiToolCall,
  ApiEvidence,
  ApiClaim,
  ApiVote,
  ApiEvent,
};
