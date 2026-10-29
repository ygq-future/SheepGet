import { type MouseEvent } from 'react';
import {
  Copy,
  Activity,
  ExternalLink,
  FolderOpen,
  Trash2,
  Globe,
  Play,
  Pause,
  RotateCcw,
  Package,
} from 'lucide-react';
import * as task from '../../bindings/sheep-get/internal/task/models';
import { Browser, Clipboard } from '@wailsio/runtime';
import { showToast } from './ui/Toast';

export interface TaskActionsProps {
  task: task.Task;
  onShowProgress?: (id: string) => void;
  onOpenFile?: (filePath: string) => void;
  onOpenFolder?: (folderPath: string) => void;
  onDelete: (task: task.Task) => void;
  onResume?: (task: task.Task) => void;
  onPause?: (task: task.Task) => void;
  onRetry?: (task: task.Task) => void;
  /** 收集中的 zip 批次：立即闭合并开始下载。 */
  onSeal?: (task: task.Task) => void;
  className?: string;
}

export function TaskActions({
  task: t,
  onShowProgress,
  onOpenFile,
  onOpenFolder,
  onDelete,
  onResume,
  onPause,
  onRetry,
  onSeal,
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

  const handleOpenPage = async (e: MouseEvent) => {
    e.stopPropagation();
    if (!t.pageUrl) return;
    try {
      await Browser.OpenURL(t.pageUrl);
      showToast('已在浏览器中打开源网页', 'info');
    } catch (err) {
      console.error('Failed to open webpage:', err);
      showToast('无法打开源网页', 'error');
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

  const handleResume = (e: MouseEvent) => {
    e.stopPropagation();
    if (onResume) {
      onResume(t);
    }
  };

  const handlePause = (e: MouseEvent) => {
    e.stopPropagation();
    if (onPause) {
      onPause(t);
    }
  };

  const handleRetry = (e: MouseEvent) => {
    e.stopPropagation();
    if (onRetry) {
      onRetry(t);
    }
  };

  const handleSeal = (e: MouseEvent) => {
    e.stopPropagation();
    if (onSeal) {
      onSeal(t);
    }
  };

  const isCompleted = t.status === task.Status.StatusCompleted;

  // 双击卡片本身会打开进度窗口（TaskItem 的 onDoubleClick），动作条浮在卡片上方，
  // 双击它的按钮不该被当成双击卡片，因此点击与双击都要拦住冒泡。
  return (
    <div
      className={`flex items-center gap-0.5 rounded-lg border border-[var(--border-subtle)] bg-[var(--bg-surface)]/95 px-1 py-0.5 shadow-md backdrop-blur-md transition-all duration-150 ${className}`}
      onClick={(e) => e.stopPropagation()}
      onDoubleClick={(e) => e.stopPropagation()}
    >
      {/* Seal a collecting zip batch: the Agent may have forgotten zipFinal. */}
      {t.status === task.Status.StatusCollecting && onSeal && (
        <button
          type="button"
          onClick={handleSeal}
          className="rounded-md p-1 text-sky-500 transition-colors hover:bg-sky-500/15 hover:text-sky-400"
          title="立即打包并开始下载"
        >
          <Package className="h-3.5 w-3.5" />
        </button>
      )}

      {/* Resume button for paused task */}
      {t.status === task.Status.StatusPaused && onResume && (
        <button
          type="button"
          onClick={handleResume}
          className="rounded-md p-1 text-emerald-500 transition-colors hover:bg-emerald-500/15 hover:text-emerald-400"
          title="继续下载并打开进度窗口"
        >
          <Play className="h-3.5 w-3.5 fill-current" />
        </button>
      )}

      {/* Pause button for downloading / queued task */}
      {(t.status === task.Status.StatusDownloading || t.status === task.Status.StatusQueued) &&
        onPause && (
          <button
            type="button"
            onClick={handlePause}
            className="rounded-md p-1 text-amber-500 transition-colors hover:bg-amber-500/15 hover:text-amber-400"
            title="暂停下载"
          >
            <Pause className="h-3.5 w-3.5" />
          </button>
        )}

      {/* Retry button for error task */}
      {t.status === task.Status.StatusError && onRetry && (
        <button
          type="button"
          onClick={handleRetry}
          className="rounded-md p-1 text-rose-500 transition-colors hover:bg-rose-500/15 hover:text-rose-400"
          title="重试下载并打开进度窗口"
        >
          <RotateCcw className="h-3.5 w-3.5" />
        </button>
      )}

      {/* Copy link button — zip batches have no single URL to copy. */}
      {Boolean(t.url) && (
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
      )}

      {/* Open source webpage */}
      {Boolean(t.pageUrl) && (
        <button
          type="button"
          onClick={(e) => {
            void handleOpenPage(e);
          }}
          className="rounded-md p-1 text-[var(--text-muted)] transition-colors hover:bg-[var(--accent-muted)] hover:text-[var(--accent)]"
          title={`打开下载源站 (${t.pageUrl})`}
        >
          <Globe className="h-3.5 w-3.5" />
        </button>
      )}

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
