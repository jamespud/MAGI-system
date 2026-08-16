import { useEffect, useState } from 'react';
import { Link } from 'react-router-dom';
import { api, type ApiMeResponse, type ApiMeUsage, type ApiAdminUsage } from '@/api/client';
import { useAuthStore } from '@/stores';
import { Button } from '@/components/ui';
import { useT } from '@/i18n';

function formatTokens(n: number): string {
  if (n >= 1_000_000) return `${(n / 1_000_000).toFixed(2)}M`;
  if (n >= 1_000) return `${(n / 1_000).toFixed(1)}k`;
  return String(n);
}

export default function Settings() {
  const t = useT();
  const [version, setVersion] = useState('');
  const [ready, setReady] = useState('');
  const [me, setMe] = useState<ApiMeResponse | null>(null);
  const [usage, setUsage] = useState<ApiMeUsage | null>(null);
  const [adminUsage, setAdminUsage] = useState<ApiAdminUsage | null>(null);
  const [error, setError] = useState('');
  const hasKey = useAuthStore((s) => s.hasKey);
  const clearStoredKey = useAuthStore((s) => s.clearApiKey);

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
        if (!cancelled) {
          setMe(m);
          if (m.user.role === 'admin') {
            api.getAdminUsage().then((u) => !cancelled && setAdminUsage(u)).catch(() => {});
          }
        }
      } catch {
        // open mode: no principal, fine
      }
    };
    void loadMe();
    const loadUsage = async () => {
      try {
        const u = await api.getMeUsage();
        if (!cancelled) setUsage(u);
      } catch {
        // open mode / not configured
      }
    };
    void loadUsage();
    return () => { cancelled = true; };
  }, []);

  const isAdmin = me?.user.role === 'admin';

  return (
    <div className="p-6 space-y-6">
      <h1 className="text-xl font-semibold">{t('settings.title')}</h1>
      {error && <p className="text-red-500">{error}</p>}

      <div className="grid grid-cols-1 md:grid-cols-3 gap-3">
        <div className="rounded border border-border-dim bg-raised p-4">
          <p className="text-xs text-text-muted">{t('settings.version')}</p>
          <p className="text-lg font-medium mt-1">{version || '—'}</p>
        </div>
        <div className="rounded border border-border-dim bg-raised p-4">
          <p className="text-xs text-text-muted">{t('settings.readiness')}</p>
          <p className="text-lg font-medium mt-1">{ready || '—'}</p>
        </div>
        <div className="rounded border border-border-dim bg-raised p-4">
          <p className="text-xs text-text-muted">{t('settings.currentUser')}</p>
          <p className="text-lg font-medium mt-1">{me ? `${me.user.name} (${me.user.role})` : t('settings.openMode')}</p>
          {me && me.keys.length > 0 && (
            <p className="text-xs text-text-muted mt-1">
              {t('settings.activeKeys', { n: me.keys.filter((k) => !k.revoked).length })}
            </p>
          )}
        </div>
      </div>

      <div className="rounded border border-border-dim bg-raised p-4">
        <div className="flex items-center justify-between">
          <div>
            <p className="text-xs text-text-muted">API key</p>
            <p className="text-sm font-medium mt-1">
              {hasKey ? 'Signed in (API key stored in this browser)' : 'Not signed in — open mode'}
            </p>
          </div>
          <div className="flex items-center gap-2">
            {hasKey && (
              <button
                type="button"
                onClick={() => clearStoredKey()}
                className="rounded border border-border-dim px-3 py-1.5 text-sm text-text-secondary hover:bg-base"
              >
                Sign out
              </button>
            )}
            <Link
              to="/login"
              className="rounded bg-accent px-3 py-1.5 text-sm font-medium"
            >
              {hasKey ? 'Change key' : 'Sign in'}
            </Link>
          </div>
        </div>
        <p className="text-xs text-text-muted mt-2">
          The API key is sent as the <span className="font-mono">X-API-Key</span> header and stored
          only in localStorage.
        </p>
      </div>

      {usage && (
        <div className="rounded border border-border-dim bg-raised p-4">
          <p className="text-xs text-text-muted mb-2">{t('settings.myUsage')}</p>
          <div className="grid grid-cols-2 md:grid-cols-4 gap-3 text-sm">
            <div><span className="text-text-muted text-xs">{t('settings.cases')}</span><p className="font-medium">{usage.cases}</p></div>
            <div><span className="text-text-muted text-xs">{t('settings.runs')}</span><p className="font-medium">{usage.runs}</p></div>
            <div>
              <span className="text-text-muted text-xs">{t('settings.tokens')}</span>
              <p className="font-medium">{formatTokens(usage.tokens)}{usage.max_tokens > 0 ? ` / ${formatTokens(usage.max_tokens)}` : ''}</p>
              {usage.tokens_exceeded && <p className="text-red-400 text-xs">{t('settings.budgetExhausted')}</p>}
            </div>
            <div>
              <span className="text-text-muted text-xs">{t('settings.costUsd')}</span>
              <p className="font-medium">${usage.cost_usd.toFixed(4)}{usage.max_cost_usd > 0 ? ` / $${usage.max_cost_usd}` : ''}</p>
              {usage.cost_exceeded && <p className="text-red-400 text-xs">{t('settings.budgetExhausted')}</p>}
            </div>
          </div>
        </div>
      )}

      {isAdmin && adminUsage && (
        <div className="rounded border border-border-dim bg-raised p-4">
          <p className="text-xs text-text-muted mb-2">{t('settings.adminUsage')}</p>
          <p className="text-sm mb-2 text-text-secondary">
            {adminUsage.total_cases} cases · {adminUsage.total_runs} runs · {formatTokens(adminUsage.total_tokens)} tokens · ${adminUsage.total_cost_usd.toFixed(4)} cost
          </p>
          <table className="w-full text-sm">
            <thead>
              <tr className="text-left text-xs text-text-muted border-b border-border-dim">
                <th className="py-1 pr-2">{t('settings.user')}</th>
                <th className="py-1 pr-2">Cases</th>
                <th className="py-1 pr-2">Runs</th>
                <th className="py-1 pr-2">Tokens</th>
                <th className="py-1">Cost (USD)</th>
              </tr>
            </thead>
            <tbody>
              {adminUsage.by_user.map((u) => (
                <tr key={u.user_id} className="border-b border-border-dim/50">
                  <td className="py-1 pr-2 font-mono text-xs">{u.user_id}</td>
                  <td className="py-1 pr-2">{u.cases}</td>
                  <td className="py-1 pr-2">{u.runs}</td>
                  <td className="py-1 pr-2">{formatTokens(u.tokens)}</td>
                  <td className="py-1">${u.cost_usd.toFixed(4)}</td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}

      <div className="rounded border border-border-dim bg-raised p-4 text-sm text-text-muted space-y-1">
        <p>{t('settings.serverManaged')}</p>
        <p>{t('settings.opsEndpoints', { metrics: "/metrics", health: "/health", openapi: "/openapi.json" })}</p>
        <p>{t('settings.traceIds', { header: "X-Trace-ID" })}</p>
        <div className="pt-2">
          <Button size="sm" onClick={() => void api.exportMemory().catch(() => {})}>{t('settings.exportMemory')}</Button>
        </div>
      </div>
    </div>
  );
}
