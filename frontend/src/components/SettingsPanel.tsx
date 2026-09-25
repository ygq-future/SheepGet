import { useState, useRef } from 'react';
import { useSettingsStore } from '../stores/settings';
import type { SettingsCorrection } from '../stores/settings';
import * as configModels from '../../bindings/sheep-get/internal/config/models';
import { Select } from './ui/Select';
import { Input } from './ui/Input';
import { Button } from './ui/Button';
import { Switch } from './ui/Switch';
import { Slider } from './ui/Slider';
import { showToast } from './ui/Toast';
import { Ports } from '../lib/protocol.generated';
import {
  OpenExtensionFolder,
  OpenFolder,
  PrepareExtensionPage,
  RestartServer,
  SelectDirectory,
  ValidateDirectory,
} from '../../bindings/sheep-get/app';
import {
  Sun,
  Moon,
  Laptop,
  Folder,
  Zap,
  Palette,
  Check,
  FolderTree,
  ArrowUp,
  ArrowDown,
  Plus,
  Trash2,
  GripVertical,
  X,
  Sliders,
  Pipette,
  FolderOpen,
  ExternalLink,
  Sparkles,
} from 'lucide-react';
import { AboutUpdatesView } from './AboutUpdatesView';
import { CleanupModal } from './CleanupModal';
import { Badge } from './ui/Badge';
import { normalizeExtensions } from '../lib/category';
import { motion, Reorder, useDragControls } from 'motion/react';

const PRESET_ACCENTS = [
  { name: '极客绿', hex: '#10b981' },
  { name: '电光蓝', hex: '#0ea5e9' },
  { name: '暗夜紫', hex: '#8b5cf6' },
  { name: '霓虹粉', hex: '#ec4899' },
  { name: '星空青', hex: '#06b6d4' },
  { name: '烈焰红', hex: '#f43f5e' },
  { name: '活力橙', hex: '#f97316' },
  { name: '琥珀金', hex: '#f59e0b' },
];

const DEFAULT_ACCENT_COLOR = PRESET_ACCENTS[0].hex;

const SHORTCUT_KEY_OPTIONS = [
  { value: 'Delete', label: 'Delete 键' },
  { value: 'Insert', label: 'Insert 键' },
  { value: 'Alt', label: 'Alt 键' },
  { value: 'Ctrl', label: 'Ctrl 键' },
  { value: 'Shift', label: 'Shift 键' },
];

type SettingsTab = 'general' | 'appearance' | 'download' | 'rules';

interface TabMeta {
  id: SettingsTab;
  label: string;
  icon: React.ComponentType<{ className?: string }>;
  title: string;
  description: string;
}

const SETTINGS_TABS: TabMeta[] = [
  {
    id: 'general',
    label: '常规',
    icon: Sliders,
    title: '常规与系统',
    description: '开机启动、剪贴板监听与存储模式',
  },
  {
    id: 'appearance',
    label: '外观',
    icon: Palette,
    title: '外观与个性化',
    description: '界面明暗主题与品牌高亮强调色',
  },
  {
    id: 'download',
    label: '下载',
    icon: Zap,
    title: '下载与网络',
    description: '路径、并发性能、下载策略与网络代理',
  },
  {
    id: 'rules',
    label: '接管与规则',
    icon: FolderTree,
    title: '接管与分类规则',
    description: '扩展通信、页面接管与分类规则',
  },
];

function SettingSection({
  title,
  description,
  action,
  children,
}: {
  title?: string;
  description?: string;
  action?: React.ReactNode;
  children: React.ReactNode;
}) {
  return (
    <div className="space-y-2">
      {(title || action) && (
        <div className="flex items-center justify-between px-1">
          <div>
            {title && <h3 className="text-xs font-semibold text-[var(--text-primary)]">{title}</h3>}
            {description && (
              <p className="mt-0.5 text-[11px] text-[var(--text-muted)]">{description}</p>
            )}
          </div>
          {action && <div>{action}</div>}
        </div>
      )}
      <div className="divide-y divide-[var(--border-subtle)] overflow-hidden rounded-xl border border-[var(--border-subtle)] bg-[var(--bg-surface)] shadow-xs">
        {children}
      </div>
    </div>
  );
}

function SettingRow({
  label,
  description,
  children,
  align = 'center',
  className = '',
}: {
  label: React.ReactNode;
  description?: React.ReactNode;
  children: React.ReactNode;
  align?: 'center' | 'top';
  className?: string;
}) {
  return (
    <div
      className={`flex ${
        align === 'top' ? 'items-start' : 'items-center'
      } justify-between gap-4 px-4 py-3 text-xs transition-colors hover:bg-[var(--bg-subtle)]/40 ${className}`}
    >
      <div className="min-w-0 flex-1">
        <div className="font-medium text-[var(--text-primary)]">{label}</div>
        {description && (
          <div className="mt-0.5 text-[11px] leading-relaxed text-[var(--text-muted)]">
            {description}
          </div>
        )}
      </div>
      <div className="flex shrink-0 items-center gap-2">{children}</div>
    </div>
  );
}

interface CustomCategoryItemProps {
  cat: configModels.CategoryConfig;
  idx: number;
  total: number;
  catNameDrafts: Record<string, string>;
  setCatNameDrafts: React.Dispatch<React.SetStateAction<Record<string, string>>>;
  catDirDrafts: Record<string, string>;
  setCatDirDrafts: React.Dispatch<React.SetStateAction<Record<string, string>>>;
  addingExtCatId: string | null;
  setAddingExtCatId: (id: string | null) => void;
  newExtVal: string;
  setNewExtVal: (val: string) => void;
  handleMoveCustomCategory: (index: number, direction: 'up' | 'down') => Promise<void>;
  handleCommitCategoryName: (catId: string, idx: number) => Promise<void>;
  handleCommitCategoryDirectory: (
    isCustom: boolean,
    index: number,
    rawPath: string,
  ) => Promise<void>;
  handleSelectCategoryDirectory: (isCustom: boolean, index: number) => Promise<void>;
  handleDeleteCustomCategory: (index: number) => Promise<void>;
  handleRemoveExtension: (isCustom: boolean, index: number, ext: string) => Promise<void>;
  handleCommitNewExtension: (isCustom: boolean, index: number) => Promise<void>;
}

function CustomCategoryItem({
  cat,
  idx,
  total,
  catNameDrafts,
  setCatNameDrafts,
  catDirDrafts,
  setCatDirDrafts,
  addingExtCatId,
  setAddingExtCatId,
  newExtVal,
  setNewExtVal,
  handleMoveCustomCategory,
  handleCommitCategoryName,
  handleCommitCategoryDirectory,
  handleSelectCategoryDirectory,
  handleDeleteCustomCategory,
  handleRemoveExtension,
  handleCommitNewExtension,
}: CustomCategoryItemProps) {
  const dragControls = useDragControls();

  return (
    <Reorder.Item
      value={cat}
      dragListener={false}
      dragControls={dragControls}
      className="space-y-2 rounded-xl border border-[var(--border-subtle)] bg-[var(--bg-surface)] p-3 transition-colors hover:border-[var(--border-hover)]"
    >
      <div className="flex items-center justify-between gap-2">
        <div className="flex min-w-0 items-center gap-1.5">
          <div
            onPointerDown={(e) => dragControls.start(e)}
            className="cursor-grab p-0.5 text-[var(--text-muted)] select-none hover:text-[var(--text-primary)] active:cursor-grabbing"
            title="拖动排序"
          >
            <GripVertical className="h-3.5 w-3.5" />
          </div>
          <div className="flex flex-col">
            <button
              type="button"
              disabled={idx === 0}
              onClick={() => {
                void handleMoveCustomCategory(idx, 'up');
              }}
              className="rounded p-0.5 text-[var(--text-muted)] hover:text-[var(--text-primary)] disabled:opacity-20"
              title="上移"
            >
              <ArrowUp className="h-2.5 w-2.5" />
            </button>
            <button
              type="button"
              disabled={idx === total - 1}
              onClick={() => {
                void handleMoveCustomCategory(idx, 'down');
              }}
              className="rounded p-0.5 text-[var(--text-muted)] hover:text-[var(--text-primary)] disabled:opacity-20"
              title="下移"
            >
              <ArrowDown className="h-2.5 w-2.5" />
            </button>
          </div>
          <Input
            value={cat.id in catNameDrafts ? catNameDrafts[cat.id] : cat.name}
            onChange={(e) => {
              setCatNameDrafts((prev) => ({ ...prev, [cat.id]: e.target.value }));
            }}
            onBlur={() => {
              void handleCommitCategoryName(cat.id, idx);
            }}
            onKeyDown={(e) => {
              if (e.key === 'Enter') {
                e.preventDefault();
                void handleCommitCategoryName(cat.id, idx);
              }
            }}
            placeholder="分类名称"
            className="h-7 w-28 px-2 text-xs font-semibold"
          />
        </div>

        <div className="flex max-w-sm min-w-0 flex-1 items-center justify-end gap-1.5">
          <Input
            value={cat.id in catDirDrafts ? catDirDrafts[cat.id] : cat.directory || ''}
            onChange={(e) => {
              setCatDirDrafts((prev) => ({ ...prev, [cat.id]: e.target.value }));
            }}
            onBlur={() => {
              const val = cat.id in catDirDrafts ? catDirDrafts[cat.id] : cat.directory || '';
              void handleCommitCategoryDirectory(true, idx, val);
              setCatDirDrafts((prev) => {
                const next = { ...prev };
                delete next[cat.id];
                return next;
              });
            }}
            onKeyDown={(e) => {
              if (e.key === 'Enter') {
                e.preventDefault();
                const val = cat.id in catDirDrafts ? catDirDrafts[cat.id] : cat.directory || '';
                void handleCommitCategoryDirectory(true, idx, val);
                setCatDirDrafts((prev) => {
                  const next = { ...prev };
                  delete next[cat.id];
                  return next;
                });
              }
            }}
            placeholder="默认保存位置"
            className="h-7 flex-1 px-2 font-mono text-xs"
          />
          <Button
            variant="secondary"
            size="sm"
            onClick={() => {
              void handleSelectCategoryDirectory(true, idx);
            }}
            className="h-7 shrink-0 gap-1 px-2 text-xs"
          >
            <Folder className="h-3 w-3" />
            <span>浏览</span>
          </Button>
          <button
            type="button"
            onClick={() => {
              void handleDeleteCustomCategory(idx);
            }}
            className="rounded p-1 text-red-500 transition-colors hover:bg-red-500/10 hover:text-red-600"
            title="删除分类"
          >
            <Trash2 className="h-3.5 w-3.5" />
          </button>
        </div>
      </div>

      <div className="flex flex-wrap items-center gap-1.5 pt-0.5">
        {(cat.extensions || []).map((ext) => (
          <span
            key={ext}
            className="group inline-flex items-center gap-1 rounded-md border border-[var(--border-subtle)] bg-[var(--bg-subtle)] px-1.5 py-0.5 font-mono text-[11px] text-[var(--text-secondary)] transition-colors hover:border-[var(--border-hover)] hover:text-[var(--text-primary)]"
          >
            <span>.{ext}</span>
            <button
              type="button"
              onClick={() => void handleRemoveExtension(true, idx, ext)}
              className="opacity-50 hover:text-red-500 hover:opacity-100"
              title={`移除 .${ext}`}
            >
              <X className="h-3 w-3" />
            </button>
          </span>
        ))}

        {addingExtCatId === cat.id ? (
          <span className="inline-flex items-center rounded-md border border-[var(--accent)] bg-[var(--accent-muted)]/20 px-1.5 py-0.5">
            <span className="font-mono text-[11px] text-[var(--accent)]">.</span>
            <input
              autoFocus
              type="text"
              value={newExtVal}
              onChange={(e) => setNewExtVal(e.target.value)}
              onKeyDown={(e) => {
                if (e.key === 'Enter') {
                  e.preventDefault();
                  void handleCommitNewExtension(true, idx);
                } else if (e.key === 'Escape') {
                  setAddingExtCatId(null);
                }
              }}
              onBlur={() => void handleCommitNewExtension(true, idx)}
              placeholder="后缀"
              className="w-14 bg-transparent font-mono text-[11px] text-[var(--text-primary)] outline-none"
            />
          </span>
        ) : (
          <button
            type="button"
            onClick={() => {
              setAddingExtCatId(cat.id);
              setNewExtVal('');
            }}
            className="inline-flex items-center gap-0.5 rounded-md border border-dashed border-[var(--border-subtle)] px-1.5 py-0.5 text-[11px] text-[var(--text-muted)] transition-colors hover:border-[var(--accent)] hover:text-[var(--accent)]"
          >
            <Plus className="h-3 w-3" />
            <span>添加</span>
          </button>
        )}
      </div>
    </Reorder.Item>
  );
}

export function SettingsPanel() {
  const { settings, storageInfo, updateSettings } = useSettingsStore();
  const [activeTab, setActiveTab] = useState<SettingsTab>('general');
  const [cleanupOpen, setCleanupOpen] = useState(false);
  const [customColor, setCustomColor] = useState('');
  const colorInputRef = useRef<HTMLInputElement | null>(null);

  const appearance = settings?.appearance || new configModels.AppearanceConfig();
  const download = settings?.download || new configModels.DownloadConfig();
  const general = settings?.general || new configModels.GeneralConfig();
  const proxy = settings?.proxy || new configModels.ProxyConfig();
  const takeover = settings?.takeover || new configModels.TakeoverConfig();
  const clipboardConfig = settings?.clipboard || new configModels.ClipboardConfig();

  const [isAddingExcludedSite, setIsAddingExcludedSite] = useState(false);
  const [newExcludedSiteVal, setNewExcludedSiteVal] = useState('');

  const [selectedProxyMode, setSelectedProxyMode] = useState<configModels.ProxyMode>(
    proxy.mode || configModels.ProxyMode.ProxyModeSystem,
  );
  const [lastSavedProxyMode, setLastSavedProxyMode] = useState(proxy.mode);
  if (proxy.mode && proxy.mode !== lastSavedProxyMode) {
    setLastSavedProxyMode(proxy.mode);
    setSelectedProxyMode(proxy.mode);
  }
  const [customProxyDraft, setCustomProxyDraft] = useState(proxy.customAddr || '');
  const [proxyError, setProxyError] = useState('');

  const [lastSavedProxyAddr, setLastSavedProxyAddr] = useState(proxy.customAddr || '');
  if (proxy.customAddr && proxy.customAddr !== lastSavedProxyAddr) {
    setLastSavedProxyAddr(proxy.customAddr);
    setCustomProxyDraft(proxy.customAddr);
  }

  const [dirInput, setDirInput] = useState(download.defaultDirectory || '');
  const [lastSavedDir, setLastSavedDir] = useState(download.defaultDirectory || '');
  if (download.defaultDirectory && download.defaultDirectory !== lastSavedDir) {
    setLastSavedDir(download.defaultDirectory);
    setDirInput(download.defaultDirectory);
  }

  const [tempDirInput, setTempDirInput] = useState(download.tempDirectory || '');
  const [lastSavedTempDir, setLastSavedTempDir] = useState(download.tempDirectory || '');
  if (download.tempDirectory && download.tempDirectory !== lastSavedTempDir) {
    setLastSavedTempDir(download.tempDirectory);
    setTempDirInput(download.tempDirectory);
  }
  const [catDirDrafts, setCatDirDrafts] = useState<Record<string, string>>({});
  const [catNameDrafts, setCatNameDrafts] = useState<Record<string, string>>({});
  const [addingExtCatId, setAddingExtCatId] = useState<string | null>(null);
  const [newExtVal, setNewExtVal] = useState('');
  const reorderTimerRef = useRef<ReturnType<typeof setTimeout> | null>(null);
  const [reorderedCategories, setReorderedCategories] = useState<
    configModels.CategoryConfig[] | null
  >(null);
  const [serverPortDraft, setServerPortDraft] = useState<string | null>(null);
  const [restartingServer, setRestartingServer] = useState(false);

  if (!settings) {
    return (
      <div className="flex h-full items-center justify-center p-8 text-xs text-[var(--text-muted)]">
        正在加载配置...
      </div>
    );
  }

  const currentTabMeta = SETTINGS_TABS.find((tab) => tab.id === activeTab) ?? SETTINGS_TABS[0];

  const handleLaunchAtStartupChange = async (checked: boolean) => {
    try {
      await updateSettings({
        general: new configModels.GeneralConfig({
          ...general,
          launchAtStartup: checked,
        }),
      });
      showToast(checked ? '已开启开机启动' : '已关闭开机启动', 'success');
    } catch (err) {
      showToast(`设置开机启动失败: ${String(err)}`, 'error');
    }
  };
  const handleSilentStartupChange = async (checked: boolean) => {
    try {
      await updateSettings({
        general: new configModels.GeneralConfig({
          ...general,
          silentStartup: checked,
        }),
      });
      showToast(checked ? '已开启静默启动' : '已关闭静默启动', 'success');
    } catch (err) {
      showToast(`设置静默启动失败: ${String(err)}`, 'error');
    }
  };

  const handleLightweightModeChange = async (checked: boolean) => {
    try {
      await updateSettings({
        general: new configModels.GeneralConfig({
          ...general,
          lightweightMode: checked,
        }),
      });
      showToast(checked ? '已开启轻量模式' : '已关闭轻量模式', 'success');
    } catch (err) {
      showToast(`设置轻量模式失败: ${String(err)}`, 'error');
    }
  };

  const handleEnableLoggingChange = async (checked: boolean) => {
    try {
      await updateSettings({
        general: new configModels.GeneralConfig({
          ...general,
          enableLogging: checked,
        }),
      });
      showToast(checked ? '已开启运行日志' : '已关闭运行日志', 'success');
    } catch (err) {
      showToast(`设置运行日志失败: ${String(err)}`, 'error');
    }
  };

  const serverPort = serverPortDraft ?? String(general.serverPort || Ports.DefaultServer);

  const handleRestartServer = async () => {
    const portNum = parseInt(serverPort, 10);
    if (isNaN(portNum) || portNum < 1024 || portNum > 65535) {
      showToast('端口号必须介于 1024 和 65535 之间', 'error');
      return;
    }
    setRestartingServer(true);
    try {
      const bound = await RestartServer(portNum);
      setServerPortDraft(null);
      showToast(`本地 HTTP 服务已就绪，当前端口: ${bound}`, 'success');
    } catch (err) {
      showToast(`重启 HTTP 服务失败: ${String(err)}`, 'error');
    } finally {
      setRestartingServer(false);
    }
  };

  const handleClipboardChange = async (checked: boolean) => {
    try {
      await updateSettings({
        clipboard: new configModels.ClipboardConfig({
          ...clipboardConfig,
          enabled: checked,
        }),
      });
      showToast(checked ? '已开启剪贴板监视' : '已关闭剪贴板监视', 'success');
    } catch (err) {
      showToast(`设置剪贴板监视失败: ${String(err)}`, 'error');
    }
  };

  const reportCorrections = (corrections: SettingsCorrection[]): boolean => {
    const [first] = corrections;
    if (!first) return false;
    showToast(first.message, 'error', '代理地址无效');
    return true;
  };

  const handleProxyModeChange = async (mode: configModels.ProxyMode) => {
    setSelectedProxyMode(mode);
    setProxyError('');

    if (mode === configModels.ProxyMode.ProxyModeCustom) {
      if (proxy.customAddr && proxy.customAddr.trim() !== '') {
        if (mode === proxy.mode) return;
        try {
          const corrections = await updateSettings({
            proxy: new configModels.ProxyConfig({
              ...proxy,
              mode: configModels.ProxyMode.ProxyModeCustom,
            }),
          });
          if (!reportCorrections(corrections)) {
            showToast('已切换至自定义代理', 'success');
          }
        } catch (err) {
          showToast(`切换代理失败: ${String(err)}`, 'error');
        }
      }
      return;
    }

    if (mode === proxy.mode) return;
    try {
      const corrections = await updateSettings({
        proxy: new configModels.ProxyConfig({
          ...proxy,
          mode,
        }),
      });
      if (!reportCorrections(corrections)) {
        showToast(
          mode === configModels.ProxyMode.ProxyModeDirect
            ? '已切换至直连模式'
            : '已切换至系统代理模式',
          'success',
        );
      }
    } catch (err) {
      showToast(`更新代理模式失败: ${String(err)}`, 'error');
    }
  };

  const handleCustomProxyCommit = async () => {
    const trimmed = customProxyDraft.trim();
    if (trimmed === '') {
      setProxyError('自定义代理地址不能为空');
      return;
    }

    try {
      const u = new URL(trimmed);
      if (!['http:', 'https:', 'socks5:'].includes(u.protocol)) {
        setProxyError('代理协议需为 http://, https:// 或 socks5://');
        return;
      }
      if (!u.host) {
        setProxyError('请输入完整的代理主机与端口 (如 http://127.0.0.1:7890)');
        return;
      }
    } catch {
      setProxyError('请输入合法的代理地址 (如 http://127.0.0.1:7890)');
      return;
    }

    if (
      trimmed === (proxy.customAddr || '') &&
      proxy.mode === configModels.ProxyMode.ProxyModeCustom
    ) {
      setProxyError('');
      return;
    }

    setProxyError('');
    try {
      const corrections = await updateSettings({
        proxy: new configModels.ProxyConfig({
          ...proxy,
          mode: configModels.ProxyMode.ProxyModeCustom,
          customAddr: trimmed,
        }),
      });
      if (reportCorrections(corrections)) {
        setCustomProxyDraft('');
        setSelectedProxyMode(configModels.ProxyMode.ProxyModeSystem);
        return;
      }
      setLastSavedProxyAddr(trimmed);
      setSelectedProxyMode(configModels.ProxyMode.ProxyModeCustom);
      showToast('自定义代理已保存并启用', 'success');
    } catch (err) {
      showToast(`保存代理地址失败: ${String(err)}`, 'error');
    }
  };

  const handleCommitNewExcludedSite = async () => {
    let clean = newExcludedSiteVal.trim().toLowerCase();
    setIsAddingExcludedSite(false);
    setNewExcludedSiteVal('');
    if (!clean) return;

    try {
      if (clean.includes('://')) {
        clean = new URL(clean).hostname;
      }
    } catch {
      // Keep as-is
    }
    clean = clean.replace(/[/]+$/, '');
    if (!clean) return;

    const currentSites = takeover.excludedSites || [];
    if (currentSites.includes(clean)) {
      return;
    }
    const updated = [...currentSites, clean];
    try {
      await updateSettings({
        takeover: new configModels.TakeoverConfig({
          ...takeover,
          excludedSites: updated,
        }),
      });
      showToast(`已添加排除站点 ${clean}`, 'success');
    } catch (err) {
      showToast(`添加排除站点失败: ${String(err)}`, 'error');
    }
  };

  const handleRemoveExcludedSite = async (site: string) => {
    const currentSites = takeover.excludedSites || [];
    const updated = currentSites.filter((s) => s !== site);
    try {
      await updateSettings({
        takeover: new configModels.TakeoverConfig({
          ...takeover,
          excludedSites: updated,
        }),
      });
    } catch (err) {
      showToast(`移除排除站点失败: ${String(err)}`, 'error');
    }
  };

  const handlePauseShortcutChange = async (key: string) => {
    try {
      await updateSettings({
        takeover: new configModels.TakeoverConfig({
          ...takeover,
          pauseShortcut: key,
        }),
      });
      showToast('暂停接管快捷键已更新', 'success');
    } catch (err) {
      showToast(`更新快捷键失败: ${String(err)}`, 'error');
    }
  };

  const handleForceShortcutChange = async (key: string) => {
    try {
      await updateSettings({
        takeover: new configModels.TakeoverConfig({
          ...takeover,
          forceShortcut: key,
        }),
      });
      showToast('强制接管快捷键已更新', 'success');
    } catch (err) {
      showToast(`更新快捷键失败: ${String(err)}`, 'error');
    }
  };

  const handleThemeChange = async (theme: configModels.ThemeMode) => {
    await updateSettings({
      appearance: new configModels.AppearanceConfig({
        ...appearance,
        theme,
      }),
    });
  };

  const handleAccentChange = async (accentColor: string) => {
    await updateSettings({
      appearance: new configModels.AppearanceConfig({
        ...appearance,
        accentColor,
      }),
    });
  };

  const handleSelectDefaultDir = async () => {
    try {
      const selected = await SelectDirectory(dirInput);
      if (selected) {
        setDirInput(selected);
        setLastSavedDir(selected);
        await updateSettings({
          download: new configModels.DownloadConfig({
            ...download,
            defaultDirectory: selected,
          }),
        });
        showToast('默认保存目录已更新', 'success');
      }
    } catch (err) {
      console.error('Failed to select default directory:', err);
    }
  };

  const handleCommitDirectory = async (rawPath: string) => {
    const trimmed = rawPath.trim();
    if (!trimmed || trimmed === download.defaultDirectory) {
      setDirInput(download.defaultDirectory || '');
      return;
    }

    try {
      const [valid, errorMsg] = await ValidateDirectory(trimmed);
      if (valid) {
        setDirInput(trimmed);
        setLastSavedDir(trimmed);
        await updateSettings({
          download: new configModels.DownloadConfig({
            ...download,
            defaultDirectory: trimmed,
          }),
        });
        showToast('默认保存目录已更新', 'success');
      } else {
        setDirInput(download.defaultDirectory || '');
        showToast(errorMsg || '所选路径不存在或不可写入', 'error', '保存路径无效');
      }
    } catch {
      setDirInput(download.defaultDirectory || '');
      showToast('目录校验异常，已恢复原路径', 'error');
    }
  };

  const handleDuplicatePolicyChange = async (policy: string) => {
    await updateSettings({
      download: new configModels.DownloadConfig({
        ...download,
        duplicateUrlPolicy: policy as configModels.DuplicateURLPolicy,
      }),
    });
  };

  const handlePreDownloadChange = async (preDownload: boolean) => {
    await updateSettings({
      download: new configModels.DownloadConfig({
        ...download,
        preDownload,
      }),
    });
  };

  const handleMaxConcurrentChange = async (val: number) => {
    await updateSettings({
      download: new configModels.DownloadConfig({
        ...download,
        maxConcurrentDownloads: val,
      }),
    });
  };

  const handleDefaultConnChange = async (val: number) => {
    await updateSettings({
      download: new configModels.DownloadConfig({
        ...download,
        defaultConnectionsPerTask: val,
      }),
    });
  };

  const handleShowProgressWindowChange = async (showProgressWindow: boolean) => {
    await updateSettings({
      download: new configModels.DownloadConfig({
        ...download,
        showProgressWindow,
      }),
    });
  };

  const handleKeepCompletedInfoChange = async (keepCompletedInfo: boolean) => {
    await updateSettings({
      download: new configModels.DownloadConfig({
        ...download,
        keepCompletedInfo,
      }),
    });
  };

  const handleAutoRemoveCompletedChange = async (autoRemoveCompletedOnOpen: boolean) => {
    await updateSettings({
      download: new configModels.DownloadConfig({
        ...download,
        autoRemoveCompletedOnOpen,
      }),
    });
  };

  const handleSelectTempDir = async () => {
    try {
      const selected = await SelectDirectory(tempDirInput);
      if (selected) {
        setTempDirInput(selected);
        setLastSavedTempDir(selected);
        await updateSettings({
          download: new configModels.DownloadConfig({
            ...download,
            tempDirectory: selected,
          }),
        });
        showToast('临时缓存目录已更新', 'success');
      }
    } catch (err) {
      console.error('Failed to select temp directory:', err);
    }
  };

  const handleCommitTempDirectory = async (rawPath: string) => {
    const trimmed = rawPath.trim();
    if (!trimmed || trimmed === download.tempDirectory) {
      setTempDirInput(download.tempDirectory || '');
      return;
    }

    try {
      const [valid, errorMsg] = await ValidateDirectory(trimmed);
      if (valid) {
        setTempDirInput(trimmed);
        setLastSavedTempDir(trimmed);
        await updateSettings({
          download: new configModels.DownloadConfig({
            ...download,
            tempDirectory: trimmed,
          }),
        });
        showToast('临时缓存目录已更新', 'success');
      } else {
        setTempDirInput(download.tempDirectory || '');
        showToast(errorMsg || '所选路径不存在或不可写入', 'error', '缓存路径无效');
      }
    } catch {
      setTempDirInput(download.tempDirectory || '');
      showToast('目录校验异常，已恢复原路径', 'error');
    }
  };

  const handleUseServerFileTimeChange = async (useServerFileTime: boolean) => {
    await updateSettings({
      download: new configModels.DownloadConfig({
        ...download,
        useServerFileTime,
      }),
    });
  };

  const isCategoryNameDuplicate = (name: string, excludeId?: string): boolean => {
    const clean = name.trim().toLowerCase();
    if (!clean) return false;
    const all = [...(download.customCategories || []), ...(download.builtinCategories || [])];
    return all.some((c) => c.id !== excludeId && c.name.trim().toLowerCase() === clean);
  };

  const handleAddCustomCategory = async () => {
    const defaultDir = download.defaultDirectory || '';
    const baseName = '新建分类';
    let name = baseName;
    let counter = 2;
    while (isCategoryNameDuplicate(name)) {
      name = `${baseName} ${counter}`;
      counter++;
    }
    const newCat = new configModels.CategoryConfig({
      id: `custom_${Date.now()}`,
      name,
      directory: defaultDir,
      extensions: [],
      isBuiltin: false,
    });
    const updatedList = [newCat, ...(download.customCategories || [])];
    await updateSettings({
      download: new configModels.DownloadConfig({
        ...download,
        customCategories: updatedList,
      }),
    });
    showToast('已添加自定义分类', 'success');
  };

  const handleCommitCategoryName = async (catId: string, idx: number) => {
    if (!(catId in catNameDrafts)) return;
    const trimmed = catNameDrafts[catId].trim();
    const cat = download.customCategories?.[idx];
    const next = { ...catNameDrafts };
    delete next[catId];
    setCatNameDrafts(next);

    if (!trimmed || !cat || trimmed === cat.name) {
      return;
    }
    if (isCategoryNameDuplicate(trimmed, catId)) {
      showToast(`分类名称【${trimmed}】已存在`, 'error', '名称冲突');
      return;
    }
    await handleUpdateCustomCategory(idx, { name: trimmed });
  };

  const handleMoveCustomCategory = async (index: number, direction: 'up' | 'down') => {
    const list = [...(download.customCategories || [])];
    const targetIndex = direction === 'up' ? index - 1 : index + 1;
    if (targetIndex < 0 || targetIndex >= list.length) return;
    const temp = list[index];
    list[index] = list[targetIndex];
    list[targetIndex] = temp;
    await updateSettings({
      download: new configModels.DownloadConfig({
        ...download,
        customCategories: list,
      }),
    });
  };

  const handleReorderCustomCategories = (newOrder: configModels.CategoryConfig[]) => {
    setReorderedCategories(newOrder);
    if (reorderTimerRef.current) {
      clearTimeout(reorderTimerRef.current);
    }
    reorderTimerRef.current = setTimeout(() => {
      void (async () => {
        await updateSettings({
          download: new configModels.DownloadConfig({
            ...download,
            customCategories: newOrder,
          }),
        });
        setReorderedCategories(null);
      })();
    }, 250);
  };

  const handleDeleteCustomCategory = async (index: number) => {
    const list = [...(download.customCategories || [])];
    list.splice(index, 1);
    await updateSettings({
      download: new configModels.DownloadConfig({
        ...download,
        customCategories: list,
      }),
    });
    showToast('已删除自定义分类', 'info');
  };

  const handleUpdateCustomCategory = async (
    index: number,
    updates: Partial<configModels.CategoryConfig>,
  ) => {
    const list = [...(download.customCategories || [])];
    list[index] = new configModels.CategoryConfig({
      ...list[index],
      ...updates,
    });
    await updateSettings({
      download: new configModels.DownloadConfig({
        ...download,
        customCategories: list,
      }),
    });
  };

  const handleUpdateBuiltinCategory = async (
    index: number,
    updates: Partial<configModels.CategoryConfig>,
  ) => {
    const list = [...(download.builtinCategories || [])];
    list[index] = new configModels.CategoryConfig({
      ...list[index],
      ...updates,
    });
    await updateSettings({
      download: new configModels.DownloadConfig({
        ...download,
        builtinCategories: list,
      }),
    });
  };

  const handleSelectCategoryDirectory = async (isCustom: boolean, index: number) => {
    try {
      const cat = isCustom
        ? download.customCategories?.[index]
        : download.builtinCategories?.[index];
      const draftVal =
        cat?.id && cat.id in catDirDrafts ? catDirDrafts[cat.id] : cat?.directory || '';
      const selected = await SelectDirectory(draftVal);
      if (selected) {
        if (isCustom) {
          await handleUpdateCustomCategory(index, { directory: selected });
        } else {
          await handleUpdateBuiltinCategory(index, { directory: selected });
        }
        showToast('分类目录已更新', 'success');
      }
    } catch (err) {
      console.error('Failed to select category directory:', err);
    }
  };

  const handleCommitCategoryDirectory = async (
    isCustom: boolean,
    index: number,
    rawPath: string,
  ) => {
    const trimmed = rawPath.trim();
    const cat = isCustom ? download.customCategories?.[index] : download.builtinCategories?.[index];
    if (!cat || trimmed === (cat.directory || '')) {
      return;
    }
    if (!trimmed) {
      if (isCustom) {
        await handleUpdateCustomCategory(index, { directory: '' });
      } else {
        await handleUpdateBuiltinCategory(index, { directory: '' });
      }
      return;
    }
    try {
      const [valid, errorMsg] = await ValidateDirectory(trimmed);
      if (valid) {
        if (isCustom) {
          await handleUpdateCustomCategory(index, { directory: trimmed });
        } else {
          await handleUpdateBuiltinCategory(index, { directory: trimmed });
        }
        showToast('分类目录已更新', 'success');
      } else {
        showToast(errorMsg || '所选路径不存在或不可写入', 'error', '保存路径无效');
      }
    } catch {
      showToast('目录校验异常', 'error');
    }
  };

  const handleRemoveExtension = async (isCustom: boolean, index: number, extToRemove: string) => {
    const cat = isCustom ? download.customCategories?.[index] : download.builtinCategories?.[index];
    if (!cat) return;
    const nextExts = (cat.extensions || []).filter(
      (e) => e.toLowerCase() !== extToRemove.toLowerCase(),
    );
    if (isCustom) {
      await handleUpdateCustomCategory(index, { extensions: nextExts });
    } else {
      await handleUpdateBuiltinCategory(index, { extensions: nextExts });
    }
  };

  const handleCommitNewExtension = async (isCustom: boolean, index: number) => {
    const cat = isCustom ? download.customCategories?.[index] : download.builtinCategories?.[index];
    if (!cat) {
      setAddingExtCatId(null);
      setNewExtVal('');
      return;
    }
    const parsed = normalizeExtensions(newExtVal);
    if (parsed.length > 0) {
      const existingLower = new Set((cat.extensions || []).map((e) => e.toLowerCase()));
      const validToAdd: string[] = [];

      for (const ext of parsed) {
        const extLower = ext.toLowerCase();
        if (existingLower.has(extLower)) {
          continue;
        }
        if (isCustom) {
          const conflict = (download.customCategories || []).find(
            (c, cIdx) =>
              cIdx !== index && (c.extensions || []).some((e) => e.toLowerCase() === extLower),
          );
          if (conflict) {
            showToast(`后缀 .${ext} 已存在于自定义分类【${conflict.name}】中`, 'error', '后缀重复');
            continue;
          }
        } else {
          const conflict = (download.builtinCategories || []).find(
            (c, cIdx) =>
              cIdx !== index && (c.extensions || []).some((e) => e.toLowerCase() === extLower),
          );
          if (conflict) {
            showToast(`后缀 .${ext} 已存在于内置分类【${conflict.name}】中`, 'error', '后缀重复');
            continue;
          }
        }
        validToAdd.push(ext);
        existingLower.add(extLower);
      }

      if (validToAdd.length > 0) {
        const nextExts = [...(cat.extensions || []), ...validToAdd];
        if (isCustom) {
          await handleUpdateCustomCategory(index, { extensions: nextExts });
        } else {
          await handleUpdateBuiltinCategory(index, { extensions: nextExts });
        }
        showToast(`已添加后缀: ${validToAdd.map((e) => `.${e}`).join(', ')}`, 'success');
      }
    }
    setAddingExtCatId(null);
    setNewExtVal('');
  };

  const handleOpenExtensionFolder = async () => {
    try {
      await OpenExtensionFolder();
    } catch (err) {
      showToast(err instanceof Error ? err.message : String(err), 'error', '打开扩展目录失败');
    }
  };

  const handlePrepareExtensionPage = async (browser: 'chrome' | 'edge') => {
    try {
      const address = await PrepareExtensionPage(browser);
      showToast(`已复制 ${address}，切到浏览器地址栏按 Ctrl+V 回车`, 'success');
    } catch (err) {
      const name = browser === 'edge' ? 'Edge' : 'Chrome';
      showToast(err instanceof Error ? err.message : String(err), 'error', `唤起 ${name} 失败`);
    }
  };

  return (
    <div className="flex h-full w-full min-w-0 flex-col font-sans">
      {/* Dynamic Header & Tab Navigation */}
      <div className="mb-4 flex items-center justify-between gap-3 border-b border-[var(--border-subtle)] pb-3">
        <div className="min-w-0 flex-1">
          <h2 className="text-base font-semibold text-[var(--text-primary)]">
            {currentTabMeta.title}
          </h2>
          <p className="mt-0.5 truncate text-xs text-[var(--text-muted)]">
            {currentTabMeta.description}
          </p>
        </div>

        {/* Tab Navigation with smooth motion pill and accent color */}
        <div className="flex shrink-0 items-center gap-1 rounded-xl border border-[var(--border-subtle)] bg-[var(--bg-surface)] p-1 shadow-xs">
          {SETTINGS_TABS.map(({ id, label, icon: Icon }) => (
            <button
              key={id}
              onClick={() => setActiveTab(id)}
              className="group relative flex items-center gap-1 rounded-lg px-2.5 py-1 text-xs font-medium transition-colors select-none"
            >
              {activeTab === id && (
                <motion.div
                  layoutId="settings-active-tab"
                  className="absolute inset-0 rounded-lg border border-[var(--border-focus)] bg-[var(--accent-muted)] shadow-xs"
                  transition={{ type: 'spring', stiffness: 400, damping: 32 }}
                />
              )}
              <span
                className={`relative z-10 flex items-center gap-1 transition-colors ${
                  activeTab === id
                    ? 'font-semibold text-[var(--accent)]'
                    : 'text-[var(--text-secondary)] group-hover:text-[var(--text-primary)]'
                }`}
              >
                <Icon className="h-3.5 w-3.5" />
                {label}
              </span>
            </button>
          ))}
        </div>
      </div>

      <div className="flex-1 overflow-y-auto pr-1">
        {/* Tab 1: General (常规与系统) */}
        {activeTab === 'general' && (
          <div className="space-y-5">
            <SettingSection title="系统行为" description="桌面启动常驻与系统剪贴板监听">
              <SettingRow label="开机自动启动" description="开机登录时自动启动并在后台托盘就绪">
                <Switch
                  checked={general.launchAtStartup}
                  onCheckedChange={(checked) => {
                    void handleLaunchAtStartupChange(checked);
                  }}
                />
              </SettingRow>
              <SettingRow label="静默启动" description="启动时不显示主窗口，仅托盘就绪">
                <Switch
                  checked={general.silentStartup}
                  onCheckedChange={(checked) => {
                    void handleSilentStartupChange(checked);
                  }}
                />
              </SettingRow>

              <SettingRow label="轻量模式" description="主窗口关闭时释放渲染进程内存">
                <Switch
                  checked={general.lightweightMode}
                  onCheckedChange={(checked) => {
                    void handleLightweightModeChange(checked);
                  }}
                />
              </SettingRow>

              <SettingRow label="监视剪贴板链接" description="复制支持的文件链接时自动弹出新建任务">
                <Switch
                  checked={clipboardConfig.enabled}
                  onCheckedChange={(checked) => {
                    void handleClipboardChange(checked);
                  }}
                />
              </SettingRow>

              <SettingRow
                label="记录运行日志"
                description="默认关闭；开启后在数据目录 logs/ 下按大小滚动记录传输与任务日志"
              >
                <Switch
                  checked={general.enableLogging}
                  onCheckedChange={(checked) => {
                    void handleEnableLoggingChange(checked);
                  }}
                />
              </SettingRow>
            </SettingSection>

            <SettingSection title="数据存储" description="应用运行数据与持久化配置">
              <SettingRow
                label={
                  <div className="flex items-center gap-2">
                    <span>存储与便携模式</span>
                    <Badge variant={storageInfo?.mode === 'portable' ? 'accent' : 'default'}>
                      {storageInfo?.mode === 'portable'
                        ? '便携模式 (Portable)'
                        : '安装模式 (Standard)'}
                    </Badge>
                  </div>
                }
                description={storageInfo?.dataDir || '默认应用数据存储目录'}
              >
                <Button
                  variant="secondary"
                  size="sm"
                  disabled={!storageInfo?.dataDir}
                  onClick={() => {
                    if (storageInfo?.dataDir) {
                      void OpenFolder(storageInfo.dataDir);
                    }
                  }}
                  className="h-8 gap-1.5 px-3 text-xs"
                >
                  <Folder className="h-3.5 w-3.5" />
                  <span>打开目录</span>
                </Button>
              </SettingRow>
            </SettingSection>

            <SettingSection
              title="版本与更新"
              description="主程序与配套浏览器扩展的版本检查、差异更新与便携升级"
            >
              <div className="p-3">
                <AboutUpdatesView />
              </div>
            </SettingSection>
          </div>
        )}

        {/* Tab 2: Appearance (外观与个性化) */}
        {activeTab === 'appearance' && (
          <div className="space-y-5">
            <SettingSection title="界面主题" description="应用界面的色彩明暗风格模式">
              <div className="p-3">
                <div className="grid grid-cols-3 gap-2.5">
                  {[
                    {
                      id: configModels.ThemeMode.ThemeSystem,
                      label: '跟随系统',
                      icon: Laptop,
                    },
                    {
                      id: configModels.ThemeMode.ThemeLight,
                      label: '浅色模式',
                      icon: Sun,
                    },
                    {
                      id: configModels.ThemeMode.ThemeDark,
                      label: '深色模式',
                      icon: Moon,
                    },
                  ].map((item) => {
                    const Icon = item.icon;
                    const isSelected = appearance.theme === item.id;
                    return (
                      <button
                        key={item.id}
                        type="button"
                        onClick={() => {
                          void handleThemeChange(item.id);
                        }}
                        className={`flex items-center justify-center gap-2 rounded-lg border py-2.5 text-xs font-medium transition-all ${
                          isSelected
                            ? 'border-[var(--accent)] bg-[var(--accent-muted)]/30 text-[var(--text-primary)] shadow-xs ring-1 ring-[var(--accent)]'
                            : 'border-[var(--border-subtle)] bg-[var(--bg-subtle)]/30 text-[var(--text-secondary)] hover:border-[var(--border-hover)] hover:text-[var(--text-primary)]'
                        }`}
                      >
                        <Icon className="h-4 w-4" />
                        <span>{item.label}</span>
                      </button>
                    );
                  })}
                </div>
              </div>
            </SettingSection>

            <SettingSection title="强调色" description="按钮、进度条指示与焦点的品牌强调颜色">
              <div className="space-y-3 p-4 text-xs transition-colors hover:bg-[var(--bg-subtle)]/40">
                <div>
                  <div className="font-medium text-[var(--text-primary)]">主题强调色</div>
                  <div className="mt-0.5 text-[11px] leading-relaxed text-[var(--text-muted)]">
                    精选预设色彩或输入自定义 Hex 颜色
                  </div>
                </div>

                <div className="flex flex-wrap items-center gap-2.5 pt-0.5">
                  {/* Preset Swatches */}
                  <div className="flex flex-wrap items-center gap-1.5">
                    {PRESET_ACCENTS.map((preset) => {
                      const isSelected =
                        appearance.accentColor?.toLowerCase() === preset.hex.toLowerCase();
                      return (
                        <button
                          key={preset.hex}
                          type="button"
                          onClick={() => {
                            void handleAccentChange(preset.hex);
                          }}
                          className={`group relative flex h-7 w-7 items-center justify-center rounded-full border border-black/10 transition-transform hover:scale-110 active:scale-95 dark:border-white/20 ${
                            isSelected
                              ? 'ring-2 ring-[var(--accent)] ring-offset-2 ring-offset-[var(--bg-surface)]'
                              : ''
                          }`}
                          style={{ backgroundColor: preset.hex }}
                          title={preset.name}
                        >
                          {isSelected && (
                            <Check className="h-3.5 w-3.5 stroke-[3] text-white drop-shadow-sm" />
                          )}
                        </button>
                      );
                    })}
                  </div>

                  {/* Refined Custom Color Picker & Hex Input */}
                  <div className="flex items-center gap-2 border-l border-[var(--border-subtle)] pl-2.5">
                    <button
                      type="button"
                      onClick={() => colorInputRef.current?.click()}
                      className="group relative flex h-7 w-7 items-center justify-center rounded-full border border-black/15 shadow-xs transition-transform hover:scale-105 active:scale-95 dark:border-white/20"
                      style={{
                        backgroundColor: appearance.accentColor || DEFAULT_ACCENT_COLOR,
                      }}
                      title="点击取色板选择自定义颜色"
                    >
                      <Pipette className="h-3.5 w-3.5 stroke-[2.5] text-white drop-shadow-xs" />
                    </button>
                    <input
                      ref={colorInputRef}
                      type="color"
                      value={appearance.accentColor || DEFAULT_ACCENT_COLOR}
                      onChange={(e) => {
                        void handleAccentChange(e.target.value);
                        setCustomColor(e.target.value);
                      }}
                      className="sr-only"
                      tabIndex={-1}
                      aria-hidden="true"
                    />

                    <div className="relative flex items-center">
                      <span
                        className="absolute left-2.5 h-2.5 w-2.5 rounded-full border border-black/10 dark:border-white/20"
                        style={{ backgroundColor: appearance.accentColor || DEFAULT_ACCENT_COLOR }}
                      />
                      <Input
                        type="text"
                        value={customColor || appearance.accentColor || ''}
                        placeholder={DEFAULT_ACCENT_COLOR}
                        onChange={(e) => setCustomColor(e.target.value)}
                        onBlur={() => {
                          if (
                            customColor &&
                            /^#([A-Fa-f0-9]{6}|[A-Fa-f0-9]{3})$/.test(customColor)
                          ) {
                            void handleAccentChange(customColor);
                          }
                        }}
                        onKeyDown={(e) => {
                          if (
                            e.key === 'Enter' &&
                            customColor &&
                            /^#([A-Fa-f0-9]{6}|[A-Fa-f0-9]{3})$/.test(customColor)
                          ) {
                            void handleAccentChange(customColor);
                          }
                        }}
                        className="h-7 w-24 pl-7 font-mono text-xs"
                      />
                    </div>
                  </div>
                </div>
              </div>
            </SettingSection>
          </div>
        )}

        {/* Tab 3: Download & Network (下载与网络) */}
        {activeTab === 'download' && (
          <div className="space-y-5">
            <SettingSection
              title="存储路径"
              description="下载保存位置与分块缓存目录"
              action={
                <Button
                  variant="secondary"
                  size="sm"
                  onClick={() => setCleanupOpen(true)}
                  className="h-7 gap-1 px-2.5 text-xs text-[var(--text-secondary)] hover:text-[var(--text-primary)]"
                >
                  <Sparkles className="h-3.5 w-3.5 text-amber-500" />
                  <span>清理历史与文件</span>
                </Button>
              }
            >
              <SettingRow label="默认保存位置" description="新建任务时的默认本地存储路径">
                <div className="flex w-80 max-w-lg flex-1 items-center gap-1.5 sm:w-[380px]">
                  <Input
                    type="text"
                    value={dirInput}
                    onChange={(e) => setDirInput(e.target.value)}
                    onBlur={() => void handleCommitDirectory(dirInput)}
                    onKeyDown={(e) => {
                      if (e.key === 'Enter') {
                        e.preventDefault();
                        void handleCommitDirectory(dirInput);
                      }
                    }}
                    placeholder="请输入有效目录路径"
                    className="h-8 flex-1 font-mono text-xs"
                  />
                  <Button
                    variant="secondary"
                    size="sm"
                    onClick={() => void handleSelectDefaultDir()}
                    className="h-8 shrink-0 gap-1 px-2.5 text-xs"
                  >
                    <Folder className="h-3.5 w-3.5" />
                    <span>浏览</span>
                  </Button>
                </div>
              </SettingRow>

              <SettingRow label="临时缓存目录" description="分块下载临时暂存路径，完成后自动转存">
                <div className="flex w-80 max-w-lg flex-1 items-center gap-1.5 sm:w-[380px]">
                  <Input
                    type="text"
                    value={tempDirInput}
                    onChange={(e) => setTempDirInput(e.target.value)}
                    onBlur={() => void handleCommitTempDirectory(tempDirInput)}
                    onKeyDown={(e) => {
                      if (e.key === 'Enter') {
                        e.preventDefault();
                        void handleCommitTempDirectory(tempDirInput);
                      }
                    }}
                    placeholder="请输入临时缓存目录路径"
                    className="h-8 flex-1 font-mono text-xs"
                  />
                  <Button
                    variant="secondary"
                    size="sm"
                    onClick={() => void handleSelectTempDir()}
                    className="h-8 shrink-0 gap-1 px-2.5 text-xs"
                  >
                    <Folder className="h-3.5 w-3.5" />
                    <span>浏览</span>
                  </Button>
                </div>
              </SettingRow>
            </SettingSection>

            <SettingSection title="传输并发性能" description="并发调度上限与多连接分块参数">
              <SettingRow
                label="全局并发任务上限"
                description="同时处于传输状态的最大任务数 (1 ~ 32)"
              >
                <div className="flex w-52 items-center gap-3">
                  <div className="flex-1">
                    <Slider
                      min={1}
                      max={32}
                      value={download.maxConcurrentDownloads || 3}
                      onChange={(val) => void handleMaxConcurrentChange(val)}
                    />
                  </div>
                  <span className="flex h-6 w-8 items-center justify-center rounded-md border border-[var(--border-subtle)] bg-[var(--bg-subtle)] font-mono text-xs font-semibold text-[var(--accent)]">
                    {download.maxConcurrentDownloads}
                  </span>
                </div>
              </SettingRow>

              <SettingRow
                label="单任务分块连接数"
                description="多连接分块下载的默认线程数 (1 ~ 32)"
              >
                <div className="flex w-52 items-center gap-3">
                  <div className="flex-1">
                    <Slider
                      min={1}
                      max={32}
                      value={download.defaultConnectionsPerTask || 8}
                      onChange={(val) => void handleDefaultConnChange(val)}
                    />
                  </div>
                  <span className="flex h-6 w-8 items-center justify-center rounded-md border border-[var(--border-subtle)] bg-[var(--bg-subtle)] font-mono text-xs font-semibold text-[var(--accent)]">
                    {download.defaultConnectionsPerTask}
                  </span>
                </div>
              </SettingRow>
            </SettingSection>

            <SettingSection title="下载策略与反馈" description="重复任务、提前下载与进度窗口行为">
              <SettingRow label="重复链接处理策略" description="已存在相同链接任务时的默认处理方式">
                <div className="w-48">
                  <Select
                    value={
                      download.duplicateUrlPolicy ||
                      configModels.DuplicateURLPolicy.DuplicatePolicyAsk
                    }
                    onChange={(val) => void handleDuplicatePolicyChange(val)}
                    options={[
                      {
                        value: configModels.DuplicateURLPolicy.DuplicatePolicyAsk,
                        label: '每次询问',
                      },
                      {
                        value: configModels.DuplicateURLPolicy.DuplicatePolicySkipShowDone,
                        label: '跳过并显示已完成',
                      },
                      {
                        value: configModels.DuplicateURLPolicy.DuplicatePolicyOverwrite,
                        label: '继续下载并覆盖',
                      },
                      {
                        value: configModels.DuplicateURLPolicy.DuplicatePolicyNumberedCopy,
                        label: '自动创建序号副本',
                      },
                    ]}
                  />
                </div>
              </SettingRow>

              <SettingRow label="提前下载" description="新建任务确认时立即在后台发起连接下载">
                <Switch
                  checked={!!download.preDownload}
                  onCheckedChange={(checked) => void handlePreDownloadChange(checked)}
                />
              </SettingRow>

              <SettingRow
                label="使用服务器修改时间"
                description="下载完成后同步服务器的文件修改时间"
              >
                <Switch
                  checked={!!download.useServerFileTime}
                  onCheckedChange={(checked) => void handleUseServerFileTimeChange(checked)}
                />
              </SettingRow>

              <SettingRow
                label="自动显示下载进度窗口"
                description="开始下载任务时自动弹出进度悬浮窗口"
              >
                <Switch
                  checked={download.showProgressWindow ?? true}
                  onCheckedChange={(checked) => void handleShowProgressWindowChange(checked)}
                />
              </SettingRow>

              <SettingRow label="保留完成信息" description="下载完成后在进度窗口保留完成记录">
                <Switch
                  checked={download.keepCompletedInfo ?? true}
                  onCheckedChange={(checked) => void handleKeepCompletedInfoChange(checked)}
                />
              </SettingRow>

              <SettingRow
                label="打开后自动移除完成信息"
                description="打开文件或文件夹后自动移除该完成记录"
              >
                <Switch
                  checked={!!download.autoRemoveCompletedOnOpen}
                  onCheckedChange={(checked) => void handleAutoRemoveCompletedChange(checked)}
                />
              </SettingRow>
            </SettingSection>

            {/* Network Proxy Integrated Section */}
            <SettingSection title="网络代理" description="配置客户端下载与元数据探测的网络代理">
              <SettingRow label="代理模式" description="选择直连、遵循系统代理或手动指定代理">
                <div className="w-52">
                  <Select
                    value={selectedProxyMode}
                    onChange={(val) => void handleProxyModeChange(val)}
                    options={[
                      {
                        value: configModels.ProxyMode.ProxyModeDirect,
                        label: '直连 (不使用代理)',
                        description: '所有网络请求直接连接目标服务器',
                      },
                      {
                        value: configModels.ProxyMode.ProxyModeSystem,
                        label: '系统代理 (默认)',
                        description: '读取并遵循操作系统的网络代理设置',
                      },
                      {
                        value: configModels.ProxyMode.ProxyModeCustom,
                        label: '自定义代理',
                        description: '手动指定代理服务器地址与端口',
                      },
                    ]}
                  />
                </div>
              </SettingRow>

              {selectedProxyMode === configModels.ProxyMode.ProxyModeCustom && (
                <SettingRow
                  label="自定义代理地址"
                  description="支持 HTTP/HTTPS 与 SOCKS5 代理协议"
                  align="top"
                >
                  <div className="flex flex-col items-end gap-1.5">
                    <div className="flex items-center gap-2">
                      <Input
                        value={customProxyDraft}
                        onChange={(e) => {
                          setCustomProxyDraft(e.target.value);
                          setProxyError('');
                        }}
                        onKeyDown={(e) => {
                          if (e.key === 'Enter') {
                            e.preventDefault();
                            void handleCustomProxyCommit();
                          }
                        }}
                        placeholder="http://127.0.0.1:7890"
                        className="h-8 w-64 font-mono text-xs"
                      />
                      <Button
                        size="sm"
                        onClick={() => void handleCustomProxyCommit()}
                        className="h-8 shrink-0 text-xs"
                      >
                        {proxy.mode === configModels.ProxyMode.ProxyModeCustom
                          ? '保存'
                          : '保存并启用'}
                      </Button>
                    </div>
                    {proxyError && (
                      <p className="text-[11px] font-medium text-rose-500">{proxyError}</p>
                    )}
                  </div>
                </SettingRow>
              )}
            </SettingSection>
          </div>
        )}

        {/* Tab 4: Rules & Interception (接管与规则) */}
        {activeTab === 'rules' && (
          <div className="space-y-5">
            <SettingSection
              title="浏览器扩展与接管"
              description="配置本地通信端口、排除站点与临时快捷键"
            >
              <div className="space-y-3 p-4">
                <div className="flex items-center justify-between gap-2.5">
                  <div className="shrink-0 text-xs font-semibold text-[var(--text-primary)]">
                    配套扩展快速安装
                  </div>
                  <div className="flex flex-wrap items-center justify-end gap-1.5">
                    <Button
                      variant="secondary"
                      size="sm"
                      onClick={() => void handleOpenExtensionFolder()}
                      className="h-7 gap-1 px-2 text-xs font-medium"
                      title="在系统文件管理器中打开并高亮扩展文件夹"
                    >
                      <FolderOpen className="h-3.5 w-3.5" />
                      <span>定位扩展目录</span>
                    </Button>
                    <Button
                      variant="secondary"
                      size="sm"
                      onClick={() => void handlePrepareExtensionPage('chrome')}
                      className="h-7 gap-1 px-2 text-xs"
                      title="唤起 Chrome，并把扩展管理页地址写入剪贴板"
                    >
                      <ExternalLink className="h-3.5 w-3.5" />
                      <span>Chrome 扩展</span>
                    </Button>
                    <Button
                      variant="secondary"
                      size="sm"
                      onClick={() => void handlePrepareExtensionPage('edge')}
                      className="h-7 gap-1 px-2 text-xs"
                      title="唤起 Edge，并把扩展管理页地址写入剪贴板"
                    >
                      <ExternalLink className="h-3.5 w-3.5" />
                      <span>Edge 扩展</span>
                    </Button>
                  </div>
                </div>

                <div className="space-y-1.5 rounded-lg border border-[var(--border-subtle)] bg-[var(--bg-subtle)]/50 p-3 text-[11px]">
                  <div className="flex items-center gap-1.5 font-medium text-[var(--text-primary)]">
                    <span className="flex h-4 w-4 items-center justify-center rounded-full bg-[var(--accent)]/15 font-mono text-[10px] text-[var(--accent)]">
                      i
                    </span>
                    <span>快速加载指引（三键一拖）</span>
                  </div>
                  <ol className="list-inside list-decimal space-y-1 pl-0.5 leading-relaxed text-[var(--text-secondary)]">
                    <li>
                      点击「<strong>Chrome / Edge 扩展</strong>」，到地址栏按{' '}
                      <strong>Ctrl+V</strong>、<strong>回车</strong>并在扩展页开启「
                      <strong>开发者模式</strong>」。
                    </li>
                    <li>
                      点击「<strong>定位扩展目录</strong>」，系统将自动打开并高亮配套扩展文件夹。
                    </li>
                    <li>
                      将高亮的扩展文件夹直接<strong>拖入浏览器扩展页面</strong>完成加载。
                    </li>
                  </ol>
                  <p className="pl-0.5 leading-relaxed text-[var(--text-muted)]">
                    扩展管理页属于浏览器特权页面，需在地址栏粘贴进入，程序无法直接打开。
                  </p>
                </div>
              </div>

              <SettingRow
                label="本地 HTTP 服务端口"
                description={`扩展通信监听端口，默认 ${Ports.DefaultServer}，修改后需重启`}
              >
                <div className="flex items-center gap-2">
                  <Input
                    type="text"
                    inputMode="numeric"
                    maxLength={5}
                    value={serverPort}
                    onChange={(e) => setServerPortDraft(e.target.value.replace(/\D/g, ''))}
                    className="w-20 text-center font-mono text-xs"
                  />
                  <Button
                    variant="secondary"
                    size="sm"
                    disabled={restartingServer}
                    onClick={() => void handleRestartServer()}
                    className="h-8 text-xs"
                  >
                    {restartingServer ? '重启中…' : '重启服务'}
                  </Button>
                </div>
              </SettingRow>

              <div className="space-y-2.5 p-4 text-xs transition-colors hover:bg-[var(--bg-subtle)]/40">
                <div>
                  <div className="font-medium text-[var(--text-primary)]">排除页面站点</div>
                  <div className="mt-0.5 text-[11px] leading-relaxed text-[var(--text-muted)]">
                    指定站点及其子域名不触发自动接管，保留浏览器下载
                  </div>
                </div>

                <div className="flex flex-wrap items-center gap-1.5 pt-0.5">
                  {(takeover.excludedSites || []).map((site) => (
                    <span
                      key={site}
                      className="group inline-flex items-center gap-1 rounded-md border border-[var(--border-subtle)] bg-[var(--bg-subtle)] px-2 py-0.5 font-mono text-[11px] text-[var(--text-secondary)] transition-colors select-none hover:border-[var(--border-hover)] hover:text-[var(--text-primary)]"
                    >
                      <span>{site}</span>
                      <button
                        type="button"
                        onClick={() => void handleRemoveExcludedSite(site)}
                        className="opacity-50 hover:text-red-500 hover:opacity-100"
                        title={`移除 ${site}`}
                      >
                        <X className="h-3 w-3" />
                      </button>
                    </span>
                  ))}

                  {isAddingExcludedSite ? (
                    <span className="inline-flex items-center rounded-md border border-[var(--accent)] bg-[var(--accent-muted)]/20 px-2 py-0.5">
                      <input
                        autoFocus
                        type="text"
                        value={newExcludedSiteVal}
                        onChange={(e) => setNewExcludedSiteVal(e.target.value)}
                        onKeyDown={(e) => {
                          if (e.key === 'Enter') {
                            e.preventDefault();
                            void handleCommitNewExcludedSite();
                          } else if (e.key === 'Escape') {
                            setIsAddingExcludedSite(false);
                          }
                        }}
                        onBlur={() => void handleCommitNewExcludedSite()}
                        placeholder="如 *.github.com"
                        className="w-32 bg-transparent font-mono text-[11px] text-[var(--text-primary)] outline-none"
                      />
                    </span>
                  ) : (
                    <button
                      type="button"
                      onClick={() => {
                        setIsAddingExcludedSite(true);
                        setNewExcludedSiteVal('');
                      }}
                      className="inline-flex items-center gap-1 rounded-md border border-dashed border-[var(--border-subtle)] px-2 py-0.5 text-[11px] text-[var(--text-muted)] transition-colors hover:border-[var(--accent)] hover:text-[var(--accent)]"
                    >
                      <Plus className="h-3 w-3" />
                      <span>添加站点</span>
                    </button>
                  )}
                </div>
              </div>

              <SettingRow label="临时接管快捷键" description="按住按键临时反转接管策略，松开恢复">
                <div className="flex items-center gap-2">
                  <div className="flex items-center gap-1">
                    <span className="text-[11px] text-[var(--text-muted)]">暂停:</span>
                    <div className="w-28">
                      <Select
                        value={takeover.pauseShortcut || 'Delete'}
                        onChange={(val) => void handlePauseShortcutChange(val)}
                        options={SHORTCUT_KEY_OPTIONS}
                      />
                    </div>
                  </div>
                  <div className="flex items-center gap-1">
                    <span className="text-[11px] text-[var(--text-muted)]">强制:</span>
                    <div className="w-28">
                      <Select
                        value={takeover.forceShortcut || 'Insert'}
                        onChange={(val) => void handleForceShortcutChange(val)}
                        options={SHORTCUT_KEY_OPTIONS}
                      />
                    </div>
                  </div>
                </div>
              </SettingRow>
            </SettingSection>

            {/* Custom Categories Section */}
            <div className="space-y-2">
              <div className="flex items-center justify-between px-1">
                <div>
                  <div className="flex items-center gap-2">
                    <h3 className="text-xs font-semibold text-[var(--text-primary)]">自定义分类</h3>
                    <Badge variant="accent">{download.customCategories?.length || 0}</Badge>
                  </div>
                  <p className="mt-0.5 text-[11px] text-[var(--text-muted)]">
                    优先按用户排序匹配；可拖动左侧滑块调整优先级
                  </p>
                </div>
                <Button
                  variant="primary"
                  size="sm"
                  onClick={() => void handleAddCustomCategory()}
                  className="h-7 gap-1 px-2.5 text-xs"
                >
                  <Plus className="h-3.5 w-3.5" />
                  <span>新建分类</span>
                </Button>
              </div>

              {(!download.customCategories || download.customCategories.length === 0) && (
                <div className="rounded-xl border border-dashed border-[var(--border-subtle)] bg-[var(--bg-surface)] py-6 text-center text-xs text-[var(--text-muted)]">
                  暂无自定义分类。点击右上角“新建分类”添加规则。
                </div>
              )}

              <Reorder.Group
                as="div"
                axis="y"
                values={reorderedCategories ?? download.customCategories ?? []}
                onReorder={(newOrder) => handleReorderCustomCategories(newOrder)}
                className="space-y-2"
              >
                {(reorderedCategories ?? download.customCategories ?? []).map((cat, idx) => (
                  <CustomCategoryItem
                    key={cat.id || idx}
                    cat={cat}
                    idx={idx}
                    total={download.customCategories?.length || 0}
                    catNameDrafts={catNameDrafts}
                    setCatNameDrafts={setCatNameDrafts}
                    catDirDrafts={catDirDrafts}
                    setCatDirDrafts={setCatDirDrafts}
                    addingExtCatId={addingExtCatId}
                    setAddingExtCatId={setAddingExtCatId}
                    newExtVal={newExtVal}
                    setNewExtVal={setNewExtVal}
                    handleMoveCustomCategory={handleMoveCustomCategory}
                    handleCommitCategoryName={handleCommitCategoryName}
                    handleCommitCategoryDirectory={handleCommitCategoryDirectory}
                    handleSelectCategoryDirectory={handleSelectCategoryDirectory}
                    handleDeleteCustomCategory={handleDeleteCustomCategory}
                    handleRemoveExtension={handleRemoveExtension}
                    handleCommitNewExtension={handleCommitNewExtension}
                  />
                ))}
              </Reorder.Group>
            </div>

            {/* Builtin Categories Section */}
            <div className="space-y-2 pt-1">
              <div className="flex items-center justify-between px-1">
                <div>
                  <div className="flex items-center gap-2">
                    <h3 className="text-xs font-semibold text-[var(--text-primary)]">内置分类</h3>
                    <Badge variant="outline">{download.builtinCategories?.length || 6}</Badge>
                  </div>
                  <p className="mt-0.5 text-[11px] text-[var(--text-muted)]">
                    预置文件类型规则；自定义分类未命中时在此兜底匹配
                  </p>
                </div>
              </div>

              <div className="space-y-2">
                {download.builtinCategories?.map((cat, idx) => {
                  const isFallback = cat.id === 'builtin-file' || cat.name === '文件';
                  return (
                    <div
                      key={cat.id || idx}
                      className="space-y-2 rounded-xl border border-[var(--border-subtle)] bg-[var(--bg-surface)] p-3 transition-colors hover:border-[var(--border-hover)]"
                    >
                      <div className="flex items-center justify-between gap-2">
                        <div className="flex items-center gap-2">
                          <span className="text-xs font-semibold text-[var(--text-primary)]">
                            {cat.name}
                          </span>
                          <Badge variant={isFallback ? 'warning' : 'outline'}>
                            {isFallback ? '默认兜底' : '内置'}
                          </Badge>
                        </div>

                        <div className="flex max-w-sm min-w-0 flex-1 items-center justify-end gap-1.5">
                          <Input
                            value={
                              cat.id in catDirDrafts ? catDirDrafts[cat.id] : cat.directory || ''
                            }
                            onChange={(e) => {
                              setCatDirDrafts({ ...catDirDrafts, [cat.id]: e.target.value });
                            }}
                            onBlur={() => {
                              const val =
                                cat.id in catDirDrafts ? catDirDrafts[cat.id] : cat.directory || '';
                              void handleCommitCategoryDirectory(false, idx, val);
                              const next = { ...catDirDrafts };
                              delete next[cat.id];
                              setCatDirDrafts(next);
                            }}
                            onKeyDown={(e) => {
                              if (e.key === 'Enter') {
                                e.preventDefault();
                                const val =
                                  cat.id in catDirDrafts
                                    ? catDirDrafts[cat.id]
                                    : cat.directory || '';
                                void handleCommitCategoryDirectory(false, idx, val);
                                const next = { ...catDirDrafts };
                                delete next[cat.id];
                                setCatDirDrafts(next);
                              }
                            }}
                            placeholder="留空则使用默认保存位置"
                            className="h-7 flex-1 px-2 font-mono text-xs"
                          />
                          <Button
                            variant="secondary"
                            size="sm"
                            onClick={() => void handleSelectCategoryDirectory(false, idx)}
                            className="h-7 shrink-0 gap-1 px-2 text-xs"
                          >
                            <Folder className="h-3 w-3" />
                            <span>浏览</span>
                          </Button>
                        </div>
                      </div>

                      <div className="flex flex-wrap items-center gap-1.5 pt-0.5">
                        {(cat.extensions || []).map((ext) => (
                          <span
                            key={ext}
                            className="group inline-flex items-center gap-1 rounded-md border border-[var(--border-subtle)] bg-[var(--bg-subtle)] px-1.5 py-0.5 font-mono text-[11px] text-[var(--text-secondary)] transition-colors hover:border-[var(--border-hover)] hover:text-[var(--text-primary)]"
                          >
                            <span>.{ext}</span>
                            <button
                              type="button"
                              onClick={() => void handleRemoveExtension(false, idx, ext)}
                              className="opacity-50 hover:text-red-500 hover:opacity-100"
                              title={`移除 .${ext}`}
                            >
                              <X className="h-3 w-3" />
                            </button>
                          </span>
                        ))}

                        {addingExtCatId === cat.id ? (
                          <span className="inline-flex items-center rounded-md border border-[var(--accent)] bg-[var(--accent-muted)]/20 px-1.5 py-0.5">
                            <span className="font-mono text-[11px] text-[var(--accent)]">.</span>
                            <input
                              autoFocus
                              type="text"
                              value={newExtVal}
                              onChange={(e) => setNewExtVal(e.target.value)}
                              onKeyDown={(e) => {
                                if (e.key === 'Enter') {
                                  e.preventDefault();
                                  void handleCommitNewExtension(false, idx);
                                } else if (e.key === 'Escape') {
                                  setAddingExtCatId(null);
                                }
                              }}
                              onBlur={() => void handleCommitNewExtension(false, idx)}
                              placeholder="后缀"
                              className="w-14 bg-transparent font-mono text-[11px] text-[var(--text-primary)] outline-none"
                            />
                          </span>
                        ) : (
                          <button
                            type="button"
                            onClick={() => {
                              setAddingExtCatId(cat.id);
                              setNewExtVal('');
                            }}
                            className="inline-flex items-center gap-0.5 rounded-md border border-dashed border-[var(--border-subtle)] px-1.5 py-0.5 text-[11px] text-[var(--text-muted)] transition-colors hover:border-[var(--accent)] hover:text-[var(--accent)]"
                          >
                            <Plus className="h-3 w-3" />
                            <span>添加</span>
                          </button>
                        )}

                        {isFallback && (
                          <span className="pl-1 text-[10px] text-[var(--text-muted)]">
                            未匹配后缀的文件默认归入此目录
                          </span>
                        )}
                      </div>
                    </div>
                  );
                })}
              </div>
            </div>
          </div>
        )}
      </div>
      <CleanupModal open={cleanupOpen} onOpenChange={setCleanupOpen} />
    </div>
  );
}
