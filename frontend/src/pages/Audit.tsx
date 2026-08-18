import { useCallback, useEffect, useState } from 'react';
import { api, type ApiAuditEvent } from '@/api/client';
import { useT } from '@/i18n';

const PAGE_SIZE = 50;

export default function Audit() {
  const t = useT();
  const [events, setEvents] = useState<ApiAuditEvent[] | null>(null);
  const [total, setTotal] = useState(0);
  const [page, setPage] = useState(0);
  const [error, setError] = useState('');

  const load = useCallback(async (pageNum: number) => {
    setError('');
    try {
      const r = await api.listAuditEvents(PAGE_SIZE, pageNum * PAGE_SIZE);
      setEvents(r.events);
      setTotal(r.total);
    } catch (e) {
      setError(e instanceof Error ? e.message : 'load audit failed');
      setEvents([]);
    }
  }, []);

  useEffect(() => {
    void load(0);
  }, [load]);

  const pages = Math.max(1, Math.ceil(total / PAGE_SIZE));

  return (
    <div className="p-6 space-y-6">
      <div className="flex items-center justify-between gap-3">
        <div>
          <h1 className="text-xl font-semibold">{t('audit.title')}</h1>
          <p className="text-sm text-text-muted">{t('audit.subtitle')}</p>
        </div>
        <button
          className="rounded border border-border-dim px-4 py-2 text-sm text-text-muted hover:text-text-secondary"
          onClick={() => void load(page)}
        >
          {t('audit.refresh')}
        </button>
      </div>

      {error && <p className="text-sm text-red-500">{error}</p>}

      <div className="overflow-x-auto rounded border border-border-dim bg-raised">
        <table className="w-full text-left text-sm">
          <thead className="border-b border-border-dim font-mono text-[10px] uppercase tracking-wider text-text-muted">
            <tr>
              <th className="px-3 py-2">{t('audit.time')}</th>
              <th className="px-3 py-2">{t('audit.user')}</th>
              <th className="px-3 py-2">{t('audit.action')}</th>
              <th className="px-3 py-2">{t('audit.resource')}</th>
              <th className="px-3 py-2">{t('audit.status')}</th>
            </tr>
          </thead>
          <tbody>
            {events?.map((e) => (
              <tr key={e.id} className="border-b border-border-dim/50 last:border-0">
                <td className="px-3 py-2 font-mono text-xs text-text-muted whitespace-nowrap">
                  {new Date(e.created_at).toLocaleString()}
                </td>
                <td className="px-3 py-2 whitespace-nowrap">
                  {e.username || <span className="text-text-muted">—</span>}
                  {e.role && <span className="ml-1 font-mono text-[10px] text-text-muted">({e.role})</span>}
                </td>
                <td className="px-3 py-2 font-mono text-xs">{e.action}</td>
                <td className="px-3 py-2 font-mono text-xs text-text-secondary break-all">{e.resource}</td>
                <td className="px-3 py-2 font-mono text-xs">
                  <span className={`rounded px-1.5 py-0.5 ${e.status < 400 ? 'bg-emerald-400/10 text-emerald-400' : 'bg-red-500/10 text-red-400'}`}>
                    {e.status}
                  </span>
                </td>
              </tr>
            ))}
            {events && events.length === 0 && (
              <tr>
                <td colSpan={5} className="px-3 py-6 text-center text-sm text-text-muted">{t('audit.empty')}</td>
              </tr>
            )}
            {!events && (
              <tr>
                <td colSpan={5} className="px-3 py-6 text-center text-sm text-text-muted">{t('replay.loading')}</td>
              </tr>
            )}
          </tbody>
        </table>
      </div>

      <div className="flex items-center justify-between">
        <span className="text-xs text-text-muted">{t('audit.total')}: {total}</span>
        <div className="flex gap-2">
          <button
            className="rounded border border-border-dim px-3 py-1 text-xs text-text-muted hover:text-text-secondary disabled:opacity-40"
            disabled={page === 0}
            onClick={() => { const next = page - 1; setPage(next); void load(next); }}
          >
            {t('audit.prev')}
          </button>
          <span className="px-2 py-1 text-xs text-text-muted">{page + 1} / {pages}</span>
          <button
            className="rounded border border-border-dim px-3 py-1 text-xs text-text-muted hover:text-text-secondary disabled:opacity-40"
            disabled={page + 1 >= pages}
            onClick={() => { const next = page + 1; setPage(next); void load(next); }}
          >
            {t('audit.next')}
          </button>
        </div>
      </div>
    </div>
  );
}
