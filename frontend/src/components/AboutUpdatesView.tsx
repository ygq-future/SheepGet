import { useState, useEffect } from 'react';
import { Events } from '@wailsio/runtime';
import {
  CheckAppUpdate,
  CheckExtensionUpdate,
  GetInstalledExtensionVersion,
  DownloadAppUpdate,
  ApplyAppUpdate,
  UpdateExtension,
  OpenExtensionFolder,
} from '../../bindings/sheep-get/app';
import type {
  AppUpdateResult,
  ExtensionUpdateResult,
  DownloadProgress,
} from '../../bindings/sheep-get/internal/update/models';
import { Event } from '../lib/protocol.generated';
import { Button } from './ui/Button';
import { Badge } from './ui/Badge';
import { showToast } from './ui/Toast';
import {
  RefreshCw,
  Download,
  CheckCircle2,
  Sparkles,
  Laptop,
  Puzzle,
  FolderOpen,
  ArrowUpCircle,
  ExternalLink,
} from 'lucide-react';
import { useSettingsStore } from '../stores/settings';

function formatBytes(bytes: number): string {
  if (bytes <= 0) return '0 B';
  const units = ['B', 'KB', 'MB', 'GB'];
  const i = Math.floor(Math.log(bytes) / Math.log(1024));
  return `${(bytes / Math.pow(1024, i)).toFixed(2)} ${units[i]}`;
}

function formatSpeed(bps: number): string {
  return `${formatBytes(bps)}/s`;
}

export function AboutUpdatesView() {
  const { storageInfo } = useSettingsStore();
  const isPortable = storageInfo?.mode === 'portable';

  // App Update State
  const [checkingApp, setCheckingApp] = useState(false);
  const [appUpdate, setAppUpdate] = useState<AppUpdateResult | null>(null);
  const [downloadingApp, setDownloadingApp] = useState(false);
  const [appProgress, setAppProgress] = useState<DownloadProgress | null>(null);
  const [downloadedAppPath, setDownloadedAppPath] = useState<string | null>(null);
  const [applyingApp, setApplyingApp] = useState(false);

  // Extension Update State
  const [installedExtVer, setInstalledExtVer] = useState<string>('读取中...');
  const [checkingExt, setCheckingExt] = useState(false);
  const [extUpdate, setExtUpdate] = useState<ExtensionUpdateResult | null>(null);
  const [updatingExt, setUpdatingExt] = useState(false);
  const [extProgress, setExtProgress] = useState<DownloadProgress | null>(null);

  // Load installed extension version on mount
  useEffect(() => {
    void (async () => {
      try {
        const ver = await GetInstalledExtensionVersion();
        setInstalledExtVer(ver || '未安装');
      } catch {
        setInstalledExtVer('未知版本');
      }
    })();
  }, []);

  // Listen for real-time progress events
  useEffect(() => {
    const unlistenApp = Events.On(Event.UpdateAppProgress, (ev: unknown) => {
      const payload = ev as { data?: DownloadProgress } | DownloadProgress;
      const progress =
        'data' in payload && payload.data ? payload.data : (payload as DownloadProgress);
      if (progress && typeof progress.percentage === 'number') {
        setAppProgress(progress);
      }
    });

    const unlistenExt = Events.On(Event.UpdateExtensionProgress, (ev: unknown) => {
      const payload = ev as { data?: DownloadProgress } | DownloadProgress;
      const progress =
        'data' in payload && payload.data ? payload.data : (payload as DownloadProgress);
      if (progress && typeof progress.percentage === 'number') {
        setExtProgress(progress);
      }
    });

    return () => {
      unlistenApp();
      unlistenExt();
    };
  }, []);

  // Handle App update check
  const handleCheckAppUpdate = async () => {
    setCheckingApp(true);
    setDownloadedAppPath(null);
    try {
      const res = await CheckAppUpdate();
      setAppUpdate(res);
      if (res.hasUpdate) {
        showToast(`发现应用新版本 v${res.latestVersion}`, 'info');
      } else {
        showToast('当前已是最新应用版本', 'success');
      }
    } catch (err) {
      showToast(`检查应用更新失败: ${String(err)}`, 'error');
    } finally {
      setCheckingApp(false);
    }
  };

  // Handle App download
  const handleDownloadApp = async () => {
    if (!appUpdate?.assetUrl) {
      showToast('未找到适配当前平台的下载资产', 'error');
      return;
    }
    setDownloadingApp(true);
    setAppProgress({
      downloadedBytes: 0,
      totalBytes: appUpdate.assetSize,
      percentage: 0,
      speedBps: 0,
    });
    try {
      const targetPath = await DownloadAppUpdate(appUpdate.assetUrl);
      setDownloadedAppPath(targetPath);
      showToast('应用安装包下载完成', 'success');
    } catch (err) {
      showToast(`下载更新失败: ${String(err)}`, 'error');
    } finally {
      setDownloadingApp(false);
    }
  };

  // Handle Apply App Update
  const handleApplyAppUpdate = async () => {
    if (!downloadedAppPath) return;
    setApplyingApp(true);
    try {
      await ApplyAppUpdate(downloadedAppPath);
      if (isPortable) {
        showToast('正在应用更新，程序即将重启...', 'info');
      } else {
        showToast('已唤起安装程序，正在退出应用...', 'info');
      }
    } catch (err) {
      showToast(`应用更新失败: ${String(err)}`, 'error');
      setApplyingApp(false);
    }
  };

  // Handle Extension update check
  const handleCheckExtUpdate = async () => {
    setCheckingExt(true);
    try {
      const res = await CheckExtensionUpdate();
      setExtUpdate(res);
      if (res.hasUpdate) {
        showToast(`发现浏览器扩展新版本 v${res.latestVersion}`, 'info');
      } else {
        showToast('浏览器扩展已是最新版本', 'success');
      }
    } catch (err) {
      showToast(`检查扩展更新失败: ${String(err)}`, 'error');
    } finally {
      setCheckingExt(false);
    }
  };

  // Handle Extension update apply
  const handleUpdateExt = async () => {
    if (!extUpdate?.assetUrl) {
      showToast('未找到扩展更新资产', 'error');
      return;
    }
    setUpdatingExt(true);
    setExtProgress({
      downloadedBytes: 0,
      totalBytes: extUpdate.assetSize,
      percentage: 0,
      speedBps: 0,
    });
    try {
      await UpdateExtension(extUpdate.assetUrl);
      showToast('浏览器扩展更新成功！请在浏览器扩展管理页点击重新加载', 'success');
      // Refresh local version display
      const freshVer = await GetInstalledExtensionVersion();
      setInstalledExtVer(freshVer);
      setExtUpdate((prev) =>
        prev ? { ...prev, hasUpdate: false, currentVersion: freshVer } : null,
      );
    } catch (err) {
      showToast(`更新扩展失败: ${String(err)}`, 'error');
    } finally {
      setUpdatingExt(false);
    }
  };

  return (
    <div className="space-y-6">
      {/* 1. Desktop App Update Card */}
      <div className="rounded-xl border border-[var(--border-subtle)] bg-[var(--bg-card)] p-5 shadow-xs">
        <div className="flex items-start justify-between gap-4">
          <div className="space-y-1">
            <div className="flex items-center gap-2">
              <Laptop className="h-5 w-5 text-[var(--accent)]" />
              <h3 className="text-sm font-semibold text-[var(--text-primary)]">
                SheepGet 桌面应用
              </h3>
              <Badge variant="outline" className="font-mono text-[11px]">
                {isPortable ? '便携版 (Portable)' : '安装版 (Setup)'}
              </Badge>
            </div>
            <p className="text-xs text-[var(--text-secondary)]">
              当前版本：
              <span className="font-mono font-medium text-[var(--text-primary)]">
                v{appUpdate?.currentVersion || '1.0.0'}
              </span>
            </p>
          </div>

          <Button
            variant="secondary"
            size="sm"
            onClick={() => void handleCheckAppUpdate()}
            disabled={checkingApp || downloadingApp || applyingApp}
            className="h-8 gap-1.5 px-3 text-xs"
          >
            <RefreshCw className={`h-3.5 w-3.5 ${checkingApp ? 'animate-spin' : ''}`} />
            <span>{checkingApp ? '检查中...' : '检查应用更新'}</span>
          </Button>
        </div>

        {/* Update result info */}
        {appUpdate && (
          <div className="mt-4 space-y-3 rounded-lg border border-[var(--border-subtle)] bg-[var(--bg-subtle)]/60 p-4">
            {appUpdate.hasUpdate ? (
              <>
                <div className="flex items-center justify-between">
                  <div className="flex items-center gap-2 text-xs font-medium text-[var(--text-primary)]">
                    <Sparkles className="h-4 w-4 text-[var(--accent)]" />
                    <span>发现新版本 v{appUpdate.latestVersion}</span>
                    {appUpdate.assetSize > 0 && (
                      <span className="font-normal text-[var(--text-muted)]">
                        ({formatBytes(appUpdate.assetSize)})
                      </span>
                    )}
                  </div>
                  {appUpdate.releaseUrl && (
                    <a
                      href={appUpdate.releaseUrl}
                      target="_blank"
                      rel="noreferrer"
                      className="flex items-center gap-1 text-[11px] text-[var(--accent)] hover:underline"
                    >
                      <span>更新日志</span>
                      <ExternalLink className="h-3 w-3" />
                    </a>
                  )}
                </div>

                {appUpdate.releaseNotes && (
                  <div className="max-h-32 overflow-y-auto rounded border border-[var(--border-subtle)] bg-[var(--bg-card)] p-2.5 font-mono text-[11px] whitespace-pre-wrap text-[var(--text-secondary)]">
                    {appUpdate.releaseNotes}
                  </div>
                )}

                <div className="text-[11px] text-[var(--text-muted)]">
                  {isPortable ? (
                    <span>
                      💡
                      便携模式：自动下载并安全解压替换程序文件，严格保留您的配置与下载数据，并自动重启。
                    </span>
                  ) : (
                    <span>💡 安装模式：下载完成后将自动唤起安装向导进行版本覆盖升级。</span>
                  )}
                </div>

                {/* Progress bar during download */}
                {downloadingApp && appProgress && (
                  <div className="space-y-1.5 pt-1">
                    <div className="flex justify-between text-[11px]">
                      <span className="text-[var(--text-secondary)]">正在下载更新包...</span>
                      <span className="font-mono text-[var(--text-primary)]">
                        {appProgress.percentage.toFixed(1)}% (
                        {formatBytes(appProgress.downloadedBytes)} /{' '}
                        {formatBytes(appProgress.totalBytes)})
                      </span>
                    </div>
                    <div className="h-2 w-full overflow-hidden rounded-full bg-[var(--border-subtle)]">
                      <div
                        className="h-full bg-[var(--accent)] transition-all duration-200"
                        style={{ width: `${Math.min(100, Math.max(0, appProgress.percentage))}%` }}
                      />
                    </div>
                    <div className="text-right font-mono text-[10px] text-[var(--text-muted)]">
                      {formatSpeed(appProgress.speedBps)}
                    </div>
                  </div>
                )}

                {/* Action buttons */}
                <div className="flex items-center justify-end gap-2 pt-1">
                  {!downloadedAppPath ? (
                    <Button
                      variant="primary"
                      size="sm"
                      onClick={() => void handleDownloadApp()}
                      disabled={downloadingApp}
                      className="h-8 gap-1.5 px-3 text-xs"
                    >
                      <Download className="h-3.5 w-3.5" />
                      <span>{downloadingApp ? '下载中...' : '下载更新包'}</span>
                    </Button>
                  ) : (
                    <Button
                      variant="primary"
                      size="sm"
                      onClick={() => void handleApplyAppUpdate()}
                      disabled={applyingApp}
                      className="h-8 gap-1.5 bg-emerald-600 px-3 text-xs text-white hover:bg-emerald-500"
                    >
                      <ArrowUpCircle className="h-3.5 w-3.5" />
                      <span>{isPortable ? '重启并完成更新' : '立即启动安装程序'}</span>
                    </Button>
                  )}
                </div>
              </>
            ) : (
              <div className="flex items-center gap-2 text-xs text-emerald-500">
                <CheckCircle2 className="h-4 w-4" />
                <span>当前应用已是最新稳定版本 (v{appUpdate.currentVersion})</span>
              </div>
            )}
          </div>
        )}
      </div>

      {/* 2. Decoupled Browser Extension Update Card */}
      <div className="rounded-xl border border-[var(--border-subtle)] bg-[var(--bg-card)] p-5 shadow-xs">
        <div className="flex items-start justify-between gap-4">
          <div className="space-y-1">
            <div className="flex items-center gap-2">
              <Puzzle className="h-5 w-5 text-indigo-500" />
              <h3 className="text-sm font-semibold text-[var(--text-primary)]">
                配套浏览器扩展 (Extension)
              </h3>
              <Badge variant="outline" className="font-mono text-[11px]">
                Chrome / Edge MV3
              </Badge>
            </div>
            <p className="text-xs text-[var(--text-secondary)]">
              本地扩展版本：
              <span className="font-mono font-medium text-[var(--text-primary)]">
                v{installedExtVer}
              </span>
            </p>
          </div>

          <div className="flex items-center gap-2">
            <Button
              variant="secondary"
              size="sm"
              onClick={() => void OpenExtensionFolder()}
              className="h-8 gap-1 px-2.5 text-xs font-medium"
              title="在系统文件管理器中打开扩展文件夹"
            >
              <FolderOpen className="h-3.5 w-3.5" />
              <span>定位目录</span>
            </Button>

            <Button
              variant="secondary"
              size="sm"
              onClick={() => void handleCheckExtUpdate()}
              disabled={checkingExt || updatingExt}
              className="h-8 gap-1.5 px-3 text-xs"
            >
              <RefreshCw className={`h-3.5 w-3.5 ${checkingExt ? 'animate-spin' : ''}`} />
              <span>{checkingExt ? '检查中...' : '检查扩展更新'}</span>
            </Button>
          </div>
        </div>

        {/* Extension update details */}
        {extUpdate && (
          <div className="mt-4 space-y-3 rounded-lg border border-[var(--border-subtle)] bg-[var(--bg-subtle)]/60 p-4">
            {extUpdate.hasUpdate ? (
              <>
                <div className="flex items-center justify-between text-xs font-medium text-[var(--text-primary)]">
                  <div className="flex items-center gap-2">
                    <Sparkles className="h-4 w-4 text-indigo-500" />
                    <span>发现扩展新版本 v{extUpdate.latestVersion}</span>
                    {extUpdate.assetSize > 0 && (
                      <span className="font-normal text-[var(--text-muted)]">
                        ({formatBytes(extUpdate.assetSize)})
                      </span>
                    )}
                  </div>
                </div>

                <div className="text-[11px] text-[var(--text-muted)]">
                  💡
                  扩展与主应用采用分离解耦架构：可随时独立更新扩展组件，无需重新下载或重启主应用。
                </div>

                {updatingExt && extProgress && (
                  <div className="space-y-1.5 pt-1">
                    <div className="flex justify-between text-[11px]">
                      <span className="text-[var(--text-secondary)]">正在下载并替换扩展...</span>
                      <span className="font-mono text-[var(--text-primary)]">
                        {extProgress.percentage.toFixed(1)}%
                      </span>
                    </div>
                    <div className="h-2 w-full overflow-hidden rounded-full bg-[var(--border-subtle)]">
                      <div
                        className="h-full bg-indigo-500 transition-all duration-200"
                        style={{ width: `${Math.min(100, Math.max(0, extProgress.percentage))}%` }}
                      />
                    </div>
                  </div>
                )}

                <div className="flex items-center justify-end gap-2 pt-1">
                  <Button
                    variant="primary"
                    size="sm"
                    onClick={() => void handleUpdateExt()}
                    disabled={updatingExt}
                    className="h-8 gap-1.5 bg-indigo-600 px-3 text-xs text-white hover:bg-indigo-500"
                  >
                    <Download className="h-3.5 w-3.5" />
                    <span>{updatingExt ? '更新中...' : '一键更新扩展'}</span>
                  </Button>
                </div>
              </>
            ) : (
              <div className="flex items-center gap-2 text-xs text-emerald-500">
                <CheckCircle2 className="h-4 w-4" />
                <span>浏览器扩展已是最新版本 (v{installedExtVer})</span>
              </div>
            )}
          </div>
        )}
      </div>
    </div>
  );
}
