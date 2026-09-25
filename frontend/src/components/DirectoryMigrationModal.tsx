import * as Dialog from '@radix-ui/react-dialog';
import { FolderSync, X, ArrowRight, Zap, RefreshCw } from 'lucide-react';
import { Button } from './ui/Button';

interface DirectoryMigrationModalProps {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  oldDir: string;
  newDir: string;
  onConfirm: (moveFiles: boolean) => Promise<void>;
  isMigrating: boolean;
}

export function DirectoryMigrationModal({
  open,
  onOpenChange,
  oldDir,
  newDir,
  onConfirm,
  isMigrating,
}: DirectoryMigrationModalProps) {
  return (
    <Dialog.Root open={open} onOpenChange={(val) => !isMigrating && onOpenChange(val)}>
      <Dialog.Portal>
        <Dialog.Overlay className="fixed inset-0 z-50 bg-black/60 backdrop-blur-xs transition-opacity duration-200" />
        <Dialog.Content className="fixed top-1/2 left-1/2 z-50 w-full max-w-md -translate-x-1/2 -translate-y-1/2 rounded-2xl border border-[var(--border-subtle)] bg-[var(--bg-card)] p-6 shadow-2xl transition-all duration-200">
          <div className="flex items-start justify-between gap-3">
            <div className="flex items-center gap-2.5">
              <div className="flex h-9 w-9 items-center justify-center rounded-xl bg-[var(--accent)]/15 text-[var(--accent)]">
                <FolderSync className="h-5 w-5" />
              </div>
              <div>
                <Dialog.Title className="text-sm font-semibold text-[var(--text-primary)]">
                  迁移已下载文件到新目录
                </Dialog.Title>
                <Dialog.Description className="mt-0.5 text-xs text-[var(--text-muted)]">
                  检测到原下载目录存在文件，是否同步移动？
                </Dialog.Description>
              </div>
            </div>

            {!isMigrating && (
              <Dialog.Close asChild>
                <button
                  type="button"
                  className="rounded-lg p-1 text-[var(--text-muted)] hover:bg-[var(--bg-subtle)] hover:text-[var(--text-primary)]"
                >
                  <X className="h-4 w-4" />
                </button>
              </Dialog.Close>
            )}
          </div>

          {/* Path comparison box */}
          <div className="mt-4 space-y-2 rounded-xl border border-[var(--border-subtle)] bg-[var(--bg-subtle)]/60 p-3.5 text-xs">
            <div className="space-y-1">
              <div className="text-[11px] font-medium text-[var(--text-muted)]">原保存位置：</div>
              <div className="rounded border border-[var(--border-subtle)] bg-[var(--bg-card)] px-2.5 py-1.5 font-mono text-[11px] break-all text-[var(--text-secondary)]">
                {oldDir}
              </div>
            </div>

            <div className="flex justify-center py-0.5">
              <ArrowRight className="h-4 w-4 text-[var(--text-muted)]" />
            </div>

            <div className="space-y-1">
              <div className="text-[11px] font-medium text-[var(--text-muted)]">新保存位置：</div>
              <div className="rounded border border-[var(--border-focus)] bg-[var(--bg-card)] px-2.5 py-1.5 font-mono text-[11px] font-semibold break-all text-[var(--accent)]">
                {newDir}
              </div>
            </div>
          </div>

          <div className="mt-3.5 flex items-start gap-2 rounded-lg bg-[var(--accent)]/10 p-2.5 text-[11px] text-[var(--accent)]">
            <Zap className="mt-0.5 h-4 w-4 shrink-0" />
            <span>
              优先采用操作系统原生指针移动（同盘符/分区瞬间完成）；若跨盘则安全转存并清理旧文件。
            </span>
          </div>

          {/* Action buttons */}
          <div className="mt-6 flex flex-col gap-2">
            <Button
              variant="primary"
              size="md"
              onClick={() => void onConfirm(true)}
              disabled={isMigrating}
              className="w-full gap-2 text-xs"
            >
              {isMigrating ? (
                <>
                  <RefreshCw className="h-3.5 w-3.5 animate-spin" />
                  <span>正在移动文件...</span>
                </>
              ) : (
                <span>同步移动已有文件到新目录</span>
              )}
            </Button>

            <div className="flex items-center gap-2">
              <Button
                variant="secondary"
                size="sm"
                onClick={() => !isMigrating && onOpenChange(false)}
                disabled={isMigrating}
                className="flex-1 text-xs"
              >
                取消
              </Button>
              <Button
                variant="secondary"
                size="sm"
                onClick={() => void onConfirm(false)}
                disabled={isMigrating}
                className="flex-1 text-xs text-[var(--text-secondary)]"
              >
                仅修改路径 (不移动)
              </Button>
            </div>
          </div>
        </Dialog.Content>
      </Dialog.Portal>
    </Dialog.Root>
  );
}
