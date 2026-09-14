import { useState, useEffect } from 'react';
import { AnimatePresence, motion } from 'motion/react';
import { AlertCircle, CheckCircle2, Info, X } from 'lucide-react';

export type ToastType = 'error' | 'success' | 'info';

export interface ToastMessage {
  id: string;
  type: ToastType;
  title?: string;
  message: string;
}

let toastListener: ((toast: ToastMessage) => void) | null = null;

export function showToast(message: string, type: ToastType = 'error', title?: string) {
  if (toastListener) {
    toastListener({
      id: Math.random().toString(36).substring(2, 9),
      type,
      title,
      message,
    });
  }
}

export function ToastContainer() {
  const [toasts, setToasts] = useState<ToastMessage[]>([]);

  useEffect(() => {
    toastListener = (toast: ToastMessage) => {
      setToasts((prev) => [...prev, toast]);
      setTimeout(() => {
        setToasts((prev) => prev.filter((t) => t.id !== toast.id));
      }, 4000);
    };

    return () => {
      toastListener = null;
    };
  }, []);

  const removeToast = (id: string) => {
    setToasts((prev) => prev.filter((t) => t.id !== id));
  };

  return (
    <div className="pointer-events-none fixed right-6 bottom-6 z-50 flex flex-col gap-2.5">
      <AnimatePresence mode="popLayout">
        {toasts.map((t) => {
          const isError = t.type === 'error';
          const isSuccess = t.type === 'success';

          return (
            <motion.div
              key={t.id}
              initial={{ opacity: 0, x: 48, scale: 0.96 }}
              animate={{ opacity: 1, x: 0, scale: 1 }}
              exit={{ opacity: 0, x: 32, scale: 0.95 }}
              transition={{
                type: 'spring',
                stiffness: 420,
                damping: 32,
                mass: 0.8,
              }}
              className={`pointer-events-auto flex items-start gap-3 rounded-xl border p-3.5 shadow-xl backdrop-blur-xl transition-colors select-none ${
                isError
                  ? 'border-rose-500/30 bg-[var(--bg-surface)] text-rose-500 shadow-rose-500/5 dark:bg-[var(--bg-surface)]'
                  : isSuccess
                    ? 'border-[var(--border-focus)] bg-[var(--bg-surface)] text-[var(--text-primary)] shadow-[var(--border-focus)]/5'
                    : 'border-[var(--border-subtle)] bg-[var(--bg-surface)] text-[var(--text-primary)]'
              }`}
              style={{ minWidth: '280px', maxWidth: '380px' }}
            >
              {isError && <AlertCircle className="mt-0.5 h-4 w-4 shrink-0 text-rose-500" />}
              {isSuccess && (
                <CheckCircle2 className="mt-0.5 h-4 w-4 shrink-0 text-[var(--accent)]" />
              )}
              {!isError && !isSuccess && (
                <Info className="mt-0.5 h-4 w-4 shrink-0 text-[var(--text-secondary)]" />
              )}

              <div className="flex-1 text-xs">
                {t.title && (
                  <div className="font-semibold text-[var(--text-primary)]">{t.title}</div>
                )}
                <div
                  className={
                    t.title
                      ? 'mt-0.5 text-[var(--text-secondary)]'
                      : 'font-medium text-[var(--text-primary)]'
                  }
                >
                  {t.message}
                </div>
              </div>

              <button
                type="button"
                onClick={() => removeToast(t.id)}
                className="rounded-md p-1 text-[var(--text-muted)] opacity-70 transition-opacity hover:opacity-100"
              >
                <X className="h-3.5 w-3.5" />
              </button>
            </motion.div>
          );
        })}
      </AnimatePresence>
    </div>
  );
}
