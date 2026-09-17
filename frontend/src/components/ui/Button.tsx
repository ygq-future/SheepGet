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
        'bg-[var(--accent)] text-white hover:opacity-90 active:scale-98 shadow-xs border border-transparent',
      secondary:
        'border border-[var(--border-subtle)] bg-[var(--bg-surface)] text-[var(--text-primary)] hover:bg-[var(--bg-surface-hover)] hover:border-[var(--border-hover)]',
      ghost:
        'border border-transparent bg-transparent text-[var(--text-secondary)] hover:bg-[var(--bg-subtle)] hover:text-[var(--text-primary)]',
      danger:
        'border border-red-500/30 bg-red-500/10 text-red-500 hover:bg-red-500/20 active:scale-98',
    }[variant];

    const sizeStyles = {
      sm: 'h-7 px-2.5 text-xs gap-1.5',
      md: 'h-8 px-3 text-xs gap-1.5',
      icon: 'h-8 w-8 p-0 shrink-0 justify-center',
    }[size];

    return (
      <button
        ref={ref}
        type={type}
        className={`inline-flex cursor-pointer items-center justify-center rounded-md font-medium transition-all duration-150 select-none focus:ring-1 focus:ring-[var(--border-focus)]/40 focus:outline-hidden disabled:cursor-not-allowed disabled:opacity-50 ${variantStyles} ${sizeStyles} ${className}`}
        {...props}
      >
        {children}
      </button>
    );
  },
);

Button.displayName = 'Button';
