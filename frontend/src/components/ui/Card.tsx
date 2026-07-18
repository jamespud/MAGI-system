import type { ReactNode } from 'react';

interface CardProps {
  children: ReactNode;
  className?: string;
  padded?: boolean;
}

export default function Card({ children, className = '', padded = true }: CardProps) {
  return (
    <div className={`rounded-lg border border-border-dim bg-elevated ${padded ? 'p-4' : ''} ${className}`}>
      {children}
    </div>
  );
}
