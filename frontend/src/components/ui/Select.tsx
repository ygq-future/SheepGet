import * as DropdownMenu from '@radix-ui/react-dropdown-menu';
import { ChevronDown, Check } from 'lucide-react';

export interface SelectOption<T extends string | number> {
  value: T;
  label: string;
  description?: string;
}

interface SelectProps<T extends string | number> {
  value: T;
  onChange: (value: T) => void;
  options: SelectOption<T>[];
  placeholder?: string;
  className?: string;
}

export function Select<T extends string | number>({
  value,
  onChange,
  options,
  placeholder = '请选择',
  className = '',
}: SelectProps<T>) {
  const selectedOption = options.find((opt) => opt.value === value);

  return (
    <DropdownMenu.Root>
      <DropdownMenu.Trigger asChild>
        <button
          type="button"
          className={`flex w-full items-center justify-between rounded-md border border-[var(--border-subtle)] bg-[var(--bg-surface)] px-2.5 text-xs font-medium text-[var(--text-primary)] shadow-xs transition-all duration-150 hover:border-[var(--border-hover)] hover:bg-[var(--bg-surface-hover)] focus:border-[var(--accent)] focus:ring-1 focus:ring-[var(--border-focus)]/40 focus:outline-hidden ${className || 'h-9 py-2'}`}
        >
          <span className="truncate">{selectedOption ? selectedOption.label : placeholder}</span>
          <ChevronDown className="h-3.5 w-3.5 shrink-0 text-[var(--text-muted)] transition-transform duration-200" />
        </button>
      </DropdownMenu.Trigger>

      <DropdownMenu.Portal>
        <DropdownMenu.Content
          align="start"
          sideOffset={6}
          className="animate-in fade-in-80 zoom-in-95 z-50 min-w-(--radix-dropdown-menu-trigger-width) overflow-hidden rounded-lg border border-[var(--border-subtle)] bg-[var(--bg-surface)] p-1 text-[var(--text-primary)] shadow-2xl backdrop-blur-xl"
        >
          {options.map((opt) => {
            const isSelected = opt.value === value;
            return (
              <DropdownMenu.Item
                key={String(opt.value)}
                onSelect={() => onChange(opt.value)}
                className={`group flex cursor-pointer items-center justify-between rounded-md px-2.5 py-1.5 text-xs font-medium outline-hidden transition-colors select-none ${
                  isSelected
                    ? 'bg-[var(--accent-muted)] text-[var(--accent)]'
                    : 'text-[var(--text-secondary)] hover:bg-[var(--bg-subtle)] hover:text-[var(--text-primary)]'
                }`}
              >
                <div className="flex flex-col">
                  <span>{opt.label}</span>
                  {opt.description && (
                    <span className="text-[10px] text-[var(--text-muted)]">{opt.description}</span>
                  )}
                </div>
                {isSelected && <Check className="ml-2 h-3.5 w-3.5 shrink-0 text-[var(--accent)]" />}
              </DropdownMenu.Item>
            );
          })}
        </DropdownMenu.Content>
      </DropdownMenu.Portal>
    </DropdownMenu.Root>
  );
}
