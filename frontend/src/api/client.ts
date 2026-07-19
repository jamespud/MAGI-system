// API response types (snake_case as returned by backend)
interface ApiCaseResponse {
  id: string;
  question: string;
  background: string;
  constraints: { label: string; value: string }[];
  status: string;
  consensus: {
    approve: number;
    reject: number;
    abstain: number;
    majority: string;
    need_reflection: boolean;
  } | null;
  confidence: number;
  round: number;
  created_at: string;
  updated_at: string;
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
  getCases: () => request<ApiCaseResponse[]>('/cases'),

  getCase: (id: string) => request<ApiCaseResponse>(`/cases/${id}`),

  createCase: (question: string, background?: string) =>
    request<ApiCaseResponse>('/cases', {
      method: 'POST',
      body: JSON.stringify({ question, background }),
    }),
};

export type { ApiCaseResponse };
