import { useCaseStore, useAgentStore } from '@/stores';
import { Card } from '@/components/ui';
import { MonoText } from '@/components/shared';
import { Check, X, Minus } from 'lucide-react';

export default function ConsensusPanel() {
  const { consensus, confidence } = useCaseStore((s) => ({
    consensus: s.case?.consensus,
    confidence: s.case?.confidence,
  }));
  const agents = useAgentStore((s) => s.agents);

  const renderVoteIcon = (stance: string | undefined) => {
    if (stance === 'Approve') return <Check size={14} className="text-accent" />;
    if (stance === 'Reject') return <X size={14} className="text-error" />;
    return <Minus size={14} className="text-text-muted" />;
  };

  const timelineSteps = ['Round 1', 'Debate', 'Reflection', 'Round 2', 'Resolved'];
  const currentStep = 0;

  return (
    <Card className="mx-4 mb-4">
      <h3 className="font-mono text-xs font-semibold text-text-secondary uppercase tracking-wider mb-3">Consensus</h3>

      <div className="grid grid-cols-4 gap-4">
        <div>
          <MonoText size="sm" muted>Current</MonoText>
          <div className="mt-1">
            <span className="font-mono text-2xl font-bold text-text-primary">
              {consensus ? `${consensus.approve} : ${consensus.reject}` : '— : —'}
            </span>
          </div>
        </div>

        <div>
          <MonoText size="sm" muted>Majority</MonoText>
          <div className="mt-1">
            <span className="font-mono text-sm font-semibold" style={{ color: consensus?.majority === 'Approve' ? 'var(--accent)' : 'var(--error)' }}>
              {consensus?.majority || 'Pending'}
            </span>
          </div>
        </div>

        <div>
          <MonoText size="sm" muted>Confidence</MonoText>
          <div className="mt-1">
            <span className="font-mono text-sm font-semibold text-accent">{confidence}%</span>
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
