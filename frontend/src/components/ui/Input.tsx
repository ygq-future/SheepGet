import { forwardRef, type InputHTMLAttributes } from 'react';

export interface InputProps extends InputHTMLAttributes<HTMLInputElement> {
  error?: boolean;
}

export const Input = forwardRef<HTMLInputElement, InputProps>(
  ({ className = '', error = false, type = 'text', ...props }, ref) => {
    return (
      <input
        ref={ref}
        type={type}
        className={`h-8 w-full rounded-md border bg-[var(--bg-base)] px-2.5 text-xs text-[var(--text-primary)] shadow-xs transition-all duration-150 placeholder:text-[var(--text-muted)] focus:outline-hidden disabled:cursor-not-allowed disabled:opacity-50 ${
          error
            ? 'border-red-500/80 focus:border-red-500 focus:ring-1 focus:ring-red-500/30'
            : 'border-[var(--border-subtle)] hover:border-[var(--border-hover)] focus:border-[var(--accent)] focus:ring-1 focus:ring-[var(--border-focus)]/40'
        } ${className}`}
        {...props}
      />
    );
  },
);

Input.displayName = 'Input';
