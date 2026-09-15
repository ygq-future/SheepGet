import { type HTMLAttributes } from 'react';

export interface BadgeProps extends HTMLAttributes<HTMLSpanElement> {
  variant?: 'default' | 'accent' | 'outline' | 'warning' | 'success';
}

export function Badge({ className = '', variant = 'default', children, ...props }: BadgeProps) {
  const variantStyles = {
    default:
      'border border-[var(--border-subtle)] bg-[var(--bg-base)] text-[var(--text-secondary)]',
    accent: 'border border-[var(--accent)]/30 bg-[var(--accent-muted)] text-[var(--accent)]',
    outline: 'border border-[var(--border-subtle)] bg-transparent text-[var(--text-muted)]',
    warning: 'border border-amber-500/30 bg-amber-500/15 text-amber-600 dark:text-amber-300',
    success:
      'border border-emerald-500/30 bg-emerald-500/15 text-emerald-600 dark:text-emerald-400',
  }[variant];

  return (
    <span
      className={`inline-flex items-center rounded-full px-2 py-0.5 text-[10px] font-medium tracking-wide select-none ${variantStyles} ${className}`}
      {...props}
    >
      {children}
    </span>
  );
}
