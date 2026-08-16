import { useEffect, useState } from 'react';
import { api, type ApiApproval } from '@/api/client';
import { useT } from '@/i18n';

export default function Approvals() {
  const t = useT();
  const [approvals, setApprovals] = useState<ApiApproval[]>([]);
  const [error, setError] = useState('');
  const [busy, setBusy] = useState('');

  const load = async () => {
    try {
      const r = await api.listApprovals();
      setApprovals(r.approvals);
    } catch (e) {
      setError(e instanceof Error ? e.message : 'failed to load approvals');
    }
  };

  useEffect(() => {
    void load();
    const timer = setInterval(() => void load(), 3000);
    return () => clearInterval(timer);
  }, []);

  const decide = async (id: string, approve: boolean) => {
    setBusy(id);
    setError('');
    try {
      if (approve) {
        await api.approveApproval(id);
      } else {
        await api.rejectApproval(id);
      }
      await load();
    } catch (e) {
      setError(e instanceof Error ? e.message : 'decision failed');
    } finally {
      setBusy('');
    }
  };

  return (
    <div className="p-6 space-y-6">
      <h1 className="text-xl font-semibold">{t('approvals.title')}</h1>
      {error && <p className="text-red-500">{error}</p>}
      <div className="space-y-3">
        {approvals.length === 0 && <p className="text-sm text-text-muted">No approval requests.</p>}
        {approvals.map((a) => (
          <div key={a.id} className="rounded border border-border-dim bg-raised p-4">
            <div className="flex items-center justify-between">
              <div className="font-mono text-xs text-text-muted">{a.status} · {a.tool_name}</div>
              {a.requested_at && (
                <div className="text-xs text-text-muted">{new Date(a.requested_at).toLocaleString()}</div>
              )}
            </div>
            <p className="mt-1 text-sm">
              <span className="font-mono text-text-muted">{a.case_id}</span>
              {a.run_id && <span className="font-mono text-text-muted"> · {a.run_id}</span>}
              {a.agent_code && <span className="font-mono text-text-muted"> · {a.agent_code}</span>}
            </p>
            {a.arguments && (
              <pre className="mt-2 rounded bg-background border border-border-dim p-2 text-xs font-mono overflow-x-auto whitespace-pre-wrap">{a.arguments}</pre>
            )}
            {a.reason && <p className="mt-1 text-sm text-text-muted">Reason: {a.reason}</p>}
            {a.decided_by && <p className="mt-1 text-xs text-text-muted">Decided by: {a.decided_by}</p>}
            {a.status === 'pending' && (
              <div className="mt-3 space-x-2">
                <button
                  className="rounded bg-accent px-3 py-1.5 text-sm disabled:opacity-50"
                  disabled={busy === a.id}
                  onClick={() => decide(a.id, true)}
                >
                  Approve
                </button>
                <button
                  className="rounded border border-red-500/40 px-3 py-1.5 text-sm text-red-400 disabled:opacity-50"
                  disabled={busy === a.id}
                  onClick={() => decide(a.id, false)}
                >
                  Reject
                </button>
              </div>
            )}
          </div>
        ))}
      </div>
    </div>
  );
}
