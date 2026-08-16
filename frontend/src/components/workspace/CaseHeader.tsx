import { NavLink, useNavigate } from 'react-router-dom';
import { Play, Pause, RotateCw, Download, Trash2, GitFork, Pin, PinOff, Archive } from 'lucide-react';
import { useCaseStore } from '@/stores';
import { api } from '@/api/client';
import { StatusBadge, MonoText } from '@/components/shared';
import { Button } from '@/components/ui';
import { CASE_STATUS_LABELS } from '@/types/case';
import { formatDistanceToNow } from 'date-fns';
import { useT } from '@/i18n';

const STATUS_COLORS: Record<string, string> = {
  INVESTIGATING: 'var(--melchior)',
  DEBATING: 'var(--warning)',
  RESOLVING: 'var(--warning)',
  RESOLVED: 'var(--accent)',
  DRAFT: 'var(--text-muted)',
};

export default function CaseHeader() {
  const t = useT();
  const c = useCaseStore((s) => s.case);
  const navigate = useNavigate();

  if (!c) return null;

  const consensus = c.consensus
    ? `${c.consensus.approve} : ${c.consensus.reject}`
    : '— : —';

  const createdAgo = formatDistanceToNow(new Date(c.createdAt), { addSuffix: true });

  const handleRun = () => void useCaseStore.getState().runCase(c.id);
  const handleCancel = () => void useCaseStore.getState().cancelCase(c.id);
  const handleReplay = () => navigate(`/replay?case=${c.id}`);
  const handleExport = () => void api.exportCase(c.id).catch(() => {});
  const handleTogglePin = () =>
    void useCaseStore.getState().updateCaseFlags(c.id, { pinned: !c.pinned });
  const handleToggleArchive = () =>
    void useCaseStore.getState().updateCaseFlags(c.id, { archived: !c.archived });
  const handleDelete = async () => {
    if (!window.confirm(t('ws.deleteConfirm', { id: c.id }))) return;
    try {
      await useCaseStore.getState().deleteCase(c.id);
      navigate('/');
    } catch {
      // error already surfaced via store
    }
  };

  return (
    <div className="flex items-start justify-between border-b border-border-dim px-6 py-4 bg-elevated">
      <div className="flex-1 min-w-0">
        <h1 className="text-lg font-semibold text-text-primary mb-3 font-sans">
          {c.question}
        </h1>

        {c.parentCaseId && (() => {
          const label = c.parentCaseId!.length > 24 ? `${c.parentCaseId!.slice(0, 24)}…` : c.parentCaseId;
          return (
            <NavLink
              to={`/case/${c.parentCaseId}`}
              className="inline-flex items-center gap-1 mb-3 font-mono text-xs text-text-muted hover:text-accent transition-colors"
            >
              <GitFork size={12} />
              {t('ws.forkOf', { id: label })}
            </NavLink>
          );
        })()}

        <div className="flex items-center gap-4 flex-wrap">
          <StatusBadge
            status={CASE_STATUS_LABELS[c.status]}
            color={STATUS_COLORS[c.status] || 'var(--text-muted)'}
          />

          <div className="flex items-center gap-1.5">
            <MonoText size="sm" muted>{t('ws.round')}</MonoText>
            <MonoText size="sm">{c.round}</MonoText>
          </div>

          <div className="flex items-center gap-1.5">
            <MonoText size="sm" muted>{t('ws.created')}</MonoText>
            <MonoText size="sm">{createdAgo}</MonoText>
          </div>

          <div className="flex items-center gap-1.5">
            <MonoText size="sm" muted>{t('ws.consensus')}</MonoText>
            <span className="font-mono text-sm font-semibold text-text-primary">{consensus}</span>
          </div>

          <div className="flex items-center gap-1.5">
            <MonoText size="sm" muted>{t('ws.confidence')}</MonoText>
            <span className="font-mono text-sm font-semibold text-accent">{Math.round(c.confidence)}%</span>
          </div>
        </div>
      </div>

      <div className="flex items-center gap-1.5 ml-4">
        <Button variant="accent" size="sm" onClick={handleRun}><Play size={12} /> {t('ws.run')}</Button>
        <Button size="sm" onClick={handleCancel}><Pause size={12} /> {t('ws.pause')}</Button>
        <Button size="sm" onClick={handleReplay}><RotateCw size={12} /> {t('ws.replay')}</Button>
        <Button size="sm" onClick={handleExport}><Download size={12} /> {t('ws.export')}</Button>
        <Button size="sm" variant={c.pinned ? 'accent' : 'default'} onClick={handleTogglePin}>
          {c.pinned ? <PinOff size={12} /> : <Pin size={12} />}
        </Button>
        <Button size="sm" variant={c.archived ? 'accent' : 'default'} onClick={handleToggleArchive}>
          <Archive size={12} /> {c.archived ? t('ws.unarchive') : t('ws.archive')}
        </Button>
        <Button variant="ghost" size="sm" onClick={handleDelete}><Trash2 size={12} /></Button>
      </div>
    </div>
  );
}
