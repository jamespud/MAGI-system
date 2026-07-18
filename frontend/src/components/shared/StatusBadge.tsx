interface StatusBadgeProps {
  status: string;
  color?: string;
  className?: string;
}

export default function StatusBadge({ status, color, className = '' }: StatusBadgeProps) {
  return (
    <span
      className={`inline-flex items-center gap-1.5 rounded-full px-2.5 py-0.5 font-mono text-xs font-medium tracking-wider uppercase ${className}`}
      style={{
        color: color || 'var(--text-secondary)',
        backgroundColor: color ? `${color}15` : 'var(--bg-raised)',
        border: `1px solid ${color ? `${color}30` : 'var(--border-dim)'}`,
      }}
    >
      <span
        className="inline-block rounded-full"
        style={{ width: 6, height: 6, backgroundColor: color || 'var(--text-muted)' }}
      />
      {status}
    </span>
  );
}
