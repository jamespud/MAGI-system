import { useState } from 'react';
import { api, type ApiEvaluation } from '@/api/client';

export default function Benchmark() {
  const [input, setInput] = useState('');
  const [results, setResults] = useState<Record<string, ApiEvaluation> | null>(null);
  const [error, setError] = useState('');
  const [busy, setBusy] = useState(false);

  const run = async () => {
    const ids = input.split(/[\n,]+/).map((s) => s.trim()).filter(Boolean);
    if (ids.length === 0) return;
    setBusy(true);
    setError('');
    try {
      setResults(await api.benchmarkCases(ids));
    } catch (e) {
      setError(e instanceof Error ? e.message : 'benchmark failed');
      setResults(null);
    } finally {
      setBusy(false);
    }
  };

  return (
    <div className="p-6 space-y-6">
      <h1 className="text-xl font-semibold">Benchmark</h1>
      <div className="space-y-2">
        <textarea
          className="w-full h-28 rounded border border-border-dim bg-background px-3 py-2 text-sm font-mono"
          placeholder={'One case ID per line, or comma-separated\ncase-…\ncase-…'}
          value={input}
          onChange={(e) => setInput(e.target.value)}
        />
        <button
          className="rounded bg-accent px-4 py-2 text-sm disabled:opacity-50"
          disabled={busy || input.trim() === ''}
          onClick={() => void run()}
        >
          {busy ? 'Benchmarking…' : 'Run benchmark'}
        </button>
      </div>
      {error && <p className="text-red-500">{error}</p>}

      {results && (
        <div className="space-y-3">
          {Object.entries(results).map(([caseId, ev]) => (
            <div key={caseId} className="rounded border border-border-dim bg-raised p-4 text-sm">
              <p className="font-mono text-xs text-text-muted">{caseId}</p>
              <div className="grid grid-cols-2 md:grid-cols-5 gap-3 mt-2">
                <div>
                  <p className="text-xs text-text-muted">Tool success</p>
                  <p className="font-medium">{(ev.tool_success_rate * 100).toFixed(0)}%</p>
                </div>
                <div>
                  <p className="text-xs text-text-muted">Gate failures</p>
                  <p className="font-medium">{ev.gate_failures}</p>
                </div>
                <div>
                  <p className="text-xs text-text-muted">Tokens</p>
                  <p className="font-medium">{ev.total_tokens.toLocaleString()}</p>
                </div>
                <div>
                  <p className="text-xs text-text-muted">Round</p>
                  <p className="font-medium">{ev.consensus_round}</p>
                </div>
                <div>
                  <p className="text-xs text-text-muted">First-round consensus</p>
                  <p className="font-medium">{ev.first_round_consensus ? 'Yes' : 'No'}</p>
                </div>
              </div>
            </div>
          ))}
        </div>
      )}
    </div>
  );
}
