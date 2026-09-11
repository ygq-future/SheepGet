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
          className={`flex w-full items-center justify-between rounded-lg border border-white/10 bg-zinc-900/90 px-3 py-2 text-xs font-medium text-zinc-100 shadow-xs transition-all duration-150 hover:border-white/20 hover:bg-zinc-800/60 focus:border-emerald-500/50 focus:ring-2 focus:ring-emerald-500/20 focus:outline-hidden ${className}`}
        >
          <span className="truncate">{selectedOption ? selectedOption.label : placeholder}</span>
          <ChevronDown className="h-3.5 w-3.5 shrink-0 text-zinc-400 transition-transform duration-200" />
        </button>
      </DropdownMenu.Trigger>

      <DropdownMenu.Portal>
        <DropdownMenu.Content
          align="start"
          sideOffset={6}
          className="animate-in fade-in-80 zoom-in-95 z-50 min-w-(--radix-dropdown-menu-trigger-width) overflow-hidden rounded-lg border border-white/10 bg-zinc-900/95 p-1 text-zinc-200 shadow-2xl backdrop-blur-xl"
        >
          {options.map((opt) => {
            const isSelected = opt.value === value;
            return (
              <DropdownMenu.Item
                key={String(opt.value)}
                onSelect={() => onChange(opt.value)}
                className={`group flex cursor-pointer items-center justify-between rounded-md px-2.5 py-1.5 text-xs font-medium outline-hidden transition-colors select-none ${
                  isSelected
                    ? 'bg-emerald-500/10 text-emerald-400'
                    : 'text-zinc-300 hover:bg-white/10 hover:text-zinc-100'
                }`}
              >
                <div className="flex flex-col">
                  <span>{opt.label}</span>
                  {opt.description && (
                    <span className="text-[10px] text-zinc-500 group-hover:text-zinc-400">
                      {opt.description}
                    </span>
                  )}
                </div>
                {isSelected && <Check className="ml-2 h-3.5 w-3.5 shrink-0 text-emerald-400" />}
              </DropdownMenu.Item>
            );
          })}
        </DropdownMenu.Content>
      </DropdownMenu.Portal>
    </DropdownMenu.Root>
  );
}
