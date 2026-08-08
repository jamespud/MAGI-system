import { useState } from 'react';
import { api, type ApiEvaluation } from '@/api/client';

interface MetricProps {
  label: string;
  value: string;
}

function Metric({ label, value }: MetricProps) {
  return (
    <div className="rounded border border-border-dim bg-raised p-4">
      <p className="text-xs text-text-muted">{label}</p>
      <p className="text-lg font-medium mt-1">{value}</p>
    </div>
  );
}

export default function Evaluation() {
  const [caseId, setCaseId] = useState('');
  const [result, setResult] = useState<ApiEvaluation | null>(null);
  const [error, setError] = useState('');
  const [busy, setBusy] = useState(false);

  const run = async () => {
    if (!caseId.trim()) return;
    setBusy(true);
    setError('');
    try {
      const ev = await api.evaluateCase(caseId.trim());
      setResult(ev);
    } catch (e) {
      setError(e instanceof Error ? e.message : 'evaluation failed');
      setResult(null);
    } finally {
      setBusy(false);
    }
  };

  return (
    <div className="p-6 space-y-6">
      <h1 className="text-xl font-semibold">Evaluation</h1>
      <div className="flex gap-2">
        <input
          className="w-full rounded border border-border-dim bg-background px-3 py-2 text-sm"
          placeholder="Case ID (e.g. case-…)"
          value={caseId}
          onChange={(e) => setCaseId(e.target.value)}
          onKeyDown={(e) => { if (e.key === 'Enter') void run(); }}
        />
        <button
          className="rounded bg-accent px-4 py-2 text-sm disabled:opacity-50"
          disabled={!caseId.trim() || busy}
          onClick={() => void run()}
        >
          {busy ? 'Evaluating…' : 'Evaluate'}
        </button>
      </div>
      {error && <p className="text-red-500">{error}</p>}

      {result && (
        <div className="grid grid-cols-2 gap-3 md:grid-cols-5">
          <Metric label="Tool success rate" value={`${(result.tool_success_rate * 100).toFixed(0)}%`} />
          <Metric label="Gate failures" value={String(result.gate_failures)} />
          <Metric label="Total tokens" value={result.total_tokens.toLocaleString()} />
          <Metric label="Consensus round" value={String(result.consensus_round)} />
          <Metric label="First-round consensus" value={result.first_round_consensus ? 'Yes' : 'No'} />
        </div>
      )}
    </div>
  );
}
