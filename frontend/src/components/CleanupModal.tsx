import * as Dialog from '@radix-ui/react-dialog';
import {
  Sparkles,
  RefreshCw,
  X,
  Clock,
  Copy,
  FileX,
  FileText,
  Trash2,
  HardDrive,
  AlertTriangle,
} from 'lucide-react';
import { useState, useEffect, useCallback } from 'react';
import { Checkbox } from './ui/Checkbox';
import { Select } from './ui/Select';
import { Button } from './ui/Button';
import { formatBytes } from '../lib/format';
import { showToast } from './ui/Toast';
import {
  CLEANUP_DAYS_OPTIONS,
  DEFAULT_CLEANUP_CONFIG,
  CleanupConfigState,
  extractCleanupSummary,
} from '../lib/cleanup';
import {
  CleanupScanOptions,
  CleanupScanResult,
  CleanupExecuteOptions,
} from '../../bindings/sheep-get/internal/engine/models';
import { ScanCleanup, ExecuteCleanup } from '../../bindings/sheep-get/app';

interface CleanupModalProps {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  onCleanupFinished?: () => void;
}

export function CleanupModal({ open, onOpenChange, onCleanupFinished }: CleanupModalProps) {
  const [config, setConfig] = useState<CleanupConfigState>(DEFAULT_CLEANUP_CONFIG);
  const [scanResult, setScanResult] = useState<CleanupScanResult | null>(null);
  const [isScanning, setIsScanning] = useState(false);
  const [isCleaning, setIsCleaning] = useState(false);

  const scanWithConfig = useCallback(async (targetConfig: CleanupConfigState) => {
    setIsScanning(true);
    try {
      const opts = new CleanupScanOptions({
        olderThanDays:
          targetConfig.olderTasksEnabled || targetConfig.oldLogsEnabled
            ? targetConfig.olderThanDays
            : 0,
        deleteOlderDiskFiles: targetConfig.olderTasksEnabled && targetConfig.deleteOlderFiles,
        checkDuplicates: targetConfig.duplicatesEnabled,
        checkMissingFiles: targetConfig.missingTasksEnabled,
        checkOldLogs: targetConfig.oldLogsEnabled,
      });
      const result = await ScanCleanup(opts);
      setScanResult(result);
    } catch (err) {
      console.error('Scan cleanup failed:', err);
      showToast('扫描可清理项目失败', 'error');
    } finally {
      setIsScanning(false);
    }
  }, []);

  useEffect(() => {
    if (!open) {
      return;
    }
    let ignore = false;
    const timer = setTimeout(() => {
      if (!ignore) {
        void scanWithConfig(config);
      }
    }, 0);

    return () => {
      ignore = true;
      clearTimeout(timer);
    };
  }, [open, config, scanWithConfig]);

  const summary = extractCleanupSummary(scanResult);

  const handleExecute = async () => {
    if (summary.totalTasks === 0 && summary.totalFiles === 0) {
      showToast('当前未发现符合条件的项目可清理', 'info');
      return;
    }

    setIsCleaning(true);
    try {
      const opts = new CleanupExecuteOptions({
        deleteOlderTasks: config.olderTasksEnabled,
        deleteOlderDiskFiles: config.olderTasksEnabled && config.deleteOlderFiles,
        olderThanDays: config.olderTasksEnabled || config.oldLogsEnabled ? config.olderThanDays : 0,
        deleteDuplicates: config.duplicatesEnabled,
        deleteMissingTasks: config.missingTasksEnabled,
        deleteOldLogs: config.oldLogsEnabled,
      });

      const res = await ExecuteCleanup(opts);
      showToast(
        `清理完成：已移除 ${res?.deletedTaskCount ?? 0} 个任务，清理 ${res?.deletedFileCount ?? 0} 个文件，释放 ${formatBytes(res?.freedBytes ?? 0)}`,
        'success',
      );
      onCleanupFinished?.();
      onOpenChange(false);
    } catch (err) {
      console.error('Execute cleanup failed:', err);
      showToast('执行清理失败', 'error');
    } finally {
      setIsCleaning(false);
    }
  };

  const hasAnyOptionSelected =
    config.olderTasksEnabled ||
    config.duplicatesEnabled ||
    config.missingTasksEnabled ||
    config.oldLogsEnabled;

  return (
    <Dialog.Root open={open} onOpenChange={onOpenChange}>
      <Dialog.Portal>
        <Dialog.Overlay className="animate-in fade-in fixed inset-0 z-50 bg-black/60 backdrop-blur-xs transition-opacity" />
        <Dialog.Content className="animate-in fade-in zoom-in-95 fixed top-1/2 left-1/2 z-50 flex max-h-[calc(100vh-48px)] w-full max-w-lg -translate-x-1/2 -translate-y-1/2 flex-col rounded-2xl border border-[var(--border-subtle)] bg-[var(--bg-surface)] p-5 text-[var(--text-primary)] shadow-2xl backdrop-blur-2xl transition-all duration-200 focus:outline-hidden">
          {/* Header */}
          <div className="flex shrink-0 items-center justify-between border-b border-[var(--border-subtle)] pb-3">
            <div className="flex items-center gap-2">
              <div className="flex h-8 w-8 items-center justify-center rounded-lg border border-amber-500/20 bg-amber-500/10 text-amber-500">
                <Sparkles className="h-4 w-4" />
              </div>
              <div>
                <Dialog.Title className="text-sm font-semibold tracking-tight text-[var(--text-primary)]">
                  下载记录与文件清理
                </Dialog.Title>
                <Dialog.Description className="text-xs text-[var(--text-muted)]">
                  安全清理旧记录、内容重复文件及失效任务
                </Dialog.Description>
              </div>
            </div>
            <Dialog.Close asChild>
              <button
                type="button"
                className="rounded-lg p-1 text-[var(--text-muted)] transition-colors hover:bg-[var(--bg-muted)] hover:text-[var(--text-primary)]"
              >
                <X className="h-4 w-4" />
              </button>
            </Dialog.Close>
          </div>

          {/* Form Options Body - Scrollable with safe gap */}
          <div className="mt-3.5 min-h-0 flex-1 space-y-2.5 overflow-y-auto pr-1">
            {/* Section 1: Older Tasks */}
            <div className="rounded-xl border border-[var(--border-subtle)] bg-[var(--bg-muted)]/30 p-3 transition-colors">
              <div className="flex items-center justify-between gap-2">
                <div className="flex items-center gap-2">
                  <Checkbox
                    id="cleanup-older"
                    checked={config.olderTasksEnabled}
                    onCheckedChange={(checked) => {
                      setConfig((prev) => ({ ...prev, olderTasksEnabled: Boolean(checked) }));
                    }}
                  />
                  <label
                    htmlFor="cleanup-older"
                    className="flex cursor-pointer items-center gap-1.5 text-xs font-medium text-[var(--text-primary)] select-none"
                  >
                    <Clock className="h-3.5 w-3.5 text-[var(--text-muted)]" />
                    <span>清理旧下载历史</span>
                  </label>
                </div>

                <div className="flex items-center gap-2">
                  <div className="w-32">
                    <Select
                      value={String(config.olderThanDays)}
                      disabled={!config.olderTasksEnabled}
                      options={CLEANUP_DAYS_OPTIONS.map((opt) => ({
                        value: String(opt.value),
                        label: opt.label,
                      }))}
                      onChange={(val) => {
                        setConfig((prev) => ({ ...prev, olderThanDays: Number(val) }));
                      }}
                    />
                  </div>
                </div>
              </div>

              {config.olderTasksEnabled && (
                <div className="mt-2.5 space-y-1.5 border-t border-[var(--border-subtle)]/60 pt-2 pl-6">
                  <div className="flex items-center gap-2">
                    <Checkbox
                      id="cleanup-delete-older-files"
                      checked={config.deleteOlderFiles}
                      onCheckedChange={(checked) => {
                        setConfig((prev) => ({
                          ...prev,
                          deleteOlderFiles: Boolean(checked),
                        }));
                      }}
                    />
                    <label
                      htmlFor="cleanup-delete-older-files"
                      className="cursor-pointer text-[11px] text-[var(--text-secondary)] select-none hover:text-[var(--text-primary)]"
                    >
                      同时删除本地已下载文件（不勾选则仅清除记录）
                    </label>
                  </div>
                  <div className="text-[11px] text-[var(--text-muted)]">
                    {scanResult ? (
                      <span>
                        扫描到{' '}
                        <strong className="font-semibold text-[var(--text-primary)]">
                          {scanResult.olderTasks?.length ?? 0}
                        </strong>{' '}
                        个历史任务
                        {config.deleteOlderFiles && (
                          <>
                            ，磁盘占用{' '}
                            <strong className="font-semibold text-amber-500">
                              {formatBytes(scanResult.olderFilesBytes ?? 0)}
                            </strong>
                          </>
                        )}
                      </span>
                    ) : (
                      <span>正在扫描历史任务...</span>
                    )}
                  </div>
                </div>
              )}
            </div>

            {/* Section 2: Duplicates */}
            <div className="rounded-xl border border-[var(--border-subtle)] bg-[var(--bg-muted)]/30 p-3 transition-colors">
              <div className="flex items-start justify-between gap-2">
                <div className="flex items-start gap-2">
                  <Checkbox
                    id="cleanup-duplicates"
                    checked={config.duplicatesEnabled}
                    onCheckedChange={(checked) => {
                      setConfig((prev) => ({ ...prev, duplicatesEnabled: Boolean(checked) }));
                    }}
                    className="mt-0.5"
                  />
                  <div>
                    <label
                      htmlFor="cleanup-duplicates"
                      className="flex cursor-pointer items-center gap-1.5 text-xs font-medium text-[var(--text-primary)] select-none"
                    >
                      <Copy className="h-3.5 w-3.5 text-[var(--text-muted)]" />
                      <span>清理重复文件与副本</span>
                    </label>
                    <p className="mt-0.5 text-[11px] leading-tight text-[var(--text-muted)]">
                      按字节与 MD5 对比；若存在序号副本保留本体删除副本，其余保留最早下载项
                    </p>
                  </div>
                </div>
              </div>

              {config.duplicatesEnabled && (
                <div className="mt-2 border-t border-[var(--border-subtle)]/60 pt-2 pl-6 text-[11px] text-[var(--text-muted)]">
                  {scanResult ? (
                    <span>
                      发现{' '}
                      <strong className="font-semibold text-[var(--text-primary)]">
                        {scanResult.duplicateGroups?.length ?? 0}
                      </strong>{' '}
                      组重复文件，共{' '}
                      <strong className="font-semibold text-amber-500">
                        {scanResult.duplicateGroups?.reduce(
                          (acc, g) => acc + (g.duplicateTasks?.length ?? 0),
                          0,
                        ) ?? 0}
                      </strong>{' '}
                      个冗余副本，可释放{' '}
                      <strong className="font-semibold text-amber-500">
                        {formatBytes(scanResult.duplicateFilesBytes ?? 0)}
                      </strong>
                    </span>
                  ) : (
                    <span>正在比对重复文件...</span>
                  )}
                </div>
              )}
            </div>

            {/* Section 3: Missing Files */}
            <div className="rounded-xl border border-[var(--border-subtle)] bg-[var(--bg-muted)]/30 p-3 transition-colors">
              <div className="flex items-start justify-between gap-2">
                <div className="flex items-start gap-2">
                  <Checkbox
                    id="cleanup-missing"
                    checked={config.missingTasksEnabled}
                    onCheckedChange={(checked) => {
                      setConfig((prev) => ({ ...prev, missingTasksEnabled: Boolean(checked) }));
                    }}
                    className="mt-0.5"
                  />
                  <div>
                    <label
                      htmlFor="cleanup-missing"
                      className="flex cursor-pointer items-center gap-1.5 text-xs font-medium text-[var(--text-primary)] select-none"
                    >
                      <FileX className="h-3.5 w-3.5 text-[var(--text-muted)]" />
                      <span>清理无效任务记录</span>
                    </label>
                    <p className="mt-0.5 text-[11px] leading-tight text-[var(--text-muted)]">
                      清理已完成但本地成品文件已被移动、重命名或删除的失效任务
                    </p>
                  </div>
                </div>
              </div>

              {config.missingTasksEnabled && (
                <div className="mt-2 border-t border-[var(--border-subtle)]/60 pt-2 pl-6 text-[11px] text-[var(--text-muted)]">
                  {scanResult ? (
                    <span>
                      发现{' '}
                      <strong className="font-semibold text-[var(--text-primary)]">
                        {scanResult.missingTasks?.length ?? 0}
                      </strong>{' '}
                      个文件已丢失的无效记录
                    </span>
                  ) : (
                    <span>正在检查无效任务...</span>
                  )}
                </div>
              )}
            </div>

            {/* Section 4: Old Log Files */}
            <div className="rounded-xl border border-[var(--border-subtle)] bg-[var(--bg-muted)]/30 p-3 transition-colors">
              <div className="flex items-center justify-between gap-2">
                <div className="flex items-center gap-2">
                  <Checkbox
                    id="cleanup-logs"
                    checked={config.oldLogsEnabled}
                    onCheckedChange={(checked) => {
                      setConfig((prev) => ({ ...prev, oldLogsEnabled: Boolean(checked) }));
                    }}
                  />
                  <label
                    htmlFor="cleanup-logs"
                    className="flex cursor-pointer items-center gap-1.5 text-xs font-medium text-[var(--text-primary)] select-none"
                  >
                    <FileText className="h-3.5 w-3.5 text-[var(--text-muted)]" />
                    <span>清理历史运行日志</span>
                  </label>
                </div>

                <div className="w-32">
                  <Select
                    value={String(config.olderThanDays)}
                    disabled={!config.oldLogsEnabled}
                    options={CLEANUP_DAYS_OPTIONS.map((opt) => ({
                      value: String(opt.value),
                      label: opt.label,
                    }))}
                    onChange={(val) => {
                      setConfig((prev) => ({ ...prev, olderThanDays: Number(val) }));
                    }}
                  />
                </div>
              </div>

              {config.oldLogsEnabled && (
                <div className="mt-2.5 border-t border-[var(--border-subtle)]/60 pt-2 pl-6 text-[11px] text-[var(--text-muted)]">
                  {scanResult ? (
                    <span>
                      扫描到{' '}
                      <strong className="font-semibold text-[var(--text-primary)]">
                        {scanResult.oldLogFiles?.length ?? 0}
                      </strong>{' '}
                      个历史日志文件，占用{' '}
                      <strong className="font-semibold text-amber-500">
                        {formatBytes(scanResult.oldLogFilesBytes ?? 0)}
                      </strong>
                      <span className="ml-1">（正在写入的那一份始终保留）</span>
                    </span>
                  ) : (
                    <span>正在扫描历史日志...</span>
                  )}
                </div>
              )}
            </div>

            {/* Disk space & safety banner */}
            <div className="mt-3 flex items-center justify-between rounded-xl border border-[var(--border-subtle)] bg-[var(--bg-muted)]/60 px-3 py-2 text-xs">
              <div className="flex items-center gap-1.5 text-[var(--text-secondary)]">
                <HardDrive className="h-3.5 w-3.5 text-[var(--text-muted)]" />
                <span>预计清理：</span>
                <span className="font-semibold text-[var(--text-primary)]">
                  {summary.totalTasks} 个任务
                </span>
                {summary.totalFiles > 0 && (
                  <>
                    <span>·</span>
                    <span className="font-semibold text-amber-500">
                      {summary.totalFiles} 个文件
                    </span>
                  </>
                )}
                {summary.totalBytes > 0 && (
                  <>
                    <span>· 释放</span>
                    <span className="font-mono font-semibold text-emerald-500">
                      {formatBytes(summary.totalBytes)}
                    </span>
                  </>
                )}
              </div>

              <button
                type="button"
                disabled={isScanning}
                onClick={() => void scanWithConfig(config)}
                className="inline-flex items-center gap-1 text-[11px] text-[var(--text-muted)] transition-colors hover:text-[var(--text-primary)] disabled:opacity-40"
                title="重新扫描"
              >
                <RefreshCw className={`h-3 w-3 ${isScanning ? 'animate-spin' : ''}`} />
                <span>{isScanning ? '扫描中' : '重新扫描'}</span>
              </button>
            </div>

            {/* Active tasks safety notice */}
            <div className="mt-2 flex items-center gap-1.5 px-1 text-[11px] text-[var(--text-muted)]">
              <AlertTriangle className="h-3 w-3 shrink-0 text-amber-500/70" />
              <span>下载中、排队中与处理中的任务受到底层保护，绝对不会被清理。</span>
            </div>
          </div>

          {/* Footer Actions */}
          <div className="mt-3 flex shrink-0 items-center justify-end gap-2 border-t border-[var(--border-subtle)] pt-3">
            <Button
              variant="secondary"
              size="sm"
              onClick={() => onOpenChange(false)}
              disabled={isCleaning}
              className="h-8 px-3 text-xs"
            >
              取消
            </Button>
            <Button
              variant="danger"
              size="sm"
              onClick={() => void handleExecute()}
              disabled={
                !hasAnyOptionSelected ||
                isCleaning ||
                isScanning ||
                (summary.totalTasks === 0 && summary.totalFiles === 0)
              }
              className="h-8 gap-1.5 px-3.5 text-xs shadow-xs"
            >
              <Trash2 className="h-3.5 w-3.5" />
              <span>{isCleaning ? '正在清理...' : '立即清理'}</span>
            </Button>
          </div>
        </Dialog.Content>
      </Dialog.Portal>
    </Dialog.Root>
  );
}
