import { type MouseEvent } from 'react';
import { Copy, Activity, ExternalLink, FolderOpen, Trash2 } from 'lucide-react';
import * as task from '../../bindings/sheep-get/internal/task/models';
import { Clipboard } from '@wailsio/runtime';
import { showToast } from './ui/Toast';

export interface TaskActionsProps {
  task: task.Task;
  onShowProgress?: (id: string) => void;
  onOpenFile?: (filePath: string) => void;
  onOpenFolder?: (folderPath: string) => void;
  onDelete: (task: task.Task) => void;
  className?: string;
}

export function TaskActions({
  task: t,
  onShowProgress,
  onOpenFile,
  onOpenFolder,
  onDelete,
  className = '',
}: TaskActionsProps) {
  const fullPath = t.directory ? `${t.directory}/${t.filename}` : t.filename;

  const handleCopy = async (e: MouseEvent) => {
    e.stopPropagation();
    if (!t.url) return;
    try {
      await Clipboard.SetText(t.url);
      showToast('已复制下载链接', 'success');
    } catch {
      try {
        await navigator.clipboard.writeText(t.url);
        showToast('已复制下载链接', 'success');
      } catch (err) {
        console.error('Failed to copy URL:', err);
        showToast('复制链接失败', 'error');
      }
    }
  };

  const handleShowProgress = (e: MouseEvent) => {
    e.stopPropagation();
    if (onShowProgress) {
      onShowProgress(t.id);
    }
  };

  const handleOpenFile = (e: MouseEvent) => {
    e.stopPropagation();
    if (onOpenFile) {
      onOpenFile(fullPath);
    }
  };

  const handleOpenFolder = (e: MouseEvent) => {
    e.stopPropagation();
    if (onOpenFolder) {
      onOpenFolder(t.directory);
    }
  };

  const handleDelete = (e: MouseEvent) => {
    e.stopPropagation();
    onDelete(t);
  };

  const isCompleted = t.status === task.Status.StatusCompleted;

  return (
    <div
      className={`flex items-center gap-0.5 rounded-lg border border-[var(--border-subtle)] bg-[var(--bg-surface)]/95 px-1 py-0.5 shadow-md backdrop-blur-md transition-all duration-150 ${className}`}
      onClick={(e) => e.stopPropagation()}
    >
      {/* Copy link button */}
      <button
        type="button"
        onClick={(e) => {
          void handleCopy(e);
        }}
        className="rounded-md p-1 text-[var(--text-muted)] transition-colors hover:bg-[var(--bg-subtle)] hover:text-[var(--text-primary)]"
        title="复制下载链接"
      >
        <Copy className="h-3.5 w-3.5" />
      </button>

      {/* Show in progress window */}
      {onShowProgress && (
        <button
          type="button"
          onClick={handleShowProgress}
          className="rounded-md p-1 text-[var(--text-muted)] transition-colors hover:bg-[var(--accent-muted)] hover:text-[var(--accent)]"
          title="在独立进度窗口中查看"
        >
          <Activity className="h-3.5 w-3.5" />
        </button>
      )}

      {/* Open File & Folder (Completed only) */}
      {isCompleted && onOpenFile && (
        <button
          type="button"
          onClick={handleOpenFile}
          className="rounded-md p-1 text-[var(--text-muted)] transition-colors hover:bg-[var(--accent-muted)] hover:text-[var(--accent)]"
          title="打开文件"
        >
          <ExternalLink className="h-3.5 w-3.5" />
        </button>
      )}
      {isCompleted && onOpenFolder && (
        <button
          type="button"
          onClick={handleOpenFolder}
          className="rounded-md p-1 text-[var(--text-muted)] transition-colors hover:bg-[var(--bg-subtle)] hover:text-[var(--text-primary)]"
          title="打开所在文件夹"
        >
          <FolderOpen className="h-3.5 w-3.5" />
        </button>
      )}

      {/* Delete */}
      <button
        type="button"
        onClick={handleDelete}
        className="rounded-md p-1 text-[var(--text-muted)] transition-colors hover:bg-rose-500/15 hover:text-rose-500"
        title="删除任务"
      >
        <Trash2 className="h-3.5 w-3.5" />
      </button>
    </div>
  );
}
