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
  GetDefaultDownloadDir,
} from '../bindings/sheep-get/app';
import { Events } from '@wailsio/runtime';
import { unwrapEventData } from './lib/utils';
import { TaskItem } from './components/TaskItem';
import { FileInfoModal } from './components/FileInfoModal';
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
import { ToastContainer } from './components/ui/Toast';

function nonNullTasks(list: (task.Task | null)[] | null | undefined): task.Task[] {
  return (list || []).filter((t): t is task.Task => t !== null);
}

export function App() {
  const [tasks, setTasks] = useState<task.Task[]>([]);
  const [filter, setFilter] = useState<'all' | 'downloading' | 'completed' | 'settings'>('all');
  const [modalOpen, setModalOpen] = useState(false);
  const [defaultDir, setDefaultDir] = useState('');
  const [deletingTask, setDeletingTask] = useState<task.Task | null>(null);
  const [updatingLinkTask, setUpdatingLinkTask] = useState<task.Task | null>(null);

  const { loadSettings } = useSettingsStore();
  const refreshTasks = async () => {
    try {
      const list = await ListTasks();
      setTasks(nonNullTasks(list));
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
        if (!ignore) setTasks(nonNullTasks(list));
      } catch (err) {
        console.error('Failed to load tasks:', err);
      }
    })();

    void (async () => {
      try {
        const dir = await GetDefaultDownloadDir();
        if (!ignore) setDefaultDir(dir);
      } catch (err) {
        console.error('Failed to get download dir:', err);
      }
    })();

    const onUpdated = (event: unknown) => {
      const updated = unwrapEventData<task.Task>(event);
      if (!updated || !updated.id) return;
      setTasks((prev) => {
        const idx = prev.findIndex((t) => t.id === updated.id);
        if (idx !== -1) {
          const next = [...prev];
          next[idx] = updated;
          return next;
        }
        return [updated, ...prev];
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
      setTasks(nonNullTasks(list));
    })();
  };

  const handleResume = (id: string) => {
    void (async () => {
      await ResumeTask(id);
      const list = await ListTasks();
      setTasks(nonNullTasks(list));
    })();
  };

  const handleRetry = (id: string) => {
    void (async () => {
      await RetryTask(id);
      const list = await ListTasks();
      setTasks(nonNullTasks(list));
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
      await OpenFile(filePath);
    })();
  };

  const handleOpenFolder = (folderPath: string) => {
    void (async () => {
      await OpenFolder(folderPath);
    })();
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
      <header className="z-10 flex h-12 items-center justify-between border-b border-[var(--border-subtle)] bg-[var(--bg-surface)]/80 px-5 backdrop-blur-xl">
        <div className="flex items-center gap-3">
          <div className="flex h-7 w-7 items-center justify-center rounded-lg border border-[var(--border-focus)] bg-[var(--accent-muted)] text-[var(--accent)] shadow-inner">
            <DownloadCloud className="h-4 w-4" />
          </div>
          <div className="flex items-baseline gap-2">
            <h1 className="text-xs font-semibold tracking-tight text-[var(--text-primary)]">
              SheepGet
            </h1>
            <span className="font-mono text-[10px] text-[var(--text-muted)]">v0.1.0</span>
          </div>
        </div>

        <button
          onClick={() => setModalOpen(true)}
          className="inline-flex items-center gap-1.5 rounded-lg bg-[var(--accent)] px-3 py-1.5 text-xs font-semibold text-white shadow-md transition-all hover:opacity-90 active:scale-98"
        >
          <Plus className="h-3.5 w-3.5 stroke-[2.5]" />
          新建任务
        </button>
      </header>

      {/* Main Container */}
      <div className="flex flex-1 overflow-hidden">
        {/* Left Sidebar with Smooth Active Pill Transition */}
        <aside className="flex w-52 flex-col justify-between border-r border-[var(--border-subtle)] bg-[var(--bg-surface)]/50 p-2.5">
          <div className="space-y-1">
            {navItems.map((item) => {
              const isActive = filter === item.id;
              const Icon = item.icon;

              return (
                <button
                  key={item.id}
                  onClick={() => setFilter(item.id)}
                  className="group relative flex w-full items-center justify-between rounded-lg px-2.5 py-1.5 text-xs font-medium outline-hidden transition-colors select-none"
                >
                  {/* Sliding Pill Indicator */}
                  {isActive && (
                    <motion.div
                      layoutId="active-pill"
                      className="absolute inset-0 rounded-lg bg-[var(--bg-subtle)] shadow-xs"
                      transition={{ type: 'spring', stiffness: 350, damping: 32 }}
                    />
                  )}

                  <span
                    className={`relative z-10 flex items-center gap-2 transition-colors ${
                      isActive
                        ? item.color
                        : 'text-[var(--text-secondary)] group-hover:text-[var(--text-primary)]'
                    }`}
                  >
                    <Icon className="h-3.5 w-3.5 shrink-0" />
                    <span>{item.label}</span>
                  </span>

                  <span
                    className={`py-0.2 relative z-10 rounded-full px-1.5 font-mono text-[10px] transition-colors ${
                      isActive ? item.badgeColor : 'bg-[var(--bg-subtle)] text-[var(--text-muted)]'
                    }`}
                  >
                    {item.count}
                  </span>
                </button>
              );
            })}
          </div>

          <div className="space-y-2">
            {/* Settings Tab Button */}
            <button
              onClick={() => setFilter('settings')}
              className="group relative flex w-full items-center justify-between rounded-lg px-2.5 py-1.5 text-xs font-medium outline-hidden transition-colors select-none"
            >
              {filter === 'settings' && (
                <motion.div
                  layoutId="active-pill"
                  className="absolute inset-0 rounded-lg bg-[var(--bg-subtle)] shadow-xs"
                  transition={{ type: 'spring', stiffness: 350, damping: 32 }}
                />
              )}
              <span
                className={`relative z-10 flex items-center gap-2 transition-colors ${
                  filter === 'settings'
                    ? 'text-[var(--text-primary)]'
                    : 'text-[var(--text-secondary)] group-hover:text-[var(--text-primary)]'
                }`}
              >
                <SettingsIcon className="h-3.5 w-3.5 shrink-0" />
                <span>偏好设置</span>
              </span>
            </button>

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
          </div>
        </aside>

        {/* Main Content Area */}
        <main className="flex-1 overflow-y-auto bg-[var(--bg-base)] p-5">
          {filter === 'settings' ? (
            <SettingsPanel />
          ) : filteredTasks.length === 0 ? (
            <motion.div
              initial={{ opacity: 0, y: 10 }}
              animate={{ opacity: 1, y: 0 }}
              className="flex h-full flex-col items-center justify-center text-center"
            >
              <div className="rounded-2xl border border-white/[0.06] bg-zinc-900/80 p-5 text-zinc-600 shadow-inner">
                <Inbox className="h-8 w-8 stroke-1 text-zinc-500" />
              </div>
              <p className="mt-3 text-xs font-medium text-zinc-300">暂无下载任务</p>
              <p className="mt-1 text-[11px] text-zinc-500">点击右上角“新建任务”开始下载</p>
            </motion.div>
          ) : (
            <div className="mx-auto max-w-4xl space-y-2.5">
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

      <FileInfoModal
        open={modalOpen}
        onOpenChange={setModalOpen}
        defaultDir={defaultDir}
        onTasksChanged={() => {
          void refreshTasks();
        }}
      />

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
