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
  pinned?: boolean;
  archived?: boolean;
  created_at: string;
  updated_at: string;
}

interface ApiCaseListResponse {
  cases: ApiCaseResponse[];
  total?: number;
  page?: number;
  page_size?: number;
}

export interface ApiAskResponse extends ApiCaseResponse {
  conversation_id: string;
}

export interface ApiConversation {
  id: string;
  title: string;
  created_at: string;
  updated_at: string;
}

export interface ApiConversationMessage {
  id: string;
  role: 'user' | 'assistant';
  content: string;
  case_id?: string;
  created_at: string;
}

export interface ApiConversationListResponse {
  conversations: ApiConversation[];
}

export interface ApiConversationDetailResponse {
  conversation: ApiConversation;
  messages: ApiConversationMessage[];
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
  seq?: number;
  type: string;
  agent_code?: string;
  run_id?: string;
  message: string;
  payload?: Record<string, unknown>;
  timestamp: string;
}

const BASE_URL = '/api/v1';
export const AUTH_STORAGE_KEY = 'magi.apiKey';
export const UNAUTHORIZED_EVENT = 'magi:unauthorized';

// --- API-key auth channel (P0: D1) ---
// The backend authenticates via `Authorization: Bearer <key>` or `X-API-Key`.
// The web UI stores the user's API key in localStorage and injects it as the
// X-API-Key header on every request. On a 401 the stored key is cleared and a
// `magi:unauthorized` event is dispatched so the app can route to /login.

export function getApiKey(): string {
  try {
    return localStorage.getItem(AUTH_STORAGE_KEY) ?? '';
  } catch {
    return '';
  }
}

export function setApiKey(key: string): void {
  try {
    if (key) {
      localStorage.setItem(AUTH_STORAGE_KEY, key);
    } else {
      localStorage.removeItem(AUTH_STORAGE_KEY);
    }
  } catch {
    // localStorage unavailable (private mode / tests): keep the session value only
  }
}

export function clearApiKey(): void {
  setApiKey('');
}

function authHeaders(): Record<string, string> {
  const key = getApiKey();
  return key ? { 'X-API-Key': key } : {};
}

function notifyUnauthorized(): void {
  clearApiKey();
  if (typeof window !== 'undefined') {
    window.dispatchEvent(new Event(UNAUTHORIZED_EVENT));
  }
}

async function request<T>(path: string, options?: RequestInit): Promise<T> {
  const res = await fetch(`${BASE_URL}${path}`, {
    ...options,
    headers: { 'Content-Type': 'application/json', ...authHeaders(), ...(options?.headers ?? {}) },
  });
  if (res.status === 401) {
    // Invalid/expired API key: drop it and ask the user to sign back in.
    notifyUnauthorized();
    const err = await res.json().catch(() => ({ error: 'unauthorized' }));
    throw new Error(err.error || 'unauthorized');
  }
  if (!res.ok) {
    const err = await res.json().catch(() => ({ error: res.statusText }));
    throw new Error(err.error || `HTTP ${res.status}`);
  }
  if (res.status === 204) return undefined as T;
  return res.json();
}

export const api = {
  // --- Persistent assistant conversations ---
  askAssistant: (message: string, conversationId?: string, background?: string) =>
    request<ApiAskResponse>('/assistant', {
      method: 'POST',
      body: JSON.stringify({ message, conversation_id: conversationId || undefined, background }),
    }),

  listConversations: (limit = 50, offset = 0) =>
    request<ApiConversationListResponse>(`/conversations?limit=${limit}&offset=${offset}`),

  getConversation: (id: string) =>
    request<ApiConversationDetailResponse>(`/conversations/${encodeURIComponent(id)}`),

  deleteConversation: (id: string) =>
    request<void>(`/conversations/${encodeURIComponent(id)}`, { method: 'DELETE' }),

  // verifyAuth returns true when the currently stored API key authenticates
  // against the protected /status endpoint (200). It is used by the login page.
  verifyAuth: async (): Promise<boolean> => {
    const res = await fetch(`${BASE_URL}/status`, {
      headers: { 'Content-Type': 'application/json', ...authHeaders() },
    });
    return res.ok;
  },

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

  pauseCase: (id: string) => request<ApiRunResponse>(`/cases/${id}/pause`, { method: 'POST' }),

  resumeCase: (id: string) => request<ApiRunResponse>(`/cases/${id}/resume`, { method: 'POST' }),

  getAgents: (id: string) => request<Record<string, ApiAgentSnapshot>>(`/cases/${id}/agents`),

  getEvidence: (id: string) => request<ApiEvidence[]>(`/cases/${id}/evidence`),

  getClaims: (id: string) => request<ApiClaim[]>(`/cases/${id}/claims`),

  getVotes: (id: string) => request<ApiVote[]>(`/cases/${id}/votes`),

  getEvents: (id: string) => request<ApiEvent[]>(`/cases/${id}/events`),

  getTrace: (id: string) => request<ApiEvent[]>(`/cases/${id}/trace`),

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

  updateMemory: (id: string, patch: ApiMemoryProjectionUpdate) =>
    request<ApiMemoryProjection>('/memory/' + encodeURIComponent(id), {
      method: 'PATCH',
      body: JSON.stringify(patch),
    }),

  deleteMemory: (id: string) => request<void>('/memory/' + encodeURIComponent(id), { method: 'DELETE' }),

  evaluateCase: (id: string) => request<ApiEvaluation>(`/evaluation/${id}`, { method: 'POST' }),

  judgeCase: (id: string) => request<ApiJudgeResult>(`/evaluation/${id}/judge`, { method: 'POST' }),
  getJudgeResult: (id: string) => request<ApiJudgeResult>(`/evaluation/${id}/judge`),

  listRecurring: () => request<ApiRecurring[]>(`/recurring`),

  createRecurring: (name: string, question: string, intervalSeconds: number) =>
    request<ApiRecurring>(`/recurring`, {
      method: 'POST',
      body: JSON.stringify({ name, question, interval_seconds: intervalSeconds }),
    }),

  setRecurringEnabled: (id: string, enabled: boolean) =>
    request<unknown>(`/recurring/${id}`, { method: 'PATCH', body: JSON.stringify({ enabled }) }),

  runRecurringNow: (id: string) => request<{ id: string; status: string }>(`/recurring/${id}/run`, { method: 'POST' }),

  getStatus: () => request<ApiStatus>(`/status`),

  listDatasetItems: (id: string) => request<ApiDatasetItem[]>(`/datasets/${id}/items`),

  updateDatasetItem: (datasetId: string, itemId: string, item: ApiDatasetItem) =>
    request<unknown>(`/datasets/${datasetId}/items/${itemId}`, { method: 'PATCH', body: JSON.stringify(item) }),

  deleteDatasetItem: (datasetId: string, itemId: string) =>
    request<unknown>(`/datasets/${datasetId}/items/${itemId}`, { method: 'DELETE' }),

  exportDatasetItems: (id: string) => request<ApiDatasetItem[]>(`/datasets/${id}/items/export`),

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

  createKnowledge: (title: string, content: string) =>
    request<ApiKnowledgeDoc>('/knowledge', {
      method: 'POST',
      body: JSON.stringify({ title, content }),
    }),

  listKnowledge: (limit = 100, offset = 0) =>
    request<{ documents: ApiKnowledgeDoc[]; total: number }>(`/knowledge?limit=${limit}&offset=${offset}`),

  getKnowledge: (id: string) => request<ApiKnowledgeDoc>(`/knowledge/${id}`),

  deleteKnowledge: async (id: string) => {
    const res = await fetch(`${BASE_URL}/knowledge/${id}`, {
      method: 'DELETE',
      headers: { 'Content-Type': 'application/json' },
    });
    if (!res.ok) {
      const err = await res.json().catch(() => ({ error: res.statusText }));
      throw new Error(err.error || `HTTP ${res.status}`);
    }
  },

  me: () => request<ApiMeResponse>('/me'),

  issueOwnKey: (name?: string) =>
    request<ApiIssuedKey>('/me/keys', { method: 'POST', body: JSON.stringify({ name }) }),

  listUsers: () => request<{ users: ApiUser[] }>('/admin/users'),

  createUser: (name: string, role: string) =>
    request<{ user: ApiUser; api_key?: ApiIssuedKey }>('/admin/users', {
      method: 'POST',
      body: JSON.stringify({ name, role }),
    }),

  deleteUser: async (id: number) => {
    const res = await fetch(`${BASE_URL}/admin/users/${id}`, { method: 'DELETE' });
    if (!res.ok) {
      const err = await res.json().catch(() => ({ error: res.statusText }));
      throw new Error(err.error || `HTTP ${res.status}`);
    }
  },

  listUserKeys: (userId: number) => request<{ keys: ApiApiKey[] }>(`/admin/users/${userId}/keys`),

  issueUserKey: (userId: number, name?: string) =>
    request<ApiIssuedKey>(`/admin/users/${userId}/keys`, {
      method: 'POST',
      body: JSON.stringify({ name }),
    }),

  revokeKey: async (keyId: string) => {
    const res = await fetch(`${BASE_URL}/admin/keys/${keyId}/revoke`, { method: 'POST' });
    if (!res.ok) {
      const err = await res.json().catch(() => ({ error: res.statusText }));
      throw new Error(err.error || `HTTP ${res.status}`);
    }
  },

  rotateKey: (keyId: string) =>
    request<ApiIssuedKey>(`/admin/keys/${keyId}/rotate`, { method: 'POST' }),

  // --- P2 additions ---
  getCasesPaged: (page: number, pageSize: number) =>
    request<ApiCaseListResponse>(`/cases?page=${page}&page_size=${pageSize}`),

  patchCase: (id: string, patch: { pinned?: boolean; archived?: boolean }) =>
    request<ApiCaseResponse>(`/cases/${id}`, {
      method: 'PATCH',
      body: JSON.stringify(patch),
    }),

  deleteCase: async (id: string) => {
    const res = await fetch(`${BASE_URL}/cases/${id}`, { method: 'DELETE' });
    if (!res.ok) {
      const err = await res.json().catch(() => ({ error: res.statusText }));
      throw new Error(err.error || `HTTP ${res.status}`);
    }
  },

  deleteDataset: async (id: string) => {
    const res = await fetch(`${BASE_URL}/datasets/${id}`, { method: 'DELETE' });
    if (!res.ok) {
      const err = await res.json().catch(() => ({ error: res.statusText }));
      throw new Error(err.error || `HTTP ${res.status}`);
    }
  },

  getMeUsage: () => request<ApiMeUsage>('/me/usage'),

  getAdminUsage: () => request<ApiAdminUsage>('/admin/usage'),

  listPrompts: () => request<ApiPromptList>('/admin/prompts'),

  updatePrompt: (key: string, content: string) =>
    request<ApiPrompt>(`/admin/prompts/${encodeURIComponent(key)}`, {
      method: 'PUT',
      body: JSON.stringify({ content }),
    }),

  restorePrompt: (key: string) =>
    request<ApiPrompt>(`/admin/prompts/${encodeURIComponent(key)}/restore`, { method: 'POST' }),

  exportCase: async (id: string) => {
    const r = await fetch(`${BASE_URL}/cases/${id}/export`, { headers: { 'Content-Type': 'application/json' } });
    if (!r.ok) throw new Error((await r.json().catch(() => ({ error: 'export failed' }))).error || 'export failed');
    return downloadJson(await r.json(), `case-${id}.json`);
  },

  exportMemory: async () => {
    const r = await fetch(`${BASE_URL}/memory/export`, { headers: { 'Content-Type': 'application/json' } });
    if (!r.ok) throw new Error((await r.json().catch(() => ({ error: 'export failed' }))).error || 'export failed');
    return downloadJson(await r.json(), 'memory-export.json');
  },

  exportEvaluation: async (id: string) => {
    const r = await fetch(`${BASE_URL}/evaluation/${id}/export`, { headers: { 'Content-Type': 'application/json' } });
    if (!r.ok) throw new Error((await r.json().catch(() => ({ error: 'export failed' }))).error || 'export failed');
    return downloadJson(await r.json(), `evaluation-${id}.json`);
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

export interface ApiMemoryProjection {
  CaseID: string;
  QuestionSummary: string;
  ContextSummary: string;
  Resolution: string;
  Annotation?: string;
  Tags?: string[];
  Outcome?: { Status: string; Learned: string } | null;
  ProjectionVersion: number;
}

export interface ApiMemoryProjectionUpdate {
  question_summary?: string;
  context_summary?: string;
  resolution?: string;
  learned?: string;
  annotation?: string;
  tags?: string[];
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

interface ApiStatus {
  model_name: string;
  max_steps: number;
  tokens_total: number;
  cost_usd: number;
  runs_active: number;
  connected: boolean;
}

interface ApiDatasetItem {
  id?: string;
  question: string;
  background?: string;
  constraints?: { label: string; value: string }[];
  expected_decision: string;
  weight?: number;
  tags?: string[];
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

interface ApiUser {
  id: number;
  name: string;
  role: string;
  active_keys: number;
  created_at?: string;
}

interface ApiApiKey {
  id: string;
  name: string;
  prefix: string;
  revoked: boolean;
  last_used_at?: string;
  created_at?: string;
}

interface ApiIssuedKey {
  id: string;
  prefix: string;
  plaintext: string;
}

interface ApiMeResponse {
  user: ApiUser;
  keys: ApiApiKey[];
}

interface ApiKnowledgeDoc {
  id: string;
  title: string;
  source_kind: string;
  source_url?: string;
  status: string;
  error?: string;
  chunks: number;
  created_at: string;
  updated_at: string;
}

export type {
  ApiUser,
  ApiApiKey,
  ApiIssuedKey,
  ApiMeResponse,
  ApiKnowledgeDoc,
  ApiCaseResponse,
  ApiDataset,
  ApiBenchmarkRun,
  ApiBenchmarkDetail,
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
  ApiStatus,
  ApiDatasetItem,
};


function downloadJson(data: unknown, filename: string) {
  const blob = new Blob([JSON.stringify(data, null, 2)], { type: 'application/json' });
  const url = URL.createObjectURL(blob);
  const a = document.createElement('a');
  a.href = url;
  a.download = filename;
  a.click();
  URL.revokeObjectURL(url);
}

export interface ApiMeUsage {
  user_id: number;
  cases: number;
  runs: number;
  tokens: number;
  cost_usd: number;
  max_tokens: number;
  max_cost_usd: number;
  tokens_exceeded: boolean;
  cost_exceeded: boolean;
}

export interface ApiAdminUsageRow {
  user_id: number;
  cases: number;
  runs: number;
  tokens: number;
  cost_usd: number;
}

export interface ApiAdminUsage {
  total_cases: number;
  total_runs: number;
  total_tokens: number;
  total_cost_usd: number;
  by_user: ApiAdminUsageRow[];
}

export interface ApiPrompt {
  key: string;
  version: number;
  active: boolean;
  content: string;
  updated_at: string;
}

export interface ApiPromptList {
  prompts: ApiPrompt[];
}
