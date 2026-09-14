import { useState, useEffect, useRef, type SyntheticEvent } from 'react';
import * as Dialog from '@radix-ui/react-dialog';
import {
  X,
  Folder,
  Link as LinkIcon,
  Cpu,
  DownloadCloud,
  FileText,
  AlertCircle,
  Copy,
  RotateCcw,
  Play,
  CheckCircle2,
} from 'lucide-react';
import { Select, type SelectOption } from './ui/Select';
import { Checkbox } from './ui/Checkbox';
import { formatBytes } from '../lib/format';
import type { task, engine } from '../../wailsjs/go/models';
import {
  ProbeURL,
  CheckFileConflict,
  ResolveDuplicate,
  StartPreDownload,
  ConfirmPreDownload,
  CancelPreDownload,
  AddTask,
  SelectDirectory,
} from '../../wailsjs/go/main/App';
import { EventsOn, EventsOff } from '../../wailsjs/runtime/runtime';

interface FileInfoModalProps {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  defaultDir: string;
  onTasksChanged?: () => void;
}

type ConflictQuestion = {
  suggestedFilename: string;
  /** Whether the answer continues into a pre-download or into the final confirmation. */
  purpose: 'prestart' | 'confirm';
};

const CONCURRENCY_OPTIONS: SelectOption<number>[] = [
  { value: 1, label: '1 通道', description: '单流保守下载' },
  { value: 2, label: '2 通道', description: '双路平衡分块' },
  { value: 4, label: '4 通道 (推荐)', description: '推荐主力性能' },
  { value: 8, label: '8 通道', description: '高速宽带并发' },
  { value: 16, label: '16 通道', description: '极限多流冲刺' },
];

export function FileInfoModal({
  open,
  onOpenChange,
  defaultDir,
  onTasksChanged,
}: FileInfoModalProps) {
  const [url, setUrl] = useState('');
  const [dir, setDir] = useState('');
  const [filename, setFilename] = useState('');
  const [maxConn, setMaxConn] = useState(4);
  const [preDownload, setPreDownload] = useState(false);

  const [probing, setProbing] = useState(false);
  const [probeResult, setProbeResult] = useState<engine.ProbeResult | null>(null);
  const [preTask, setPreTask] = useState<task.Task | null>(null);
  const [duplicateTask, setDuplicateTask] = useState<task.Task | null>(null);
  const [conflict, setConflict] = useState<ConflictQuestion | null>(null);

  const [loading, setLoading] = useState(false);
  const [error, setError] = useState('');

  const currentDir = dir || defaultDir;
  const probeTimeoutRef = useRef<number | null>(null);
  /** URL that the running pre-download was started for, so an edited URL cannot confirm stale data. */
  const preTaskUrlRef = useRef<string | null>(null);
  /** Set once the user types a save name, so probing stops overwriting their choice. */
  const nameEditedRef = useRef(false);
  /** Latest dialog state, readable from async probe/pre-download callbacks without stale closures. */
  const latestRef = useRef({ filename, maxConn, currentDir, preDownload, duplicateTask, preTask });

  useEffect(() => {
    latestRef.current = { filename, maxConn, currentDir, preDownload, duplicateTask, preTask };
  });

  const resetDialogState = () => {
    setProbeResult(null);
    setDuplicateTask(null);
    setPreTask(null);
    setConflict(null);
    setError('');
    preTaskUrlRef.current = null;
    nameEditedRef.current = false;
  };

  const closeDialog = () => {
    onOpenChange(false);
  };

  const handleCancel = async () => {
    if (preTask) {
      try {
        await CancelPreDownload(preTask.id);
      } catch (err) {
        console.warn('CancelPreDownload error:', err);
      }
      preTaskUrlRef.current = null;
    }
    resetDialogState();
    closeDialog();
    onTasksChanged?.();
  };

  const handleOpenChange = (nextOpen: boolean) => {
    if (nextOpen) {
      onOpenChange(true);
      return;
    }
    void handleCancel();
  };

  // Live pre-download progress: the Go backend stays the single source of truth for task state.
  useEffect(() => {
    if (!open) return;
    const onTaskUpdated = (updatedTask: task.Task) => {
      if (updatedTask.id === preTask?.id) setPreTask(updatedTask);
    };
    EventsOn('task:updated', onTaskUpdated);
    return () => {
      EventsOff('task:updated');
    };
  }, [open, preTask?.id]);

  // Probe metadata for the current URL without touching the body.
  useEffect(() => {
    if (!open) return;
    window.clearTimeout(probeTimeoutRef.current ?? undefined);

    const trimmed = url.trim();
    probeTimeoutRef.current = window.setTimeout(() => {
      if (!trimmed.startsWith('http')) {
        setProbeResult(null);
        setDuplicateTask(null);
        return;
      }
      void (async () => {
        setProbing(true);
        setError('');
        try {
          const result = await ProbeURL(trimmed);
          setProbeResult(result);
          setDuplicateTask(result.duplicateTask ?? null);
          setFilename((current) => current || result.filename || '');
        } catch (err) {
          // Metadata is optional: an unreachable probe still allows a manual download.
          setProbeResult(null);
          setDuplicateTask(null);
          console.warn('Probe error:', err);
        } finally {
          setProbing(false);
        }
      })();
    }, 300);

    return () => {
      window.clearTimeout(probeTimeoutRef.current ?? undefined);
    };
  }, [open, url]);

  const startPreDownload = async (targetName: string, targetDir: string, targetUrl: string) => {
    const state = latestRef.current;
    if (!targetName || state.preTask) return;
    try {
      const started = await StartPreDownload(targetUrl, targetDir, targetName, state.maxConn);
      preTaskUrlRef.current = targetUrl;
      setPreTask(started);
    } catch (err) {
      setError(err instanceof Error ? err.message : '提前下载启动失败');
    }
  };

  /**
   * Starts the background transfer only when no existing file would be silently replaced: an
   * existing target file becomes a question instead, so overwriting stays the user's explicit choice.
   * enabled is passed in because a just-toggled switch is not yet visible in latestRef.
   */
  const tryStartPreDownload = async (
    probed: engine.ProbeResult | null,
    targetUrl: string,
    enabled: boolean,
  ) => {
    const state = latestRef.current;
    if (!enabled || state.preTask || state.duplicateTask) return;
    if (!targetUrl.trim().startsWith('http') || !probed) return;

    const targetName = (state.filename.trim() || probed.filename || '').trim();
    if (!targetName) return;

    try {
      const existing = await CheckFileConflict(state.currentDir, targetName);
      if (existing.exists) {
        setConflict({ suggestedFilename: existing.suggestedFilename, purpose: 'prestart' });
        return;
      }
    } catch (err) {
      console.warn('Conflict check error:', err);
    }
    await startPreDownload(targetName, state.currentDir, targetUrl);
  };

  // Probe metadata for the current URL without touching the body.
  useEffect(() => {
    if (!open) return;
    window.clearTimeout(probeTimeoutRef.current ?? undefined);

    const trimmed = url.trim();
    probeTimeoutRef.current = window.setTimeout(() => {
      if (!trimmed.startsWith('http')) {
        setProbeResult(null);
        setDuplicateTask(null);
        return;
      }
      void (async () => {
        setProbing(true);
        setError('');
        try {
          const result = await ProbeURL(trimmed);
          setProbeResult(result);
          setDuplicateTask(result.duplicateTask ?? null);
          // Follow the probed name so an edited URL never keeps the previous resource's name,
          // unless the user typed a name of their own.
          if (!nameEditedRef.current) setFilename(result.filename || '');
          await tryStartPreDownload(result, trimmed, latestRef.current.preDownload);
        } catch (err) {
          // Metadata is optional: an unreachable probe still allows a manual download.
          setProbeResult(null);
          setDuplicateTask(null);
          console.warn('Probe error:', err);
        } finally {
          setProbing(false);
        }
      })();
    }, 300);

    return () => {
      window.clearTimeout(probeTimeoutRef.current ?? undefined);
    };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [open, url]);

  // A pre-download belongs to one URL: editing the URL discards the stale transfer instead of
  // letting confirmation save a different resource under the new link's name.
  useEffect(() => {
    if (!preTask || preTaskUrlRef.current === null) return;
    if (preTaskUrlRef.current === url.trim()) return;
    void (async () => {
      try {
        await CancelPreDownload(preTask.id);
      } catch (err) {
        console.warn('CancelPreDownload error:', err);
      }
      preTaskUrlRef.current = null;
      setPreTask(null);
      setConflict(null);
    })();
  }, [url, preTask]);

  const handlePreDownloadToggle = (enabled: boolean) => {
    setPreDownload(enabled);
    if (enabled) {
      void tryStartPreDownload(probeResult, url.trim(), enabled);
    }
  };

  const handleSelectFolder = async () => {
    try {
      const selected = await SelectDirectory();
      if (selected) setDir(selected);
    } catch (err) {
      console.warn('Folder selection error:', err);
    }
  };

  const executeConfirm = async (finalFilename: string) => {
    setLoading(true);
    setError('');
    try {
      if (preTask) {
        await ConfirmPreDownload(preTask.id, currentDir, finalFilename, maxConn);
      } else {
        await AddTask(url.trim(), currentDir, finalFilename, maxConn);
      }
      resetDialogState();
      setUrl('');
      setFilename('');
      closeDialog();
      onTasksChanged?.();
    } catch (err) {
      setError(err instanceof Error ? err.message : '确认下载失败');
    } finally {
      setLoading(false);
    }
  };

  const handleSubmit = async (e: SyntheticEvent) => {
    e.preventDefault();
    if (!url.trim()) {
      setError('请输入有效的下载链接');
      return;
    }
    const finalName = (filename.trim() || probeResult?.filename || 'download.bin').trim();

    try {
      const existing = await CheckFileConflict(currentDir, finalName);
      if (existing.exists) {
        setConflict({ suggestedFilename: existing.suggestedFilename, purpose: 'confirm' });
        return;
      }
    } catch (err) {
      console.warn('Conflict check error:', err);
    }
    await executeConfirm(finalName);
  };

  /** Applies the user's explicit answer to an existing same-name file. */
  const resolveConflict = async (choice: 'overwrite' | 'numbered') => {
    if (!conflict) return;
    const chosenName =
      choice === 'numbered'
        ? conflict.suggestedFilename
        : filename.trim() || probeResult?.filename || '';
    const purpose = conflict.purpose;
    setConflict(null);
    if (choice === 'numbered') setFilename(chosenName);

    if (purpose === 'prestart') {
      await startPreDownload(chosenName.trim(), currentDir, url.trim());
      return;
    }
    await executeConfirm(chosenName.trim());
  };

  const handleResolveDuplicate = async (
    strategy: 'continue' | 'redownload' | 'copy' | 'show_completed',
  ) => {
    if (!duplicateTask) return;
    setLoading(true);
    setError('');
    try {
      await ResolveDuplicate(duplicateTask.id, strategy, currentDir, filename.trim(), maxConn);
      resetDialogState();
      setUrl('');
      setFilename('');
      closeDialog();
      onTasksChanged?.();
    } catch (err) {
      setError(err instanceof Error ? err.message : '处理重复任务失败');
    } finally {
      setLoading(false);
    }
  };

  const preTaskPercent =
    preTask && preTask.totalBytes > 0
      ? Math.min(100, Math.round((preTask.downloaded / preTask.totalBytes) * 100))
      : 0;

  return (
    <Dialog.Root open={open} onOpenChange={handleOpenChange}>
      <Dialog.Portal>
        <Dialog.Overlay className="animate-in fade-in fixed inset-0 z-50 bg-black/80 backdrop-blur-md" />
        <Dialog.Content
          onEscapeKeyDown={(e) => {
            e.preventDefault();
            void handleCancel();
          }}
          className="animate-in fade-in zoom-in-95 fixed top-1/2 left-1/2 z-50 w-full max-w-xl -translate-x-1/2 -translate-y-1/2 rounded-2xl border border-white/10 bg-zinc-950 p-6 shadow-2xl backdrop-blur-2xl focus:outline-hidden"
        >
          {/* Header */}
          <div className="flex items-center justify-between border-b border-white/[0.06] pb-4">
            <div className="flex items-center gap-2.5">
              <div className="flex h-8 w-8 items-center justify-center rounded-xl border border-emerald-500/20 bg-emerald-500/10 text-emerald-400">
                <DownloadCloud className="h-4 w-4" />
              </div>
              <div>
                <Dialog.Title className="text-sm font-semibold tracking-tight text-zinc-100">
                  文件信息
                </Dialog.Title>
                <p className="text-[11px] text-zinc-400">核对资源信息与保存位置，确认后开始下载</p>
              </div>
            </div>
            <button
              type="button"
              onClick={() => void handleCancel()}
              className="rounded-lg p-1.5 text-zinc-400 hover:bg-white/5 hover:text-zinc-200"
            >
              <X className="h-4 w-4" />
            </button>
          </div>

          <form
            onSubmit={(e) => {
              void handleSubmit(e);
            }}
            className="mt-5 space-y-4"
          >
            {error && (
              <div className="flex items-center gap-2 rounded-xl border border-rose-500/20 bg-rose-500/10 p-3 text-xs text-rose-300">
                <AlertCircle className="h-4 w-4 shrink-0 text-rose-400" />
                <span>{error}</span>
              </div>
            )}

            {/* Duplicate link */}
            {duplicateTask && (
              <div className="space-y-2.5 rounded-xl border border-amber-500/30 bg-amber-500/10 p-3.5 text-xs text-amber-200">
                <div className="flex items-center gap-2 font-medium">
                  <AlertCircle className="h-4 w-4 text-amber-400" />
                  <span>
                    该链接已有下载任务（状态: {duplicateTask.status}，文件: {duplicateTask.filename}
                    ）
                  </span>
                </div>
                <p className="text-[11px] text-zinc-400">请选择处理方式：</p>
                <div className="flex flex-wrap items-center gap-2 pt-1">
                  {duplicateTask.status === 'completed' ? (
                    <button
                      type="button"
                      onClick={() => void handleResolveDuplicate('show_completed')}
                      className="flex items-center gap-1.5 rounded-lg border border-emerald-500/30 bg-emerald-500/20 px-2.5 py-1 text-xs text-emerald-100 hover:bg-emerald-500/30"
                    >
                      <CheckCircle2 className="h-3 w-3" />
                      查看已完成任务
                    </button>
                  ) : (
                    <button
                      type="button"
                      onClick={() => void handleResolveDuplicate('continue')}
                      className="flex items-center gap-1.5 rounded-lg border border-amber-500/30 bg-amber-500/20 px-2.5 py-1 text-xs text-amber-100 hover:bg-amber-500/30"
                    >
                      <Play className="h-3 w-3" />
                      继续已有任务
                    </button>
                  )}
                  <button
                    type="button"
                    onClick={() => void handleResolveDuplicate('copy')}
                    className="flex items-center gap-1.5 rounded-lg border border-white/10 bg-zinc-800 px-2.5 py-1 text-xs text-zinc-200 hover:bg-zinc-700"
                  >
                    <Copy className="h-3 w-3" />
                    保存为序号副本
                  </button>
                  <button
                    type="button"
                    onClick={() => void handleResolveDuplicate('redownload')}
                    className="flex items-center gap-1.5 rounded-lg border border-white/10 bg-zinc-800 px-2.5 py-1 text-xs text-zinc-200 hover:bg-zinc-700"
                  >
                    <RotateCcw className="h-3 w-3" />
                    重新下载
                  </button>
                </div>
              </div>
            )}

            {/* Same-name file conflict: overwriting is only ever the user's explicit choice. */}
            {conflict && (
              <div className="space-y-2.5 rounded-xl border border-sky-500/30 bg-sky-500/10 p-3.5 text-xs text-sky-200">
                <div className="flex items-center gap-2 font-medium">
                  <FileText className="h-4 w-4 text-sky-400" />
                  <span>目标目录已存在同名文件，需要您决定如何处理</span>
                </div>
                <div className="flex flex-wrap items-center gap-2 pt-1">
                  <button
                    type="button"
                    onClick={() => void resolveConflict('numbered')}
                    className="rounded-lg bg-sky-500 px-3 py-1 text-xs font-semibold text-zinc-950 hover:bg-sky-400"
                  >
                    添加序号并保存为 {conflict.suggestedFilename}
                  </button>
                  <button
                    type="button"
                    onClick={() => void resolveConflict('overwrite')}
                    className="rounded-lg border border-white/10 bg-zinc-800 px-3 py-1 text-xs text-zinc-200 hover:bg-zinc-700"
                  >
                    覆盖现有文件
                  </button>
                  <button
                    type="button"
                    onClick={() => setConflict(null)}
                    className="rounded-lg border border-white/10 px-3 py-1 text-xs text-zinc-400 hover:bg-white/5"
                  >
                    暂不处理
                  </button>
                </div>
              </div>
            )}

            <div className="space-y-1.5">
              <label className="flex items-center gap-1.5 text-[11px] font-medium tracking-wide text-zinc-400 uppercase">
                <LinkIcon className="h-3.5 w-3.5 text-zinc-500" />
                资源链接 (URL)
              </label>
              <input
                type="text"
                value={url}
                onChange={(e) => setUrl(e.target.value)}
                placeholder="https://example.com/file.zip"
                className="w-full rounded-xl border border-white/10 bg-zinc-900/80 px-3.5 py-2.5 text-xs text-zinc-100 placeholder-zinc-500 focus:border-emerald-500/50 focus:ring-2 focus:ring-emerald-500/20 focus:outline-hidden"
                autoFocus
              />
            </div>

            <div className="grid grid-cols-3 gap-2.5 rounded-xl border border-white/5 bg-zinc-900/50 p-3 text-xs">
              <div>
                <span className="text-[10px] text-zinc-500 uppercase">大小</span>
                <p className="font-mono font-medium text-zinc-200">
                  {probing ? '正在获取...' : formatBytes(probeResult?.totalBytes ?? -1)}
                </p>
              </div>
              <div>
                <span className="text-[10px] text-zinc-500 uppercase">类型</span>
                <p className="truncate font-mono font-medium text-zinc-300">
                  {probeResult?.contentType || '未知'}
                </p>
              </div>
              <div>
                <span className="text-[10px] text-zinc-500 uppercase">断点续传</span>
                <p className="font-medium text-zinc-300">
                  {probeResult ? (probeResult.resumable ? '支持' : '不支持') : '未知'}
                </p>
              </div>
            </div>

            <div className="grid grid-cols-2 gap-3.5">
              <div className="space-y-1.5">
                <label className="text-[11px] font-medium tracking-wide text-zinc-400 uppercase">
                  保存名称
                </label>
                <input
                  type="text"
                  value={filename}
                  onChange={(e) => {
                    nameEditedRef.current = true;
                    setFilename(e.target.value);
                  }}
                  placeholder={probeResult?.filename || '请输入保存文件名'}
                  className="w-full rounded-xl border border-white/10 bg-zinc-900/80 px-3.5 py-2 text-xs text-zinc-100 placeholder-zinc-500 focus:border-emerald-500/50 focus:ring-2 focus:ring-emerald-500/20 focus:outline-hidden"
                />
              </div>

              <div className="space-y-1.5">
                <label className="flex items-center gap-1 text-[11px] font-medium tracking-wide text-zinc-400 uppercase">
                  <Cpu className="h-3.5 w-3.5 text-zinc-500" />
                  单任务连接数
                </label>
                <Select
                  value={maxConn}
                  onChange={setMaxConn}
                  options={CONCURRENCY_OPTIONS}
                  className="rounded-xl py-2"
                />
              </div>
            </div>

            <div className="space-y-1.5">
              <label className="flex items-center justify-between text-[11px] font-medium tracking-wide text-zinc-400 uppercase">
                <span className="flex items-center gap-1.5">
                  <Folder className="h-3.5 w-3.5 text-zinc-500" />
                  保存目录
                </span>
                <button
                  type="button"
                  onClick={() => {
                    void handleSelectFolder();
                  }}
                  className="text-emerald-400 hover:text-emerald-300 hover:underline"
                >
                  浏览...
                </button>
              </label>
              <input
                type="text"
                value={currentDir}
                onChange={(e) => setDir(e.target.value)}
                className="w-full rounded-xl border border-white/10 bg-zinc-900/80 px-3.5 py-2 text-xs text-zinc-100 placeholder-zinc-500 focus:border-emerald-500/50 focus:ring-2 focus:ring-emerald-500/20 focus:outline-hidden"
              />
            </div>

            <div className="space-y-2 rounded-xl border border-white/5 bg-zinc-900/40 p-3">
              <Checkbox
                id="pre-download-switch"
                checked={preDownload}
                onCheckedChange={handlePreDownloadToggle}
                label="显示文件信息时立即下载"
                description="对话框打开期间先开始传输，仍可修改名称与目录"
              />

              {preTask && (
                <div className="space-y-1 pt-1 text-[11px]">
                  <div className="flex items-center justify-between text-zinc-400">
                    <span className="flex items-center gap-1 text-sky-400">
                      <DownloadCloud className="h-3 w-3 animate-pulse" />
                      {preTask.status === 'completed' ? '已提前完成' : '后台传输中'}
                    </span>
                    <span className="font-mono">
                      {formatBytes(preTask.downloaded)} / {formatBytes(preTask.totalBytes)} (
                      {preTaskPercent}%)
                    </span>
                  </div>
                  <div className="h-1.5 w-full overflow-hidden rounded-full bg-zinc-800">
                    <div
                      className="h-full bg-sky-400 transition-all duration-300"
                      style={{ width: `${preTaskPercent}%` }}
                    />
                  </div>
                </div>
              )}
            </div>

            <div className="flex items-center justify-end gap-2.5 border-t border-white/[0.06] pt-3">
              <button
                type="button"
                onClick={() => void handleCancel()}
                className="rounded-xl border border-white/10 px-4 py-2 text-xs font-medium text-zinc-300 hover:bg-white/5 hover:text-zinc-100"
              >
                取消
              </button>
              <button
                type="submit"
                disabled={loading}
                className="flex items-center gap-1.5 rounded-xl bg-emerald-500 px-5 py-2 text-xs font-semibold text-zinc-950 shadow-lg shadow-emerald-500/20 hover:bg-emerald-400 disabled:opacity-50"
              >
                {loading
                  ? '正在处理...'
                  : preTask?.status === 'completed'
                    ? '确认并使用成品'
                    : '确认下载'}
              </button>
            </div>
          </form>
        </Dialog.Content>
      </Dialog.Portal>
    </Dialog.Root>
  );
}
