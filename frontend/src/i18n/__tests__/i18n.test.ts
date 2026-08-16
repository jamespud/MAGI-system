import { describe, it, expect } from 'vitest';
import { t } from '@/i18n';
import { en, zh } from '@/i18n/resources';

describe('i18n', () => {
  it('resolves English strings from the source dictionary', () => {
    expect(t('nav.memory', undefined, 'en')).toBe('Memory');
    expect(t('settings.title', undefined, 'en')).toBe('Settings');
  });

  it('resolves Chinese translations', () => {
    expect(t('nav.memory', undefined, 'zh')).toBe('记忆');
    expect(t('ws.run', undefined, 'zh')).toBe('运行');
  });

  it('falls back to English then to the raw key for missing translations', () => {
    // A key that only exists in en must still resolve in zh mode.
    expect(t('app.language', undefined, 'zh')).toBe(en['app.language']);
    // A completely unknown key degrades to itself.
    expect(t('missing.key', undefined, 'zh')).toBe('missing.key');
  });

  it('substitutes placeholders', () => {
    expect(t('ws.forkOf', { id: 'case-1' }, 'en')).toBe('Fork of case-1');
    expect(t('settings.activeKeys', { n: 3 }, 'en')).toBe('3 active API key(s)');
  });

  it('covers the full navigation and workspace chrome in Chinese', () => {
    const keys = [
      'app.decisionCenter', 'app.pinned', 'app.running', 'app.completed', 'app.archived',
      'nav.decision', 'nav.memory', 'nav.replay', 'nav.approvals', 'nav.evaluation',
      'nav.dataset', 'nav.tools', 'nav.settings', 'nav.templates', 'nav.benchmark', 'nav.history',
      'ws.newDecision', 'ws.createCase', 'ws.runDecision', 'ws.run', 'ws.pause', 'ws.replay',
      'ws.export', 'ws.archive', 'ws.unarchive',
      'memory.title', 'tools.title', 'approvals.title', 'history.title',
      'templates.title', 'benchmark.title', 'evaluation.title', 'dataset.title', 'settings.title',
    ];
    for (const k of keys) {
      expect(zh[k], `zh missing: ${k}`).toBeTruthy();
      expect(en[k], `en missing: ${k}`).toBeTruthy();
    }
  });
});
