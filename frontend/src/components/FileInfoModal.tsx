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
import { Switch } from './ui/Switch';
import { formatBytes } from '../lib/format';
import * as task from '../../bindings/sheep-get/internal/task/models';
import type * as engine from '../../bindings/sheep-get/internal/engine/models';
import {
  ProbeURL,
  CheckFileConflict,
  ResolveDuplicate,
  StartPreDownload,
  ConfirmPreDownload,
  CancelPreDownload,
  AddTask,
  SelectDirectory,
} from '../../bindings/sheep-get/app';
import { Events } from '@wailsio/runtime';
import { unwrapEventData } from '../lib/utils';
import { useSettingsStore } from '../stores/settings';
interface FileInfoModalProps {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  defaultDir: string;
  onTasksChanged?: () => void;
}

const CONCURRENCY_OPTIONS: SelectOption<number>[] = [
  { value: 1, label: '1 通道 (单线程)' },
  { value: 2, label: '2 通道' },
  { value: 4, label: '4 通道 (推荐)' },
  { value: 8, label: '8 通道 (快速)' },
  { value: 16, label: '16 通道 (极速)' },
  { value: 32, label: '32 通道 (最大)' },
];

export function FileInfoModal({
  open,
  onOpenChange,
  defaultDir,
  onTasksChanged,
}: FileInfoModalProps) {
  const { settings } = useSettingsStore();
  const defaultConn = settings?.download?.defaultConnectionsPerTask || 8;
  const defaultPreDownload = !!settings?.download?.preDownload;

  const [url, setUrl] = useState('');
  const [filename, setFilename] = useState('');
  const [currentDir, setDir] = useState(defaultDir);
  const [maxConn, setMaxConn] = useState(defaultConn);
  const [preDownload, setPreDownload] = useState(defaultPreDownload);
  const [preTask, setPreTask] = useState<task.Task | null>(null);

  const [probing, setProbing] = useState(false);
  const [probeResult, setProbeResult] = useState<engine.ProbeResult | null>(null);
  const [duplicateTask, setDuplicateTask] = useState<task.Task | null>(null);
  const [conflict, setConflict] = useState<{ exists: boolean; suggestedFilename: string } | null>(
    null,
  );

  const [loading, setLoading] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const activePreTaskRef = useRef<task.Task | null>(null);
  const probeSeqRef = useRef(0);
  const nameEditedRef = useRef(false);
  const [lastDefaultDir, setLastDefaultDir] = useState(defaultDir);

  if (defaultDir !== lastDefaultDir) {
    setLastDefaultDir(defaultDir);
    setDir(defaultDir);
  }

  useEffect(() => {
    activePreTaskRef.current = preTask;
  }, [preTask]);
  // Keep preTask updated via events
  useEffect(() => {
    const onUpdated = (event: unknown) => {
      const updated = unwrapEventData<task.Task>(event);
      if (!updated) return;
      if (activePreTaskRef.current && updated.id === activePreTaskRef.current.id) {
        setPreTask(updated);
      }
    };

    const unsubscribe = Events.On('task:updated', onUpdated);
    return () => {
      unsubscribe();
    };
  }, []);

  // Probing logic
  const probe = async (targetUrl: string) => {
    const seq = ++probeSeqRef.current;
    setProbing(true);
    setError(null);
    try {
      const result = await ProbeURL(targetUrl);
      if (seq !== probeSeqRef.current) return;

      if (result) {
        setProbeResult(result);
        setDuplicateTask(result.duplicateTask || null);

        if (!nameEditedRef.current && result.filename) {
          setFilename(result.filename);
          void checkConflict(result.filename, currentDir);
        }
      }
    } catch (err) {
      if (seq !== probeSeqRef.current) return;
      setError(String(err));
    } finally {
      if (seq === probeSeqRef.current) {
        setProbing(false);
      }
    }
  };

  const checkConflict = async (name: string, dir: string) => {
    if (!name) return;
    try {
      const res = await CheckFileConflict(dir, name);
      if (res && res.exists) {
        setConflict(res);
      } else {
        setConflict(null);
      }
    } catch (err) {
      console.error('Failed to check file conflict:', err);
    }
  };

  const handleUrlBlur = () => {
    const trimmed = url.trim();
    if (!trimmed) return;

    void probe(trimmed);

    if (preDownload && !preTask) {
      void (async () => {
        try {
          const t = await StartPreDownload(trimmed, currentDir, filename, maxConn);
          setPreTask(t);
        } catch (err) {
          console.error('Failed to start pre-download:', err);
        }
      })();
    }
  };

  const handlePreDownloadToggle = (checked: boolean) => {
    setPreDownload(checked);
    if (checked && !preTask && url.trim()) {
      void (async () => {
        try {
          const t = await StartPreDownload(url.trim(), currentDir, filename, maxConn);
          setPreTask(t);
        } catch (err) {
          console.error('Failed to start pre-download:', err);
        }
      })();
    }
  };

  const handleSelectFolder = async () => {
    try {
      const selected = await SelectDirectory();
      if (selected) {
        setDir(selected);
        if (filename) {
          void checkConflict(filename, selected);
        }
      }
    } catch (err) {
      console.error('Failed to open directory picker:', err);
    }
  };

  const resolveConflict = (action: 'overwrite' | 'numbered') => {
    if (action === 'numbered' && conflict?.suggestedFilename) {
      setFilename(conflict.suggestedFilename);
      setConflict(null);
    } else {
      setConflict(null);
    }
  };

  const handleResolveDuplicate = async (strategy: string) => {
    if (!duplicateTask) return;
    setLoading(true);
    try {
      await ResolveDuplicate(duplicateTask.id, strategy, currentDir, filename, maxConn);
      onOpenChange(false);
      resetState();
      onTasksChanged?.();
    } catch (err) {
      setError(String(err));
    } finally {
      setLoading(false);
    }
  };

  const handleSubmit = async (e: SyntheticEvent) => {
    e.preventDefault();
    if (!url.trim()) {
      setError('请输入有效的资源链接');
      return;
    }

    setLoading(true);
    setError(null);

    try {
      if (preTask) {
        await ConfirmPreDownload(preTask.id, currentDir, filename, maxConn);
      } else {
        await AddTask(url.trim(), currentDir, filename, maxConn);
      }

      onOpenChange(false);
      resetState();
      onTasksChanged?.();
    } catch (err) {
      setError(String(err));
    } finally {
      setLoading(false);
    }
  };

  const handleCancel = async () => {
    if (preTask) {
      try {
        await CancelPreDownload(preTask.id);
      } catch (err) {
        console.error('Failed to cancel pre-download:', err);
      }
    }
    onOpenChange(false);
    resetState();
  };

  const resetState = () => {
    setUrl('');
    setFilename('');
    setDir(defaultDir);
    setMaxConn(settings?.download?.defaultConnectionsPerTask || 8);
    setPreDownload(!!settings?.download?.preDownload);
    setPreTask(null);
    setProbeResult(null);
    setDuplicateTask(null);
    setConflict(null);
    setError(null);
    nameEditedRef.current = false;
  };
  const handleOpenChange = (nextOpen: boolean) => {
    if (!nextOpen) {
      void handleCancel();
    } else {
      onOpenChange(true);
    }
  };

  const preTaskPercent =
    preTask && preTask.totalBytes > 0
      ? Math.min(100, Math.round((preTask.downloaded / preTask.totalBytes) * 100))
      : 0;

  return (
    <Dialog.Root open={open} onOpenChange={handleOpenChange}>
      <Dialog.Portal>
        <Dialog.Overlay className="animate-in fade-in fixed inset-0 z-50 bg-black/40 backdrop-blur-sm dark:bg-black/80 dark:backdrop-blur-md" />
        <Dialog.Content
          onEscapeKeyDown={(e) => {
            e.preventDefault();
            void handleCancel();
          }}
          className="animate-in fade-in zoom-in-95 fixed top-1/2 left-1/2 z-50 w-full max-w-xl -translate-x-1/2 -translate-y-1/2 rounded-2xl border border-[var(--border-subtle)] bg-[var(--bg-surface)] p-6 shadow-2xl backdrop-blur-2xl focus:outline-hidden"
        >
          {/* Header */}
          <div className="flex items-center justify-between border-b border-[var(--border-subtle)] pb-4">
            <div className="flex items-center gap-2.5">
              <div className="flex h-8 w-8 items-center justify-center rounded-xl border border-[var(--border-focus)] bg-[var(--accent-muted)] text-[var(--accent)]">
                <DownloadCloud className="h-4 w-4" />
              </div>
              <div>
                <Dialog.Title className="text-sm font-semibold tracking-tight text-[var(--text-primary)]">
                  文件信息
                </Dialog.Title>
                <p className="text-[11px] text-[var(--text-muted)]">
                  核对资源信息与保存位置，确认后开始下载
                </p>
              </div>
            </div>
            <button
              type="button"
              onClick={() => void handleCancel()}
              className="rounded-lg p-1.5 text-[var(--text-muted)] hover:bg-[var(--bg-subtle)] hover:text-[var(--text-primary)]"
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
              <div className="flex items-center gap-2 rounded-xl border border-rose-500/20 bg-rose-500/10 p-3 text-xs text-rose-500 dark:text-rose-300">
                <AlertCircle className="h-4 w-4 shrink-0 text-rose-500" />
                <span>{error}</span>
              </div>
            )}

            {/* Duplicate link */}
            {duplicateTask && (
              <div className="space-y-2.5 rounded-xl border border-amber-500/30 bg-amber-500/10 p-3.5 text-xs text-amber-700 dark:text-amber-200">
                <div className="flex items-center gap-2 font-medium">
                  <AlertCircle className="h-4 w-4 text-amber-500" />
                  <span>已存在完全相同的下载链接</span>
                </div>
                <div className="flex flex-wrap items-center gap-2 pt-1">
                  {duplicateTask.status === task.Status.StatusCompleted ? (
                    <button
                      type="button"
                      onClick={() => void handleResolveDuplicate('show_completed')}
                      className="flex items-center gap-1.5 rounded-lg border border-[var(--border-focus)] bg-[var(--accent-muted)] px-2.5 py-1 text-xs text-[var(--accent)] hover:opacity-90"
                    >
                      <CheckCircle2 className="h-3 w-3" />
                      查看已完成任务
                    </button>
                  ) : (
                    <button
                      type="button"
                      onClick={() => void handleResolveDuplicate('continue')}
                      className="flex items-center gap-1.5 rounded-lg border border-white/20 bg-white/10 px-2.5 py-1 text-xs text-zinc-100 hover:bg-white/20"
                    >
                      <Play className="h-3 w-3" />
                      继续已有任务
                    </button>
                  )}

                  <button
                    type="button"
                    onClick={() => void handleResolveDuplicate('overwrite')}
                    className="flex items-center gap-1.5 rounded-lg border border-amber-500/30 bg-amber-500/20 px-2.5 py-1 text-xs text-amber-100 hover:bg-amber-500/30"
                  >
                    <RotateCcw className="h-3 w-3" />
                    重新覆盖下载
                  </button>

                  <button
                    type="button"
                    onClick={() => void handleResolveDuplicate('copy')}
                    className="flex items-center gap-1.5 rounded-lg border border-white/20 bg-white/10 px-2.5 py-1 text-xs text-zinc-100 hover:bg-white/20"
                  >
                    <Copy className="h-3 w-3" />
                    建立序号副本
                  </button>
                </div>
              </div>
            )}

            {/* Conflict filename alert */}
            {conflict && (
              <div className="space-y-2.5 rounded-xl border border-sky-500/30 bg-sky-500/10 p-3.5 text-xs text-sky-700 dark:text-sky-200">
                <div className="flex items-center gap-2 font-medium">
                  <FileText className="h-4 w-4 text-sky-500" />
                  <span>目标目录已存在同名文件，需要您决定如何处理</span>
                </div>
                <div className="flex flex-wrap items-center gap-2 pt-1">
                  <button
                    type="button"
                    onClick={() => void resolveConflict('numbered')}
                    className="rounded-lg bg-sky-500 px-3 py-1 text-xs font-semibold text-white hover:bg-sky-400"
                  >
                    添加序号并保存为 {conflict.suggestedFilename}
                  </button>
                  <button
                    type="button"
                    onClick={() => setConflict(null)}
                    className="rounded-lg border border-[var(--border-subtle)] bg-[var(--bg-subtle)] px-3 py-1 text-xs text-[var(--text-secondary)] hover:bg-[var(--bg-surface-hover)] hover:text-[var(--text-primary)]"
                  >
                    暂不处理
                  </button>
                </div>
              </div>
            )}

            <div className="space-y-1.5">
              <label className="flex items-center gap-1.5 text-[11px] font-medium tracking-wide text-[var(--text-secondary)] uppercase">
                <LinkIcon className="h-3.5 w-3.5 text-[var(--text-muted)]" />
                资源链接 (URL)
              </label>
              <input
                type="text"
                value={url}
                onChange={(e) => setUrl(e.target.value)}
                onBlur={handleUrlBlur}
                placeholder="https://example.com/file.zip"
                className="w-full rounded-xl border border-[var(--border-subtle)] bg-[var(--bg-base)] px-3.5 py-2.5 text-xs text-[var(--text-primary)] placeholder-[var(--text-muted)] transition-all duration-200 focus:border-[var(--accent)] focus:ring-2 focus:ring-[var(--border-focus)] focus:outline-hidden"
                autoFocus
              />
            </div>

            <div className="grid grid-cols-3 gap-2.5 rounded-xl border border-[var(--border-subtle)] bg-[var(--bg-base)] p-3 text-xs">
              <div>
                <span className="text-[10px] text-[var(--text-muted)] uppercase">大小</span>
                <p className="font-mono font-medium text-[var(--text-primary)]">
                  {probing ? '正在获取...' : formatBytes(probeResult?.totalBytes ?? -1)}
                </p>
              </div>
              <div>
                <span className="text-[10px] text-[var(--text-muted)] uppercase">类型</span>
                <p className="truncate font-mono font-medium text-[var(--text-primary)]">
                  {probeResult?.contentType || '未知'}
                </p>
              </div>
              <div>
                <span className="text-[10px] text-[var(--text-muted)] uppercase">断点续传</span>
                <p className="font-medium text-[var(--text-primary)]">
                  {probeResult ? (probeResult.resumable ? '支持' : '不支持') : '未知'}
                </p>
              </div>
            </div>

            <div className="grid grid-cols-2 gap-3.5">
              <div className="space-y-1.5">
                <label className="text-[11px] font-medium tracking-wide text-[var(--text-secondary)] uppercase">
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
                  className="w-full rounded-xl border border-[var(--border-subtle)] bg-[var(--bg-base)] px-3.5 py-2 text-xs text-[var(--text-primary)] placeholder-[var(--text-muted)] transition-all duration-200 focus:border-[var(--accent)] focus:ring-2 focus:ring-[var(--border-focus)] focus:outline-hidden"
                />
              </div>

              <div className="space-y-1.5">
                <label className="flex items-center gap-1 text-[11px] font-medium tracking-wide text-[var(--text-secondary)] uppercase">
                  <Cpu className="h-3.5 w-3.5 text-[var(--text-muted)]" />
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
              <label className="flex items-center justify-between text-[11px] font-medium tracking-wide text-[var(--text-secondary)] uppercase">
                <span className="flex items-center gap-1.5">
                  <Folder className="h-3.5 w-3.5 text-[var(--text-muted)]" />
                  保存目录
                </span>
                <button
                  type="button"
                  onClick={() => {
                    void handleSelectFolder();
                  }}
                  className="text-[var(--accent)] hover:underline hover:opacity-80"
                >
                  浏览...
                </button>
              </label>
              <input
                type="text"
                value={currentDir}
                onChange={(e) => setDir(e.target.value)}
                className="w-full rounded-xl border border-[var(--border-subtle)] bg-[var(--bg-base)] px-3.5 py-2 text-xs text-[var(--text-primary)] placeholder-[var(--text-muted)] transition-all duration-200 focus:border-[var(--accent)] focus:ring-2 focus:ring-[var(--border-focus)] focus:outline-hidden"
              />
            </div>

            <div className="space-y-2 rounded-xl border border-[var(--border-subtle)] bg-[var(--bg-base)] p-3.5">
              <Switch
                id="pre-download-switch"
                checked={preDownload}
                onCheckedChange={handlePreDownloadToggle}
                label="显示文件信息时立即下载"
                description="对话框打开期间先开始传输，仍可修改名称与目录"
              />

              {preTask && (
                <div className="space-y-1 pt-2 text-[11px]">
                  <div className="flex items-center justify-between text-[var(--text-muted)]">
                    <span className="flex items-center gap-1 text-[var(--accent)]">
                      <DownloadCloud className="h-3 w-3 animate-pulse" />
                      {preTask.status === task.Status.StatusCompleted ? '已提前完成' : '后台传输中'}
                    </span>
                    <span className="font-mono">
                      {formatBytes(preTask.downloaded)} / {formatBytes(preTask.totalBytes)} (
                      {preTaskPercent}%)
                    </span>
                  </div>
                  <div className="h-1.5 w-full overflow-hidden rounded-full bg-[var(--bg-subtle)]">
                    <div
                      className="h-full bg-[var(--accent)] transition-all duration-300"
                      style={{ width: `${preTaskPercent}%` }}
                    />
                  </div>
                </div>
              )}
            </div>

            <div className="flex items-center justify-end gap-2.5 border-t border-[var(--border-subtle)] pt-4">
              <button
                type="button"
                onClick={() => void handleCancel()}
                className="rounded-xl border border-[var(--border-subtle)] bg-[var(--bg-surface)] px-4 py-2 text-xs font-medium text-[var(--text-secondary)] transition-all hover:bg-[var(--bg-subtle)] hover:text-[var(--text-primary)]"
              >
                取消
              </button>

              <button
                type="submit"
                disabled={loading}
                className="flex items-center gap-1.5 rounded-xl bg-[var(--accent)] px-5 py-2 text-xs font-semibold text-white shadow-md transition-all hover:opacity-90 disabled:opacity-50"
              >
                {loading ? '正在处理...' : preTask ? '确认并下载' : '确认下载'}
              </button>
            </div>
          </form>
        </Dialog.Content>
      </Dialog.Portal>
    </Dialog.Root>
  );
}
