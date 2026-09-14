interface SwitchProps {
  id?: string;
  checked: boolean;
  onCheckedChange: (checked: boolean) => void;
  label?: string;
  description?: string;
  disabled?: boolean;
  className?: string;
}

export function Switch({
  id,
  checked,
  onCheckedChange,
  label,
  description,
  disabled = false,
  className = '',
}: SwitchProps) {
  return (
    <div
      onClick={() => {
        if (!disabled) onCheckedChange(!checked);
      }}
      className={`group flex items-start gap-3 select-none ${
        disabled ? 'cursor-not-allowed opacity-50' : 'cursor-pointer'
      } ${className}`}
    >
      <button
        type="button"
        id={id}
        role="switch"
        aria-checked={checked}
        disabled={disabled}
        onClick={(e) => {
          e.stopPropagation();
          if (!disabled) onCheckedChange(!checked);
        }}
        className={`relative mt-0.5 inline-flex h-5 w-9 shrink-0 cursor-pointer items-center rounded-full border border-transparent transition-colors duration-200 ease-in-out focus:ring-2 focus:ring-[var(--border-focus)] focus:outline-hidden ${
          checked ? 'bg-[var(--accent)]' : 'bg-[var(--bg-subtle)]'
        }`}
      >
        <span
          className={`pointer-events-none inline-block h-4 w-4 transform rounded-full bg-white shadow-md ring-0 transition duration-200 ease-in-out ${
            checked ? 'translate-x-4' : 'translate-x-0.5'
          }`}
        />
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
