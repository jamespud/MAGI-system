import type { ReactNode } from 'react';

interface GlowBorderProps {
  children: ReactNode;
  color?: string;
  className?: string;
  active?: boolean;
}

export default function GlowBorder({ children, color = 'var(--border-glow)', className = '', active = false }: GlowBorderProps) {
  return (
    <div
      className={`relative rounded-lg border transition-all duration-500 ${active ? 'border-[var(--border-active)]' : 'border-border-dim'} ${className}`}
      style={{
        boxShadow: active ? `0 0 12px ${color}` : 'none',
        borderColor: active ? 'var(--border-active)' : 'var(--border-dim)',
      }}
    >
      {children}
    </div>
  );
}
