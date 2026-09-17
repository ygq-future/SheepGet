import React from 'react';
import { Check } from 'lucide-react';

export interface CheckboxProps {
  id?: string;
  checked: boolean;
  onCheckedChange: (checked: boolean, shiftKey?: boolean) => void;
  label?: string;
  description?: string;
  className?: string;
  disabled?: boolean;
}

export function Checkbox({
  id,
  checked,
  onCheckedChange,
  label,
  description,
  className = '',
  disabled = false,
}: CheckboxProps) {
  const handleClick = (e: React.MouseEvent) => {
    e.stopPropagation();
    if (disabled) return;
    onCheckedChange(!checked, e.shiftKey);
  };
  return (
    <div
      onClick={handleClick}
      className={`group flex items-center gap-2.5 select-none ${
        disabled ? 'cursor-not-allowed opacity-50' : 'cursor-pointer'
      } ${className}`}
    >
      <button
        type="button"
        id={id}
        role="checkbox"
        disabled={disabled}
        aria-checked={checked}
        onClick={handleClick}
        className={`flex h-4 w-4 shrink-0 items-center justify-center rounded-md border transition-all duration-150 ${
          disabled
            ? 'cursor-not-allowed'
            : 'cursor-pointer group-hover:border-[var(--border-focus)]'
        } focus:ring-2 focus:ring-[var(--border-focus)] focus:outline-hidden ${
          checked
            ? 'border-[var(--accent)] bg-[var(--accent)] text-white shadow-xs'
            : 'border-[var(--border-subtle)] bg-[var(--bg-surface)] hover:border-[var(--border-hover)]'
        }`}
      >
        {checked && <Check className="h-3 w-3 stroke-[3]" />}
      </button>

      {(label || description) && (
        <div className="flex flex-col">
          {label && (
            <span className="text-xs font-medium text-[var(--text-primary)] transition-colors">
              {label}
            </span>
          )}
          {description && (
            <span className="text-[11px] text-[var(--text-muted)]">{description}</span>
          )}
        </div>
      )}
    </div>
  );
}
