import { useState, useEffect, useRef, useCallback } from 'react';
import { AnimatePresence, motion } from 'motion/react';
import { AlertCircle, AlertTriangle, CheckCircle2, Info, X } from 'lucide-react';

export type ToastType = 'info' | 'success' | 'warning' | 'error';

export interface ToastMessage {
  id: string;
  type: ToastType;
  title?: string;
  message: string;
  createdAt: number;
}
export interface ToastStyleState {
  y: number;
  scale: number;
  opacity: number;
  zIndex: number;
  pointerEvents: 'auto' | 'none';
}
export const MAX_TOASTS = 3;
export const VISIBLE_STACK_COUNT = 3;
export const STACK_OFFSET_Y = 10;
export const STACK_SCALE_FACTOR = 0.04;
export const STACK_OPACITY_STEP = 0.18;
export const TOAST_GAP = 6;
export const STACK_BASE_Z_INDEX = 30;
export const TOAST_HEIGHT_SINGLE = 46;
export const TOAST_HEIGHT_TITLED = 60;

export const DEFAULT_TOAST_DURATIONS: Record<ToastType, number> = {
  info: 2000,
  success: 2000,
  warning: 3000,
  error: 3500,
};

export function getToastHeight(toast: ToastMessage): number {
  return toast.title ? TOAST_HEIGHT_TITLED : TOAST_HEIGHT_SINGLE;
}

export function getToastStyle(
  depth: number,
  isHovered: boolean,
  expandedOffsetY: number,
): ToastStyleState {
  if (isHovered) {
    return {
      y: expandedOffsetY === 0 ? 0 : -expandedOffsetY,
      scale: 1,
      opacity: 1,
      zIndex: 50 - depth,
      pointerEvents: 'auto',
    };
  }

  if (depth === 0) {
    return {
      y: 0,
      scale: 1,
      opacity: 1,
      zIndex: STACK_BASE_Z_INDEX,
      pointerEvents: 'auto',
    };
  }

  if (depth < VISIBLE_STACK_COUNT) {
    return {
      y: -depth * STACK_OFFSET_Y,
      scale: 1 - depth * STACK_SCALE_FACTOR,
      opacity: Math.max(0.4, 1 - depth * STACK_OPACITY_STEP),
      zIndex: STACK_BASE_Z_INDEX - depth,
      pointerEvents: 'none',
    };
  }

  return {
    y: -VISIBLE_STACK_COUNT * STACK_OFFSET_Y,
    scale: 1 - VISIBLE_STACK_COUNT * STACK_SCALE_FACTOR,
    opacity: 0,
    zIndex: 0,
    pointerEvents: 'none',
  };
}

let toastListener: ((toast: ToastMessage) => void) | null = null;

export function showToast(message: string, type: ToastType = 'error', title?: string) {
  if (toastListener) {
    toastListener({
      id: Math.random().toString(36).substring(2, 9),
      type,
      title,
      message,
      createdAt: Date.now(),
    });
  }
}

const TOAST_THEMES: Record<
  ToastType,
  {
    border: string;
    shadow: string;
    icon: React.ComponentType<{ className?: string }>;
  }
> = {
  info: {
    border: 'border-[var(--border-subtle)]',
    shadow: 'shadow-black/20',
    icon: (props) => <Info className={`text-[var(--text-secondary)] ${props.className ?? ''}`} />,
  },
  success: {
    border: 'border-[var(--border-focus)]',
    shadow: 'shadow-[var(--border-focus)]/10',
    icon: (props) => <CheckCircle2 className={`text-[var(--accent)] ${props.className ?? ''}`} />,
  },
  warning: {
    border: 'border-amber-500/30',
    shadow: 'shadow-amber-500/10',
    icon: (props) => <AlertTriangle className={`text-amber-500 ${props.className ?? ''}`} />,
  },
  error: {
    border: 'border-rose-500/30',
    shadow: 'shadow-rose-500/10',
    icon: (props) => <AlertCircle className={`text-rose-500 ${props.className ?? ''}`} />,
  },
};
export function ToastContainer() {
  const [toasts, setToasts] = useState<ToastMessage[]>([]);
  const [isHovered, setIsHovered] = useState(false);
  const timerRef = useRef<number | null>(null);

  const removeToast = useCallback((id: string) => {
    setToasts((prev) => prev.filter((t) => t.id !== id));
  }, []);

  useEffect(() => {
    toastListener = (toast: ToastMessage) => {
      setToasts((prev) => {
        const filtered = prev.filter(
          (t) => !(t.message === toast.message && t.title === toast.title && t.type === toast.type),
        );
        return [...filtered, toast].slice(-MAX_TOASTS);
      });
    };
    return () => {
      toastListener = null;
    };
  }, []);

  const clearTimer = useCallback(() => {
    if (timerRef.current !== null) {
      window.clearTimeout(timerRef.current);
      timerRef.current = null;
    }
  }, []);

  useEffect(() => {
    clearTimer();

    if (toasts.length === 0 || isHovered) {
      return;
    }

    const oldestToast = toasts[0];
    const duration = DEFAULT_TOAST_DURATIONS[oldestToast.type] ?? 2000;
    const elapsed = Date.now() - oldestToast.createdAt;
    const remaining = Math.max(50, duration - elapsed);

    timerRef.current = window.setTimeout(() => {
      removeToast(oldestToast.id);
    }, remaining);

    return clearTimer;
  }, [toasts, isHovered, removeToast, clearTimer]);
  if (toasts.length === 0) {
    return null;
  }

  const reversed = [...toasts].reverse();

  let cumulativeY = 0;
  const offsets: number[] = [];
  for (let i = 0; i < reversed.length; i++) {
    offsets.push(cumulativeY);
    cumulativeY += getToastHeight(reversed[i]) + TOAST_GAP;
  }

  return (
    <aside
      aria-label="通知提示"
      onMouseEnter={() => setIsHovered(true)}
      onMouseLeave={() => setIsHovered(false)}
      className="pointer-events-auto fixed right-6 bottom-6 z-50 w-[320px] select-none"
      style={{ height: isHovered ? Math.max(60, cumulativeY) : 60 }}
    >
      <div className="relative h-full w-full">
        <AnimatePresence mode="popLayout">
          {reversed.map((t, depth) => {
            const styleState = getToastStyle(depth, isHovered, offsets[depth]);
            const theme = TOAST_THEMES[t.type];
            const IconComponent = theme.icon;
            return (
              <motion.div
                key={t.id}
                initial={{ opacity: 0, x: 40, scale: 0.9 }}
                animate={{
                  x: 0,
                  y: styleState.y,
                  scale: styleState.scale,
                  opacity: styleState.opacity,
                  zIndex: styleState.zIndex,
                }}
                exit={{
                  opacity: 0,
                  x: 80,
                  scale: 0.9,
                  transition: { duration: 0.16, ease: 'easeOut' },
                }}
                transition={{
                  type: 'spring',
                  stiffness: 380,
                  damping: 28,
                  mass: 0.8,
                }}
                className={`absolute right-0 bottom-0 w-full rounded-xl border bg-[var(--bg-surface)] p-2.5 shadow-xl backdrop-blur-xl transition-colors ${theme.border} ${theme.shadow}`}
                style={{
                  pointerEvents: styleState.pointerEvents,
                  transformOrigin: 'bottom center',
                }}
              >
                <div className="flex items-start gap-2.5">
                  <IconComponent className="mt-0.5 h-4 w-4 shrink-0" />
                  <div className="min-w-0 flex-1 text-xs">
                    {t.title && (
                      <div className="truncate font-semibold text-[var(--text-primary)]">
                        {t.title}
                      </div>
                    )}
                    <div
                      className={`leading-snug break-words ${
                        t.title
                          ? 'mt-0.5 text-[var(--text-secondary)]'
                          : 'font-medium text-[var(--text-primary)]'
                      }`}
                    >
                      {t.message}
                    </div>
                  </div>

                  <button
                    type="button"
                    onClick={(e) => {
                      e.stopPropagation();
                      removeToast(t.id);
                    }}
                    className="cursor-pointer rounded-md p-1 text-[var(--text-muted)] opacity-70 transition-all hover:bg-[var(--bg-subtle)] hover:opacity-100"
                    title="关闭"
                  >
                    <X className="h-3.5 w-3.5" />
                  </button>
                </div>
              </motion.div>
            );
          })}
        </AnimatePresence>
      </div>
    </aside>
  );
}
