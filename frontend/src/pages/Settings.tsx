import { useEffect, useState } from 'react';
import { api, type ApiMeResponse } from '@/api/client';

export default function Settings() {
  const [version, setVersion] = useState('');
  const [ready, setReady] = useState('');
  const [me, setMe] = useState<ApiMeResponse | null>(null);
  const [error, setError] = useState('');

  useEffect(() => {
    let cancelled = false;
    (async () => {
      try {
        const [v, r] = await Promise.all([api.getVersion(), api.getReady()]);
        if (!cancelled) {
          setVersion(v.version);
          setReady(r.status);
        }
      } catch (e) {
        if (!cancelled) setError(e instanceof Error ? e.message : 'failed to load system status');
      }
    })();
    const loadMe = async () => {
      try {
        const m = await api.me();
        if (!cancelled) setMe(m);
      } catch {
        // open mode: no principal, fine
      }
    };
    void loadMe();
    return () => { cancelled = true; };
  }, []);

  return (
    <div className="p-6 space-y-6">
      <h1 className="text-xl font-semibold">Settings</h1>
      {error && <p className="text-red-500">{error}</p>}

      <div className="grid grid-cols-1 md:grid-cols-2 gap-3">
        <div className="rounded border border-border-dim bg-raised p-4">
          <p className="text-xs text-text-muted">Version</p>
          <p className="text-lg font-medium mt-1">{version || '—'}</p>
        </div>
        <div className="rounded border border-border-dim bg-raised p-4">
          <p className="text-xs text-text-muted">Readiness</p>
          <p className="text-lg font-medium mt-1">{ready || '—'}</p>
        </div>
        <div className="rounded border border-border-dim bg-raised p-4">
          <p className="text-xs text-text-muted">Current user</p>
          <p className="text-lg font-medium mt-1">{me ? `${me.user.name} (${me.user.role})` : 'open mode'}</p>
          {me && me.keys.length > 0 && (
            <p className="text-xs text-text-muted mt-1">
              {me.keys.filter((k) => !k.revoked).length} active API key(s)
            </p>
          )}
        </div>
      </div>

      <div className="rounded border border-border-dim bg-raised p-4 text-sm text-text-muted space-y-1">
        <p>Configuration, secrets, auth keys and limits are managed server-side (backend/conf/magi.yaml).</p>
        <p>Operational endpoints: <span className="font-mono">/metrics</span>, <span className="font-mono">/health</span>, <span className="font-mono">/openapi.json</span>.</p>
        <p>Trace IDs are returned in the <span className="font-mono">X-Trace-ID</span> response header.</p>
      </div>
    </div>
  );
}
