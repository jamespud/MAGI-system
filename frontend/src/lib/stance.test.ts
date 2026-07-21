import { describe, it, expect } from 'vitest';
import { normalizeStance, stanceColor, stanceLabel } from './stance';

describe('normalizeStance', () => {
  it('lowercases known stances', () => {
    expect(normalizeStance('Approve')).toBe('approve');
    expect(normalizeStance('REJECT')).toBe('reject');
    expect(normalizeStance('Abstain')).toBe('abstain');
    expect(normalizeStance('conditional_approve')).toBe('conditional_approve');
    expect(normalizeStance('Conditional_Approve')).toBe('conditional_approve');
  });

  it('falls back to abstain for unknown/empty/null', () => {
    expect(normalizeStance('')).toBe('abstain');
    expect(normalizeStance(undefined)).toBe('abstain');
    expect(normalizeStance(null)).toBe('abstain');
    expect(normalizeStance('nonsense')).toBe('abstain');
  });
});

describe('stanceColor', () => {
  it('maps each stance to a CSS variable', () => {
    expect(stanceColor('approve')).toBe('var(--accent)');
    expect(stanceColor('reject')).toBe('var(--error)');
    expect(stanceColor('abstain')).toBe('var(--text-muted)');
    expect(stanceColor('conditional_approve')).toBe('var(--warning)');
  });

  it('is case-insensitive', () => {
    expect(stanceColor('Approve')).toBe('var(--accent)');
    expect(stanceColor('REJECT')).toBe('var(--error)');
  });
});

describe('stanceLabel', () => {
  it('uppercases simple stances', () => {
    expect(stanceLabel('approve')).toBe('APPROVE');
    expect(stanceLabel('reject')).toBe('REJECT');
    expect(stanceLabel('abstain')).toBe('ABSTAIN');
  });

  it('labels conditional_approve as two words', () => {
    expect(stanceLabel('conditional_approve')).toBe('CONDITIONAL APPROVE');
  });
});
