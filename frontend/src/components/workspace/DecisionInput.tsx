import { useCaseStore } from '@/stores';
import { Card } from '@/components/ui';
import { MonoText } from '@/components/shared';
import { FileQuestion, BookOpen, Wrench } from 'lucide-react';
import { useT } from '@/i18n';

export default function DecisionInput() {
  const t = useT();
  const c = useCaseStore((s) => s.case);

  if (!c || c.status !== 'DRAFT') return null;

  return (
    <Card className="mx-4 mb-4">
      <h3 className="font-mono text-xs font-semibold text-text-secondary uppercase tracking-wider mb-3">{t('ws.decisionInput')}</h3>

      <div className="mb-3">
        <div className="flex items-center gap-1.5 mb-1">
          <FileQuestion size={14} className="text-text-muted" />
          <MonoText size="sm" muted>{t('ws.decisionQuestion')}</MonoText>
        </div>
        <p className="text-sm text-text-primary">{c.question}</p>
      </div>

      <div className="mb-3">
        <div className="flex items-center gap-1.5 mb-1">
          <BookOpen size={14} className="text-text-muted" />
          <MonoText size="sm" muted>{t('ws.background')}</MonoText>
        </div>
        <p className="text-sm text-text-secondary leading-relaxed">{c.background}</p>
      </div>

      {c.constraints.length > 0 && (
        <div>
          <div className="flex items-center gap-1.5 mb-1">
            <Wrench size={14} className="text-text-muted" />
            <MonoText size="sm" muted>{t('ws.constraints')}</MonoText>
          </div>
          <div className="grid grid-cols-2 gap-2">
            {c.constraints.map((ct, i) => (
              <div key={i} className="bg-raised rounded p-2">
                <MonoText size="sm" muted>{ct.label}</MonoText>
                <div className="text-sm text-text-primary mt-0.5">{ct.value}</div>
              </div>
            ))}
          </div>
        </div>
      )}
    </Card>
  );
}
