import * as Dialog from '@radix-ui/react-dialog';
import { AlertTriangle, Trash2, X } from 'lucide-react';
import { useState } from 'react';
import { Checkbox } from './ui/Checkbox';

interface DeleteConfirmModalProps {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  filename: string;
  onConfirm: (deleteDiskFile: boolean) => void;
}

export function DeleteConfirmModal({
  open,
  onOpenChange,
  filename,
  onConfirm,
}: DeleteConfirmModalProps) {
  const [deleteDiskFile, setDeleteDiskFile] = useState(false);

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
        <Dialog.Content className="animate-in fade-in zoom-in-95 fixed top-1/2 left-1/2 z-50 w-full max-w-md -translate-x-1/2 -translate-y-1/2 rounded-2xl border border-[var(--border-subtle)] bg-[var(--bg-surface)] p-6 text-[var(--text-primary)] shadow-2xl backdrop-blur-2xl transition-all duration-200 focus:outline-hidden">
          <div className="flex items-center justify-between border-b border-[var(--border-subtle)] pb-3">
            <div className="flex items-center gap-2.5 text-rose-500 dark:text-rose-400">
              <div className="flex h-7 w-7 items-center justify-center rounded-lg border border-rose-500/20 bg-rose-500/10">
                <AlertTriangle className="h-4 w-4 text-rose-500 dark:text-rose-400" />
              </div>
              <Dialog.Title className="text-sm font-semibold tracking-tight text-[var(--text-primary)]">
                确认删除下载任务
              </Dialog.Title>
            </div>
            <Dialog.Close className="rounded-lg p-1.5 text-[var(--text-muted)] transition-colors hover:bg-[var(--bg-subtle)] hover:text-[var(--text-primary)]">
              <X className="h-4 w-4" />
            </Dialog.Close>
          </div>

          <div className="mt-4 space-y-4">
            <p className="text-xs leading-relaxed text-[var(--text-secondary)]">
              确定要从任务列表中移除任务{' '}
              <span className="font-mono font-semibold text-[var(--text-primary)]">
                "{filename}"
              </span>{' '}
              吗？
            </p>

            <div className="rounded-xl border border-[var(--border-subtle)] bg-[var(--bg-subtle)]/50 p-3.5">
              <Checkbox
                id="delete-disk-file"
                checked={deleteDiskFile}
                onCheckedChange={setDeleteDiskFile}
                label="同时删除磁盘上的本地文件"
                description="包含未完成的临时文件 (.sheepget) 或已完成的成品"
              />
            </div>

            <div className="flex items-center justify-end gap-2.5 border-t border-[var(--border-subtle)] pt-3">
              <button
                type="button"
                onClick={() => handleOpenChange(false)}
                className="cursor-pointer rounded-xl border border-[var(--border-subtle)] px-4 py-2 text-xs font-medium text-[var(--text-secondary)] transition-colors hover:bg-[var(--bg-subtle)] hover:text-[var(--text-primary)]"
              >
                取消
              </button>
              <button
                type="button"
                onClick={handleConfirm}
                className="inline-flex cursor-pointer items-center gap-1.5 rounded-xl bg-rose-500 px-4 py-2 text-xs font-semibold text-white shadow-lg shadow-rose-500/20 transition-all hover:bg-rose-600 active:scale-98"
              >
                <Trash2 className="h-3.5 w-3.5" />
                确认删除
              </button>
            </div>
          </div>
        </Dialog.Content>
      </Dialog.Portal>
    </Dialog.Root>
  );
}
