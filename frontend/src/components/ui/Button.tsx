import type { ReactNode, ButtonHTMLAttributes } from 'react';

interface ButtonProps extends ButtonHTMLAttributes<HTMLButtonElement> {
  children: ReactNode;
  variant?: 'default' | 'ghost' | 'accent';
  size?: 'sm' | 'md';
}

export default function Button({ children, variant = 'default', size = 'md', className = '', ...props }: ButtonProps) {
  const base = 'inline-flex items-center justify-center gap-1.5 rounded-md font-mono text-xs font-medium tracking-wider uppercase transition-colors cursor-pointer disabled:opacity-40 disabled:cursor-not-allowed';
  const variants = {
    default: 'bg-raised text-text-secondary border border-border-dim hover:bg-overlay hover:text-text-primary',
    ghost: 'text-text-muted hover:text-text-primary hover:bg-raised',
    accent: 'text-accent border border-[var(--accent)] hover:bg-[var(--accent)]/10',
  };
  const sizes = { sm: 'px-2 py-1 text-[10px]', md: 'px-3 py-1.5' };

  return (
    <button className={`${base} ${variants[variant]} ${sizes[size]} ${className}`} {...props}>
      {children}
    </button>
  );
}
