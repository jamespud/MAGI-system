import { Play, Pause, RotateCw, Download, Trash2 } from 'lucide-react';
import { useCaseStore } from '@/stores';
import { StatusBadge, MonoText } from '@/components/shared';
import { Button } from '@/components/ui';
import { CASE_STATUS_LABELS } from '@/types/case';
import { formatDistanceToNow } from 'date-fns';

const STATUS_COLORS: Record<string, string> = {
  INVESTIGATING: 'var(--melchior)',
  DEBATING: 'var(--warning)',
  RESOLVING: 'var(--warning)',
  RESOLVED: 'var(--accent)',
  DRAFT: 'var(--text-muted)',
};

export default function CaseHeader() {
  const c = useCaseStore((s) => s.case);

  if (!c) return null;

  const consensus = c.consensus
    ? `${c.consensus.approve} : ${c.consensus.reject}`
    : '— : —';

  const createdAgo = formatDistanceToNow(new Date(c.createdAt), { addSuffix: true });

  return (
    <div className="flex items-start justify-between border-b border-border-dim px-6 py-4 bg-elevated">
      <div className="flex-1 min-w-0">
        <h1 className="text-lg font-semibold text-text-primary mb-3 font-sans">
          {c.question}
        </h1>

        <div className="flex items-center gap-4 flex-wrap">
          <StatusBadge
            status={CASE_STATUS_LABELS[c.status]}
            color={STATUS_COLORS[c.status] || 'var(--text-muted)'}
          />

          <div className="flex items-center gap-1.5">
            <MonoText size="sm" muted>Round</MonoText>
            <MonoText size="sm">{c.round}</MonoText>
          </div>

          <div className="flex items-center gap-1.5">
            <MonoText size="sm" muted>Created</MonoText>
            <MonoText size="sm">{createdAgo}</MonoText>
          </div>

          <div className="flex items-center gap-1.5">
            <MonoText size="sm" muted>Consensus</MonoText>
            <span className="font-mono text-sm font-semibold text-text-primary">{consensus}</span>
          </div>

          <div className="flex items-center gap-1.5">
            <MonoText size="sm" muted>Confidence</MonoText>
            <span className="font-mono text-sm font-semibold text-accent">{c.confidence}%</span>
          </div>
        </div>
      </div>

      <div className="flex items-center gap-1.5 ml-4">
        <Button variant="accent" size="sm"><Play size={12} /> Run</Button>
        <Button size="sm"><Pause size={12} /> Pause</Button>
        <Button size="sm"><RotateCw size={12} /> Replay</Button>
        <Button size="sm"><Download size={12} /> Export</Button>
        <Button variant="ghost" size="sm"><Trash2 size={12} /></Button>
      </div>
    </div>
  );
}
