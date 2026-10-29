import { useState, useEffect } from 'react';
import * as DropdownMenu from '@radix-ui/react-dropdown-menu';
import { ChevronDown, Check, X } from 'lucide-react';
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
  clearable?: boolean;
  disabled?: boolean;
  /** ghost 只保留细边框与文字，不铺底色。 */
  ghost?: boolean;
}

export function Select<T extends string | number>({
  value,
  onChange,
  options,
  placeholder = '请选择',
  className = '',
  clearable = false,
  disabled = false,
  ghost = false,
}: SelectProps<T>) {
  const selectedOption = options.find((opt) => opt.value === value);
  const hasValue = value !== '' && value !== undefined && value !== null;
  const [open, setOpen] = useState(false);
  const showClear = clearable && Boolean(hasValue && selectedOption);

  useEffect(() => {
    if (!open) return;
    const close = () => setOpen(false);
    window.addEventListener('wails:titlebar:pointerdown', close);
    return () => {
      window.removeEventListener('wails:titlebar:pointerdown', close);
    };
  }, [open]);

  const handleClear = () => {
    onChange('' as T);
  };

  return (
    <DropdownMenu.Root open={open} onOpenChange={setOpen}>
      {/* 清空控件必须是 trigger 的兄弟而不是子节点：<button> 里不能再放可聚焦控件。 */}
      <div className="relative flex w-full min-w-0">
        <DropdownMenu.Trigger asChild>
          <button
            type="button"
            data-select-trigger
            disabled={disabled}
            className={`flex w-full min-w-0 items-center justify-between text-xs font-medium text-[var(--text-primary)] transition-all duration-150 focus:outline-hidden disabled:pointer-events-none disabled:opacity-50 ${
              ghost
                ? `rounded-lg border px-2 ${
                    open
                      ? 'border-[var(--accent)] bg-black/5 ring-1 ring-[var(--border-focus)]/40 dark:bg-white/10'
                      : 'border-[var(--border-subtle)] bg-transparent hover:border-[var(--border-hover)] hover:bg-black/5 dark:hover:bg-white/10'
                  }`
                : `rounded-md border px-2.5 shadow-xs ${
                    open
                      ? 'border-[var(--accent)] bg-[var(--bg-surface-hover)] ring-1 ring-[var(--border-focus)]/40'
                      : 'border-[var(--border-subtle)] bg-[var(--bg-surface)] hover:border-[var(--border-hover)] hover:bg-[var(--bg-surface-hover)]'
                  }`
            } ${className || 'h-8 py-1.5'}`}
          >
            <span
              className={`truncate ${showClear ? 'pr-5' : ''} ${
                selectedOption ? 'text-[var(--text-primary)]' : 'text-[var(--text-muted)]'
              }`}
            >
              {selectedOption ? selectedOption.label : placeholder}
            </span>
            {!showClear && (
              <ChevronDown
                className={`h-3.5 w-3.5 shrink-0 text-[var(--text-muted)] transition-transform duration-200 ${
                  open ? 'rotate-180 text-[var(--accent)]' : ''
                }`}
              />
            )}
          </button>
        </DropdownMenu.Trigger>
        {showClear && (
          <button
            type="button"
            title="清空"
            aria-label="清空"
            disabled={disabled}
            onClick={handleClear}
            className={`absolute top-1/2 ${ghost ? 'right-2' : 'right-2.5'} flex h-4 w-4 -translate-y-1/2 items-center justify-center rounded-sm text-[var(--text-muted)] transition-colors hover:bg-white/10 hover:text-[var(--text-primary)] disabled:pointer-events-none disabled:opacity-50`}
          >
            <X className="h-3 w-3" />
          </button>
        )}
      </div>

      <DropdownMenu.Portal>
        <DropdownMenu.Content
          align="start"
          sideOffset={4}
          collisionPadding={8}
          className="animate-in fade-in-80 zoom-in-95 z-[9999] flex max-h-52 min-w-(--radix-dropdown-menu-trigger-width) flex-col gap-1 overflow-y-auto rounded-lg border border-[var(--border-subtle)] bg-[var(--bg-surface)] p-1 text-[var(--text-primary)] shadow-2xl backdrop-blur-xl"
        >
          {options.map((opt) => {
            const isSelected = opt.value === value;
            return (
              <DropdownMenu.Item
                key={String(opt.value)}
                onSelect={() => {
                  onChange(opt.value);
                  setOpen(false);
                }}
                onClick={() => {
                  onChange(opt.value);
                  setOpen(false);
                }}
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
