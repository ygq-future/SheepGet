import { useState, type SyntheticEvent } from 'react';
import * as Dialog from '@radix-ui/react-dialog';
import { X, RefreshCw, AlertTriangle, CheckCircle2, Link2, KeyRound } from 'lucide-react';
import type { task } from '../../wailsjs/go/models';
import {
  CheckURLConsistency,
  UpdateTaskURL,
  ResetAndDownloadWithNewURL,
} from '../../wailsjs/go/main/App';

interface UpdateLinkModalProps {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  task: task.Task | null;
  onUpdated?: () => void;
}

/** Serializes request headers as one "Name: value" line each, which is how users copy them out of devtools. */
function parseHeaderLines(text: string): Record<string, string> {
  const headers: Record<string, string> = {};
  for (const line of text.split('\n')) {
    const separator = line.indexOf(':');
    if (separator <= 0) continue;
    const name = line.slice(0, separator).trim();
    const value = line.slice(separator + 1).trim();
    if (name) headers[name] = value;
  }
  return headers;
}

function formatHeaderLines(headers?: Record<string, string>): string {
  if (!headers) return '';
  return Object.entries(headers)
    .map(([name, value]) => `${name}: ${value}`)
    .join('\n');
}

export function UpdateLinkModal({
  open,
  onOpenChange,
  task: currentTask,
  onUpdated,
}: UpdateLinkModalProps) {
  const [newUrl, setNewUrl] = useState('');
  const [headerText, setHeaderText] = useState(() =>
    formatHeaderLines(currentTask?.requestHeaders),
  );
  const [checking, setChecking] = useState(false);
  const [updating, setUpdating] = useState(false);
  const [inconsistentReason, setInconsistentReason] = useState<string | null>(null);
  const [error, setError] = useState('');

  if (!currentTask) return null;

  const requestHeaders = () => parseHeaderLines(headerText);

  const handleCheckAndApply = async (e: SyntheticEvent) => {
    e.preventDefault();
    if (!newUrl.trim()) {
      setError('请输入新的下载链接');
      return;
    }
    setError('');
    setInconsistentReason(null);
    setChecking(true);

    try {
      const headers = requestHeaders();
      const consistency = await CheckURLConsistency(currentTask.id, newUrl.trim(), headers);
      if (consistency.consistent) {
        setUpdating(true);
        // Adopting the verified link continues the download from the existing progress.
        await UpdateTaskURL(currentTask.id, newUrl.trim(), headers);
        onOpenChange(false);
        onUpdated?.();
      } else {
        setInconsistentReason(consistency.reason || '文件版本或大小与原下载不一致');
      }
    } catch (err) {
      setError(err instanceof Error ? err.message : '检查链接失败');
    } finally {
      setChecking(false);
      setUpdating(false);
    }
  };

  const handleForceReset = async () => {
    setError('');
    setUpdating(true);
    try {
      await ResetAndDownloadWithNewURL(currentTask.id, newUrl.trim(), requestHeaders());
      onOpenChange(false);
      onUpdated?.();
    } catch (err) {
      setError(err instanceof Error ? err.message : '重新下载失败');
    } finally {
      setUpdating(false);
    }
  };

  return (
    <Dialog.Root open={open} onOpenChange={onOpenChange}>
      <Dialog.Portal>
        <Dialog.Overlay className="animate-in fade-in fixed inset-0 z-50 bg-black/80 backdrop-blur-sm" />
        <Dialog.Content className="animate-in fade-in zoom-in-95 fixed top-1/2 left-1/2 z-50 w-full max-w-lg -translate-x-1/2 -translate-y-1/2 rounded-2xl border border-white/10 bg-zinc-950 p-6 shadow-2xl focus:outline-hidden">
          <div className="flex items-center justify-between border-b border-white/[0.06] pb-4">
            <div className="flex items-center gap-2.5">
              <div className="flex h-8 w-8 items-center justify-center rounded-xl border border-sky-500/20 bg-sky-500/10 text-sky-400">
                <RefreshCw className="h-4 w-4" />
              </div>
              <div>
                <Dialog.Title className="text-sm font-semibold text-zinc-100">
                  更新下载链接
                </Dialog.Title>
                <p className="text-[11px] text-zinc-400">
                  用于恢复失效或过期的临时鉴权链接，自动校验一致性
                </p>
              </div>
            </div>
            <Dialog.Close className="rounded-lg p-1.5 text-zinc-400 hover:bg-white/5 hover:text-zinc-200">
              <X className="h-4 w-4" />
            </Dialog.Close>
          </div>

          <form
            onSubmit={(e) => {
              void handleCheckAndApply(e);
            }}
            className="mt-5 space-y-4"
          >
            {error && (
              <div className="flex items-center gap-2 rounded-xl border border-rose-500/20 bg-rose-500/10 p-3 text-xs text-rose-300">
                <AlertTriangle className="h-4 w-4 shrink-0 text-rose-400" />
                <span>{error}</span>
              </div>
            )}

            <div className="space-y-1.5">
              <label className="text-[11px] font-medium text-zinc-400 uppercase">当前文件名</label>
              <div className="rounded-xl border border-white/5 bg-zinc-900/50 px-3.5 py-2 font-mono text-xs text-zinc-300">
                {currentTask.filename}
              </div>
            </div>

            <div className="space-y-1.5">
              <label className="flex items-center gap-1.5 text-[11px] font-medium text-zinc-400 uppercase">
                <Link2 className="h-3.5 w-3.5 text-zinc-500" />
                新下载链接 (URL)
              </label>
              <input
                type="text"
                value={newUrl}
                onChange={(e) => setNewUrl(e.target.value)}
                placeholder="请输入有效的新下载链接"
                className="w-full rounded-xl border border-white/10 bg-zinc-900/80 px-3.5 py-2.5 text-xs text-zinc-100 placeholder-zinc-500 focus:border-sky-500/50 focus:ring-2 focus:ring-sky-500/20 focus:outline-hidden"
                autoFocus
              />
            </div>

            <div className="space-y-1.5">
              <label className="flex items-center gap-1.5 text-[11px] font-medium text-zinc-400 uppercase">
                <KeyRound className="h-3.5 w-3.5 text-zinc-500" />
                请求信息（可选，每行一条 Header: value）
              </label>
              <textarea
                value={headerText}
                onChange={(e) => setHeaderText(e.target.value)}
                rows={3}
                placeholder={'Referer: https://example.com/watch\nCookie: session=...'}
                className="w-full resize-none rounded-xl border border-white/10 bg-zinc-900/80 px-3.5 py-2.5 font-mono text-xs text-zinc-100 placeholder-zinc-500 focus:border-sky-500/50 focus:ring-2 focus:ring-sky-500/20 focus:outline-hidden"
              />
              <p className="text-[11px] text-zinc-500">
                过期链接常需要 Referer 或 Cookie；这里填写的信息会用于后续探测与传输。
              </p>
            </div>

            {inconsistentReason && (
              <div className="space-y-3 rounded-xl border border-amber-500/30 bg-amber-500/10 p-4">
                <div className="flex items-start gap-2.5 text-xs text-amber-300">
                  <AlertTriangle className="mt-0.5 h-4 w-4 shrink-0 text-amber-400" />
                  <div className="space-y-1">
                    <p className="font-semibold text-amber-200">文件一致性校验未通过</p>
                    <p className="text-[11px] text-amber-300/90">{inconsistentReason}</p>
                    <p className="text-[11px] text-zinc-400">
                      若继续断点续传可能导致成品损坏，建议放弃历史分块并重新下载。
                    </p>
                  </div>
                </div>
                <div className="flex items-center justify-end gap-2 pt-1">
                  <button
                    type="button"
                    onClick={() => onOpenChange(false)}
                    className="rounded-lg border border-white/10 px-3 py-1.5 text-xs text-zinc-300 hover:bg-white/5"
                  >
                    取消
                  </button>
                  <button
                    type="button"
                    onClick={() => {
                      void handleForceReset();
                    }}
                    className="rounded-lg bg-amber-500 px-3 py-1.5 text-xs font-semibold text-zinc-950 hover:bg-amber-400"
                  >
                    {updating ? '正在重置...' : '重置并重新下载'}
                  </button>
                </div>
              </div>
            )}

            {!inconsistentReason && (
              <div className="flex items-center justify-end gap-2.5 border-t border-white/[0.06] pt-3">
                <button
                  type="button"
                  onClick={() => onOpenChange(false)}
                  className="rounded-xl border border-white/10 px-4 py-2 text-xs font-medium text-zinc-300 hover:bg-white/5 hover:text-zinc-100"
                >
                  取消
                </button>
                <button
                  type="submit"
                  disabled={checking || updating || !newUrl.trim()}
                  className="flex items-center gap-1.5 rounded-xl bg-sky-500 px-5 py-2 text-xs font-semibold text-zinc-950 shadow-lg shadow-sky-500/20 hover:bg-sky-400 disabled:opacity-50"
                >
                  {checking ? (
                    '正在校验一致性...'
                  ) : updating ? (
                    '正在更新...'
                  ) : (
                    <>
                      <CheckCircle2 className="h-3.5 w-3.5" />
                      校验并更新
                    </>
                  )}
                </button>
              </div>
            )}
          </form>
        </Dialog.Content>
      </Dialog.Portal>
    </Dialog.Root>
  );
}
