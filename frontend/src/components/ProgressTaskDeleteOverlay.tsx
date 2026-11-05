import { useEffect, useRef, useState } from 'react';
import { motion } from 'motion/react';
import { AlertTriangle, Trash2 } from 'lucide-react';
import { Checkbox } from './ui/Checkbox';
import * as taskModels from '../../bindings/sheep-get/internal/task/models';

export interface ProgressTaskDeleteOverlayProps {
  task: taskModels.Task;
  defaultDeleteDiskFile?: boolean;
  onConfirm: (deleteDiskFile: boolean) => void;
  onCancel: () => void;
}

/**
 * 进度窗口任务卡片专属行内删除确认覆盖条。
 *
 * 替代全屏遮罩 Dialog：当用户点击某一任务卡片的删除按钮时，
 * 该长条二次确认浮层直接原地绝对定位覆盖在对应的任务卡片上（类似首页悬浮按钮条）。
 * 既适配单任务最小高度，又避免多任务高窗口时全屏中心弹出遮罩带来的视觉割裂与遮挡。
 */
export function ProgressTaskDeleteOverlay({
  task,
  defaultDeleteDiskFile = false,
  onConfirm,
  onCancel,
}: ProgressTaskDeleteOverlayProps) {
  const [deleteDiskFile, setDeleteDiskFile] = useState(defaultDeleteDiskFile);
  const overlayRef = useRef<HTMLDivElement>(null);

  // 监听 Escape 键快速取消
  useEffect(() => {
    const handleKeyDown = (e: KeyboardEvent) => {
      if (e.key === 'Escape') {
        e.stopPropagation();
        onCancel();
      }
    };
    window.addEventListener('keydown', handleKeyDown);
    return () => window.removeEventListener('keydown', handleKeyDown);
  }, [onCancel]);

  // 监听点击覆盖条外部区域自动取消
  useEffect(() => {
    const handlePointerDown = (e: PointerEvent) => {
      if (overlayRef.current && !overlayRef.current.contains(e.target as Node)) {
        onCancel();
      }
    };
    window.addEventListener('pointerdown', handlePointerDown);
    return () => window.removeEventListener('pointerdown', handlePointerDown);
  }, [onCancel]);

  return (
    <motion.div
      ref={overlayRef}
      initial={{ opacity: 0, scale: 0.99 }}
      animate={{ opacity: 1, scale: 1 }}
      exit={{ opacity: 0, scale: 0.99 }}
      transition={{ duration: 0.15, ease: 'easeOut' }}
      onClick={(e) => e.stopPropagation()}
      role="region"
      aria-label="删除任务确认"
      className="absolute inset-0 z-20 flex items-center justify-between gap-2.5 rounded-xl border border-rose-500/40 bg-[var(--bg-surface)]/95 px-3 py-1.5 text-[var(--text-primary)] shadow-md backdrop-blur-md select-none"
    >
      {/* 左侧：警示图标 + 标题 + 文件名（截断） */}
      <div className="flex min-w-0 flex-1 items-center gap-2">
        <div className="flex h-6 w-6 shrink-0 items-center justify-center rounded-md border border-rose-500/20 bg-rose-500/10 text-rose-500">
          <AlertTriangle className="h-3.5 w-3.5" />
        </div>
        <div className="flex min-w-0 items-center gap-1.5">
          <span className="shrink-0 text-xs font-semibold tracking-tight text-[var(--text-primary)]">
            确认删除下载任务
          </span>
          {task.filename?.trim() ? (
            <span
              className="min-w-0 truncate font-mono text-[11px] text-[var(--text-secondary)]"
              title={task.filename}
            >
              "{task.filename}"
            </span>
          ) : null}
        </div>
      </div>

      {/* 右侧：紧凑复选框 + 细分割线 + 取消/确认按钮组 */}
      <div className="flex shrink-0 items-center gap-2.5">
        <Checkbox
          id={`progress-delete-disk-${task.id}`}
          checked={deleteDiskFile}
          onCheckedChange={(checked) => setDeleteDiskFile(checked)}
          label="同时删除本地文件"
          className="text-xs text-[var(--text-secondary)] hover:text-[var(--text-primary)]"
        />
        <div className="h-4 w-[1px] bg-[var(--border-subtle)]" />
        <button
          type="button"
          onClick={onCancel}
          className="h-7 cursor-pointer rounded-lg border border-[var(--border-subtle)] px-2.5 text-xs font-medium text-[var(--text-secondary)] transition-colors hover:bg-[var(--bg-subtle)] hover:text-[var(--text-primary)]"
        >
          取消
        </button>
        <button
          type="button"
          onClick={() => onConfirm(deleteDiskFile)}
          className="inline-flex h-7 cursor-pointer items-center gap-1.5 rounded-lg bg-rose-500 px-3 text-xs font-medium text-white shadow-xs transition-colors hover:bg-rose-600 active:scale-98"
        >
          <Trash2 className="h-3.5 w-3.5" />
          <span>确认删除</span>
        </button>
      </div>
    </motion.div>
  );
}
