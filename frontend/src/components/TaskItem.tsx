import { motion } from 'motion/react';
import { useState } from 'react';
import type { task } from '../../wailsjs/go/models';
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
} from 'lucide-react';

interface TaskItemProps {
  task: task.Task;
  onPause: (id: string) => void;
  onResume: (id: string) => void;
  onRetry: (id: string) => void;
  onDelete: (id: string) => void;
  onOpenFile: (filePath: string) => void;
  onOpenFolder: (folderPath: string) => void;
}

export function TaskItem({
  task: t,
  onPause,
  onResume,
  onRetry,
  onDelete,
  onOpenFile,
  onOpenFolder,
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
      case 'downloading':
        return (
          <span className="inline-flex items-center gap-1.5 rounded-full border border-sky-500/20 bg-sky-500/10 px-2 py-0.5 text-[10px] font-medium text-sky-400">
            <span className="h-1.5 w-1.5 animate-pulse rounded-full bg-sky-400" />
            下载中
          </span>
        );
      case 'queued':
        return (
          <span className="inline-flex items-center gap-1.5 rounded-full border border-amber-500/20 bg-amber-500/10 px-2 py-0.5 text-[10px] font-medium text-amber-400">
            <Clock className="h-2.5 w-2.5" />
            排队中
          </span>
        );
      case 'paused':
        return (
          <span className="inline-flex items-center gap-1.5 rounded-full border border-white/5 bg-zinc-800 px-2 py-0.5 text-[10px] font-medium text-zinc-400">
            <Pause className="h-2.5 w-2.5" />
            已暂停
          </span>
        );
      case 'completed':
        return (
          <span className="inline-flex items-center gap-1.5 rounded-full border border-emerald-500/20 bg-emerald-500/10 px-2 py-0.5 text-[10px] font-medium text-emerald-400">
            <CheckCircle2 className="h-2.5 w-2.5" />
            已完成
          </span>
        );
      case 'error':
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
      className="group relative rounded-xl border border-white/[0.06] bg-zinc-900/50 p-4 shadow-xs backdrop-blur-sm transition-all duration-200 hover:border-white/[0.14] hover:bg-zinc-900/80 hover:shadow-lg hover:shadow-black/40"
    >
      <div className="flex items-start justify-between gap-4">
        <div className="min-w-0 flex-1 space-y-1.5">
          <div className="flex items-center gap-2.5">
            <div className="flex h-8 w-8 shrink-0 items-center justify-center rounded-lg border border-white/5 bg-zinc-800/80 text-zinc-400 transition-colors group-hover:text-zinc-200">
              <HardDrive className="h-4 w-4" />
            </div>
            <div className="min-w-0 flex-1">
              <div className="flex items-center gap-2">
                <span
                  className="truncate text-xs font-semibold tracking-tight text-zinc-100"
                  title={t.filename}
                >
                  {t.filename}
                </span>
                {renderStatusBadge()}
              </div>
              <div className="flex items-center gap-2 truncate font-mono text-[11px] text-zinc-500">
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
          {t.status === 'completed' && (
            <>
              <button
                onClick={() => onOpenFile(fullPath)}
                className="rounded-lg p-1.5 text-zinc-400 transition-colors hover:bg-emerald-500/10 hover:text-emerald-400"
                title="打开文件"
              >
                <ExternalLink className="h-3.5 w-3.5" />
              </button>
              <button
                onClick={() => onOpenFolder(t.directory)}
                className="rounded-lg p-1.5 text-zinc-400 transition-colors hover:bg-white/10 hover:text-zinc-200"
                title="打开所在文件夹"
              >
                <FolderOpen className="h-3.5 w-3.5" />
              </button>
            </>
          )}

          {t.status === 'downloading' && (
            <button
              onClick={() => onPause(t.id)}
              className="rounded-lg p-1.5 text-zinc-400 transition-colors hover:bg-white/10 hover:text-amber-400"
              title="暂停任务"
            >
              <Pause className="h-3.5 w-3.5" />
            </button>
          )}

          {(t.status === 'paused' || t.status === 'queued') && (
            <button
              onClick={() => onResume(t.id)}
              className="rounded-lg p-1.5 text-zinc-400 transition-colors hover:bg-white/10 hover:text-emerald-400"
              title="继续下载"
            >
              <Play className="h-3.5 w-3.5" />
            </button>
          )}

          {t.status === 'error' && (
            <button
              onClick={() => onRetry(t.id)}
              className="rounded-lg p-1.5 text-zinc-400 transition-colors hover:bg-white/10 hover:text-sky-400"
              title="重试下载"
            >
              <RotateCcw className="h-3.5 w-3.5" />
            </button>
          )}

          <button
            onClick={() => onDelete(t.id)}
            className="rounded-lg p-1.5 text-zinc-500 transition-colors hover:bg-rose-500/10 hover:text-rose-400"
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
                  className="relative h-2 overflow-hidden rounded-sm border border-white/5 bg-zinc-800/90 p-[0.5px]"
                  title={`通道 ${idx + 1}: ${formatBytes(chunk.downloaded)} / ${formatBytes(chunkSize)} (${chunkPercent}%)`}
                >
                  <div
                    className={`h-full rounded-xs transition-all duration-200 ${
                      chunk.completed
                        ? 'bg-emerald-500 shadow-xs shadow-emerald-500/50'
                        : t.status === 'error'
                          ? 'bg-rose-500'
                          : 'bg-sky-400 shadow-xs shadow-sky-400/50'
                    }`}
                    style={{ width: `${chunkPercent}%` }}
                  />
                </div>
              );
            })}
          </div>
        ) : (
          // Single Stream Progress Bar
          <div className="h-1.5 w-full overflow-hidden rounded-full bg-zinc-800/80 p-[1px]">
            <div
              className={`h-full rounded-full transition-all duration-200 ${
                t.status === 'completed'
                  ? 'bg-emerald-500 shadow-xs shadow-emerald-500/50'
                  : t.status === 'error'
                    ? 'bg-rose-500'
                    : 'bg-sky-400 shadow-xs shadow-sky-400/50'
              }`}
              style={{ width: `${percent}%` }}
            />
          </div>
        )}

        <div className="flex items-center justify-between text-[11px] text-zinc-400">
          <div className="flex items-center gap-2">
            <span className="font-mono text-zinc-300">
              {formatBytes(t.downloaded)} / {formatBytes(t.totalBytes)}
            </span>
            {t.totalBytes > 0 && <span className="font-mono text-zinc-500">({percent}%)</span>}
          </div>

          <div className="flex items-center gap-3">
            {t.status === 'downloading' && (
              <span className="flex items-center gap-1 font-mono font-medium text-sky-400">
                <ArrowDownCircle className="h-3 w-3" />
                {formatSpeed(t.speed)}
              </span>
            )}
            {t.status === 'error' && t.errorMsg && (
              <span className="max-w-xs truncate font-mono text-rose-400" title={t.errorMsg}>
                {t.errorMsg}
              </span>
            )}
            <span className="rounded-md border border-white/5 bg-zinc-800/60 px-1.5 py-0.5 text-[10px] text-zinc-500">
              {t.maxConcurrency} 通道
            </span>
          </div>
        </div>
      </div>
    </motion.div>
  );
}
