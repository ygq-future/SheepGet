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
        <Dialog.Overlay className="animate-in fade-in fixed inset-0 z-50 bg-black/75 backdrop-blur-md transition-opacity" />
        <Dialog.Content className="animate-in fade-in zoom-in-95 fixed top-1/2 left-1/2 z-50 w-full max-w-md -translate-x-1/2 -translate-y-1/2 rounded-2xl border border-white/10 bg-zinc-950/95 p-6 shadow-2xl shadow-black/80 backdrop-blur-2xl transition-all duration-200 focus:outline-hidden">
          <div className="flex items-center justify-between border-b border-white/[0.06] pb-3">
            <div className="flex items-center gap-2.5 text-rose-400">
              <div className="flex h-7 w-7 items-center justify-center rounded-lg border border-rose-500/20 bg-rose-500/10">
                <AlertTriangle className="h-4 w-4 text-rose-400" />
              </div>
              <Dialog.Title className="text-sm font-semibold tracking-tight text-zinc-100">
                确认删除下载任务
              </Dialog.Title>
            </div>
            <Dialog.Close className="rounded-lg p-1.5 text-zinc-400 transition-colors hover:bg-white/5 hover:text-zinc-200">
              <X className="h-4 w-4" />
            </Dialog.Close>
          </div>

          <div className="mt-4 space-y-4">
            <p className="text-xs leading-relaxed text-zinc-300">
              确定要从任务列表中移除任务{' '}
              <span className="font-mono font-semibold text-zinc-100">"{filename}"</span> 吗？
            </p>

            <div className="rounded-xl border border-white/5 bg-zinc-900/60 p-3.5">
              <Checkbox
                id="delete-disk-file"
                checked={deleteDiskFile}
                onCheckedChange={setDeleteDiskFile}
                label="同时删除磁盘上的本地文件"
                description="包含未完成的临时文件 (.sheepget) 或已完成的成品"
              />
            </div>

            <div className="flex items-center justify-end gap-2.5 border-t border-white/[0.06] pt-3">
              <button
                type="button"
                onClick={() => handleOpenChange(false)}
                className="rounded-xl border border-white/10 px-4 py-2 text-xs font-medium text-zinc-300 transition-colors hover:bg-white/5 hover:text-zinc-100"
              >
                取消
              </button>
              <button
                type="button"
                onClick={handleConfirm}
                className="inline-flex items-center gap-1.5 rounded-xl bg-rose-500 px-4 py-2 text-xs font-semibold text-white shadow-lg shadow-rose-500/20 transition-all hover:bg-rose-600 active:scale-98"
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
