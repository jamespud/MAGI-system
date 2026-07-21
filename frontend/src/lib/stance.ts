export type Stance = 'approve' | 'reject' | 'abstain' | 'conditional_approve';

const KNOWN: Stance[] = ['approve', 'reject', 'abstain', 'conditional_approve'];

export function normalizeStance(s: string | undefined | null): Stance {
  const n = (s ?? '').toLowerCase();
  return (KNOWN as string[]).includes(n) ? (n as Stance) : 'abstain';
}

export function stanceColor(s: string | undefined | null): string {
  const n = normalizeStance(s);
  if (n === 'approve') return 'var(--accent)';
  if (n === 'reject') return 'var(--error)';
  if (n === 'conditional_approve') return 'var(--warning)';
  return 'var(--text-muted)';
}

export function stanceLabel(s: string | undefined | null): string {
  const n = normalizeStance(s);
  return n === 'conditional_approve' ? 'CONDITIONAL APPROVE' : n.toUpperCase();
}
