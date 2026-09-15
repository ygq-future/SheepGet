import { useState, useEffect } from 'react';
import { AnimatePresence, motion } from 'motion/react';
import * as task from '../bindings/sheep-get/internal/task/models';
import {
  ListTasks,
  PauseTask,
  ResumeTask,
  RetryTask,
  DeleteTask,
  OpenFile,
  OpenFolder,
  OpenNewDownload,
  TriggerDownload,
} from '../bindings/sheep-get/app';
import { Clipboard, Events } from '@wailsio/runtime';
import { unwrapEventData } from './lib/utils';
import { DownloadRequest } from '../bindings/sheep-get/internal/window/models';
import { TaskItem } from './components/TaskItem';
import { UpdateLinkModal } from './components/UpdateLinkModal';
import { DeleteConfirmModal } from './components/DeleteConfirmModal';
import {
  Plus,
  DownloadCloud,
  Layers,
  CheckCircle2,
  PauseCircle,
  AlertCircle,
  Inbox,
  Settings as SettingsIcon,
} from 'lucide-react';
import { useSettingsStore, initSettingsListener } from './stores/settings';
import { SettingsPanel } from './components/SettingsPanel';
import { ToastContainer, showToast } from './components/ui/Toast';
function nonNullTasks(list: (task.Task | null)[] | null | undefined): task.Task[] {
  return (list || []).filter((t): t is task.Task => t !== null);
}

function sortTasks(taskList: task.Task[]): task.Task[] {
  return [...taskList].sort((a, b) => {
    const timeA = new Date(a.createdAt || 0).getTime();
    const timeB = new Date(b.createdAt || 0).getTime();
    if (timeA !== timeB) {
      return timeB - timeA;
    }
    return b.id.localeCompare(a.id);
  });
}

export function App() {
  const [tasks, setTasks] = useState<task.Task[]>([]);
  const [filter, setFilter] = useState<'all' | 'downloading' | 'completed' | 'settings'>('all');
  const [deletingTask, setDeletingTask] = useState<task.Task | null>(null);
  const [updatingLinkTask, setUpdatingLinkTask] = useState<task.Task | null>(null);
  const [sidebarCollapsed, setSidebarCollapsed] = useState<boolean>(() => {
    try {
      return localStorage.getItem('sheep_sidebar_collapsed') === 'true';
    } catch {
      return false;
    }
  });

  useEffect(() => {
    try {
      localStorage.setItem('sheep_sidebar_collapsed', String(sidebarCollapsed));
    } catch {
      // ignore
    }
  }, [sidebarCollapsed]);

  const { loadSettings } = useSettingsStore();
  const refreshTasks = async () => {
    try {
      const list = await ListTasks();
      setTasks(sortTasks(nonNullTasks(list)));
    } catch (err) {
      console.error('Failed to load tasks:', err);
    }
  };

  useEffect(() => {
    void loadSettings();
    const unlistenSettings = initSettingsListener();
    let ignore = false;
    void (async () => {
      try {
        const list = await ListTasks();
        if (!ignore) setTasks(sortTasks(nonNullTasks(list)));
      } catch (err) {
        console.error('Failed to load tasks:', err);
      }
    })();

    const onUpdated = (event: unknown) => {
      const updated = unwrapEventData<task.Task>(event);
      if (!updated || !updated.id) return;
      setTasks((prev) => {
        const idx = prev.findIndex((t) => t.id === updated.id);
        let next: task.Task[];
        if (idx !== -1) {
          next = [...prev];
          next[idx] = updated;
        } else {
          next = [updated, ...prev];
        }
        return sortTasks(next);
      });
    };

    const unsubscribe = Events.On('task:updated', onUpdated);
    return () => {
      ignore = true;
      unsubscribe();
      unlistenSettings();
    };
  }, [loadSettings]);

  const handlePause = (id: string) => {
    void (async () => {
      await PauseTask(id);
      const list = await ListTasks();
      setTasks(sortTasks(nonNullTasks(list)));
    })();
  };

  const handleResume = (id: string) => {
    void (async () => {
      await ResumeTask(id);
      const list = await ListTasks();
      setTasks(sortTasks(nonNullTasks(list)));
    })();
  };

  const handleRetry = (id: string) => {
    void (async () => {
      await RetryTask(id);
      const list = await ListTasks();
      setTasks(sortTasks(nonNullTasks(list)));
    })();
  };

  const handleDeleteRequest = (id: string) => {
    const target = tasks.find((t) => t.id === id);
    if (target) {
      setDeletingTask(target);
    }
  };

  const handleConfirmDelete = (deleteDiskFile: boolean) => {
    if (!deletingTask) return;
    const targetId = deletingTask.id;
    void (async () => {
      await DeleteTask(targetId, deleteDiskFile);
      setTasks((prev) => prev.filter((t) => t.id !== targetId));
      setDeletingTask(null);
    })();
  };

  const handleOpenFile = (filePath: string) => {
    void (async () => {
      try {
        await OpenFile(filePath);
      } catch (err: unknown) {
        const msg = err instanceof Error ? err.message : String(err);
        showToast(msg || '文件不存在或已被移动/删除', 'error', '无法打开文件');
      }
    })();
  };

  const handleOpenFolder = (folderPath: string) => {
    void (async () => {
      await OpenFolder(folderPath);
    })();
  };

  const handleNewDownload = async () => {
    try {
      let clipText = '';
      try {
        clipText = await Clipboard.Text();
      } catch {
        clipText = await navigator.clipboard.readText().catch(() => '');
      }

      const trimmed = (clipText || '').trim();
      if (trimmed.startsWith('http://') || trimmed.startsWith('https://')) {
        try {
          new URL(trimmed);
          await TriggerDownload(new DownloadRequest({ url: trimmed }));
          return;
        } catch {
          // Not a valid URL, fall through
        }
      }
    } catch (err) {
      console.error('Failed to read clipboard for new download:', err);
    }
    await OpenNewDownload();
  };

  const filteredTasks = tasks.filter((t) => {
    if (filter === 'downloading')
      return t.status === task.Status.StatusDownloading || t.status === task.Status.StatusQueued;
    if (filter === 'completed') return t.status === task.Status.StatusCompleted;
    return true;
  });

  const counts = {
    total: tasks.length,
    downloading: tasks.filter(
      (t) => t.status === task.Status.StatusDownloading || t.status === task.Status.StatusQueued,
    ).length,
    paused: tasks.filter((t) => t.status === task.Status.StatusPaused).length,
    completed: tasks.filter((t) => t.status === task.Status.StatusCompleted).length,
    error: tasks.filter((t) => t.status === task.Status.StatusError).length,
  };

  const navItems = [
    {
      id: 'all' as const,
      label: '全部任务',
      icon: Layers,
      count: counts.total,
      color: 'text-[var(--text-primary)]',
      badgeColor: 'bg-[var(--bg-subtle)] text-[var(--text-secondary)]',
    },
    {
      id: 'downloading' as const,
      label: '正在下载',
      icon: DownloadCloud,
      count: counts.downloading,
      color: 'text-sky-500',
      badgeColor: 'bg-sky-500/15 text-sky-600 dark:text-sky-400',
    },
    {
      id: 'completed' as const,
      label: '已完成',
      icon: CheckCircle2,
      count: counts.completed,
      color: 'text-[var(--accent)]',
      badgeColor: 'bg-[var(--accent-muted)] text-[var(--accent)]',
    },
  ];

  return (
    <div className="flex h-screen w-screen flex-col bg-[var(--bg-base)] font-sans text-[var(--text-primary)] antialiased select-none">
      {/* Top Refined Bar */}
      <header className="z-10 flex h-12 items-center justify-between border-b border-[var(--border-subtle)] bg-[var(--bg-surface)]/80 pr-5 backdrop-blur-xl">
        <div className="flex items-center">
          <div className="flex h-12 w-14 shrink-0 items-center justify-center">
            <button
              type="button"
              onClick={() => setSidebarCollapsed((prev) => !prev)}
              title={sidebarCollapsed ? '展开侧边栏' : '折叠侧边栏'}
              className="group flex h-7 w-7 cursor-pointer items-center justify-center rounded-lg border border-[var(--border-focus)] bg-[var(--accent-muted)] text-[var(--accent)] shadow-inner transition-transform hover:scale-105 active:scale-95"
            >
              <DownloadCloud className="h-4 w-4 transition-transform group-hover:rotate-6" />
            </button>
          </div>
          <div className="flex items-baseline gap-2 pl-1">
            <h1 className="text-xs font-semibold tracking-tight text-[var(--text-primary)]">
              SheepGet
            </h1>
            <span className="font-mono text-[10px] text-[var(--text-muted)]">v0.1.0</span>
          </div>
        </div>

        <button
          onClick={() => void handleNewDownload()}
          className="inline-flex cursor-pointer items-center gap-1.5 rounded-lg bg-[var(--accent)] px-3 py-1.5 text-xs font-semibold text-white shadow-md transition-all hover:opacity-90 active:scale-98"
        >
          <Plus className="h-3.5 w-3.5 stroke-[2.5]" />
          新建任务
        </button>
      </header>

      {/* Main Container */}
      <div className="flex flex-1 overflow-hidden">
        {/* Left Sidebar */}
        <aside
          className={`flex flex-col justify-between border-r border-[var(--border-subtle)] bg-[var(--bg-surface)]/50 p-2.5 transition-all duration-200 ease-in-out ${
            sidebarCollapsed ? 'w-14 items-center' : 'w-52'
          }`}
        >
          <div className="w-full space-y-1">
            {navItems.map((item) => {
              const isActive = filter === item.id;
              const Icon = item.icon;

              return (
                <button
                  key={item.id}
                  onClick={() => setFilter(item.id)}
                  title={item.label}
                  className={`group relative flex w-full items-center rounded-lg text-xs font-medium outline-hidden transition-all duration-150 select-none ${
                    sidebarCollapsed ? 'justify-center px-0 py-2' : 'justify-between px-2.5 py-1.5'
                  } ${
                    isActive
                      ? 'bg-[var(--bg-subtle)] shadow-xs'
                      : 'hover:bg-[var(--bg-surface-hover)]'
                  }`}
                >
                  <span
                    className={`flex items-center gap-2 transition-colors ${
                      isActive
                        ? item.color
                        : 'text-[var(--text-secondary)] group-hover:text-[var(--text-primary)]'
                    }`}
                  >
                    <Icon className="h-4 w-4 shrink-0" />
                    {!sidebarCollapsed && <span>{item.label}</span>}
                  </span>

                  {!sidebarCollapsed && (
                    <span
                      className={`py-0.2 rounded-full px-1.5 font-mono text-[10px] transition-colors ${
                        isActive
                          ? item.badgeColor
                          : 'bg-[var(--bg-subtle)] text-[var(--text-muted)]'
                      }`}
                    >
                      {item.count}
                    </span>
                  )}
                </button>
              );
            })}
          </div>

          <div className="w-full space-y-2">
            {/* Settings Tab Button */}
            <button
              onClick={() => setFilter('settings')}
              title="偏好设置"
              className={`group relative flex w-full items-center rounded-lg text-xs font-medium outline-hidden transition-all duration-150 select-none ${
                sidebarCollapsed ? 'justify-center px-0 py-2' : 'justify-between px-2.5 py-1.5'
              } ${
                filter === 'settings'
                  ? 'bg-[var(--bg-subtle)] text-[var(--text-primary)] shadow-xs'
                  : 'text-[var(--text-secondary)] hover:bg-[var(--bg-surface-hover)] hover:text-[var(--text-primary)]'
              }`}
            >
              <span
                className={`flex items-center gap-2 transition-colors ${
                  filter === 'settings'
                    ? 'text-[var(--text-primary)]'
                    : 'text-[var(--text-secondary)] group-hover:text-[var(--text-primary)]'
                }`}
              >
                <SettingsIcon className="h-4 w-4 shrink-0" />
                {!sidebarCollapsed && <span>偏好设置</span>}
              </span>
            </button>

            {sidebarCollapsed ? (
              <div className="flex w-full flex-col items-center gap-2 rounded-xl border border-[var(--border-subtle)] bg-[var(--bg-surface)] py-2 text-[10px]">
                <div
                  className="flex flex-col items-center gap-0.5"
                  title={`已暂停: ${counts.paused}`}
                >
                  <PauseCircle className="h-3.5 w-3.5 text-[var(--text-muted)]" />
                  <span className="font-mono font-medium text-[var(--text-primary)]">
                    {counts.paused}
                  </span>
                </div>
                <div className="h-px w-4 bg-[var(--border-subtle)]" />
                <div
                  className="flex flex-col items-center gap-0.5"
                  title={`异常状态: ${counts.error}`}
                >
                  <AlertCircle className="h-3.5 w-3.5 text-rose-500" />
                  <span className="font-mono font-medium text-rose-500">{counts.error}</span>
                </div>
              </div>
            ) : (
              <div className="space-y-2 rounded-xl border border-[var(--border-subtle)] bg-[var(--bg-surface)] p-2.5 text-[11px] text-[var(--text-secondary)]">
                <div className="flex items-center justify-between">
                  <span className="flex items-center gap-1.5 text-[var(--text-secondary)]">
                    <PauseCircle className="h-3 w-3 text-[var(--text-muted)]" /> 已暂停
                  </span>
                  <span className="font-mono font-medium text-[var(--text-primary)]">
                    {counts.paused}
                  </span>
                </div>
                <div className="flex items-center justify-between">
                  <span className="flex items-center gap-1.5 text-[var(--text-secondary)]">
                    <AlertCircle className="h-3 w-3 text-rose-500" /> 异常状态
                  </span>
                  <span className="font-mono font-medium text-rose-500">{counts.error}</span>
                </div>
              </div>
            )}
          </div>
        </aside>

        {/* Main Content Area */}
        <main className="flex min-w-0 flex-1 flex-col overflow-x-hidden overflow-y-auto bg-[var(--bg-base)] p-5">
          {filter === 'settings' ? (
            <SettingsPanel />
          ) : filteredTasks.length === 0 ? (
            <motion.div
              initial={{ opacity: 0, y: 10 }}
              animate={{ opacity: 1, y: 0 }}
              className="flex flex-1 flex-col items-center justify-center text-center select-none"
            >
              <div className="rounded-2xl border border-[var(--border-subtle)] bg-[var(--bg-surface)] p-5 text-[var(--text-muted)] shadow-xs">
                <Inbox className="h-8 w-8 stroke-1 text-[var(--text-muted)]" />
              </div>
              <p className="mt-3 text-xs font-medium text-[var(--text-primary)]">暂无下载任务</p>
              <p className="mt-1 text-[11px] text-[var(--text-muted)]">
                点击右上角“新建任务”开始下载
              </p>
            </motion.div>
          ) : (
            <div className="w-full min-w-0 space-y-2.5">
              <AnimatePresence mode="popLayout">
                {filteredTasks.map((t) => (
                  <TaskItem
                    key={t.id}
                    task={t}
                    onPause={handlePause}
                    onResume={handleResume}
                    onRetry={handleRetry}
                    onDelete={handleDeleteRequest}
                    onOpenFile={handleOpenFile}
                    onOpenFolder={handleOpenFolder}
                    onUpdateLink={(task) => setUpdatingLinkTask(task)}
                  />
                ))}
              </AnimatePresence>
            </div>
          )}
        </main>
      </div>

      <UpdateLinkModal
        key={updatingLinkTask?.id ?? 'none'}
        open={Boolean(updatingLinkTask)}
        onOpenChange={(open) => {
          if (!open) setUpdatingLinkTask(null);
        }}
        task={updatingLinkTask}
        onUpdated={() => {
          void refreshTasks();
        }}
      />

      <DeleteConfirmModal
        open={Boolean(deletingTask)}
        onOpenChange={(open) => {
          if (!open) setDeletingTask(null);
        }}
        filename={deletingTask?.filename || ''}
        onConfirm={handleConfirmDelete}
      />

      <ToastContainer />
    </div>
  );
}

export default App;
