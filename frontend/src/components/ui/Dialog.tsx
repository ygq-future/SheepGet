import * as DialogPrimitive from '@radix-ui/react-dialog';
import { forwardRef, type ReactNode } from 'react';
import { X } from 'lucide-react';

export const Dialog = DialogPrimitive.Root;
export const DialogTrigger = DialogPrimitive.Trigger;
export const DialogPortal = DialogPrimitive.Portal;
export const DialogClose = DialogPrimitive.Close;

export const DialogOverlay = forwardRef<HTMLDivElement, DialogPrimitive.DialogOverlayProps>(
  ({ className = '', ...props }, ref) => (
    <DialogPrimitive.Overlay
      ref={ref}
      className={`animate-in fade-in fixed inset-0 z-50 bg-black/60 backdrop-blur-xs duration-150 ${className}`}
      {...props}
    />
  ),
);
DialogOverlay.displayName = 'DialogOverlay';

export interface DialogContentProps extends Omit<DialogPrimitive.DialogContentProps, 'title'> {
  title?: ReactNode;
  description?: ReactNode;
  showClose?: boolean;
}

export const DialogContent = forwardRef<HTMLDivElement, DialogContentProps>(
  ({ className = '', children, title, description, showClose = true, ...props }, ref) => (
    <DialogPortal>
      <DialogOverlay />
      <DialogPrimitive.Content
        ref={ref}
        className={`animate-in fade-in zoom-in-95 fixed top-1/2 left-1/2 z-50 w-full max-w-md -translate-x-1/2 -translate-y-1/2 rounded-xl border border-[var(--border-subtle)] bg-[var(--bg-surface)] p-5 text-[var(--text-primary)] shadow-2xl duration-150 focus:outline-hidden ${className}`}
        {...props}
      >
        {(title || showClose) && (
          <div className="flex items-center justify-between border-b border-[var(--border-subtle)] pb-3">
            {title ? (
              <DialogPrimitive.Title className="text-sm font-semibold tracking-wide">
                {title}
              </DialogPrimitive.Title>
            ) : (
              <div />
            )}
            {showClose && (
              <DialogPrimitive.Close className="rounded p-1 text-[var(--text-muted)] transition-colors hover:bg-[var(--bg-subtle)] hover:text-[var(--text-primary)]">
                <X className="h-4 w-4" />
              </DialogPrimitive.Close>
            )}
          </div>
        )}
        {description && (
          <DialogPrimitive.Description className="mt-1 text-xs text-[var(--text-secondary)]">
            {description}
          </DialogPrimitive.Description>
        )}
        <div className="mt-3">{children}</div>
      </DialogPrimitive.Content>
    </DialogPortal>
  ),
);
DialogContent.displayName = 'DialogContent';
