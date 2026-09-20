import { useEffect, useState, useMemo, useRef } from 'react';
import { Events } from '@wailsio/runtime';
import { AnimatePresence, motion, LayoutGroup } from 'motion/react';
import { unwrapEventData } from '../lib/utils';
import { Event } from '../lib/events';
import {
  DownloadCloud,
  CheckCircle2,
  Folder,
  ExternalLink,
  X,
  Minus,
  Pin,
  Play,
  Pause,
  RotateCcw,
  Clock,
  AlertCircle,
  Loader2,
  Layers,
} from 'lucide-react';
import { useSettingsStore } from '../stores/settings';
import {
  ListTasks,
  OpenFile,
  OpenFolder,
  PauseTask,
  ResumeTask,
  RetryTask,
  RetryProcessingTask,
  MinimiseProgressWindow,
  HideProgressWindow,
  SetProgressWindowHeight,
  ToggleProgressWindowAlwaysOnTop,
  IsProgressWindowAlwaysOnTop,
} from '../../bindings/sheep-get/app';
import { ToastContainer, showToast } from '../components/ui/Toast';
import * as taskModels from '../../bindings/sheep-get/internal/task/models';
import { formatBytes, formatSpeed } from '../lib/format';
import { progressWindowHeightFor } from '../lib/windowSize';
import {
  calculateChunkProgress,
  calculateChunkDividers,
  aggregateSegmentCells,
  isProcessingFailure,
} from '../lib/progress';

/**
 * Sort active (non-completed) tasks:
 * 1. StatusProcessing
 * 2. StatusDownloading (sorted by progress percentage descending; equal/unknown sorted by downloaded bytes descending)
 * 3. StatusQueued
 * 4. StatusPaused
 * 5. StatusError
 */
export function sortActiveTasks(taskList: taskModels.Task[]): taskModels.Task[] {
  return [...taskList].sort((a, b) => {
    const statusPriority = (t: taskModels.Task) => {
      switch (t.status) {
        case taskModels.Status.StatusProcessing:
          return 1;
        case taskModels.Status.StatusDownloading:
          return 2;
        case taskModels.Status.StatusQueued:
          return 3;
        case taskModels.Status.StatusPaused:
          return 4;
        case taskModels.Status.StatusError:
          return 5;
        default:
          return 6;
      }
    };

    const prioA = statusPriority(a);
    const prioB = statusPriority(b);
    if (prioA !== prioB) {
      return prioA - prioB;
    }

    // Within downloading: sort by progress percentage descending
    if (
      a.status === taskModels.Status.StatusDownloading &&
      b.status === taskModels.Status.StatusDownloading
    ) {
      const pctA = a.totalBytes > 0 ? (a.downloaded / a.totalBytes) * 100 : -1;
      const pctB = b.totalBytes > 0 ? (b.downloaded / b.totalBytes) * 100 : -1;
      if (pctA !== pctB) {
        return pctB - pctA; // Descending order
      }
      if (a.downloaded !== b.downloaded) {
        return b.downloaded - a.downloaded;
      }
    }

    // Secondary sort: newest first
    const timeA = new Date(a.updatedAt || a.createdAt || 0).getTime();
    const timeB = new Date(b.updatedAt || b.createdAt || 0).getTime();
    return timeB - timeA;
  });
}

export function ProgressView() {
  const { settings, loadSettings } = useSettingsStore();
  const [activeTasks, setActiveTasks] = useState<taskModels.Task[]>([]);
  const [completedTasks, setCompletedTasks] = useState<taskModels.Task[]>([]);
  const [focusedTaskId, setFocusedTaskId] = useState<string | null>(() => {
    const params = new URLSearchParams(window.location.search);
    return params.get('focus');
  });

  const [alwaysOnTop, setAlwaysOnTop] = useState<boolean>(false);

  useEffect(() => {
    let ignore = false;
    void (async () => {
      try {
        const top = await IsProgressWindowAlwaysOnTop();
        if (!ignore) {
          setAlwaysOnTop(top);
        }
      } catch (err) {
        console.error('Failed to get always on top state:', err);
      }
    })();
    return () => {
      ignore = true;
    };
  }, []);

  const handleToggleAlwaysOnTop = async () => {
    try {
      const next = await ToggleProgressWindowAlwaysOnTop();
      setAlwaysOnTop(next);
    } catch (err) {
      console.error('Failed to toggle always on top:', err);
    }
  };
  const contentRef = useRef<HTMLDivElement>(null);

  const keepCompletedInfo = settings?.download?.keepCompletedInfo ?? true;
  const autoRemoveCompletedOnOpen = settings?.download?.autoRemoveCompletedOnOpen ?? false;

  // Dynamic window height observation
  // 窗口高度跟随内容自然高度，只受上限约束：超过上限后由内容区滚动，不再继续变高。
  useEffect(() => {
    const el = contentRef.current;
    if (!el) return;

    let lastHeight = 0;
    const observer = new ResizeObserver((entries) => {
      for (const entry of entries) {
        const contentHeight = Math.ceil(
          entry.borderBoxSize?.[0]?.blockSize ?? entry.contentRect.height,
        );
        const targetHeight = progressWindowHeightFor(contentHeight);
        if (Math.abs(targetHeight - lastHeight) >= 2) {
          lastHeight = targetHeight;
          void SetProgressWindowHeight(targetHeight);
        }
      }
    });

    observer.observe(el);
    return () => observer.disconnect();
  }, []);

  // Initial load and live event subscription
  useEffect(() => {
    void loadSettings();
    let ignore = false;

    void (async () => {
      try {
        const list = await ListTasks();
        if (ignore) return;
        const valid = (list || []).filter((t): t is taskModels.Task => t !== null);

        // Active tasks: all non-completed tasks
        const active = valid.filter((t) => t.status !== taskModels.Status.StatusCompleted);
        setActiveTasks(sortActiveTasks(active));

        // Completed tasks: only if explicitly focused on startup via URL param
        const initialFocus = new URLSearchParams(window.location.search).get('focus');
        let hasFocused = false;
        if (initialFocus) {
          const focused = valid.find(
            (t) => t.id === initialFocus && t.status === taskModels.Status.StatusCompleted,
          );
          if (focused) {
            hasFocused = true;
            setCompletedTasks([focused]);
          }
        }

        // If on initial open there are no active tasks and no focused task, close window
        if (active.length === 0 && !hasFocused) {
          void HideProgressWindow();
        }
      } catch (err) {
        console.error('Failed to fetch tasks in progress window:', err);
      }
    })();
    // Listen for live task updates
    const unlistenTask = Events.On(Event.TaskUpdated, (ev: unknown) => {
      const updated = unwrapEventData<taskModels.Task>(ev);
      if (!updated || !updated.id) return;
      if (updated.status === taskModels.Status.StatusCompleted) {
        // Transition from active to completed
        setActiveTasks((prev) => {
          const next = prev.filter((t) => t.id !== updated.id);
          // If keepCompletedInfo is disabled and all active downloads have finished, auto-close window
          if (!keepCompletedInfo && next.length === 0) {
            setTimeout(() => {
              setCompletedTasks([]);
              void HideProgressWindow();
            }, 350);
          }
          return next;
        });

        if (keepCompletedInfo) {
          setCompletedTasks((prev) => [updated, ...prev.filter((t) => t.id !== updated.id)]);
        }
      } else {
        // Active status update: remove from completedTasks if present, update activeTasks
        setCompletedTasks((prev) => prev.filter((t) => t.id !== updated.id));
        setActiveTasks((prev) => {
          const idx = prev.findIndex((t) => t.id === updated.id);
          let next: taskModels.Task[];
          if (idx !== -1) {
            next = [...prev];
            next[idx] = updated;
          } else {
            next = [updated, ...prev];
          }
          return sortActiveTasks(next);
        });
      }
    });

    // Append task on focus without re-fetching or replacing existing items
    const handleAppendTask = (id: string) => {
      setFocusedTaskId(id);
      void (async () => {
        try {
          const list = await ListTasks();
          const target = (list || []).find((t) => t?.id === id);
          if (!target) return;

          if (target.status === taskModels.Status.StatusCompleted) {
            // Manual view of completed task: append to completedTasks AND remove from activeTasks
            setActiveTasks((prev) => prev.filter((t) => t.id !== id));
            setCompletedTasks((prev) => {
              if (prev.some((t) => t.id === id)) {
                return prev;
              }
              return [target, ...prev];
            });
          } else {
            // Active task: append to activeTasks AND remove from completedTasks
            setCompletedTasks((prev) => prev.filter((t) => t.id !== id));
            setActiveTasks((prev) => {
              if (prev.some((t) => t.id === id)) {
                return prev;
              }
              return sortActiveTasks([target, ...prev]);
            });
          }
        } catch (err) {
          console.error('Failed to append focused task:', err);
        }
      })();
    };

    const unlistenFocus = Events.On(Event.ProgressFocusCompleted, (ev: unknown) => {
      const id = unwrapEventData<string>(ev);
      if (id) {
        handleAppendTask(id);
      }
    });

    const unlistenFocusTask = Events.On(Event.ProgressFocusTask, (ev: unknown) => {
      const id = unwrapEventData<string>(ev);
      if (id) {
        handleAppendTask(id);
      }
    });

    // Clear manually viewed tasks on window close or new download initiation
    const unlistenClear = Events.On(Event.ProgressClearViewed, () => {
      setCompletedTasks([]);
    });

    // Listen for task deletion to keep progress window clean
    const unlistenDeleted = Events.On(Event.TaskDeleted, (ev: unknown) => {
      const deletedId = unwrapEventData<string>(ev);
      if (!deletedId) return;
      setActiveTasks((prev) => prev.filter((t) => t.id !== deletedId));
      setCompletedTasks((prev) => prev.filter((t) => t.id !== deletedId));
    });

    return () => {
      ignore = true;
      unlistenTask();
      unlistenFocus();
      unlistenFocusTask();
      unlistenClear();
      unlistenDeleted();
    };
  }, [loadSettings, keepCompletedInfo]);

  const handleOpenFile = async (t: taskModels.Task) => {
    const fullPath = `${t.directory}/${t.filename}`.replace(/\\/g, '/');
    try {
      await OpenFile(fullPath);
      if (autoRemoveCompletedOnOpen) {
        setCompletedTasks((prev) => {
          const next = prev.filter((item) => item.id !== t.id);
          if (next.length === 0 && activeTasks.length === 0) {
            void HideProgressWindow();
          }
          return next;
        });
      }
    } catch (err: unknown) {
      const msg = err instanceof Error ? err.message : String(err);
      showToast(msg || '文件不存在或已被移动/删除', 'error', '无法打开文件');
    }
  };

  const handleOpenFolder = async (t: taskModels.Task) => {
    try {
      await OpenFolder(t.directory);
      if (autoRemoveCompletedOnOpen) {
        setCompletedTasks((prev) => {
          const next = prev.filter((item) => item.id !== t.id);
          if (next.length === 0 && activeTasks.length === 0) {
            void HideProgressWindow();
          }
          return next;
        });
      }
    } catch (err: unknown) {
      const msg = err instanceof Error ? err.message : String(err);
      showToast(msg || '文件夹不存在或无法访问', 'error', '无法打开文件夹');
    }
  };

  const handleDismissCompleted = (id: string) => {
    setCompletedTasks((prev) => {
      const next = prev.filter((t) => t.id !== id);
      if (next.length === 0 && activeTasks.length === 0) {
        void HideProgressWindow();
      }
      return next;
    });
  };

  const handleClearAllCompleted = () => {
    setCompletedTasks([]);
    if (activeTasks.length === 0) {
      void HideProgressWindow();
    }
  };
  const handlePause = async (id: string) => {
    try {
      await PauseTask(id);
    } catch (err) {
      console.error('Failed to pause task:', err);
    }
  };

  const handleResume = async (id: string) => {
    try {
      await ResumeTask(id);
    } catch (err) {
      console.error('Failed to resume task:', err);
    }
  };

  const handleRetry = async (t: taskModels.Task) => {
    try {
      if (isProcessingFailure(t)) {
        await RetryProcessingTask(t.id);
      } else {
        await RetryTask(t.id);
      }
    } catch (err) {
      console.error('Failed to retry task:', err);
    }
  };

  const sortedActive = useMemo(() => {
    const completedIds = new Set(completedTasks.map((c) => c.id));
    const nonCompleted = activeTasks.filter(
      (t) => t.status !== taskModels.Status.StatusCompleted && !completedIds.has(t.id),
    );
    return sortActiveTasks(nonCompleted);
  }, [activeTasks, completedTasks]);
  return (
    <div
      className="flex h-screen w-full flex-col overflow-hidden rounded-lg border border-[var(--border-subtle)] bg-[var(--bg-base)] font-sans text-[var(--text-primary)] shadow-2xl select-none [&::-webkit-scrollbar]:hidden"
      style={{ scrollbarWidth: 'none', msOverflowStyle: 'none' }}
    >
      {/* Frameless Draggable Header */}
      <header
        className="relative flex h-8 shrink-0 cursor-default items-center justify-between border-b border-[var(--border-subtle)] bg-[var(--bg-surface)] px-3 select-none"
        style={{ ['--wails-draggable' as string]: 'drag' }}
      >
        <div className="pointer-events-none flex items-center gap-1.5">
          <DownloadCloud className="h-3.5 w-3.5 text-[var(--accent)]" />
          <span className="text-[11px] font-semibold tracking-wide text-[var(--text-primary)]">
            下载进度
          </span>
          <span className="py-0.2 ml-1 rounded-full bg-[var(--bg-subtle)] px-1.5 text-[10px] font-medium text-[var(--text-muted)]">
            {sortedActive.length} 进行中
            {completedTasks.length > 0 && ` · ${completedTasks.length} 已完成`}
          </span>
        </div>

        <div
          className="flex items-center gap-1"
          style={{ ['--wails-draggable' as string]: 'no-drag' }}
        >
          <button
            type="button"
            onClick={() => void handleToggleAlwaysOnTop()}
            title={alwaysOnTop ? '取消置顶' : '窗口置顶'}
            aria-label={alwaysOnTop ? '取消置顶' : '窗口置顶'}
            className={`flex h-5 w-5 items-center justify-center rounded transition-colors ${
              alwaysOnTop
                ? 'bg-[var(--accent-muted)] text-[var(--accent)]'
                : 'text-[var(--text-muted)] hover:bg-[var(--bg-subtle)] hover:text-[var(--text-primary)]'
            }`}
          >
            <Pin className={`h-3 w-3 ${alwaysOnTop ? 'fill-current' : ''}`} />
          </button>
          <button
            type="button"
            onClick={() => void MinimiseProgressWindow()}
            title="最小化"
            className="flex h-5 w-5 items-center justify-center rounded text-[var(--text-muted)] transition-colors hover:bg-[var(--bg-subtle)] hover:text-[var(--text-primary)]"
          >
            <Minus className="h-3 w-3" />
          </button>
          <button
            type="button"
            onClick={() => {
              setCompletedTasks([]);
              void HideProgressWindow();
            }}
            title="关闭窗口 (后台继续下载)"
            className="flex h-5 w-5 items-center justify-center rounded text-[var(--text-muted)] transition-colors hover:bg-red-500/20 hover:text-red-400"
          >
            <X className="h-3 w-3" />
          </button>
        </div>
      </header>

      {/* Main Scrollable Area with contentRef wrapper for natural height observation */}
      <main className="flex-1 overflow-y-auto p-3">
        <div ref={contentRef} className="space-y-3">
          {/* Pinned Completed Area at Top */}
          {completedTasks.length > 0 && (
            <div className="space-y-2">
              <div className="flex items-center justify-between px-0.5 text-[11px] font-semibold text-emerald-600 dark:text-emerald-400">
                <div className="flex items-center gap-1.5">
                  <CheckCircle2 className="h-3.5 w-3.5" />
                  <span>已完成任务 ({completedTasks.length})</span>
                </div>
                {completedTasks.length > 1 && (
                  <button
                    type="button"
                    onClick={handleClearAllCompleted}
                    className="text-[10px] text-[var(--text-muted)] transition-colors hover:text-[var(--text-primary)]"
                  >
                    全部移除
                  </button>
                )}
              </div>

              <AnimatePresence mode="popLayout">
                {completedTasks.map((t) => {
                  const isFocused = focusedTaskId === t.id;
                  return (
                    <motion.div
                      key={t.id}
                      layout="position"
                      initial={{ opacity: 0, y: -8 }}
                      animate={{ opacity: 1, y: 0 }}
                      exit={{ opacity: 0, scale: 0.95 }}
                      transition={{
                        layout: { type: 'spring', stiffness: 350, damping: 28 },
                        opacity: { duration: 0.15 },
                      }}
                      className={`rounded-xl border p-2.5 text-xs shadow-xs transition-all ${
                        isFocused
                          ? 'border-emerald-500 bg-emerald-500/15 ring-1 ring-emerald-500'
                          : 'border-emerald-500/30 bg-emerald-500/10'
                      }`}
                    >
                      <div className="flex items-center justify-between gap-3">
                        <div className="min-w-0 flex-1 space-y-0.5">
                          <div
                            className="truncate text-xs font-semibold text-[var(--text-primary)]"
                            title={t.filename}
                          >
                            {t.filename}
                          </div>
                          <div className="flex items-center gap-2 truncate text-[11px] text-[var(--text-secondary)]">
                            <span className="font-mono">
                              {formatBytes(t.totalBytes || t.downloaded)}
                            </span>
                            <span>•</span>
                            <span className="truncate font-mono" title={t.directory}>
                              {t.directory}
                            </span>
                          </div>
                        </div>

                        <div className="flex shrink-0 items-center gap-1.5">
                          <button
                            type="button"
                            onClick={() => void handleOpenFile(t)}
                            className="flex items-center gap-1.5 rounded-lg border border-[var(--border-subtle)] bg-[var(--bg-surface)] px-2.5 py-1 text-xs font-medium text-[var(--text-primary)] shadow-xs transition-colors hover:bg-[var(--bg-surface-hover)]"
                          >
                            <ExternalLink className="h-3 w-3 text-emerald-500" />
                            <span>打开文件</span>
                          </button>
                          <button
                            type="button"
                            onClick={() => void handleOpenFolder(t)}
                            className="flex items-center gap-1.5 rounded-lg border border-[var(--border-subtle)] bg-[var(--bg-surface)] px-2.5 py-1 text-xs font-medium text-[var(--text-primary)] shadow-xs transition-colors hover:bg-[var(--bg-surface-hover)]"
                          >
                            <Folder className="h-3 w-3 text-emerald-500" />
                            <span>打开文件夹</span>
                          </button>
                          <button
                            type="button"
                            onClick={() => handleDismissCompleted(t.id)}
                            title="从进度窗口移除"
                            className="rounded p-1 text-[var(--text-muted)] transition-colors hover:bg-black/10 hover:text-[var(--text-primary)] dark:hover:bg-white/10"
                          >
                            <X className="h-3.5 w-3.5" />
                          </button>
                        </div>
                      </div>
                    </motion.div>
                  );
                })}
              </AnimatePresence>
            </div>
          )}

          {/* Active Downloading / Queue Tasks */}
          {sortedActive.length > 0 && (
            <div className="space-y-2">
              {completedTasks.length > 0 && (
                <div className="flex items-center gap-1.5 px-0.5 pt-1 text-[11px] font-semibold text-[var(--text-secondary)]">
                  <Layers className="h-3.5 w-3.5 text-[var(--accent)]" />
                  <span>正在下载 / 队列中 ({sortedActive.length})</span>
                </div>
              )}

              <LayoutGroup id="active-tasks-list">
                <AnimatePresence mode="popLayout">
                  {sortedActive.map((t) => {
                    const percent =
                      t.totalBytes > 0
                        ? Math.min(100, Math.round((t.downloaded / t.totalBytes) * 100))
                        : 0;
                    const isUnknownSize = t.totalBytes <= 0;
                    const isProcessingError = isProcessingFailure(t);
                    const isFocused = focusedTaskId === t.id;

                    return (
                      <motion.div
                        key={t.id}
                        layout
                        initial={{ opacity: 0, y: 10, scale: 0.98 }}
                        animate={{ opacity: 1, y: 0, scale: 1 }}
                        exit={{ opacity: 0, scale: 0.95 }}
                        transition={{
                          layout: { type: 'spring', stiffness: 260, damping: 26 },
                          opacity: { duration: 0.2 },
                        }}
                        className={`rounded-xl border border-[var(--border-subtle)] bg-[var(--bg-surface)] p-2.5 shadow-xs transition-colors hover:border-[var(--border-hover)] ${
                          isFocused ? 'ring-1 ring-[var(--accent)]' : ''
                        }`}
                      >
                        {/* Top Row: Filename + Status Badge + Actions */}
                        <div className="flex items-center justify-between gap-2">
                          {/* min-w-0 让这一列可以收缩：长文件名在这里截断省略，
                              而不是把行撑爆、把状态徽章挤到逐字换行。 */}
                          <div className="flex min-w-0 flex-1 items-center gap-2">
                            <span
                              className="min-w-0 flex-1 truncate text-xs font-semibold text-[var(--text-primary)]"
                              title={t.filename}
                            >
                              {t.filename}
                            </span>
                            {/* Status Badge */}
                            {t.status === taskModels.Status.StatusDownloading && (
                              <span className="py-0.2 inline-flex shrink-0 items-center gap-1 rounded-full border border-[var(--border-focus)] bg-[var(--accent-muted)] px-1.5 text-[9px] font-medium text-[var(--accent)]">
                                <span className="h-1 w-1 animate-pulse rounded-full bg-[var(--accent)]" />
                                下载中
                              </span>
                            )}
                            {t.status === taskModels.Status.StatusProcessing && (
                              <span className="py-0.2 inline-flex shrink-0 items-center gap-1 rounded-full border border-cyan-500/30 bg-cyan-500/15 px-1.5 text-[9px] font-medium text-cyan-400">
                                <Loader2 className="h-2 w-2 animate-spin" />
                                处理中
                              </span>
                            )}
                            {t.status === taskModels.Status.StatusQueued && (
                              <span className="py-0.2 inline-flex shrink-0 items-center gap-1 rounded-full border border-amber-500/30 bg-amber-500/15 px-1.5 text-[9px] font-medium text-amber-400">
                                <Clock className="h-2 w-2" />
                                排队中
                              </span>
                            )}
                            {t.status === taskModels.Status.StatusPaused && (
                              <span className="py-0.2 inline-flex shrink-0 items-center gap-1 rounded-full border border-[var(--border-subtle)] bg-[var(--bg-subtle)] px-1.5 text-[9px] font-medium text-[var(--text-muted)]">
                                <Pause className="h-2 w-2" />
                                已暂停
                              </span>
                            )}
                            {t.status === taskModels.Status.StatusError && (
                              <span className="py-0.2 inline-flex shrink-0 items-center gap-1 rounded-full border border-rose-500/30 bg-rose-500/15 px-1.5 text-[9px] font-medium text-rose-400">
                                <AlertCircle className="h-2 w-2" />
                                {isProcessingError ? '处理失败' : '下载失败'}
                              </span>
                            )}
                          </div>

                          {/* Control Buttons */}
                          <div className="flex shrink-0 items-center gap-1">
                            {t.status === taskModels.Status.StatusDownloading && (
                              <button
                                type="button"
                                onClick={() => void handlePause(t.id)}
                                className="rounded-md p-1 text-[var(--text-muted)] transition-colors hover:bg-amber-500/10 hover:text-amber-500"
                                title="暂停下载"
                              >
                                <Pause className="h-3.5 w-3.5" />
                              </button>
                            )}
                            {(t.status === taskModels.Status.StatusPaused ||
                              t.status === taskModels.Status.StatusQueued) && (
                              <button
                                type="button"
                                onClick={() => void handleResume(t.id)}
                                className="rounded-md p-1 text-[var(--text-muted)] transition-colors hover:bg-[var(--accent-muted)] hover:text-[var(--accent)]"
                                title="继续下载"
                              >
                                <Play className="h-3.5 w-3.5" />
                              </button>
                            )}
                            {t.status === taskModels.Status.StatusError && (
                              <button
                                type="button"
                                onClick={() => void handleRetry(t)}
                                className="flex items-center gap-1 rounded-md px-1.5 py-0.5 text-[10px] font-medium text-sky-400 transition-colors hover:bg-sky-500/10 hover:text-sky-300"
                                title={isProcessingError ? '仅重试媒体处理' : '重试下载'}
                              >
                                <RotateCcw className="h-3 w-3" />
                                <span>{isProcessingError ? '重试处理' : '重试'}</span>
                              </button>
                            )}
                          </div>
                        </div>

                        {/* Seamless Multi-Chunk Progress Bar with In-place Split Dividers */}
                        <div className="mt-1.5 space-y-1">
                          <div className="relative h-1.5 w-full overflow-hidden rounded-full bg-[var(--bg-subtle)] p-[0.5px]">
                            {t.segmentDone && t.segmentDone.length > 1 ? (
                              /* HLS 分段格子条：一格一分片（长清单按组聚合），完成一格亮一格。
                                 和普通 HTTP 的多线程分段同样的视觉——多路并发抓分片要看得见，
                                 不能是一根从头到尾的实心条。放最前：HLS 多数大小未知，
                                 落到后面的 unknown-size 分支就只剩 indeterminate 动画了。 */
                              (() => {
                                const cells = aggregateSegmentCells(t.segmentDone);
                                const isSegmentError = t.status === taskModels.Status.StatusError;
                                return (
                                  <div className="absolute inset-0 flex items-stretch gap-[1px]">
                                    {cells.map((ratio, i) => (
                                      <div
                                        key={i}
                                        className="relative h-full min-w-0 flex-1"
                                        title={`分片 ${i + 1}/${cells.length}${ratio < 1 && ratio > 0 ? `（${Math.round(ratio * 100)}%）` : ''}`}
                                      >
                                        {ratio > 0 && (
                                          <div
                                            className={`absolute inset-y-0 left-0 rounded-[1px] ${
                                              isSegmentError ? 'bg-rose-500' : 'bg-[var(--accent)]'
                                            }`}
                                            style={{ width: `${ratio * 100}%` }}
                                          />
                                        )}
                                      </div>
                                    ))}
                                  </div>
                                );
                              })()
                            ) : isUnknownSize &&
                              t.status === taskModels.Status.StatusDownloading ? (
                              <div className="animate-indeterminate h-full w-2/5 rounded-full bg-[var(--accent)]" />
                            ) : t.chunks && t.chunks.length > 1 ? (
                              (() => {
                                const chunkItems = calculateChunkProgress(t.chunks, t.totalBytes);
                                const dividers = calculateChunkDividers(t.chunks, t.totalBytes);
                                return (
                                  <>
                                    {/* Active / Completed Chunk Fills */}
                                    {chunkItems.map((item) => {
                                      if (item.downloadedPercent <= 0) return null;
                                      return (
                                        <div
                                          key={item.id}
                                          className={`absolute top-0 bottom-0 transition-[width] duration-200 ease-linear ${
                                            t.status === taskModels.Status.StatusError
                                              ? 'bg-rose-500'
                                              : item.completed
                                                ? 'bg-[var(--accent)]'
                                                : 'bg-[var(--accent)]/90'
                                          }`}
                                          style={{
                                            left: `${item.startPercent}%`,
                                            width: `${item.downloadedPercent}%`,
                                          }}
                                          title={`通道 ${item.index + 1}${
                                            item.assisted ? ' (动态协助)' : ''
                                          }: ${formatBytes(item.downloaded)} / ${formatBytes(
                                            item.chunkSize,
                                          )} (${Math.round(
                                            (item.downloaded / item.chunkSize) * 100,
                                          )}%)`}
                                        />
                                      );
                                    })}

                                    {/* In-place Spatial Chunk Dividers */}
                                    {dividers.map((div) => (
                                      <div
                                        key={div.id}
                                        className={`pointer-events-none absolute top-0 bottom-0 z-10 w-[1px] ${
                                          div.assisted
                                            ? 'bg-amber-400/80 shadow-[0_0_2px_rgba(251,191,36,0.8)]'
                                            : 'bg-[var(--bg-surface)]/80'
                                        }`}
                                        style={{ left: `${div.positionPercent}%` }}
                                        title={div.assisted ? '慢块就地协助切分点' : '分块边界'}
                                      />
                                    ))}
                                  </>
                                );
                              })()
                            ) : (
                              <div
                                className={`h-full rounded-full transition-[width] duration-200 ease-linear ${
                                  t.status === taskModels.Status.StatusError
                                    ? 'bg-rose-500'
                                    : 'bg-[var(--accent)]'
                                }`}
                                style={{ width: `${percent}%` }}
                              />
                            )}
                          </div>

                          {/* Detail Metrics Row */}
                          <div className="flex items-center justify-between font-mono text-[10px] text-[var(--text-muted)]">
                            <div className="flex items-center gap-2">
                              <span>{formatBytes(t.downloaded)}</span>
                              <span>/</span>
                              <span>{isUnknownSize ? '未知大小' : formatBytes(t.totalBytes)}</span>
                              {/* HLS 分片进度：分片是多路并发抓取的，把计数亮出来，
                                  「正在并发下载」对用户才是可见的，而不是看起来像单线程流。 */}
                              {(t.segmentsTotal ?? 0) > 0 && (
                                <>
                                  <span>•</span>
                                  <span className="font-semibold text-[var(--text-secondary)]">
                                    分片 {t.segmentsDone ?? 0}/{t.segmentsTotal}
                                  </span>
                                </>
                              )}
                              {t.speed > 0 && t.status === taskModels.Status.StatusDownloading && (
                                <>
                                  <span>•</span>
                                  <span className="font-semibold text-[var(--text-secondary)]">
                                    {formatSpeed(t.speed)}
                                  </span>
                                </>
                              )}
                            </div>
                            <div>{isUnknownSize ? '未知大小' : `${percent}%`}</div>
                          </div>

                          {/* Error message if error */}
                          {t.status === taskModels.Status.StatusError && t.errorMsg && (
                            <div className="rounded bg-rose-500/10 px-2 py-1 text-[10px] text-rose-400">
                              {t.errorMsg}
                            </div>
                          )}
                        </div>
                      </motion.div>
                    );
                  })}
                </AnimatePresence>
              </LayoutGroup>
            </div>
          )}

          {/* Empty State: compact ~100px so window height is exactly 160px */}
          {completedTasks.length === 0 && sortedActive.length === 0 && (
            <div className="flex h-[100px] flex-col items-center justify-center space-y-1 text-center text-xs text-[var(--text-muted)]">
              <div className="flex h-8 w-8 items-center justify-center rounded-lg bg-[var(--bg-subtle)] text-[var(--text-muted)]">
                <DownloadCloud className="h-4 w-4" />
              </div>
              <p className="font-medium text-[var(--text-secondary)]">当前没有下载任务</p>
              <p className="text-[10px] text-[var(--text-muted)]">
                新建下载或从主列表继续后将在此实时展示
              </p>
            </div>
          )}
        </div>
      </main>

      <ToastContainer />
    </div>
  );
}
