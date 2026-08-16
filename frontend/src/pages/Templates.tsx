import { useEffect, useState } from 'react';
import { api, type ApiRecurring } from '@/api/client';
import { useT } from '@/i18n';

export default function Templates() {
  const t = useT();
  const [templates, setTemplates] = useState<ApiRecurring[]>([]);
  const [name, setName] = useState('');
  const [question, setQuestion] = useState('');
  const [interval, setInterval] = useState('3600');
  const [error, setError] = useState('');
  const [busy, setBusy] = useState('');

  const load = async () => {
    try {
      setTemplates(await api.listRecurring());
    } catch (e) {
      setError(e instanceof Error ? e.message : 'load failed');
    }
  };

  useEffect(() => {
    void load();
  }, []);

  const create = async () => {
    if (!name.trim() || !question.trim()) return;
    const secs = Number(interval);
    if (!Number.isFinite(secs) || secs <= 0) return;
    setBusy('create');
    setError('');
    try {
      await api.createRecurring(name.trim(), question.trim(), secs);
      setName('');
      setQuestion('');
      await load();
    } catch (e) {
      setError(e instanceof Error ? e.message : 'create failed');
    } finally {
      setBusy('');
    }
  };

  const toggle = async (t: ApiRecurring) => {
    setBusy(`toggle-${t.id}`);
    setError('');
    try {
      await api.setRecurringEnabled(t.id, !t.enabled);
      await load();
    } catch (e) {
      setError(e instanceof Error ? e.message : 'update failed');
    } finally {
      setBusy('');
    }
  };

  const runNow = async (t: ApiRecurring) => {
    setBusy(`run-${t.id}`);
    setError('');
    try {
      await api.runRecurringNow(t.id);
      await load();
    } catch (e) {
      setError(e instanceof Error ? e.message : 'run failed');
    } finally {
      setBusy('');
    }
  };

  const remove = async (t: ApiRecurring) => {
    setBusy(`del-${t.id}`);
    setError('');
    try {
      await api.deleteRecurring(t.id);
      await load();
    } catch (e) {
      setError(e instanceof Error ? e.message : 'delete failed');
    } finally {
      setBusy('');
    }
  };

  return (
    <div className="p-6 space-y-6">
      <h1 className="text-xl font-semibold">{t('templates.title')}</h1>
      {error && <p className="text-red-500">{error}</p>}

      <div className="rounded border border-border-dim bg-raised p-4 space-y-2">
        <input
          className="w-full rounded border border-border-dim bg-background px-3 py-2 text-sm"
          placeholder="Template name"
          value={name}
          onChange={(e) => setName(e.target.value)}
        />
        <input
          className="w-full rounded border border-border-dim bg-background px-3 py-2 text-sm"
          placeholder="Decision question"
          value={question}
          onChange={(e) => setQuestion(e.target.value)}
        />
        <div className="flex gap-2">
          <input
            className="w-40 rounded border border-border-dim bg-background px-3 py-2 text-sm"
            placeholder="Interval (seconds)"
            value={interval}
            onChange={(e) => setInterval(e.target.value)}
          />
          <button
            className="rounded bg-accent px-3 py-2 text-sm disabled:opacity-50"
            disabled={!name.trim() || !question.trim() || busy === 'create'}
            onClick={create}
          >
            {busy === 'create' ? 'Creating…' : 'Create template'}
          </button>
        </div>
      </div>

      <div className="space-y-3">
        {templates.map((t) => (
          <div key={t.id} className="rounded border border-border-dim bg-raised p-4">
            <div className="flex items-center justify-between">
              <div>
                <p className="font-medium">{t.name}</p>
                <p className="text-sm text-text-muted">{t.question}</p>
                <p className="text-xs text-text-muted mt-1">
                  every {t.interval_seconds}s · {t.enabled ? 'enabled' : 'paused'}
                  {t.last_run_at ? ` · last run ${new Date(t.last_run_at).toLocaleString()}` : ''}
                </p>
              </div>
              <div className="space-x-2">
                <button className="rounded border border-border-dim px-3 py-1.5 text-sm" disabled={busy === `toggle-${t.id}`} onClick={() => toggle(t)}>
                  {t.enabled ? 'Pause' : 'Enable'}
                </button>
                <button className="rounded border border-border-dim px-3 py-1.5 text-sm" disabled={busy === `run-${t.id}`} onClick={() => runNow(t)}>
                  Run now
                </button>
                <button className="rounded border border-red-500/50 px-3 py-1.5 text-sm" disabled={busy === `del-${t.id}`} onClick={() => remove(t)}>
                  Delete
                </button>
              </div>
            </div>
          </div>
        ))}
        {templates.length === 0 && <p className="text-sm text-text-muted">No templates yet.</p>}
      </div>
    </div>
  );
}
