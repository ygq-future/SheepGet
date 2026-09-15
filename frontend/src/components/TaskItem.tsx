import { motion } from 'motion/react';
import { useState } from 'react';
import * as task from '../../bindings/sheep-get/internal/task/models';
import { formatBytes, formatSpeed } from '../lib/format';
import {
  Play,
  Pause,
  RotateCcw,
  Trash2,
  CheckCircle2,
  AlertCircle,
  Clock,
  ArrowDownCircle,
  HardDrive,
  FolderOpen,
  ExternalLink,
  Copy,
  Check,
  Link2,
} from 'lucide-react';

interface TaskItemProps {
  task: task.Task;
  onPause: (id: string) => void;
  onResume: (id: string) => void;
  onRetry: (id: string) => void;
  onDelete: (id: string) => void;
  onOpenFile: (filePath: string) => void;
  onOpenFolder: (folderPath: string) => void;
  onUpdateLink?: (task: task.Task) => void;
}

export function TaskItem({
  task: t,
  onPause,
  onResume,
  onRetry,
  onDelete,
  onOpenFile,
  onOpenFolder,
  onUpdateLink,
}: TaskItemProps) {
  const [copied, setCopied] = useState(false);

  const percent =
    t.totalBytes > 0 ? Math.min(100, Math.round((t.downloaded / t.totalBytes) * 100)) : 0;

  const fullPath = t.directory ? `${t.directory}/${t.filename}` : t.filename;

  const handleCopyURL = async () => {
    try {
      await navigator.clipboard.writeText(t.url);
      setCopied(true);
      setTimeout(() => setCopied(false), 2000);
    } catch (err) {
      console.error('Failed to copy url:', err);
    }
  };

  const renderStatusBadge = () => {
    switch (t.status) {
      case task.Status.StatusDownloading:
        return (
          <span className="inline-flex items-center gap-1.5 rounded-full border border-[var(--border-focus)] bg-[var(--accent-muted)] px-2 py-0.5 text-[10px] font-medium text-[var(--accent)]">
            <span className="h-1.5 w-1.5 animate-pulse rounded-full bg-[var(--accent)]" />
            下载中
          </span>
        );
      case task.Status.StatusQueued:
        return (
          <span className="inline-flex items-center gap-1.5 rounded-full border border-amber-500/20 bg-amber-500/10 px-2 py-0.5 text-[10px] font-medium text-amber-400">
            <Clock className="h-2.5 w-2.5" />
            排队中
          </span>
        );
      case task.Status.StatusPaused:
        return (
          <span className="inline-flex items-center gap-1.5 rounded-full border border-[var(--border-subtle)] bg-[var(--bg-subtle)] px-2 py-0.5 text-[10px] font-medium text-[var(--text-muted)]">
            <Pause className="h-2.5 w-2.5" />
            已暂停
          </span>
        );
      case task.Status.StatusCompleted:
        return (
          <span className="inline-flex items-center gap-1.5 rounded-full border border-[var(--border-focus)] bg-[var(--accent-muted)] px-2 py-0.5 text-[10px] font-medium text-[var(--accent)]">
            <CheckCircle2 className="h-2.5 w-2.5" />
            已完成
          </span>
        );
      case task.Status.StatusError:
        return (
          <span className="inline-flex items-center gap-1.5 rounded-full border border-rose-500/20 bg-rose-500/10 px-2 py-0.5 text-[10px] font-medium text-rose-400">
            <AlertCircle className="h-2.5 w-2.5" />
            异常
          </span>
        );
      default:
        return null;
    }
  };

  return (
    <motion.div
      layout
      initial={{ opacity: 0, y: 8, scale: 0.98 }}
      animate={{ opacity: 1, y: 0, scale: 1 }}
      exit={{ opacity: 0, scale: 0.96 }}
      transition={{ duration: 0.2, ease: [0.16, 1, 0.3, 1] }}
      className="group relative rounded-xl border border-[var(--border-subtle)] bg-[var(--bg-surface)] p-4 shadow-xs backdrop-blur-sm transition-all duration-200 hover:border-[var(--border-hover)] hover:bg-[var(--bg-surface-hover)] hover:shadow-md"
    >
      <div className="flex items-start justify-between gap-4">
        <div className="min-w-0 flex-1 space-y-1.5">
          <div className="flex items-center gap-2.5">
            <div className="flex h-8 w-8 shrink-0 items-center justify-center rounded-lg border border-[var(--border-subtle)] bg-[var(--bg-subtle)] text-[var(--text-muted)] transition-colors group-hover:text-[var(--text-primary)]">
              <HardDrive className="h-4 w-4" />
            </div>
            <div className="min-w-0 flex-1">
              <div className="flex items-center gap-2">
                <span
                  className="truncate text-xs font-semibold tracking-tight text-[var(--text-primary)]"
                  title={t.filename}
                >
                  {t.filename}
                </span>
                {renderStatusBadge()}
              </div>
              <div className="flex items-center gap-2 truncate font-mono text-[11px] text-[var(--text-muted)]">
                <div className="group/url flex min-w-0 items-center gap-1 truncate">
                  <span className="truncate" title={t.url}>
                    {t.url}
                  </span>
                  <button
                    type="button"
                    onClick={() => {
                      void handleCopyURL();
                    }}
                    className="inline-flex shrink-0 items-center rounded-sm p-0.5 text-zinc-500 transition-colors hover:bg-white/10 hover:text-zinc-300"
                    title={copied ? '已复制链接' : '复制下载链接'}
                  >
                    {copied ? (
                      <Check className="h-3 w-3 text-emerald-400" />
                    ) : (
                      <Copy className="h-3 w-3" />
                    )}
                  </button>
                </div>
                <span className="text-zinc-600">·</span>
                <span
                  className="shrink-0 truncate text-zinc-400"
                  title={`保存目录: ${t.directory}`}
                >
                  {t.directory}
                </span>
              </div>
            </div>
          </div>
        </div>

        {/* Action Controls */}
        <div className="flex items-center gap-1 opacity-80 transition-opacity group-hover:opacity-100">
          {t.status === task.Status.StatusCompleted && (
            <>
              <button
                onClick={() => onOpenFile(fullPath)}
                className="rounded-lg p-1.5 text-[var(--text-muted)] transition-colors hover:bg-[var(--accent-muted)] hover:text-[var(--accent)]"
                title="打开文件"
              >
                <ExternalLink className="h-3.5 w-3.5" />
              </button>
              <button
                onClick={() => onOpenFolder(t.directory)}
                className="rounded-lg p-1.5 text-[var(--text-muted)] transition-colors hover:bg-[var(--bg-subtle)] hover:text-[var(--text-primary)]"
                title="打开所在文件夹"
              >
                <FolderOpen className="h-3.5 w-3.5" />
              </button>
            </>
          )}

          {t.status === task.Status.StatusDownloading && (
            <button
              onClick={() => onPause(t.id)}
              className="rounded-lg p-1.5 text-[var(--text-muted)] transition-colors hover:bg-amber-500/10 hover:text-amber-500"
              title="暂停任务"
            >
              <Pause className="h-3.5 w-3.5" />
            </button>
          )}

          {(t.status === task.Status.StatusPaused || t.status === task.Status.StatusQueued) && (
            <button
              onClick={() => onResume(t.id)}
              className="rounded-lg p-1.5 text-[var(--text-muted)] transition-colors hover:bg-[var(--accent-muted)] hover:text-[var(--accent)]"
              title="继续下载"
            >
              <Play className="h-3.5 w-3.5" />
            </button>
          )}

          {t.status === task.Status.StatusError && (
            <button
              onClick={() => onRetry(t.id)}
              className="rounded-lg p-1.5 text-[var(--text-muted)] transition-colors hover:bg-sky-500/10 hover:text-sky-500"
              title="重试下载"
            >
              <RotateCcw className="h-3.5 w-3.5" />
            </button>
          )}
          {(t.status === task.Status.StatusPaused || t.status === task.Status.StatusError) &&
            onUpdateLink && (
              <button
                onClick={() => onUpdateLink(t)}
                className="rounded-lg p-1.5 text-[var(--text-muted)] transition-colors hover:bg-sky-500/10 hover:text-sky-500"
                title="更新链接"
              >
                <Link2 className="h-3.5 w-3.5" />
              </button>
            )}

          <button
            onClick={() => onDelete(t.id)}
            className="rounded-lg p-1.5 text-[var(--text-muted)] transition-colors hover:bg-rose-500/10 hover:text-rose-500"
            title="删除任务"
          >
            <Trash2 className="h-3.5 w-3.5" />
          </button>
        </div>
      </div>

      {/* Multi-chunk Progress Track */}
      <div className="mt-3.5 space-y-2">
        {t.chunks && t.chunks.length > 1 ? (
          // Visualized Multi-Channel Chunks
          <div
            className="grid gap-1"
            style={{ gridTemplateColumns: `repeat(${t.chunks.length}, minmax(0, 1fr))` }}
          >
            {t.chunks.map((chunk, idx) => {
              const chunkSize = chunk.end - chunk.start + 1;
              const chunkPercent =
                chunkSize > 0 ? Math.min(100, Math.round((chunk.downloaded / chunkSize) * 100)) : 0;

              return (
                <div
                  key={idx}
                  className="relative h-2 overflow-hidden rounded-sm border border-[var(--border-subtle)] bg-[var(--bg-subtle)] p-[0.5px]"
                  title={`通道 ${idx + 1}: ${formatBytes(chunk.downloaded)} / ${formatBytes(
                    chunkSize,
                  )} (${chunkPercent}%)`}
                >
                  <div
                    className={`h-full rounded-xs transition-all duration-200 ${
                      chunk.completed
                        ? 'bg-[var(--accent)] shadow-xs'
                        : t.status === task.Status.StatusError
                          ? 'bg-rose-500'
                          : 'bg-[var(--accent)] opacity-90 shadow-xs'
                    }`}
                    style={{ width: `${chunkPercent}%` }}
                  />
                </div>
              );
            })}
          </div>
        ) : (
          // Single Stream Progress Bar
          <div className="h-1.5 w-full overflow-hidden rounded-full bg-[var(--bg-subtle)] p-[1px]">
            <div
              className={`h-full rounded-full transition-all duration-200 ${
                t.status === task.Status.StatusCompleted
                  ? 'bg-[var(--accent)] shadow-xs'
                  : t.status === task.Status.StatusError
                    ? 'bg-rose-500'
                    : 'bg-[var(--accent)] shadow-xs'
              }`}
              style={{ width: `${percent}%` }}
            />
          </div>
        )}

        <div className="flex items-center justify-between text-[11px] text-[var(--text-muted)]">
          <div className="flex items-center gap-2">
            <span className="font-mono text-[var(--text-primary)]">
              {formatBytes(t.downloaded)} / {formatBytes(t.totalBytes)}
            </span>
            {t.totalBytes > 0 && (
              <span className="font-mono text-[var(--text-muted)]">({percent}%)</span>
            )}
          </div>

          <div className="flex items-center gap-3">
            {t.status === task.Status.StatusDownloading && (
              <span className="flex items-center gap-1 font-mono font-medium text-[var(--accent)]">
                <ArrowDownCircle className="h-3 w-3" />
                {formatSpeed(t.speed)}
              </span>
            )}
            {t.status === task.Status.StatusError && t.errorMsg && (
              <span className="max-w-xs truncate font-mono text-rose-400" title={t.errorMsg}>
                {t.errorMsg}
              </span>
            )}
            <span className="rounded-md border border-[var(--border-subtle)] bg-[var(--bg-subtle)] px-2 py-0.5 text-[10px] font-medium text-[var(--text-secondary)]">
              {t.maxConcurrency} 通道
            </span>
          </div>
        </div>
      </div>
    </motion.div>
  );
}
