import { useEffect, useRef, useState } from 'react';
import { api, type ApiBenchmarkDetail, type ApiDataset } from '@/api/client';

interface DemoItem {
  question: string;
  expected_decision: string;
}

const DEMO_ITEMS: DemoItem[] = [
  { question: 'Should we adopt Rust for the core service?', expected_decision: 'approve' },
  { question: 'Should we migrate to a new database now?', expected_decision: 'reject' },
];

export default function Dataset() {
  const [datasets, setDatasets] = useState<ApiDataset[]>([]);
  const [name, setName] = useState('');
  const [description, setDescription] = useState('');
  const [error, setError] = useState('');
  const [busy, setBusy] = useState('');
  const [runs, setRuns] = useState<Record<string, ApiBenchmarkDetail | null>>({});
  const pollTimers = useRef<Record<string, ReturnType<typeof setInterval>>>({});

  const load = async () => {
    try {
      const r = await api.listDatasets();
      setDatasets(r.datasets);
    } catch (e) {
      setError(e instanceof Error ? e.message : 'failed to load datasets');
    }
  };

  useEffect(() => {
    void load();
    return () => {
      Object.values(pollTimers.current).forEach(clearInterval);
    };
  }, []);

  const stopPolling = (runId: string) => {
    const t = pollTimers.current[runId];
    if (t) {
      clearInterval(t);
      delete pollTimers.current[runId];
    }
  };

  const pollRun = (runId: string) => {
    const tick = async () => {
      try {
        const detail = await api.getBenchmarkRun(runId);
        setRuns((prev) => ({ ...prev, [runId]: detail }));
        if (detail.run.status === 'succeeded' || detail.run.status === 'failed') {
          stopPolling(runId);
        }
      } catch {
        stopPolling(runId);
      }
    };
    void tick();
    pollTimers.current[runId] = setInterval(tick, 2000);
  };

  const create = async () => {
    if (!name.trim()) return;
    setBusy('create');
    setError('');
    try {
      await api.createDataset(name.trim(), description.trim() || undefined);
      setName('');
      setDescription('');
      await load();
    } catch (e) {
      setError(e instanceof Error ? e.message : 'create failed');
    } finally {
      setBusy('');
    }
  };

  const addItems = async (id: string) => {
    setBusy(`items-${id}`);
    setError('');
    try {
      await api.addDatasetItems(id, DEMO_ITEMS);
      await load();
    } catch (e) {
      setError(e instanceof Error ? e.message : 'add items failed');
    } finally {
      setBusy('');
    }
  };

  const run = async (id: string) => {
    setBusy(`run-${id}`);
    setError('');
    try {
      const run = await api.startDatasetRun(id);
      setRuns((prev) => ({ ...prev, [run.id]: null }));
      pollRun(run.id);
    } catch (e) {
      setError(e instanceof Error ? e.message : 'run failed');
    } finally {
      setBusy('');
    }
  };

  const detailFor = (datasetId: string): ApiBenchmarkDetail | null => {
    const entries = Object.values(runs);
    return entries.find((d) => d?.run.dataset_id === datasetId) ?? null;
  };

  return (
    <div className="p-6 space-y-6">
      <h1 className="text-xl font-semibold">Dataset Evaluation</h1>
      {error && <p className="text-red-500">{error}</p>}

      <div className="rounded border border-border-dim bg-raised p-4 space-y-2">
        <input
          className="w-full rounded border border-border-dim bg-background px-3 py-2 text-sm"
          placeholder="Dataset name"
          value={name}
          onChange={(e) => setName(e.target.value)}
        />
        <input
          className="w-full rounded border border-border-dim bg-background px-3 py-2 text-sm"
          placeholder="Description (optional)"
          value={description}
          onChange={(e) => setDescription(e.target.value)}
        />
        <button
          className="rounded bg-accent px-3 py-2 text-sm disabled:opacity-50"
          disabled={!name.trim() || busy === 'create'}
          onClick={create}
        >
          {busy === 'create' ? 'Creating…' : 'Create dataset'}
        </button>
      </div>

      <div className="space-y-3">
        {datasets.map((d) => {
          const detail = detailFor(d.id);
          return (
            <div key={d.id} className="rounded border border-border-dim bg-raised p-4">
              <div className="flex items-center justify-between">
                <div>
                  <p className="font-medium">{d.name}</p>
                  <p className="text-sm text-text-muted">
                    {d.item_count} items · {d.description || 'no description'}
                  </p>
                </div>
                <div className="space-x-2">
                  <button
                    className="rounded border border-border-dim px-3 py-1.5 text-sm disabled:opacity-50"
                    disabled={busy === `items-${d.id}`}
                    onClick={() => addItems(d.id)}
                  >
                    Add demo items
                  </button>
                  <button
                    className="rounded bg-accent px-3 py-1.5 text-sm disabled:opacity-50"
                    disabled={busy === `run-${d.id}` || d.item_count === 0}
                    onClick={() => run(d.id)}
                  >
                    Run benchmark
                  </button>
                </div>
              </div>
              {detail && (
                <div className="mt-3 text-sm">
                  <p>
                    Status: <span className="font-medium">{detail.run.status}</span>
                    {detail.run.status === 'succeeded' && (
                      <> · accuracy <span className="font-medium">{(detail.run.accuracy * 100).toFixed(0)}%</span> ({detail.run.matched}/{detail.run.total})</>
                    )}
                  </p>
                  {detail.results.length > 0 && (
                    <ul className="mt-2 space-y-1">
                      {detail.results.slice(0, 5).map((r) => (
                        <li key={r.id} className="text-text-muted">
                          {r.matched ? '✓' : '✗'} {r.case_id} → {r.actual_decision || '—'}
                          {r.feedback ? ` · feedback: ${r.feedback}` : ''}
                        </li>
                      ))}
                    </ul>
                  )}
                </div>
              )}
            </div>
          );
        })}
        {datasets.length === 0 && <p className="text-sm text-text-muted">No datasets yet.</p>}
      </div>
    </div>
  );
}
