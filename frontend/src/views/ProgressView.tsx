import { useEffect, useState } from 'react';
import { Events } from '@wailsio/runtime';
import { unwrapEventData } from '../lib/utils';
import { DownloadCloud, CheckCircle2, Folder, ExternalLink, X } from 'lucide-react';
import { useSettingsStore } from '../stores/settings';
import { ListTasks, OpenFile, OpenFolder } from '../../bindings/sheep-get/app';
import { ToastContainer, showToast } from '../components/ui/Toast';
import type * as taskModels from '../../bindings/sheep-get/internal/task/models';
import { formatBytes } from '../lib/format';

export function ProgressView() {
  const { loadSettings } = useSettingsStore();
  const [completedTaskId, setCompletedTaskId] = useState<string | null>(() => {
    const params = new URLSearchParams(window.location.search);
    return params.get('focus');
  });
  const [completedTask, setCompletedTask] = useState<taskModels.Task | null>(null);

  useEffect(() => {
    void loadSettings();

    const fetchTask = async (id: string) => {
      try {
        const tasks = await ListTasks();
        const found = (tasks || []).find((t) => t?.id === id);
        if (found) {
          setCompletedTask(found);
        }
      } catch (err) {
        console.error('Failed to load completed task:', err);
      }
    };

    if (completedTaskId) {
      void fetchTask(completedTaskId);
    }

    const unlisten = Events.On('progress:focus_completed', (ev: unknown) => {
      const id = unwrapEventData<string>(ev);
      if (id) {
        setCompletedTaskId(id);
        void fetchTask(id);
      }
    });

    return () => {
      unlisten();
    };
  }, [loadSettings, completedTaskId]);

  const handleOpenFile = () => {
    if (completedTask) {
      const fullPath = `${completedTask.directory}/${completedTask.filename}`.replace(/\\/g, '/');
      void (async () => {
        try {
          await OpenFile(fullPath);
        } catch (err: unknown) {
          const msg = err instanceof Error ? err.message : String(err);
          showToast(msg || '文件不存在或已被移动/删除', 'error', '无法打开文件');
        }
      })();
    }
  };

  const handleOpenFolder = () => {
    if (completedTask) {
      void (async () => {
        try {
          await OpenFolder(completedTask.directory);
        } catch (err: unknown) {
          const msg = err instanceof Error ? err.message : String(err);
          showToast(msg || '文件夹不存在或无法访问', 'error', '无法打开文件夹');
        }
      })();
    }
  };

  return (
    <div className="flex min-h-screen flex-col bg-[var(--bg-app)] font-sans text-[var(--text-primary)] select-none">
      <header className="flex h-12 shrink-0 items-center justify-between border-b border-[var(--border-subtle)] px-4">
        <div className="flex items-center gap-2">
          <div className="flex h-6 w-6 items-center justify-center rounded-lg bg-[var(--accent-muted)] text-[var(--accent)]">
            <DownloadCloud className="h-3.5 w-3.5" />
          </div>
          <h1 className="text-xs font-semibold tracking-wide">下载进度</h1>
        </div>
      </header>

      <main className="flex-1 space-y-3 overflow-y-auto p-4">
        {/* Completed Info Area pinned at top */}
        {completedTask && (
          <div className="rounded-xl border border-emerald-500/30 bg-emerald-500/10 p-3 text-xs text-[var(--text-primary)]">
            <div className="flex items-center justify-between">
              <div className="flex items-center gap-2 font-medium text-emerald-600 dark:text-emerald-400">
                <CheckCircle2 className="h-4 w-4 shrink-0" />
                <span>已完成任务</span>
              </div>
              <button
                type="button"
                onClick={() => {
                  setCompletedTask(null);
                  setCompletedTaskId(null);
                }}
                className="rounded p-1 text-[var(--text-muted)] hover:text-[var(--text-primary)]"
              >
                <X className="h-3.5 w-3.5" />
              </button>
            </div>

            <div className="mt-2 space-y-1">
              <div className="truncate font-semibold text-[var(--text-primary)]">
                {completedTask.filename}
              </div>
              <div className="flex items-center gap-2 text-[11px] text-[var(--text-secondary)]">
                <span>{formatBytes(completedTask.totalBytes)}</span>
                <span>•</span>
                <span className="truncate">{completedTask.directory}</span>
              </div>
            </div>

            <div className="mt-3 flex items-center gap-2">
              <button
                type="button"
                onClick={handleOpenFile}
                className="flex items-center gap-1.5 rounded-lg border border-[var(--border-subtle)] bg-[var(--bg-surface)] px-2.5 py-1 text-xs font-medium text-[var(--text-primary)] hover:bg-[var(--bg-surface-hover)]"
              >
                <ExternalLink className="h-3 w-3" />
                <span>打开文件</span>
              </button>
              <button
                type="button"
                onClick={handleOpenFolder}
                className="flex items-center gap-1.5 rounded-lg border border-[var(--border-subtle)] bg-[var(--bg-surface)] px-2.5 py-1 text-xs font-medium text-[var(--text-primary)] hover:bg-[var(--bg-surface-hover)]"
              >
                <Folder className="h-3 w-3" />
                <span>打开文件夹</span>
              </button>
            </div>
          </div>
        )}

        <div className="flex flex-col items-center justify-center py-12 text-center text-xs text-[var(--text-muted)]">
          <p>当前没有正在进行的传输任务</p>
        </div>
      </main>
      <ToastContainer />
    </div>
  );
}
