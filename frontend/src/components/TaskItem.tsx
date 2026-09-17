import { type MouseEvent } from 'react';
import { motion } from 'motion/react';
import * as task from '../../bindings/sheep-get/internal/task/models';
import { formatBytes, formatSpeed, formatDuration, formatDateTime } from '../lib/format';
import { FileTypeIcon, isMediaFile } from '../lib/fileIcon';
import { isProcessingFailure } from '../lib/progress';
import { TaskActions } from './TaskActions';
import { CheckCircle2, AlertCircle, Clock, Pause, Loader2 } from 'lucide-react';

export interface SelectionModifiers {
  ctrlKey: boolean;
  shiftKey: boolean;
}

interface TaskItemProps {
  task: task.Task;
  selected?: boolean;
  onToggleSelect?: (id: string, modifiers: SelectionModifiers) => void;
  onDelete: (task: task.Task) => void;
  onOpenFile: (filePath: string) => void;
  onOpenFolder: (folderPath: string) => void;
  onShowProgress?: (id: string) => void;
}

export function TaskItem({
  task: t,
  selected = false,
  onToggleSelect,
  onDelete,
  onOpenFile,
  onOpenFolder,
  onShowProgress,
}: TaskItemProps) {
  const percent =
    t.totalBytes > 0 ? Math.min(100, Math.round((t.downloaded / t.totalBytes) * 100)) : 0;

  // Check if media file has duration information
  const hasDuration =
    isMediaFile(t.filename) && typeof (t as { duration?: number }).duration === 'number';
  const durationText = hasDuration
    ? formatDuration((t as { duration?: number }).duration || 0)
    : null;

  const handleRowClick = (e: MouseEvent) => {
    if (onToggleSelect) {
      onToggleSelect(t.id, {
        ctrlKey: e.ctrlKey || e.metaKey,
        shiftKey: e.shiftKey,
      });
    }
  };

  const handleRowDoubleClick = (e: MouseEvent) => {
    e.stopPropagation();
    if (onShowProgress) {
      onShowProgress(t.id);
    }
  };

  const renderStatus = () => {
    switch (t.status) {
      case task.Status.StatusCompleted:
        return (
          <span className="flex items-center gap-1 text-[11px] font-medium text-emerald-500">
            <CheckCircle2 className="h-3 w-3" />
            <span>已完成</span>
          </span>
        );
      case task.Status.StatusDownloading:
        return (
          <div className="flex items-center gap-1.5 font-mono text-[11px]">
            <span className="font-semibold text-[var(--accent)]">{percent}%</span>
            {t.speed > 0 && (
              <span className="text-[10px] text-[var(--text-muted)]">{formatSpeed(t.speed)}</span>
            )}
          </div>
        );
      case task.Status.StatusPaused:
        return (
          <span className="flex items-center gap-1 font-mono text-[11px] text-amber-500/90">
            <Pause className="h-2.5 w-2.5" />
            <span>{percent}% (暂停)</span>
          </span>
        );
      case task.Status.StatusQueued:
        return (
          <span className="flex items-center gap-1 font-mono text-[11px] text-zinc-400">
            <Clock className="h-2.5 w-2.5" />
            <span>排队中</span>
          </span>
        );
      case task.Status.StatusProcessing:
        return (
          <span className="flex items-center gap-1 font-mono text-[11px] text-sky-400">
            <Loader2 className="h-2.5 w-2.5 animate-spin" />
            <span>处理中</span>
          </span>
        );
      case task.Status.StatusError:
        return (
          <span
            className="flex items-center gap-1 font-mono text-[11px] text-rose-500"
            title={t.errorMsg || (isProcessingFailure(t) ? '处理失败' : '下载失败')}
          >
            <AlertCircle className="h-2.5 w-2.5" />
            <span>失败</span>
          </span>
        );
      default:
        return null;
    }
  };

  return (
    <motion.div
      layout
      initial={{ opacity: 0, y: 4 }}
      animate={{ opacity: 1, y: 0 }}
      exit={{ opacity: 0, scale: 0.98 }}
      transition={{ duration: 0.15, ease: 'easeOut' }}
      onClick={handleRowClick}
      onDoubleClick={handleRowDoubleClick}
      className={`group relative flex cursor-pointer items-center justify-between gap-2.5 rounded-lg border px-3 py-1.5 shadow-2xs backdrop-blur-xs transition-all duration-150 select-none ${
        selected
          ? 'border-[var(--accent)]/50 bg-[var(--accent)]/15 text-[var(--text-primary)] shadow-xs ring-1 ring-[var(--accent)]/30'
          : 'border-[var(--border-subtle)] bg-[var(--bg-surface)] hover:border-[var(--border-hover)] hover:bg-[var(--bg-surface-hover)]'
      }`}
      title="单击选中/取消选中，双击在独立进度窗口中查看"
    >
      {/* Left: Compact Icon, Filename */}
      <div className="flex min-w-0 flex-1 items-center gap-2.5">
        <FileTypeIcon
          filename={t.filename}
          className="h-5.5 w-5.5 shrink-0"
          iconClassName="h-3 w-3"
        />

        <span
          className="truncate text-xs font-medium tracking-tight text-[var(--text-primary)]"
          title={t.filename}
        >
          {t.filename}
        </span>
      </div>

      {/* Right: File Size / Duration, Last Connected Time, Status / Percentage */}
      <div className="flex shrink-0 items-center gap-3">
        {/* File size and optional media duration */}
        <div className="text-right font-mono text-[11px] text-[var(--text-secondary)]">
          <span>{formatBytes(t.totalBytes > 0 ? t.totalBytes : t.downloaded)}</span>
          {durationText && <span className="text-[var(--text-muted)]"> · {durationText}</span>}
        </div>

        {/* Last connected time with year */}
        <div className="font-mono text-[11px] text-[var(--text-muted)]" title="最后连接时间">
          {formatDateTime(t.updatedAt || t.createdAt)}
        </div>

        {/* Progress percent / Status badge */}
        <div className="min-w-16 text-right">{renderStatus()}</div>
      </div>

      {/* Hover Floating Actions (Absolute overlay - zero layout footprint when idle) */}
      <div className="pointer-events-none absolute top-1/2 right-2.5 -translate-y-1/2 opacity-0 transition-opacity duration-150 group-hover:pointer-events-auto group-hover:opacity-100">
        <TaskActions
          task={t}
          onShowProgress={onShowProgress}
          onOpenFile={onOpenFile}
          onOpenFolder={onOpenFolder}
          onDelete={onDelete}
        />
      </div>
    </motion.div>
  );
}
