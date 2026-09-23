import * as Dialog from '@radix-ui/react-dialog';
import { AlertTriangle, Trash2, X } from 'lucide-react';
import { useState } from 'react';
import { Checkbox } from './ui/Checkbox';

interface DeleteConfirmModalProps {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  filename?: string;
  count?: number;
  onConfirm: (deleteDiskFile: boolean) => void;
}

export function DeleteConfirmModal({
  open,
  onOpenChange,
  filename = '',
  count = 1,
  onConfirm,
}: DeleteConfirmModalProps) {
  const [deleteDiskFile, setDeleteDiskFile] = useState(false);

  const isBatch = count > 1;

  const handleConfirm = () => {
    onConfirm(deleteDiskFile);
    setDeleteDiskFile(false);
    onOpenChange(false);
  };

  const handleOpenChange = (nextOpen: boolean) => {
    if (!nextOpen) {
      setDeleteDiskFile(false);
    }
    onOpenChange(nextOpen);
  };

  return (
    <Dialog.Root open={open} onOpenChange={handleOpenChange}>
      <Dialog.Portal>
        <Dialog.Overlay className="animate-in fade-in fixed inset-0 z-50 bg-black/60 backdrop-blur-xs transition-opacity" />
        <Dialog.Content className="animate-in fade-in zoom-in-95 fixed top-1/2 left-1/2 z-50 w-full max-w-[360px] -translate-x-1/2 -translate-y-1/2 rounded-xl border border-[var(--border-subtle)] bg-[var(--bg-surface)] p-4 text-[var(--text-primary)] shadow-2xl backdrop-blur-2xl transition-all duration-200 focus:outline-hidden">
          <div className="flex items-center justify-between pb-2.5">
            <div className="flex items-center gap-2 text-rose-500 dark:text-rose-400">
              <div className="flex h-6 w-6 items-center justify-center rounded-md border border-rose-500/20 bg-rose-500/10">
                <AlertTriangle className="h-3.5 w-3.5 text-rose-500 dark:text-rose-400" />
              </div>
              <Dialog.Title className="text-xs font-semibold tracking-tight text-[var(--text-primary)]">
                {isBatch ? `确认批量删除 ${count} 个任务` : '确认删除下载任务'}
              </Dialog.Title>
            </div>
            <Dialog.Close className="rounded-md p-1 text-[var(--text-muted)] transition-colors hover:bg-[var(--bg-subtle)] hover:text-[var(--text-primary)]">
              <X className="h-3.5 w-3.5" />
            </Dialog.Close>
          </div>

          <div className="space-y-3 pt-0.5">
            <p className="text-xs leading-relaxed text-[var(--text-secondary)]">
              {isBatch ? (
                <>
                  确定要从任务列表中移除选中的{' '}
                  <span className="font-mono font-semibold text-[var(--text-primary)]">
                    {count}
                  </span>{' '}
                  个任务吗？
                </>
              ) : (
                <>
                  确定要从任务列表中移除任务{' '}
                  {filename?.trim() ? (
                    <span
                      className="font-mono font-semibold break-all text-[var(--text-primary)]"
                      title={filename}
                    >
                      "{filename}"
                    </span>
                  ) : (
                    <span className="font-semibold text-[var(--text-primary)]">此任务</span>
                  )}{' '}
                  吗？
                </>
              )}
            </p>

            <div className="rounded-lg border border-[var(--border-subtle)]/60 bg-[var(--bg-subtle)]/40 p-2.5">
              <Checkbox
                id="delete-disk-file"
                checked={deleteDiskFile}
                onCheckedChange={(checked) => setDeleteDiskFile(checked)}
                label="同时删除磁盘上的本地文件"
                description="包含未完成的临时文件 (.sheepget) 或已完成的成品"
              />
            </div>

            <div className="flex items-center justify-end gap-2 pt-1">
              <button
                type="button"
                onClick={() => handleOpenChange(false)}
                className="h-7.5 cursor-pointer rounded-lg border border-[var(--border-subtle)] px-3 text-xs font-medium text-[var(--text-secondary)] transition-colors hover:bg-[var(--bg-subtle)] hover:text-[var(--text-primary)]"
              >
                取消
              </button>
              <button
                type="button"
                onClick={handleConfirm}
                className="inline-flex h-7.5 cursor-pointer items-center gap-1.5 rounded-lg bg-rose-500 px-3 text-xs font-medium text-white shadow-xs transition-colors hover:bg-rose-600 active:scale-98"
              >
                <Trash2 className="h-3.5 w-3.5" />
                {isBatch ? `确认删除 (${count})` : '确认删除'}
              </button>
            </div>
          </div>
        </Dialog.Content>
      </Dialog.Portal>
    </Dialog.Root>
  );
}
