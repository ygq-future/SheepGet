import { useState, useEffect, useRef, useCallback, type SyntheticEvent } from 'react';
import {
  Folder,
  DownloadCloud,
  AlertCircle,
  Copy,
  RotateCcw,
  CheckCircle2,
  AlertTriangle,
  Loader2,
  X,
  Minus,
} from 'lucide-react';
import { Input } from '../components/ui/Input';
import { Button } from '../components/ui/Button';
import { Badge } from '../components/ui/Badge';
import { formatBytes } from '../lib/format';
import {
  GetActiveFileInfo,
  SubmitFileInfo,
  CancelCurrentFileInfo,
  MinimiseFileInfoWindow,
  SetFileInfoWindowHeight,
  SelectDirectory,
  ProbeURL,
  CheckFileConflict,
  ShowProgressWindow,
} from '../../bindings/sheep-get/app';
import type * as windowModels from '../../bindings/sheep-get/internal/window/models';
import * as configModels from '../../bindings/sheep-get/internal/config/models';
import * as taskModels from '../../bindings/sheep-get/internal/task/models';
import { Events } from '@wailsio/runtime';
import { unwrapEventData } from '../lib/utils';
import { useSettingsStore } from '../stores/settings';

export function FileInfoView() {
  const { loadSettings } = useSettingsStore();
  const [activeItem, setActiveItem] = useState<windowModels.FileInfoItem | null>(null);

  // Form states
  const [url, setUrl] = useState('');
  const [filename, setFilename] = useState('');
  const [directory, setDirectory] = useState('');
  const [maxConn, setMaxConn] = useState(4);
  const [preDownload, setPreDownload] = useState(false);
  const [duplicateStrategy, setDuplicateStrategy] = useState<string | undefined>(undefined);

  // Conflict & probe states
  const [probing, setProbing] = useState(false);
  const [fileConflict, setFileConflict] = useState(false);
  const [suggestedFilename, setSuggestedFilename] = useState('');
  const [overwriteConflict, setOverwriteConflict] = useState(false);

  // Status states
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const nameEditedRef = useRef(false);
  const probeSeqRef = useRef(0);
  const containerRef = useRef<HTMLDivElement>(null);

  const initItem = useCallback((item: windowModels.FileInfoItem) => {
    const currentSettings = useSettingsStore.getState().settings;
    const currentPolicy = item.duplicatePolicy || currentSettings?.download?.duplicateUrlPolicy;
    let initialStrategy: string | undefined = undefined;
    if (item.duplicateTask) {
      if (currentPolicy === configModels.DuplicateURLPolicy.DuplicatePolicyOverwrite) {
        initialStrategy = 'continue_overwrite';
      } else if (currentPolicy === configModels.DuplicateURLPolicy.DuplicatePolicyNumberedCopy) {
        initialStrategy = 'copy';
      }
    }
    setActiveItem(item);
    setUrl(item.url || '');
    setFilename(item.filename || '');
    setDirectory(item.directory || currentSettings?.download?.defaultDirectory || '');
    setMaxConn(item.maxConn || currentSettings?.download?.defaultConnectionsPerTask || 4);
    setPreDownload(item.preDownload ?? !!currentSettings?.download?.preDownload);
    setFileConflict(!!item.fileConflict);
    setSuggestedFilename(item.suggestedFilename || '');
    setDuplicateStrategy(initialStrategy);
    setOverwriteConflict(false);
    setError(null);
    setLoading(false);
    nameEditedRef.current = false;
  }, []);

  useEffect(() => {
    void loadSettings();

    // Fetch initial active request
    void (async () => {
      try {
        const item = await GetActiveFileInfo();
        if (item) {
          initItem(item);
        }
      } catch (err) {
        console.error('Failed to get active file info:', err);
      }
    })();

    // Listen for next item in queue
    const unlistenNext = Events.On('fileinfo:next', (ev: unknown) => {
      const item = unwrapEventData<windowModels.FileInfoItem>(ev);
      if (item) {
        initItem(item);
      } else {
        setActiveItem(null);
      }
    });

    // Listen for queue updates
    const unlistenQueue = Events.On('fileinfo:queue_updated', (ev: unknown) => {
      const status = unwrapEventData<{ index: number; total: number }>(ev);
      if (status) {
        setActiveItem((prev) =>
          prev
            ? {
                ...prev,
                queueIndex: status.index,
                queueTotal: status.total,
              }
            : null,
        );
      }
    });

    return () => {
      unlistenNext();
      unlistenQueue();
    };
  }, [loadSettings, initItem]);

  // Handle URL probe when URL is changed manually
  const probeManualURL = async (rawUrl: string) => {
    const trimmed = rawUrl.trim();
    if (!trimmed) {
      return;
    }
    const seq = ++probeSeqRef.current;
    setProbing(true);

    try {
      const result = await ProbeURL(trimmed);
      if (seq !== probeSeqRef.current || !result) return;
      const currentSettings = useSettingsStore.getState().settings;
      const currentPolicy =
        activeItem?.duplicatePolicy ||
        currentSettings?.download?.duplicateUrlPolicy ||
        configModels.DuplicateURLPolicy.DuplicatePolicyPrompt;

      let chosenName =
        !nameEditedRef.current && result.filename ? result.filename : filename || result.filename;
      let initStrategy: string | undefined = undefined;

      // Check conflict
      if (directory && chosenName) {
        const conf = await CheckFileConflict(directory, chosenName);
        setFileConflict(conf.exists);
        setSuggestedFilename(conf.suggestedFilename);

        if (result.duplicateTask) {
          if (currentPolicy === configModels.DuplicateURLPolicy.DuplicatePolicyNumberedCopy) {
            initStrategy = 'copy';
            // Dual check: only add (n) if file actually exists on disk!
            if (conf.exists) {
              chosenName = conf.suggestedFilename;
              setFileConflict(false);
            }
          } else if (currentPolicy === configModels.DuplicateURLPolicy.DuplicatePolicyOverwrite) {
            initStrategy = 'continue_overwrite';
          }
        }
      }

      setFilename(chosenName);
      setDuplicateStrategy(initStrategy);

      setActiveItem((prev) => {
        if (!prev) return null;
        return {
          ...prev,
          url: trimmed,
          filename: chosenName,
          totalBytes: result.totalBytes,
          resumable: result.resumable,
          duplicateTask: result.duplicateTask || null,
        };
      });
      setError(null);
    } catch (err: unknown) {
      if (seq !== probeSeqRef.current) return;
      const msg = err instanceof Error ? err.message : String(err);
      setError(`资源探测失败: ${msg}`);
    } finally {
      if (seq === probeSeqRef.current) {
        setProbing(false);
      }
    }
  };

  const handleSelectDir = async () => {
    try {
      const selected = await SelectDirectory();
      if (selected) {
        setDirectory(selected);
        if (filename) {
          const conf = await CheckFileConflict(selected, filename);
          setFileConflict(conf.exists);
          setSuggestedFilename(conf.suggestedFilename);
        }
      }
    } catch (err) {
      console.error('Failed to select directory:', err);
    }
  };

  const handleConfirm = useCallback(
    async (strategyOverride?: string, e?: SyntheticEvent) => {
      if (e) e.preventDefault();
      if (!url.trim()) {
        setError('请输入下载链接');
        return;
      }
      if (!filename.trim()) {
        setError('请输入文件名');
        return;
      }
      if (!directory.trim()) {
        setError('请选择保存目录');
        return;
      }

      const effectiveStrategy = strategyOverride ?? duplicateStrategy;
      const isOverwrite =
        effectiveStrategy === 'continue_overwrite' || effectiveStrategy === 'redownload';

      // Disk conflict check: only block if not an explicit overwrite!
      if (fileConflict && !overwriteConflict && !isOverwrite) {
        setError('目标目录存在同名文件，请确认是否覆盖或使用建议名称');
        return;
      }
      setLoading(true);
      setError(null);
      const parsedConn = Math.min(32, Math.max(1, Number(maxConn) || 4));

      try {
        await SubmitFileInfo({
          requestId: activeItem?.id || '',
          url: url.trim(),
          filename: filename.trim(),
          directory: directory.trim(),
          maxConn: parsedConn,
          duplicateStrategy: effectiveStrategy,
          preDownload,
          overwriteConflict: overwriteConflict || isOverwrite,
        });
      } catch (err: unknown) {
        const msg = err instanceof Error ? err.message : String(err);
        setError(`提交失败: ${msg}`);
      } finally {
        setLoading(false);
      }
    },
    [
      activeItem,
      directory,
      duplicateStrategy,
      fileConflict,
      filename,
      maxConn,
      overwriteConflict,
      preDownload,
      url,
    ],
  );

  const handleCancel = async () => {
    try {
      await CancelCurrentFileInfo();
    } catch (err) {
      console.error('Failed to cancel file info:', err);
    }
  };

  const handleMinimise = () => {
    void MinimiseFileInfoWindow();
  };

  // Keyboard shortcut listener
  useEffect(() => {
    const handleKeyDown = (e: KeyboardEvent) => {
      if (e.key === 'Escape') {
        e.preventDefault();
        void handleCancel();
      } else if (e.key === 'Enter' && !e.shiftKey && !e.ctrlKey && !e.metaKey) {
        if ((e.target as HTMLElement)?.tagName === 'TEXTAREA') return;
        e.preventDefault();
        void handleConfirm();
      }
    };
    window.addEventListener('keydown', handleKeyDown);
    return () => window.removeEventListener('keydown', handleKeyDown);
  }, [handleConfirm]);

  // Resize window dynamically to wrap content perfectly
  useEffect(() => {
    const el = containerRef.current;
    if (!el) return;

    let lastHeight = 0;
    const observer = new ResizeObserver((entries) => {
      for (const entry of entries) {
        const height = Math.ceil(entry.borderBoxSize?.[0]?.blockSize ?? entry.contentRect.height);
        if (height > 0 && Math.abs(height - lastHeight) >= 2) {
          lastHeight = height;
          void SetFileInfoWindowHeight(height);
        }
      }
    });

    observer.observe(el);
    return () => observer.disconnect();
  }, []);

  return (
    <div
      ref={containerRef}
      className="flex flex-col overflow-hidden rounded-lg border border-[var(--border-subtle)] bg-[var(--bg-base)] font-sans text-[var(--text-primary)] shadow-2xl select-none"
    >
      <header
        className="flex h-8 shrink-0 cursor-default items-center justify-between border-b border-[var(--border-subtle)] bg-[var(--bg-surface)] px-3 select-none"
        style={{ ['--wails-draggable' as string]: 'drag' }}
      >
        <div className="pointer-events-none flex items-center gap-1.5">
          <DownloadCloud className="h-3.5 w-3.5 text-[var(--accent)]" />
          <span className="text-[11px] font-semibold tracking-wide text-[var(--text-primary)]">
            新建下载
          </span>
          {activeItem && activeItem.queueTotal > 1 && (
            <Badge variant="accent">
              {activeItem.queueIndex} / {activeItem.queueTotal}
            </Badge>
          )}
        </div>
        <div
          className="flex items-center gap-1"
          style={{ ['--wails-draggable' as string]: 'no-drag' }}
        >
          <button
            type="button"
            onClick={handleMinimise}
            title="最小化"
            className="flex h-5 w-5 items-center justify-center rounded text-[var(--text-muted)] transition-colors hover:bg-[var(--bg-subtle)] hover:text-[var(--text-primary)]"
          >
            <Minus className="h-3 w-3" />
          </button>
          <button
            type="button"
            onClick={() => void handleCancel()}
            title="关闭"
            className="flex h-5 w-5 items-center justify-center rounded text-[var(--text-muted)] transition-colors hover:bg-red-500/20 hover:text-red-400"
          >
            <X className="h-3 w-3" />
          </button>
        </div>
      </header>

      {/* Main Content (compact form with NO scrollbars and stable initial paint) */}
      <main className="flex-1 space-y-2 overflow-hidden p-3">
        {/* URL Input */}
        <div className="space-y-0.5">
          <label className="text-[11px] font-medium text-[var(--text-secondary)]">下载链接</label>
          <div className="relative">
            <Input
              value={url}
              error={Boolean(error && error === '请输入下载链接')}
              onChange={(e) => {
                setUrl(e.target.value);
                if (error) setError(null);
                void probeManualURL(e.target.value);
              }}
              placeholder="https://..."
            />
            {probing && (
              <div className="absolute top-2 right-2.5">
                <Loader2 className="h-3.5 w-3.5 animate-spin text-[var(--accent)]" />
              </div>
            )}
          </div>
          {error && (
            <p className="mt-0.5 text-[10px] leading-tight text-red-500 dark:text-red-400">
              {error}
            </p>
          )}
        </div>

        {/* Compact Resource Meta Line */}
        <div className="flex items-center justify-between px-0.5 text-[11px] text-[var(--text-muted)]">
          <span>
            预估大小:{' '}
            <span className="font-medium text-[var(--text-secondary)]">
              {activeItem && activeItem.totalBytes > 0
                ? formatBytes(activeItem.totalBytes)
                : '未知大小'}
            </span>
          </span>
          <span>
            续传支持:{' '}
            <span
              className={
                activeItem?.resumable
                  ? 'font-medium text-emerald-600 dark:text-emerald-400'
                  : 'text-[var(--text-muted)]'
              }
            >
              {activeItem?.resumable ? '支持' : '不支持 / 未知'}
            </span>
          </span>
        </div>

        {/* Duplicate Task Alert */}
        {activeItem?.duplicateTask && (
          <div className="space-y-1.5 rounded-md border border-amber-500/30 bg-amber-500/10 p-2 text-[11px] font-medium text-amber-950 dark:text-amber-100">
            <div className="flex items-center gap-1.5">
              <AlertTriangle className="h-3 w-3 shrink-0 text-amber-600 dark:text-amber-400" />
              <span>已存在相同链接任务</span>
            </div>
            <div className="flex flex-wrap gap-1.5 pt-0.5">
              {activeItem.duplicateTask.status === taskModels.Status.StatusCompleted && (
                <Button
                  size="sm"
                  variant="secondary"
                  onClick={() => {
                    setDuplicateStrategy('show_completed');
                    if (activeItem.duplicateTask?.id) {
                      void ShowProgressWindow(activeItem.duplicateTask.id);
                    }
                    void handleCancel();
                  }}
                  className={`h-6 px-2 text-[10px] ${
                    duplicateStrategy === 'show_completed'
                      ? 'border-[var(--accent)] bg-[var(--accent-muted)] font-semibold text-[var(--accent)] shadow-xs'
                      : ''
                  }`}
                >
                  <CheckCircle2 className="h-2.5 w-2.5" />
                  <span>跳过并查看完成</span>
                </Button>
              )}
              <Button
                size="sm"
                variant="secondary"
                onClick={() => {
                  setDuplicateStrategy('continue_overwrite');
                  void handleConfirm('continue_overwrite');
                }}
                className={`h-6 px-2 text-[10px] ${
                  duplicateStrategy === 'continue_overwrite'
                    ? 'border-[var(--accent)] bg-[var(--accent-muted)] font-semibold text-[var(--accent)] shadow-xs'
                    : ''
                }`}
              >
                <RotateCcw className="h-2.5 w-2.5" />
                <span>继续下载并覆盖</span>
              </Button>
              <Button
                size="sm"
                variant="secondary"
                onClick={() => {
                  setDuplicateStrategy('copy');
                  void (async () => {
                    if (directory && filename) {
                      const conf = await CheckFileConflict(
                        directory,
                        activeItem?.filename || filename,
                      );
                      if (conf.exists) {
                        setFilename(conf.suggestedFilename);
                        setFileConflict(false);
                      }
                    }
                  })();
                }}
                className={`h-6 px-2 text-[10px] ${
                  duplicateStrategy === 'copy'
                    ? 'border-[var(--accent)] bg-[var(--accent-muted)] font-semibold text-[var(--accent)] shadow-xs'
                    : ''
                }`}
              >
                <Copy className="h-2.5 w-2.5" />
                <span>序号副本</span>
              </Button>
            </div>
          </div>
        )}

        {/* File Conflict Alert */}
        {fileConflict &&
          duplicateStrategy !== 'continue_overwrite' &&
          duplicateStrategy !== 'redownload' && (
            <div className="space-y-1 rounded-md border border-amber-500/30 bg-amber-500/10 p-2 text-[11px] font-medium text-amber-950 dark:text-amber-100">
              <div className="flex items-center gap-1.5">
                <AlertCircle className="h-3 w-3 shrink-0 text-amber-600 dark:text-amber-400" />
                <span>目标目录存在同名文件: {filename}</span>
              </div>
              <div className="flex flex-wrap gap-1.5 pt-0.5">
                <Button
                  size="sm"
                  variant="secondary"
                  onClick={() => setOverwriteConflict(true)}
                  className={`h-6 px-2 text-[10px] ${
                    overwriteConflict
                      ? 'border-[var(--accent)] bg-[var(--accent-muted)] font-semibold text-[var(--accent)] shadow-xs'
                      : ''
                  }`}
                >
                  覆盖现有
                </Button>
                {suggestedFilename && (
                  <Button
                    size="sm"
                    variant="secondary"
                    onClick={() => {
                      setFilename(suggestedFilename);
                      setFileConflict(false);
                      setOverwriteConflict(false);
                    }}
                    className="h-6 px-2 text-[10px]"
                  >
                    使用序号: {suggestedFilename}
                  </Button>
                )}
              </div>
            </div>
          )}
        {/* Filename & Concurrency (Same row, equal height h-8) */}
        <div className="flex items-end gap-2">
          <div className="flex-1 space-y-0.5">
            <label className="text-[11px] font-medium text-[var(--text-secondary)]">文件名</label>
            <Input
              value={filename}
              onChange={(e) => {
                nameEditedRef.current = true;
                setFilename(e.target.value);
                if (directory) {
                  void (async () => {
                    const conf = await CheckFileConflict(directory, e.target.value);
                    setFileConflict(conf.exists);
                    setSuggestedFilename(conf.suggestedFilename);
                  })();
                }
              }}
            />
          </div>

          <div className="w-20 shrink-0 space-y-0.5">
            <label className="text-[11px] font-medium text-[var(--text-secondary)]">并发数</label>
            <Input
              type="text"
              inputMode="numeric"
              value={maxConn}
              onChange={(e) => {
                const cleaned = e.target.value.replace(/[^\d]/g, '');
                if (cleaned === '') {
                  setMaxConn(0);
                } else {
                  const val = parseInt(cleaned, 10);
                  setMaxConn(Math.min(32, Math.max(1, val)));
                }
              }}
              onBlur={() => {
                if (!maxConn || Number(maxConn) < 1) {
                  setMaxConn(1);
                }
              }}
              className="text-center font-mono"
            />
          </div>
        </div>

        {/* Save Directory */}
        <div className="space-y-0.5">
          <label className="text-[11px] font-medium text-[var(--text-secondary)]">保存目录</label>
          <div className="flex gap-1.5">
            <Input
              value={directory}
              onChange={(e) => {
                const newDir = e.target.value;
                setDirectory(newDir);
                if (filename) {
                  void (async () => {
                    const conf = await CheckFileConflict(newDir, filename);
                    setFileConflict(conf.exists);
                    setSuggestedFilename(conf.suggestedFilename);
                  })();
                }
              }}
              className="flex-1"
            />
            <Button
              size="md"
              variant="secondary"
              onClick={() => void handleSelectDir()}
              className="shrink-0 gap-1"
            >
              <Folder className="h-3.5 w-3.5" />
              <span>浏览</span>
            </Button>
          </div>
        </div>
      </main>

      {/* Compact Footer */}
      <footer className="flex h-10 shrink-0 items-center justify-between border-t border-[var(--border-subtle)] bg-[var(--bg-surface)] px-3">
        <Button size="sm" variant="ghost" onClick={() => void handleCancel()}>
          取消 (Esc)
        </Button>
        <Button
          size="sm"
          variant="primary"
          disabled={loading || probing}
          onClick={() => void handleConfirm()}
        >
          {loading ? (
            <>
              <Loader2 className="h-3 w-3 animate-spin" />
              <span>提交中...</span>
            </>
          ) : (
            <span>开始下载 (Enter)</span>
          )}
        </Button>
      </footer>
    </div>
  );
}
