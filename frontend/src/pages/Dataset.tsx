import { useEffect, useRef, useState } from 'react';
import { api, type ApiBenchmarkDetail, type ApiDataset, type ApiDatasetItem } from '@/api/client';

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
  const [runsPerItem, setRunsPerItem] = useState(1);
  const [expanded, setExpanded] = useState<string | null>(null);
  const [items, setItems] = useState<Record<string, ApiDatasetItem[]>>({});
  const [newQuestion, setNewQuestion] = useState('');
  const [newDecision, setNewDecision] = useState('approve');
  const [importText, setImportText] = useState('');
  const [regressionThreshold, setRegressionThreshold] = useState(0);
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

  const toggleItems = async (id: string) => {
    if (expanded === id) { setExpanded(null); return; }
    setExpanded(id);
    try {
      const list = await api.listDatasetItems(id);
      setItems((prev) => ({ ...prev, [id]: list }));
    } catch (e) {
      setError(e instanceof Error ? e.message : 'load items failed');
    }
  };

  const addCustomItem = async (id: string) => {
    if (!newQuestion.trim()) return;
    setBusy(`add-${id}`);
    try {
      await api.addDatasetItems(id, [{ question: newQuestion.trim(), expected_decision: newDecision }]);
      setNewQuestion('');
      const list = await api.listDatasetItems(id);
      setItems((prev) => ({ ...prev, [id]: list }));
      await load();
    } catch (e) { setError(e instanceof Error ? e.message : 'add failed'); } finally { setBusy(''); }
  };

  const deleteItem = async (datasetId: string, itemId: string) => {
    setBusy(`del-${itemId}`);
    try {
      await api.deleteDatasetItem(datasetId, itemId);
      setItems((prev) => ({ ...prev, [datasetId]: (prev[datasetId] || []).filter((it) => it.id !== itemId) }));
      await load();
    } catch (e) { setError(e instanceof Error ? e.message : 'delete failed'); } finally { setBusy(''); }
  };

  const exportItems = async (id: string) => {
    try {
      const list = await api.exportDatasetItems(id);
      const blob = new Blob([JSON.stringify(list, null, 2)], { type: 'application/json' });
      const url = URL.createObjectURL(blob);
      const a = document.createElement('a');
      a.href = url;
      a.download = `${id}-items.json`;
      a.click();
      URL.revokeObjectURL(url);
    } catch (e) { setError(e instanceof Error ? e.message : 'export failed'); }
  };

  const importItems = async (id: string) => {
    try {
      const parsed = JSON.parse(importText) as ApiDatasetItem[];
      if (!Array.isArray(parsed)) throw new Error('expected an array');
      await api.addDatasetItems(id, parsed.map((p) => ({ question: p.question, expected_decision: p.expected_decision })));
      setImportText('');
      const list = await api.listDatasetItems(id);
      setItems((prev) => ({ ...prev, [id]: list }));
      await load();
    } catch (e) { setError(e instanceof Error ? e.message : 'import failed'); }
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
      const run = await api.startDatasetRun(id, runsPerItem, regressionThreshold);
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
                    onClick={() => toggleItems(d.id)}
                  >
                    {expanded === d.id ? 'Hide items' : 'Manage items'}
                  </button>
                  <button
                    className="rounded border border-border-dim px-3 py-1.5 text-sm disabled:opacity-50"
                    disabled={busy === `items-${d.id}`}
                    onClick={() => addItems(d.id)}
                  >
                    Add demo items
                  </button>
                  <button className="rounded border border-border-dim px-3 py-1.5 text-sm" onClick={() => exportItems(d.id)}>
                    Export
                  </button>
                  <label className="flex items-center gap-1 text-xs text-text-muted">
                    runs
                    <input
                      type="number"
                      min={1}
                      max={10}
                      value={runsPerItem}
                      onChange={(e) => setRunsPerItem(Number(e.target.value) || 1)}
                      className="w-14 rounded border border-border-dim bg-background px-1 py-1 text-xs font-mono"
                    />
                  </label>
                  <label className="flex items-center gap-1 text-xs text-text-muted">
                    threshold
                    <input
                      type="number"
                      min={0}
                      max={1}
                      step={0.05}
                      value={regressionThreshold}
                      onChange={(e) => setRegressionThreshold(Number(e.target.value) || 0)}
                      className="w-16 rounded border border-border-dim bg-background px-1 py-1 text-xs font-mono"
                    />
                  </label>
                  <button
                    className="rounded bg-accent px-3 py-1.5 text-sm disabled:opacity-50"
                    disabled={busy === `run-${d.id}` || d.item_count === 0}
                    onClick={() => run(d.id)}
                  >
                    Run benchmark
                  </button>
                </div>
              </div>
              {expanded === d.id && (
                <div className="mt-3 border-t border-border-dim pt-3">
                  <div className="flex gap-2">
                    <input
                      className="flex-1 rounded border border-border-dim bg-background px-2 py-1 text-sm"
                      placeholder="Question"
                      value={newQuestion}
                      onChange={(e) => setNewQuestion(e.target.value)}
                    />
                    <select
                      className="rounded border border-border-dim bg-background px-2 py-1 text-sm"
                      value={newDecision}
                      onChange={(e) => setNewDecision(e.target.value)}
                    >
                      <option value="approve">approve</option>
                      <option value="reject">reject</option>
                      <option value="abstain">abstain</option>
                    </select>
                    <button className="rounded bg-accent px-3 py-1 text-sm disabled:opacity-50" disabled={!newQuestion.trim()} onClick={() => addCustomItem(d.id)}>
                      Add
                    </button>
                  </div>
                  <ul className="mt-2 space-y-1">
                    {(items[d.id] || []).map((it, i) => (
                      <li key={it.id || i} className="flex items-center justify-between rounded bg-raised px-3 py-1.5 text-sm">
                        <span className="truncate">{it.question} → {it.expected_decision}</span>
                        <button
                          className="text-xs text-red-400 hover:underline"
                          onClick={() => { if (it.id) void deleteItem(d.id, it.id); }}
                        >
                          Delete
                        </button>
                      </li>
                    ))}
                  </ul>
                  <textarea
                    className="mt-2 w-full h-20 rounded border border-border-dim bg-background px-2 py-1 text-xs font-mono"
                    placeholder={'Import JSON array: [{"question":"...","expected_decision":"approve"}]'}
                    value={importText}
                    onChange={(e) => setImportText(e.target.value)}
                  />
                  <button className="mt-1 rounded border border-border-dim px-3 py-1 text-sm" onClick={() => importItems(d.id)}>
                    Import
                  </button>
                </div>
              )}
              {detail && (
                <div className="mt-3 text-sm">
                  <p>
                    Status: <span className="font-medium">{detail.run.status}</span>
                    {detail.run.status === 'succeeded' && (
                      <> · accuracy <span className="font-medium">{(detail.run.accuracy * 100).toFixed(0)}%</span> ({detail.run.matched}/{detail.run.total})</>
                    )}
                    {detail.run.runs_per_item ? ` · stability $((detail.run.stability ?? 0) * 100).toFixed(0)% (runs=${detail.run.runs_per_item})` : ''}
                    {detail.run.regression_failed ? ` · REGRESSION FAILED: ${detail.run.failure_reason || ''}` : ''}
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
