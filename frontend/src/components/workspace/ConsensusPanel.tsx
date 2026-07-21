import { useCaseStore, useAgentStore } from '@/stores';
import { Card } from '@/components/ui';
import { MonoText } from '@/components/shared';
import { Check, X, Minus } from 'lucide-react';
import { CASE_STATUS_LABELS, type CaseStatus } from '@/types/case';
import { normalizeStance, stanceColor } from '@/lib/stance';

export default function ConsensusPanel() {
  const consensus = useCaseStore((s) => s.case?.consensus ?? null);
  const confidence = useCaseStore((s) => s.case?.confidence ?? 0);
  const status = useCaseStore((s) => s.case?.status ?? 'DRAFT');
  const agents = useAgentStore((s) => s.agents);

  const renderVoteIcon = (stance: string | undefined) => {
    const s = normalizeStance(stance);
    if (s === 'approve') return <Check size={14} className="text-accent" />;
    if (s === 'reject') return <X size={14} className="text-error" />;
    if (s === 'conditional_approve') return <Check size={14} className="text-warning" />;
    return <Minus size={14} className="text-text-muted" />;
  };

  // When there is no persisted consensus (e.g. DEADLOCKED cases never reach
  // RESOLVED so no resolution is stored), derive the vote distribution from
  // the agents' votes so the panel still reflects what happened.
  const derived = (() => {
    if (consensus) return { approve: consensus.approve, reject: consensus.reject, abstain: consensus.abstain };
    let approve = 0, reject = 0, abstain = 0;
    for (const id of ['melchior', 'balthasar', 'casper'] as const) {
      const s = normalizeStance(agents[id]?.vote?.stance);
      if (s === 'approve') approve++;
      else if (s === 'reject') reject++;
      else if (s === 'abstain') abstain++;
      // conditional_approve is neutral: counted into neither side
    }
    return { approve, reject, abstain };
  })();

  const isTerminal = status === 'RESOLVED' || status === 'DEADLOCKED' || status === 'FAILED' || status === 'CANCELLED' || status === 'TIMED_OUT';
  const majorityLabel = consensus?.majority
    ?? (status === 'DEADLOCKED' ? 'Deadlocked'
      : (derived.approve === 0 && derived.reject === 0 ? 'Pending' : status === 'RESOLVED' ? 'Resolved' : CASE_STATUS_LABELS[status as CaseStatus]));

  const timelineSteps = ['Round 1', 'Debate', 'Reflection', 'Round 2', 'Resolved'];
  const currentStep = (() => {
    switch (status) {
      case 'INVESTIGATING':
      case 'EVIDENCE_GATING':
      case 'COLLECTING_VOTES':
      case 'CONSENSUS_CHECK':
        return 0;
      case 'DEBATING':
        return 1;
      case 'REFLECTING':
        return 2;
      case 'REVOTING':
        return 3;
      case 'RESOLVING':
      case 'GENERATING_REPORT':
      case 'SAVING_MEMORY':
      case 'EVALUATING':
      case 'RESOLVED':
      case 'DEADLOCKED':
      case 'FAILED':
      case 'CANCELLED':
      case 'TIMED_OUT':
        return 4;
      default:
        return 0;
    }
  })();

  return (
    <Card className="mx-4 mb-4">
      <div className="flex items-center justify-between mb-3">
        <h3 className="font-mono text-xs font-semibold text-text-secondary uppercase tracking-wider">Consensus</h3>
        {isTerminal && (
          <span className={`font-mono text-[10px] px-2 py-0.5 rounded border ${
            status === 'RESOLVED' ? 'text-accent border-[var(--accent)]' : 'text-warning border-[var(--warning)]'
          }`}>
            {CASE_STATUS_LABELS[status as CaseStatus]}
          </span>
        )}
      </div>

      <div className="grid grid-cols-4 gap-4">
        <div>
          <MonoText size="sm" muted>Current</MonoText>
          <div className="mt-1">
            <span className="font-mono text-2xl font-bold text-text-primary">
              {`${derived.approve} : ${derived.reject}`}
            </span>
          </div>
        </div>

        <div>
          <MonoText size="sm" muted>Majority</MonoText>
          <div className="mt-1">
            <span className="font-mono text-sm font-semibold" style={{ color: stanceColor(majorityLabel) }}>
              {majorityLabel}
            </span>
          </div>
        </div>

        <div>
          <MonoText size="sm" muted>Confidence</MonoText>
          <div className="mt-1">
            <span className="font-mono text-sm font-semibold text-accent">{confidence > 0 ? `${Math.round(confidence * 100)}%` : '-'}</span>
          </div>
        </div>

        <div>
          <MonoText size="sm" muted>Need Reflection</MonoText>
          <div className="mt-1">
            <span className={`font-mono text-sm font-semibold ${consensus?.needReflection ? 'text-warning' : 'text-accent'}`}>
              {consensus?.needReflection ? 'YES' : 'NO'}
            </span>
          </div>
        </div>
      </div>

      <div className="flex gap-4 mt-4 pt-4 border-t border-border-dim">
        {(['melchior', 'balthasar', 'casper'] as const).map((id) => {
          const agent = agents[id];
          const v = agent?.vote;
          return (
            <div key={id} className="flex-1 flex items-center gap-2 p-2 rounded bg-raised">
              {renderVoteIcon(v?.stance)}
              <div className="flex-1 min-w-0">
                <div className="text-xs font-mono text-text-secondary">{id.toUpperCase()}</div>
                {v ? (
                  <div className="text-[10px] text-text-muted truncate">{v.stance} ({v.confidence}%)</div>
                ) : (
                  <div className="text-[10px] text-text-muted">Pending</div>
                )}
              </div>
            </div>
          );
        })}
      </div>

      <div className="flex items-center gap-2 mt-4 pt-4 border-t border-border-dim">
        {timelineSteps.map((step, i) => (
          <div key={step} className="flex items-center gap-2">
            <div className={`flex items-center gap-1.5 font-mono text-[10px] ${i <= currentStep ? 'text-accent' : 'text-text-muted'}`}>
              <span className={`inline-block w-2 h-2 rounded-full ${i <= currentStep ? 'bg-accent' : 'bg-border-dim'}`} />
              {step}
            </div>
            {i < timelineSteps.length - 1 && <span className="w-3 h-px bg-border-dim" />}
          </div>
        ))}
      </div>
    </Card>
  );
}
