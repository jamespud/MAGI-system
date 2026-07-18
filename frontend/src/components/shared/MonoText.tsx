import type { ReactNode } from 'react';

interface MonoTextProps {
  children: ReactNode;
  className?: string;
  muted?: boolean;
  size?: 'sm' | 'md' | 'lg';
}

const sizes = { sm: 'text-xs', md: 'text-sm', lg: 'text-base' };

export default function MonoText({ children, className = '', muted = false, size = 'md' }: MonoTextProps) {
  return (
    <span
      className={`font-mono ${sizes[size]} ${muted ? 'text-text-muted' : 'text-text-secondary'} ${className}`}
    >
      {children}
    </span>
  );
}
