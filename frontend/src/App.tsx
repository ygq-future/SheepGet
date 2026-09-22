import { useState, useEffect, useMemo, lazy, Suspense } from 'react';
import { AnimatePresence, motion } from 'motion/react';
import * as task from '../bindings/sheep-get/internal/task/models';
import {
  ListTasks,
  DeleteTask,
  OpenFile,
  OpenFolder,
  OpenNewDownload,
  TriggerDownload,
  ShowProgressWindow,
  GetServerStatus,
} from '../bindings/sheep-get/app';
import { Clipboard, Events } from '@wailsio/runtime';
import { unwrapEventData } from './lib/utils';
import { Event } from './lib/events';
import { DEFAULT_SERVER_PORT } from './lib/constants';
import { DownloadRequest } from '../bindings/sheep-get/internal/window/models';
import { TaskItem } from './components/TaskItem';
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
  Activity,
  Trash2,
  CheckCheck,
  X,
} from 'lucide-react';
import { useSettingsStore, initSettingsListener } from './stores/settings';
import { ToastContainer, showToast } from './components/ui/Toast';
import { Select } from './components/ui/Select';
import { matchTaskCategory } from './lib/category';
const SettingsPanel = lazy(() =>
  import('./components/SettingsPanel').then((m) => ({ default: m.SettingsPanel })),
);

function nonNullTasks(list: (task.Task | null)[] | null | undefined): task.Task[] {
  return (list || []).filter((t): t is task.Task => t !== null);
}

// 按「最后连接时间」倒序：续传或重试历史任务后它同样要浮到最前，
// 只按创建时间排序会让刚恢复的任务留在列表深处。列表右侧展示的也是这个时间。
// 老记录可能没有 updatedAt，退回创建时间；两者相同再按 id 稳定排序。
function sortTasks(taskList: task.Task[]): task.Task[] {
  return [...taskList].sort((a, b) => {
    const timeA = new Date(a.updatedAt || a.createdAt || 0).getTime();
    const timeB = new Date(b.updatedAt || b.createdAt || 0).getTime();
    if (timeA !== timeB) {
      return timeB - timeA;
    }
    return b.id.localeCompare(a.id);
  });
}

interface ServerStatusState {
  running: boolean;
  port: number;
  connectedCount: number;
  error?: string;
}

export function App() {
  const [tasks, setTasks] = useState<task.Task[]>([]);
  const [filter, setFilter] = useState<'all' | 'paused' | 'completed' | 'settings'>(() => {
    const params = new URLSearchParams(window.location.search);
    return params.get('open') === 'settings' ? 'settings' : 'all';
  });
  const [deletingTask, setDeletingTask] = useState<task.Task | null>(null);
  const [selectedCategory, setSelectedCategory] = useState<string>('');
  const [isBatchDeleting, setIsBatchDeleting] = useState<boolean>(false);
  const [selectedTaskIds, setSelectedTaskIds] = useState<Set<string>>(new Set());
  const [selectionAnchor, setSelectionAnchor] = useState<string | null>(null);
  const [baseSelectedIds, setBaseSelectedIds] = useState<Set<string>>(new Set());
  const [sidebarCollapsed, setSidebarCollapsed] = useState<boolean>(() => {
    try {
      return localStorage.getItem('sheep_sidebar_collapsed') === 'true';
    } catch {
      return false;
    }
  });

  const [serverStatus, setServerStatus] = useState<ServerStatusState>({
    running: false,
    port: DEFAULT_SERVER_PORT,
    connectedCount: 0,
  });

  useEffect(() => {
    void GetServerStatus().then((st) => {
      if (st) {
        setServerStatus({
          running: Boolean(st.running),
          port: st.port || DEFAULT_SERVER_PORT,
          connectedCount: st.connectedCount || 0,
          error: st.error,
        });
      }
    });

    const unsubscribeServer = Events.On(Event.ServerStatusChanged, (e) => {
      const data = unwrapEventData<ServerStatusState>(e.data);
      if (data) {
        setServerStatus({
          running: Boolean(data.running),
          port: data.port || DEFAULT_SERVER_PORT,
          connectedCount: data.connectedCount || 0,
          error: data.error,
        });
      }
    });

    return () => {
      unsubscribeServer();
    };
  }, []);
  useEffect(() => {
    try {
      localStorage.setItem('sheep_sidebar_collapsed', String(sidebarCollapsed));
    } catch {
      // ignore
    }
  }, [sidebarCollapsed]);

  const { settings, loadSettings } = useSettingsStore();

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

    const onDeleted = (event: unknown) => {
      const deletedId = unwrapEventData<string>(event);
      if (!deletedId) return;
      setTasks((prev) => prev.filter((t) => t.id !== deletedId));
      setSelectedTaskIds((prev) => {
        if (!prev.has(deletedId)) return prev;
        const next = new Set(prev);
        next.delete(deletedId);
        return next;
      });
    };

    const unsubscribeDeleted = Events.On(Event.TaskDeleted, onDeleted);

    const unsubscribe = Events.On(Event.TaskUpdated, onUpdated);
    const unsubscribeOpenSettings = Events.On(Event.AppOpenSettings, () => {
      setFilter('settings');
    });
    return () => {
      ignore = true;
      unsubscribe();
      unsubscribeDeleted();
      unsubscribeOpenSettings();
      unlistenSettings();
    };
  }, [loadSettings]);

  const categoryOptions = useMemo(() => {
    const opts: { value: string; label: string }[] = [];
    for (const c of settings?.download?.customCategories || []) {
      opts.push({ value: c.id, label: c.name });
    }
    for (const c of settings?.download?.builtinCategories || []) {
      opts.push({ value: c.id, label: c.name });
    }
    return opts;
  }, [settings?.download?.customCategories, settings?.download?.builtinCategories]);

  const filteredTasks = tasks.filter((t) => {
    if (filter === 'paused' && t.status !== task.Status.StatusPaused) return false;
    if (filter === 'completed' && t.status !== task.Status.StatusCompleted) return false;
    if (selectedCategory) {
      const catId = matchTaskCategory(t.filename, settings);
      if (catId !== selectedCategory) return false;
    }
    return true;
  });

  // Handle single task delete request
  const handleDeleteSingleRequest = (target: task.Task) => {
    setDeletingTask(target);
  };

  const handleConfirmSingleDelete = async (deleteDiskFile: boolean) => {
    if (!deletingTask) return;
    const targetId = deletingTask.id;
    try {
      await DeleteTask(targetId, deleteDiskFile);
      setTasks((prev) => prev.filter((t) => t.id !== targetId));
      setSelectedTaskIds((prev) => {
        const next = new Set(prev);
        next.delete(targetId);
        return next;
      });
      setBaseSelectedIds((prev) => {
        const next = new Set(prev);
        next.delete(targetId);
        return next;
      });
      setDeletingTask(null);
      showToast('任务已删除', 'success');
    } catch (err) {
      console.error('Failed to delete task:', err);
      showToast('删除任务失败', 'error');
    }
  };

  // Selection model: Exclusive Single Selection by default, Shift for range, Ctrl/Cmd for toggle
  const handleToggleSelect = (id: string, modifiers: { ctrlKey: boolean; shiftKey: boolean }) => {
    // 1. Shift key pressed: continuous range selection based on anchor
    if (modifiers.shiftKey) {
      const anchorId = selectionAnchor || id;
      const anchorIdx = filteredTasks.findIndex((t) => t.id === anchorId);
      const currIdx = filteredTasks.findIndex((t) => t.id === id);
      if (anchorIdx !== -1 && currIdx !== -1) {
        const start = Math.min(anchorIdx, currIdx);
        const end = Math.max(anchorIdx, currIdx);
        const rangeIds = new Set<string>();
        for (let i = start; i <= end; i++) {
          rangeIds.add(filteredTasks[i].id);
        }
        const next = new Set([...baseSelectedIds, ...rangeIds]);
        setSelectedTaskIds(next);
        if (!selectionAnchor) {
          setSelectionAnchor(id);
        }
        return;
      }
    }

    // 2. Ctrl / Cmd key pressed: non-continuous discrete toggle selection
    if (modifiers.ctrlKey) {
      const next = new Set(selectedTaskIds);
      if (next.has(id)) {
        next.delete(id);
      } else {
        next.add(id);
      }
      setSelectedTaskIds(next);
      setSelectionAnchor(id);
      setBaseSelectedIds(next);
      return;
    }

    // 3. Normal single click: mutually exclusive, selects only current item
    const next = new Set([id]);
    setSelectedTaskIds(next);
    setSelectionAnchor(id);
    setBaseSelectedIds(next);
  };

  const allFilteredSelected =
    filteredTasks.length > 0 && filteredTasks.every((t) => selectedTaskIds.has(t.id));

  const handleToggleSelectAll = () => {
    if (allFilteredSelected) {
      setSelectedTaskIds(new Set());
      setSelectionAnchor(null);
      setBaseSelectedIds(new Set());
    } else {
      const allIds = new Set(filteredTasks.map((t) => t.id));
      setSelectedTaskIds(allIds);
      setSelectionAnchor(filteredTasks[0]?.id || null);
      setBaseSelectedIds(allIds);
    }
  };

  const handleCancelSelection = () => {
    setSelectedTaskIds(new Set());
    setSelectionAnchor(null);
    setBaseSelectedIds(new Set());
  };

  // Progress window trigger button logic
  const handleProgressWindowClick = () => {
    if (selectedTaskIds.size > 0) {
      const ids = Array.from(selectedTaskIds);
      void ShowProgressWindow(ids[0]);
      ids.forEach((id) => {
        void Events.Emit(Event.ProgressFocusCompleted, id);
        void Events.Emit(Event.ProgressFocusTask, id);
      });
      return;
    }

    const hasActive = tasks.some(
      (t) =>
        t.status === task.Status.StatusDownloading ||
        t.status === task.Status.StatusProcessing ||
        t.status === task.Status.StatusQueued,
    );
    if (hasActive) {
      void ShowProgressWindow('');
      return;
    }

    showToast('当前没有正在进行的下载任务', 'info', '进度窗口');
  };

  // Batch delete logic
  const handleRequestBatchDelete = () => {
    if (selectedTaskIds.size === 0) return;
    setIsBatchDeleting(true);
  };

  const handleConfirmBatchDelete = async (deleteDiskFile: boolean) => {
    const idsToDelete = Array.from(selectedTaskIds);
    setIsBatchDeleting(false);
    try {
      await Promise.all(idsToDelete.map((id) => DeleteTask(id, deleteDiskFile)));
      setTasks((prev) => prev.filter((t) => !selectedTaskIds.has(t.id)));
      setSelectedTaskIds(new Set());
      setSelectionAnchor(null);
      setBaseSelectedIds(new Set());
      showToast(`已成功删除 ${idsToDelete.length} 个任务`, 'success');
    } catch (err) {
      console.error('Failed to batch delete tasks:', err);
      showToast('部分或全部任务删除失败', 'error');
    }
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

  const counts = {
    total: tasks.length,
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
    },
    {
      id: 'paused' as const,
      label: '已暂停',
      icon: PauseCircle,
      count: counts.paused,
      color: 'text-amber-500',
    },
    {
      id: 'completed' as const,
      label: '已完成',
      icon: CheckCircle2,
      count: counts.completed,
      color: 'text-[var(--accent)]',
    },
  ];
  const isServerErr = !serverStatus.running || Boolean(serverStatus.error);
  const isServerConnected = serverStatus.running && serverStatus.connectedCount > 0;
  const serverStatusColor = isServerErr
    ? 'bg-rose-500 shadow-[0_0_8px_rgba(244,63,94,0.6)]'
    : isServerConnected
      ? 'bg-emerald-400 shadow-[0_0_8px_rgba(52,211,153,0.7)]'
      : 'bg-amber-400 shadow-[0_0_8px_rgba(251,191,36,0.6)] animate-pulse';

  const serverStatusTitle = isServerErr
    ? serverStatus.error
      ? `HTTP 服务异常: ${serverStatus.error}`
      : `HTTP 服务未运行或端口被占用`
    : isServerConnected
      ? `HTTP 服务已连接 (端口 ${serverStatus.port})，浏览器扩展通信就绪`
      : `HTTP 服务监听中 (端口 ${serverStatus.port})，等待浏览器扩展连接`;

  return (
    <div className="flex h-screen w-screen overflow-hidden bg-[var(--bg-base)] font-sans text-[var(--text-primary)] antialiased select-none">
      {/* Left Full-Height Aside (Unified vertical border-r, Linear-inspired) */}
      <aside
        className={`flex h-screen shrink-0 flex-col justify-between border-r border-[var(--border-subtle)] bg-[var(--bg-app)]/80 backdrop-blur-xl transition-all duration-200 ${
          sidebarCollapsed ? 'w-14 items-center px-2 py-3' : 'w-44 p-3'
        }`}
      >
        {/* Aside Top: Brand Header & Categorized Navigation */}
        <div className="w-full space-y-4">
          {/* Brand Header */}
          <div
            className={`flex h-9 items-center ${
              sidebarCollapsed ? 'justify-center' : 'gap-2.5 px-1'
            }`}
          >
            <button
              type="button"
              onClick={() => setSidebarCollapsed(!sidebarCollapsed)}
              title={sidebarCollapsed ? '展开侧边栏' : '收起侧边栏'}
              className="flex h-8 w-8 shrink-0 cursor-pointer items-center justify-center text-[var(--accent)] transition-transform hover:scale-105 active:scale-95"
            >
              <DownloadCloud className="h-5 w-5" />
            </button>

            {!sidebarCollapsed && (
              <span className="text-sm font-semibold tracking-tight text-[var(--text-primary)]">
                SheepGet
              </span>
            )}
          </div>

          {/* Navigation Section */}
          <div className="space-y-1">
            {!sidebarCollapsed && (
              <div className="px-2 pb-1 text-[10px] font-semibold tracking-wider text-[var(--text-dim)] uppercase">
                任务列表
              </div>
            )}

            {navItems.map((item) => {
              const Icon = item.icon;
              const isActive = filter === item.id;

              return (
                <button
                  key={item.id}
                  onClick={() => setFilter(item.id)}
                  title={sidebarCollapsed ? `${item.label} (${item.count})` : undefined}
                  className={`group relative flex w-full cursor-pointer items-center rounded-lg text-xs font-medium outline-hidden transition-all duration-150 select-none ${
                    sidebarCollapsed ? 'justify-center px-0 py-2.5' : 'justify-between px-2.5 py-2'
                  } ${
                    isActive
                      ? 'bg-white/[0.07] text-[var(--text-primary)] shadow-xs ring-1 ring-white/5 dark:bg-white/[0.06]'
                      : 'text-[var(--text-secondary)] hover:bg-[var(--bg-surface-hover)] hover:text-[var(--text-primary)]'
                  }`}
                >
                  {/* Active Indicator Bar */}
                  {isActive && !sidebarCollapsed && (
                    <span className="absolute top-2 bottom-2 left-1 w-0.5 rounded-full bg-[var(--accent)] shadow-xs" />
                  )}

                  <span
                    className={`flex items-center gap-2.5 transition-colors ${
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
                      className={`rounded-full px-1.5 py-0.5 font-mono text-[10px] transition-colors ${
                        isActive
                          ? 'bg-[var(--accent-muted)] font-semibold text-[var(--accent)]'
                          : 'bg-[var(--bg-subtle)] text-[var(--text-muted)] group-hover:text-[var(--text-secondary)]'
                      }`}
                    >
                      {item.count}
                    </span>
                  )}
                </button>
              );
            })}
          </div>
        </div>

        {/* Aside Footer: Settings Tab Button & Error status */}
        <div className="w-full space-y-2 border-t border-[var(--border-subtle)] pt-2.5">
          <button
            onClick={() => setFilter('settings')}
            title="偏好设置"
            className={`group relative flex w-full cursor-pointer items-center rounded-lg text-xs font-medium outline-hidden transition-all duration-150 select-none ${
              sidebarCollapsed ? 'justify-center px-0 py-2.5' : 'justify-between px-2.5 py-2'
            } ${
              filter === 'settings'
                ? 'bg-white/[0.07] text-[var(--text-primary)] shadow-xs ring-1 ring-white/5 dark:bg-white/[0.06]'
                : 'text-[var(--text-secondary)] hover:bg-[var(--bg-surface-hover)] hover:text-[var(--text-primary)]'
            }`}
          >
            {filter === 'settings' && !sidebarCollapsed && (
              <span className="absolute top-2 bottom-2 left-1 w-0.5 rounded-full bg-[var(--accent)] shadow-xs" />
            )}

            <span
              className={`flex items-center gap-2.5 transition-colors ${
                filter === 'settings'
                  ? 'text-[var(--text-primary)]'
                  : 'text-[var(--text-secondary)] group-hover:text-[var(--text-primary)]'
              }`}
            >
              <SettingsIcon className="h-4 w-4 shrink-0" />
              {!sidebarCollapsed && <span>偏好设置</span>}
            </span>

            {sidebarCollapsed ? (
              <span
                className={`absolute top-2 right-2 h-2 w-2 rounded-full ${serverStatusColor}`}
                title={serverStatusTitle}
              />
            ) : (
              <div
                className="flex items-center gap-1.5 rounded-md px-1.5 py-0.5 transition-colors group-hover:bg-white/5"
                title={serverStatusTitle}
              >
                <span className={`h-2 w-2 shrink-0 rounded-full ${serverStatusColor}`} />
                <span className="font-mono text-[10px] text-[var(--text-muted)]">
                  {serverStatus.port}
                </span>
              </div>
            )}
          </button>

          {counts.error > 0 && (
            <div
              className={`rounded-lg border border-rose-500/20 bg-rose-500/10 p-2 text-rose-500 ${
                sidebarCollapsed
                  ? 'flex justify-center'
                  : 'flex items-center justify-between text-[11px]'
              }`}
              title={`异常状态: ${counts.error}`}
            >
              <span className="flex items-center gap-1.5">
                <AlertCircle className="h-3 w-3" />
                {!sidebarCollapsed && <span>异常状态</span>}
              </span>
              {!sidebarCollapsed && <span className="font-mono font-medium">{counts.error}</span>}
            </div>
          )}
        </div>
      </aside>

      {/* Right Main Workspace */}
      <div className="flex min-w-0 flex-1 flex-col overflow-hidden">
        {/* Right Header Bar: Aligns perfectly on top of right workspace */}
        <header className="flex h-12 w-full shrink-0 items-center justify-between border-b border-[var(--border-subtle)] bg-[var(--bg-app)]/40 px-4 backdrop-blur-md">
          {/* Header Left: Icon-based Select All & Icon-based Deselect */}
          <div className="flex items-center gap-2">
            {filter !== 'settings' && (
              <>
                <div className="w-28 sm:w-32">
                  <Select
                    value={selectedCategory}
                    onChange={(val) => {
                      setSelectedCategory(String(val));
                      setSelectedTaskIds(new Set());
                      setBaseSelectedIds(new Set());
                      setSelectionAnchor(null);
                    }}
                    clearable
                    placeholder="全部分类"
                    options={categoryOptions}
                    className="h-7 py-1 text-xs"
                  />
                </div>

                {filteredTasks.length > 0 && (
                  <div className="flex items-center gap-2">
                    <button
                      type="button"
                      onClick={handleToggleSelectAll}
                      title={allFilteredSelected ? '取消全选' : '全部选择'}
                      className={`flex h-7 w-7 cursor-pointer items-center justify-center rounded-lg border transition-all duration-150 ${
                        allFilteredSelected
                          ? 'border-[var(--accent)] bg-[var(--accent-muted)] text-[var(--accent)] shadow-xs'
                          : 'border-[var(--border-subtle)] bg-[var(--bg-surface)] text-[var(--text-muted)] hover:border-[var(--border-hover)] hover:text-[var(--text-primary)]'
                      }`}
                    >
                      <CheckCheck className="h-4 w-4" />
                    </button>

                    {selectedTaskIds.size > 0 && (
                      <div className="flex items-center gap-1.5 rounded-lg border border-[var(--border-subtle)] bg-[var(--bg-surface)] px-2 py-1 text-xs">
                        <span className="font-medium text-[var(--text-secondary)]">
                          已选择{' '}
                          <span className="font-mono font-semibold text-[var(--accent)]">
                            {selectedTaskIds.size}
                          </span>{' '}
                          项
                        </span>
                        <button
                          type="button"
                          onClick={handleCancelSelection}
                          title="取消选择"
                          className="flex h-4 w-4 cursor-pointer items-center justify-center rounded text-[var(--text-muted)] transition-colors hover:bg-white/10 hover:text-[var(--text-primary)]"
                        >
                          <X className="h-3 w-3" />
                        </button>
                      </div>
                    )}
                  </div>
                )}
              </>
            )}
          </div>

          {/* Header Right: Progress Window, Batch Delete, New Download */}
          <div className="flex items-center gap-2">
            {/* Shared Progress Window Button */}
            <button
              type="button"
              onClick={handleProgressWindowClick}
              className="inline-flex cursor-pointer items-center gap-1.5 rounded-lg border border-[var(--border-subtle)] bg-[var(--bg-surface)] px-2.5 py-1.5 text-xs font-medium text-[var(--text-secondary)] shadow-xs transition-colors hover:bg-[var(--bg-surface-hover)] hover:text-[var(--text-primary)] active:scale-98"
              title="打开下载进度窗口"
            >
              <Activity className="h-3.5 w-3.5 text-[var(--accent)]" />
              <span className="hidden sm:inline">下载进度</span>
            </button>

            {/* Batch Delete Button */}
            <button
              type="button"
              onClick={handleRequestBatchDelete}
              disabled={selectedTaskIds.size === 0}
              className={`inline-flex items-center gap-1.5 rounded-lg border px-2.5 py-1.5 text-xs font-medium shadow-xs transition-colors ${
                selectedTaskIds.size > 0
                  ? 'cursor-pointer border-rose-500/30 bg-rose-500/10 text-rose-500 hover:bg-rose-500 hover:text-white active:scale-98'
                  : 'cursor-not-allowed border-[var(--border-subtle)] bg-[var(--bg-surface)] text-[var(--text-muted)] opacity-50'
              }`}
              title={
                selectedTaskIds.size > 0
                  ? `批量删除已选择的 ${selectedTaskIds.size} 项`
                  : '删除任务（请先选择任务）'
              }
            >
              <Trash2 className="h-3.5 w-3.5" />
              <span className="hidden sm:inline">
                删除{selectedTaskIds.size > 0 ? ` (${selectedTaskIds.size})` : ''}
              </span>
            </button>

            {/* New Download */}
            <button
              onClick={() => void handleNewDownload()}
              className="inline-flex cursor-pointer items-center gap-1.5 rounded-lg bg-[var(--accent)] px-3 py-1.5 text-xs font-semibold text-white shadow-md transition-all hover:opacity-90 active:scale-98"
            >
              <Plus className="h-3.5 w-3.5 stroke-[2.5]" />
              新建任务
            </button>
          </div>
        </header>

        {/* Main Content Area */}
        <main className="flex min-w-0 flex-1 flex-col overflow-x-hidden overflow-y-auto bg-[var(--bg-base)] p-3">
          {filter === 'settings' ? (
            <Suspense fallback={null}>
              <SettingsPanel />
            </Suspense>
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
            <div className="w-full min-w-0 space-y-1">
              <AnimatePresence mode="popLayout">
                {filteredTasks.map((t) => (
                  <TaskItem
                    key={t.id}
                    task={t}
                    selected={selectedTaskIds.has(t.id)}
                    onToggleSelect={handleToggleSelect}
                    onDelete={handleDeleteSingleRequest}
                    onOpenFile={handleOpenFile}
                    onOpenFolder={handleOpenFolder}
                    onShowProgress={(id) => void ShowProgressWindow(id)}
                  />
                ))}
              </AnimatePresence>
            </div>
          )}
        </main>
      </div>

      {/* Single Delete Confirm */}
      <DeleteConfirmModal
        open={Boolean(deletingTask)}
        onOpenChange={(open) => {
          if (!open) setDeletingTask(null);
        }}
        filename={deletingTask?.filename || ''}
        count={1}
        onConfirm={(deleteDiskFile) => {
          void handleConfirmSingleDelete(deleteDiskFile);
        }}
      />

      {/* Batch Delete Confirm */}
      <DeleteConfirmModal
        open={isBatchDeleting}
        onOpenChange={(open) => {
          if (!open) setIsBatchDeleting(false);
        }}
        count={selectedTaskIds.size}
        onConfirm={(deleteDiskFile) => {
          void handleConfirmBatchDelete(deleteDiskFile);
        }}
      />

      <ToastContainer />
    </div>
  );
}

export default App;
