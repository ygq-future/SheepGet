import { Check } from 'lucide-react';

interface CheckboxProps {
  id?: string;
  checked: boolean;
  onCheckedChange: (checked: boolean) => void;
  label: string;
  description?: string;
  className?: string;
}

export function Checkbox({
  id,
  checked,
  onCheckedChange,
  label,
  description,
  className = '',
}: CheckboxProps) {
  return (
    <div
      onClick={() => onCheckedChange(!checked)}
      className={`group flex cursor-pointer items-start gap-2.5 select-none ${className}`}
    >
      <button
        type="button"
        id={id}
        role="checkbox"
        aria-checked={checked}
        onClick={(e) => {
          e.stopPropagation();
          onCheckedChange(!checked);
        }}
        className={`mt-0.5 flex h-4 w-4 shrink-0 cursor-pointer items-center justify-center rounded-md border transition-all duration-150 group-hover:border-white/40 focus:ring-2 focus:ring-emerald-500/20 focus:outline-hidden ${
          checked
            ? 'border-emerald-500 bg-emerald-500 text-zinc-950 shadow-xs'
            : 'border-white/20 bg-zinc-900'
        }`}
      >
        {checked && <Check className="h-3 w-3 stroke-[3]" />}
      </button>

      <div className="flex flex-col">
        <span className="text-xs font-medium text-zinc-200 transition-colors group-hover:text-zinc-100">
          {label}
        </span>
        {description && <span className="text-[11px] text-zinc-500">{description}</span>}
      </div>
    </div>
  );
}
