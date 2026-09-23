import { forwardRef, type ButtonHTMLAttributes } from 'react';

export interface ButtonProps extends ButtonHTMLAttributes<HTMLButtonElement> {
  variant?: 'primary' | 'secondary' | 'ghost' | 'danger';
  size?: 'sm' | 'md' | 'icon';
}

export const Button = forwardRef<HTMLButtonElement, ButtonProps>(
  (
    { className = '', variant = 'secondary', size = 'md', type = 'button', children, ...props },
    ref,
  ) => {
    const variantStyles = {
      primary:
        'border border-[var(--accent)]/45 bg-[var(--bg-surface)] text-[var(--text-primary)] shadow-[0_0_12px_-3px_var(--accent-muted),inset_0_1px_0_rgba(255,255,255,0.08)] hover:border-[var(--accent)]/80 hover:bg-[var(--bg-surface-hover)] hover:shadow-[0_0_16px_-2px_var(--accent-muted),inset_0_1px_0_rgba(255,255,255,0.14)] active:scale-[0.98]',
      secondary:
        'border border-[var(--border-subtle)] bg-[var(--bg-surface)] text-[var(--text-primary)] hover:bg-[var(--bg-surface-hover)] hover:border-[var(--border-hover)] active:scale-[0.98]',
      ghost:
        'border border-transparent bg-transparent text-[var(--text-secondary)] hover:bg-[var(--bg-subtle)] hover:text-[var(--text-primary)] active:scale-[0.98]',
      danger:
        'border border-red-500/30 bg-red-500/10 text-red-500 hover:bg-red-500/20 active:scale-[0.98]',
    }[variant];

    const sizeStyles = {
      sm: 'h-7 px-2.5 text-xs gap-1.5 rounded-lg',
      md: 'h-8 px-3 text-xs gap-1.5 rounded-lg',
      icon: 'h-8 w-8 p-0 shrink-0 justify-center rounded-lg',
    }[size];

    return (
      <button
        ref={ref}
        type={type}
        className={`inline-flex cursor-pointer items-center justify-center font-medium whitespace-nowrap transition-all duration-150 select-none focus:ring-1 focus:ring-[var(--border-focus)]/40 focus:outline-hidden disabled:cursor-not-allowed disabled:opacity-50 ${variantStyles} ${sizeStyles} ${className}`}
        {...props}
      >
        {children}
      </button>
    );
  },
);

Button.displayName = 'Button';
