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
import { SelectDirectory, ValidateDirectory } from '../../bindings/sheep-get/app';
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
  Globe,
  Wifi,
  RotateCcw,
  Info,
} from 'lucide-react';
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
      {/* Row 1: Name, Directory, Actions */}
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

      {/* Row 2: Tag stream with inline Add button */}
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
  const [activeTab, setActiveTab] = useState<
    'general' | 'appearance' | 'download' | 'categories' | 'takeover' | 'network'
  >('general');
  const [customColor, setCustomColor] = useState('');
  const appearance = settings?.appearance || new configModels.AppearanceConfig();
  const download = settings?.download || new configModels.DownloadConfig();
  const general = settings?.general || new configModels.GeneralConfig();
  const proxy = settings?.proxy || new configModels.ProxyConfig();
  const takeover = settings?.takeover || new configModels.TakeoverConfig();
  const clipboardConfig = settings?.clipboard || new configModels.ClipboardConfig();

  const [isAddingTakeoverExt, setIsAddingTakeoverExt] = useState(false);
  const [newTakeoverExtVal, setNewTakeoverExtVal] = useState('');
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
  if (!settings) {
    return (
      <div className="flex h-full items-center justify-center p-8 text-xs text-[var(--text-muted)]">
        正在加载配置...
      </div>
    );
  }

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

  // 后端校验回退了提交值时，用错误提示代替成功提示，让用户知道该值未被采用。
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

    // Dirty check: if address hasn't changed AND mode is already custom, do nothing!
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
        // 后端未采用该地址并回退到系统代理，草稿与选中模式同步为实际生效值。
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
  const handleCommitNewTakeoverExt = async () => {
    const clean = newTakeoverExtVal.replace(/^\./, '').trim().toLowerCase();
    setIsAddingTakeoverExt(false);
    setNewTakeoverExtVal('');
    if (!clean) return;

    const currentExts = takeover.extensions || [];
    if (currentExts.includes(clean)) {
      return;
    }
    const updated = [...currentExts, clean];
    try {
      await updateSettings({
        takeover: new configModels.TakeoverConfig({
          ...takeover,
          extensions: updated,
        }),
      });
      showToast(`已添加后缀 .${clean}`, 'success');
    } catch (err) {
      showToast(`添加后缀失败: ${String(err)}`, 'error');
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

  const handleRemoveTakeoverExt = async (ext: string) => {
    const currentExts = takeover.extensions || [];
    const updated = currentExts.filter((e) => e !== ext);
    try {
      await updateSettings({
        takeover: new configModels.TakeoverConfig({
          ...takeover,
          extensions: updated,
        }),
      });
    } catch (err) {
      showToast(`移除后缀失败: ${String(err)}`, 'error');
    }
  };

  const handleResetTakeoverExts = async () => {
    try {
      await updateSettings({
        takeover: new configModels.TakeoverConfig({
          ...takeover,
          extensions: [],
        }),
      });
      showToast('已重置为预置常用接管后缀', 'success');
    } catch (err) {
      showToast(`重置失败: ${String(err)}`, 'error');
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
      const selected = await SelectDirectory();
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
        // Immediately revert to valid saved directory and pop toast
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
      const selected = await SelectDirectory();
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
      const selected = await SelectDirectory();
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
      }
    }
    setAddingExtCatId(null);
    setNewExtVal('');
  };

  return (
    <div className="flex h-full w-full min-w-0 flex-col font-sans">
      <div className="mb-6 flex items-center justify-between border-b border-[var(--border-subtle)] pb-4">
        <div>
          <h2 className="text-base font-semibold text-[var(--text-primary)]">偏好设置</h2>
          <p className="mt-0.5 text-xs text-[var(--text-muted)]">管理应用外观与下载规则配置</p>
        </div>

        {/* Tab Navigation with smooth motion pill and accent color */}
        <div className="flex flex-wrap gap-1 rounded-xl border border-[var(--border-subtle)] bg-[var(--bg-surface)] p-1 shadow-xs">
          {[
            { id: 'general', label: '常规', icon: Sliders },
            { id: 'appearance', label: '外观', icon: Palette },
            { id: 'download', label: '下载', icon: Zap },
            { id: 'categories', label: '分类', icon: FolderTree },
            { id: 'takeover', label: '接管', icon: Globe },
            { id: 'network', label: '代理', icon: Wifi },
          ].map(({ id, label, icon: Icon }) => (
            <button
              key={id}
              onClick={() => setActiveTab(id as typeof activeTab)}
              className="group relative flex items-center gap-1.5 rounded-lg px-3 py-1.5 text-xs font-medium transition-colors select-none"
            >
              {activeTab === id && (
                <motion.div
                  layoutId="settings-active-tab"
                  className="absolute inset-0 rounded-lg border border-[var(--border-focus)] bg-[var(--accent-muted)] shadow-xs"
                  transition={{ type: 'spring', stiffness: 400, damping: 32 }}
                />
              )}
              <span
                className={`relative z-10 flex items-center gap-1.5 transition-colors ${
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
        {/* General Tab */}
        {activeTab === 'general' && (
          <div className="space-y-6">
            {/* Launch at startup */}
            <div className="rounded-xl border border-[var(--border-subtle)] bg-[var(--bg-surface)] p-4">
              <div className="flex items-center justify-between">
                <div>
                  <label className="text-xs font-semibold text-[var(--text-primary)]">
                    开机自动启动
                  </label>
                  <p className="mt-0.5 text-[11px] text-[var(--text-muted)]">
                    开机登录系统时自动启动 SheepGet 并在后台托盘就绪
                  </p>
                </div>
                <Switch
                  checked={general.launchAtStartup}
                  onCheckedChange={(checked) => {
                    void handleLaunchAtStartupChange(checked);
                  }}
                />
              </div>
            </div>

            {/* Clipboard Monitoring */}
            <div className="rounded-xl border border-[var(--border-subtle)] bg-[var(--bg-surface)] p-4">
              <div className="flex items-center justify-between">
                <div>
                  <label className="text-xs font-semibold text-[var(--text-primary)]">
                    监视剪贴板链接
                  </label>
                  <p className="mt-0.5 text-[11px] text-[var(--text-muted)]">
                    复制匹配接管后缀的文件下载链接时自动呼出下载确认窗口
                  </p>
                </div>
                <Switch
                  checked={clipboardConfig.enabled}
                  onCheckedChange={(checked) => {
                    void handleClipboardChange(checked);
                  }}
                />
              </div>
            </div>

            {/* System Tray Behavior Note */}
            <div className="rounded-xl border border-[var(--border-subtle)] bg-[var(--bg-surface)] p-4">
              <div className="flex items-start gap-3">
                <div className="mt-0.5 flex h-6 w-6 shrink-0 items-center justify-center rounded-lg bg-[var(--accent-muted)] text-[var(--accent)]">
                  <Info className="h-3.5 w-3.5" />
                </div>
                <div className="space-y-1 text-xs">
                  <div className="font-semibold text-[var(--text-primary)]">常驻系统托盘</div>
                  <p className="text-[11px] leading-relaxed text-[var(--text-muted)]">
                    关闭主窗口后，SheepGet
                    会固定最小化常驻在系统托盘中，保持已有下载任务与剪贴板/浏览器监听。如需彻底退出程序，请在系统托盘图标右键菜单中点击「退出」。
                  </p>
                </div>
              </div>
            </div>

            {/* Storage Info */}
            <div className="rounded-xl border border-[var(--border-subtle)] bg-[var(--bg-surface)] p-4">
              <label className="text-xs font-semibold text-[var(--text-primary)]">
                存储与便携模式
              </label>
              <p className="mt-0.5 text-[11px] text-[var(--text-muted)]">
                应用配置与下载任务数据独立保存在本地
              </p>
              <div className="mt-3 flex items-center gap-2">
                <Badge variant={storageInfo?.mode === 'portable' ? 'accent' : 'default'}>
                  {storageInfo?.mode === 'portable' ? '便携模式 (Portable)' : '安装模式 (Standard)'}
                </Badge>
                <span
                  className="truncate font-mono text-xs text-[var(--text-secondary)]"
                  title={storageInfo?.dataDir || ''}
                >
                  {storageInfo?.dataDir || '默认数据目录'}
                </span>
              </div>
            </div>
          </div>
        )}

        {/* Appearance Tab */}
        {activeTab === 'appearance' && (
          <div className="space-y-6">
            {/* Theme Mode */}
            <div className="rounded-xl border border-[var(--border-subtle)] bg-[var(--bg-surface)] p-4">
              <label className="text-xs font-semibold text-[var(--text-primary)]">主题模式</label>
              <p className="mt-0.5 text-[11px] text-[var(--text-muted)]">
                选择应用界面模式，支持跟随系统明暗设置切换
              </p>

              <div className="mt-3 grid grid-cols-3 gap-3">
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
                      onClick={() => {
                        void handleThemeChange(item.id);
                      }}
                      className={`flex items-center justify-center gap-2 rounded-lg border p-3 text-xs font-medium transition-all ${
                        isSelected
                          ? 'border-[var(--accent)] bg-[var(--accent-muted)] text-[var(--text-primary)] shadow-xs'
                          : 'border-[var(--border-subtle)] bg-[var(--bg-surface)] text-[var(--text-secondary)] hover:border-[var(--border-hover)] hover:text-[var(--text-primary)]'
                      }`}
                    >
                      <Icon className="h-4 w-4" />
                      {item.label}
                    </button>
                  );
                })}
              </div>
            </div>

            {/* Accent Color */}
            <div className="rounded-xl border border-[var(--border-subtle)] bg-[var(--bg-surface)] p-4">
              <label className="text-xs font-semibold text-[var(--text-primary)]">强调色</label>
              <p className="mt-0.5 text-[11px] text-[var(--text-muted)]">
                自定义按钮、进度条及高亮焦点的强调颜色
              </p>

              <div className="mt-3 flex flex-wrap items-center gap-2.5">
                {PRESET_ACCENTS.map((preset) => {
                  const isSelected =
                    appearance.accentColor?.toLowerCase() === preset.hex.toLowerCase();
                  return (
                    <button
                      key={preset.hex}
                      onClick={() => {
                        void handleAccentChange(preset.hex);
                      }}
                      className="group relative flex h-7 w-7 items-center justify-center rounded-full border border-black/10 transition-transform hover:scale-105 active:scale-95 dark:border-white/20"
                      style={{ backgroundColor: preset.hex }}
                      title={preset.name}
                    >
                      {isSelected && (
                        <Check className="h-4 w-4 stroke-[3] text-white drop-shadow-md" />
                      )}
                    </button>
                  );
                })}

                {/* Custom Color Picker */}
                <div className="ml-2 flex items-center gap-2 border-l border-[var(--border-subtle)] pl-3">
                  <input
                    type="color"
                    value={appearance.accentColor || '#10b981'}
                    onChange={(e) => {
                      void handleAccentChange(e.target.value);
                    }}
                    className="h-7 w-7 cursor-pointer appearance-none rounded-lg border border-[var(--border-subtle)] bg-transparent p-0"
                    title="选择自定义颜色"
                  />
                  <Input
                    type="text"
                    value={customColor || appearance.accentColor || ''}
                    placeholder="#10b981"
                    onChange={(e) => setCustomColor(e.target.value)}
                    onBlur={() => {
                      if (customColor && /^#([A-Fa-f0-9]{6}|[A-Fa-f0-9]{3})$/.test(customColor)) {
                        void handleAccentChange(customColor);
                      }
                    }}
                    className="w-20 font-mono text-[11px]"
                  />
                </div>
              </div>
            </div>
          </div>
        )}

        {/* Download Tab */}
        {activeTab === 'download' && (
          <div className="space-y-6">
            {/* Default Directory with editable input and validation */}
            <div className="rounded-xl border border-[var(--border-subtle)] bg-[var(--bg-surface)] p-4">
              <label className="text-xs font-semibold text-[var(--text-primary)]">
                默认保存位置
              </label>
              <p className="mt-0.5 text-[11px] text-[var(--text-muted)]">
                新建下载时默认使用的本地存储路径，支持直接编辑输入或浏览选择
              </p>

              <div className="mt-2.5 flex items-center gap-2">
                <Input
                  type="text"
                  value={dirInput}
                  onChange={(e) => {
                    setDirInput(e.target.value);
                  }}
                  onBlur={() => {
                    void handleCommitDirectory(dirInput);
                  }}
                  onKeyDown={(e) => {
                    if (e.key === 'Enter') {
                      e.preventDefault();
                      void handleCommitDirectory(dirInput);
                    }
                  }}
                  placeholder="请输入有效目录路径"
                  className="flex-1 font-mono text-xs"
                />
                <Button
                  variant="secondary"
                  size="md"
                  onClick={() => {
                    void handleSelectDefaultDir();
                  }}
                  className="shrink-0 gap-1.5"
                >
                  <Folder className="h-3.5 w-3.5" />
                  <span>浏览选择</span>
                </Button>
              </div>
            </div>

            {/* Temp Directory with editable input and validation */}
            <div className="rounded-xl border border-[var(--border-subtle)] bg-[var(--bg-surface)] p-4">
              <label className="text-xs font-semibold text-[var(--text-primary)]">
                临时缓存目录
              </label>
              <p className="mt-0.5 text-[11px] text-[var(--text-muted)]">
                分块下载与中转临时文件存放路径。下载完成后将自动安全转存至目标保存位置
              </p>

              <div className="mt-2.5 flex items-center gap-2">
                <Input
                  type="text"
                  value={tempDirInput}
                  onChange={(e) => {
                    setTempDirInput(e.target.value);
                  }}
                  onBlur={() => {
                    void handleCommitTempDirectory(tempDirInput);
                  }}
                  onKeyDown={(e) => {
                    if (e.key === 'Enter') {
                      e.preventDefault();
                      void handleCommitTempDirectory(tempDirInput);
                    }
                  }}
                  placeholder="请输入临时缓存目录路径"
                  className="flex-1 font-mono text-xs"
                />
                <Button
                  variant="secondary"
                  size="md"
                  onClick={() => {
                    void handleSelectTempDir();
                  }}
                  className="shrink-0 gap-1.5"
                >
                  <Folder className="h-3.5 w-3.5" />
                  <span>浏览选择</span>
                </Button>
              </div>
            </div>
            {/* Concurrency & Connections */}
            <div className="grid grid-cols-2 gap-4">
              <div className="rounded-xl border border-[var(--border-subtle)] bg-[var(--bg-surface)] p-4">
                <label className="text-xs font-semibold text-[var(--text-primary)]">
                  全局并发任务上限
                </label>
                <p className="mt-0.5 text-[11px] text-[var(--text-muted)]">
                  同时处于下载传输状态的最大任务数 (1 ~ 32)
                </p>
                <div className="mt-3 flex items-center gap-3">
                  <div className="flex-1">
                    <Slider
                      min={1}
                      max={16}
                      value={download.maxConcurrentDownloads || 3}
                      onChange={(val) => {
                        void handleMaxConcurrentChange(val);
                      }}
                    />
                  </div>
                  <span className="flex h-6 w-8 items-center justify-center rounded-md border border-[var(--border-subtle)] bg-[var(--bg-subtle)] font-mono text-xs font-semibold text-[var(--accent)]">
                    {download.maxConcurrentDownloads}
                  </span>
                </div>
              </div>

              <div className="rounded-xl border border-[var(--border-subtle)] bg-[var(--bg-surface)] p-4">
                <label className="text-xs font-semibold text-[var(--text-primary)]">
                  单任务默认分块连接数
                </label>
                <p className="mt-0.5 text-[11px] text-[var(--text-muted)]">
                  支持多线程分块下载时的默认连接数 (1 ~ 32)
                </p>
                <div className="mt-3 flex items-center gap-3">
                  <div className="flex-1">
                    <Slider
                      min={1}
                      max={32}
                      value={download.defaultConnectionsPerTask || 8}
                      onChange={(val) => {
                        void handleDefaultConnChange(val);
                      }}
                    />
                  </div>
                  <span className="flex h-6 w-8 items-center justify-center rounded-md border border-[var(--border-subtle)] bg-[var(--bg-subtle)] font-mono text-xs font-semibold text-[var(--accent)]">
                    {download.defaultConnectionsPerTask}
                  </span>
                </div>
              </div>
            </div>

            {/* Duplicate Policy & Pre-Download */}
            <div className="space-y-4 rounded-xl border border-[var(--border-subtle)] bg-[var(--bg-surface)] p-4">
              <div className="flex items-center justify-between">
                <div>
                  <label className="text-xs font-semibold text-[var(--text-primary)]">
                    重复链接处理策略
                  </label>
                  <p className="mt-0.5 text-[11px] text-[var(--text-muted)]">
                    检测到已存在相同 URL 任务时的全局默认行为
                  </p>
                </div>
                <div className="w-48">
                  <Select
                    value={
                      download.duplicateUrlPolicy ||
                      configModels.DuplicateURLPolicy.DuplicatePolicyAsk
                    }
                    onChange={(val) => {
                      void handleDuplicatePolicyChange(val);
                    }}
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
              </div>

              <div className="border-t border-[var(--border-subtle)] pt-4">
                <div className="flex items-center justify-between">
                  <div>
                    <label className="text-xs font-semibold text-[var(--text-primary)]">
                      提前下载
                    </label>
                    <p className="mt-0.5 text-[11px] text-[var(--text-muted)]">
                      打开新建确认窗口时立即在后台下载数据
                    </p>
                  </div>
                  <Switch
                    checked={!!download.preDownload}
                    onCheckedChange={(checked) => {
                      void handlePreDownloadChange(checked);
                    }}
                  />
                </div>
              </div>

              <div className="border-t border-[var(--border-subtle)] pt-4">
                <div className="flex items-center justify-between">
                  <div className="pr-4">
                    <label className="text-xs font-semibold text-[var(--text-primary)]">
                      使用服务器修改时间
                    </label>
                    <p className="mt-0.5 text-[11px] text-[var(--text-muted)]">
                      下载完成后将本地文件修改时间设置为服务器提供的修改时间，未提供则使用完成时间。
                    </p>
                  </div>
                  <Switch
                    checked={!!download.useServerFileTime}
                    onCheckedChange={(checked) => {
                      void handleUseServerFileTimeChange(checked);
                    }}
                  />
                </div>
              </div>
            </div>

            {/* Progress Window & Completed Info Settings */}
            <div className="space-y-4 rounded-xl border border-[var(--border-subtle)] bg-[var(--bg-surface)] p-4">
              <div className="flex items-center justify-between">
                <div>
                  <label className="text-xs font-semibold text-[var(--text-primary)]">
                    自动显示下载进度窗口
                  </label>
                  <p className="mt-0.5 text-[11px] text-[var(--text-muted)]">
                    开始下载任务时自动唤起共享下载进度窗口
                  </p>
                </div>
                <Switch
                  checked={download.showProgressWindow ?? true}
                  onCheckedChange={(checked) => {
                    void handleShowProgressWindowChange(checked);
                  }}
                />
              </div>

              <div className="border-t border-[var(--border-subtle)] pt-4">
                <div className="flex items-center justify-between">
                  <div>
                    <label className="text-xs font-semibold text-[var(--text-primary)]">
                      保留完成信息
                    </label>
                    <p className="mt-0.5 text-[11px] text-[var(--text-muted)]">
                      任务下载完成后在进度窗口顶部保留该条完成记录与操作
                    </p>
                  </div>
                  <Switch
                    checked={download.keepCompletedInfo ?? true}
                    onCheckedChange={(checked) => {
                      void handleKeepCompletedInfoChange(checked);
                    }}
                  />
                </div>
              </div>

              <div className="border-t border-[var(--border-subtle)] pt-4">
                <div className="flex items-center justify-between">
                  <div>
                    <label className="text-xs font-semibold text-[var(--text-primary)]">
                      打开后自动移除完成信息
                    </label>
                    <p className="mt-0.5 text-[11px] text-[var(--text-muted)]">
                      在进度窗口点击打开文件或打开文件夹后，自动移除该条完成信息
                    </p>
                  </div>
                  <Switch
                    checked={!!download.autoRemoveCompletedOnOpen}
                    onCheckedChange={(checked) => {
                      void handleAutoRemoveCompletedChange(checked);
                    }}
                  />
                </div>
              </div>
            </div>
          </div>
        )}

        {/* Categories Tab */}
        {activeTab === 'categories' && (
          <div className="space-y-4">
            {/* Header info */}
            <div className="rounded-xl border border-[var(--border-subtle)] bg-[var(--bg-surface)] p-3">
              <div className="flex items-center gap-2">
                <FolderTree className="h-4 w-4 text-[var(--accent)]" />
                <span className="text-xs font-semibold text-[var(--text-primary)]">
                  分类保存规则
                </span>
              </div>
              <p className="mt-1 text-[11px] text-[var(--text-muted)]">
                根据文件后缀自动分类并分配保存目录；新建下载窗口中的手动指定优先。
              </p>
            </div>

            {/* Custom Categories */}
            <div className="space-y-2.5">
              <div className="flex items-center justify-between px-1">
                <div className="flex items-center gap-2">
                  <h3 className="text-xs font-semibold text-[var(--text-primary)]">自定义分类</h3>
                  <Badge variant="accent">{download.customCategories?.length || 0}</Badge>
                </div>
                <Button
                  variant="primary"
                  size="sm"
                  onClick={() => {
                    void handleAddCustomCategory();
                  }}
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
                onReorder={(newOrder) => {
                  handleReorderCustomCategories(newOrder);
                }}
                className="space-y-2.5"
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

            {/* Builtin Categories */}
            <div className="space-y-2.5 border-t border-[var(--border-subtle)] pt-4">
              <div className="flex items-center justify-between px-1">
                <div className="flex items-center gap-2">
                  <h3 className="text-xs font-semibold text-[var(--text-primary)]">内置分类</h3>
                  <Badge variant="outline">{download.builtinCategories?.length || 6}</Badge>
                </div>
              </div>

              {download.builtinCategories?.map((cat, idx) => {
                const isFallback = cat.id === 'builtin-file' || cat.name === '文件';
                return (
                  <div
                    key={cat.id || idx}
                    className="space-y-2 rounded-xl border border-[var(--border-subtle)] bg-[var(--bg-surface)] p-3"
                  >
                    {/* Row 1: Name, Badge, Directory */}
                    <div className="flex items-center justify-between gap-2">
                      <div className="flex items-center gap-2">
                        <span className="text-xs font-semibold text-[var(--text-primary)]">
                          {cat.name}
                        </span>
                        <Badge variant={isFallback ? 'warning' : 'outline'}>
                          {isFallback ? '默认' : '内置'}
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
                                cat.id in catDirDrafts ? catDirDrafts[cat.id] : cat.directory || '';
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
                          onClick={() => {
                            void handleSelectCategoryDirectory(false, idx);
                          }}
                          className="h-7 shrink-0 gap-1 px-2 text-xs"
                        >
                          <Folder className="h-3 w-3" />
                          <span>浏览</span>
                        </Button>
                      </div>
                    </div>

                    {/* Row 2: Tag stream with inline Add button */}
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
        )}

        {/* Takeover Tab */}
        {activeTab === 'takeover' && (
          <div className="space-y-6">
            {/* Automatic Takeover Extensions */}
            <div className="rounded-xl border border-[var(--border-subtle)] bg-[var(--bg-surface)] p-4">
              <div className="flex items-center justify-between">
                <div>
                  <label className="text-xs font-semibold text-[var(--text-primary)]">
                    自动接管文件类型
                  </label>
                  <p className="mt-0.5 text-[11px] text-[var(--text-muted)]">
                    配置浏览器扩展与剪贴板监视时自动触发接管的文件后缀
                  </p>
                </div>
                <Button
                  variant="ghost"
                  size="sm"
                  onClick={() => {
                    void handleResetTakeoverExts();
                  }}
                  className="h-7 text-xs text-[var(--text-muted)] hover:text-[var(--text-primary)]"
                >
                  <RotateCcw className="mr-1 h-3 w-3" />
                  恢复默认
                </Button>
              </div>

              {/* Tag stream with inline Add button */}
              <div className="mt-3 flex flex-wrap items-center gap-1.5">
                {(takeover.extensions || []).map((ext) => (
                  <span
                    key={ext}
                    className="group inline-flex items-center gap-1 rounded-md border border-[var(--border-subtle)] bg-[var(--bg-subtle)] px-1.5 py-0.5 font-mono text-[11px] text-[var(--text-secondary)] transition-colors select-none hover:border-[var(--border-hover)] hover:text-[var(--text-primary)]"
                  >
                    <span>.{ext}</span>
                    <button
                      type="button"
                      onClick={() => {
                        void handleRemoveTakeoverExt(ext);
                      }}
                      className="opacity-50 hover:text-red-500 hover:opacity-100"
                      title={`移除 .${ext}`}
                    >
                      <X className="h-3 w-3" />
                    </button>
                  </span>
                ))}

                {isAddingTakeoverExt ? (
                  <span className="inline-flex items-center rounded-md border border-[var(--accent)] bg-[var(--accent-muted)]/20 px-1.5 py-0.5">
                    <span className="font-mono text-[11px] text-[var(--accent)]">.</span>
                    <input
                      autoFocus
                      type="text"
                      value={newTakeoverExtVal}
                      onChange={(e) => setNewTakeoverExtVal(e.target.value)}
                      onKeyDown={(e) => {
                        if (e.key === 'Enter') {
                          e.preventDefault();
                          void handleCommitNewTakeoverExt();
                        } else if (e.key === 'Escape') {
                          setIsAddingTakeoverExt(false);
                        }
                      }}
                      onBlur={() => void handleCommitNewTakeoverExt()}
                      placeholder="后缀"
                      className="w-16 bg-transparent font-mono text-[11px] text-[var(--text-primary)] outline-none"
                    />
                  </span>
                ) : (
                  <button
                    type="button"
                    onClick={() => {
                      setIsAddingTakeoverExt(true);
                      setNewTakeoverExtVal('');
                    }}
                    className="inline-flex items-center gap-0.5 rounded-md border border-dashed border-[var(--border-subtle)] px-1.5 py-0.5 text-[11px] text-[var(--text-muted)] transition-colors hover:border-[var(--accent)] hover:text-[var(--accent)]"
                  >
                    <Plus className="h-3 w-3" />
                    <span>添加</span>
                  </button>
                )}
              </div>
            </div>

            {/* Excluded Sites */}
            <div className="rounded-xl border border-[var(--border-subtle)] bg-[var(--bg-surface)] p-4">
              <label className="text-xs font-semibold text-[var(--text-primary)]">
                排除页面站点
              </label>
              <p className="mt-0.5 text-[11px] text-[var(--text-muted)]">
                以下站点及其子域名中发起的下载保留在浏览器中进行
              </p>

              {/* Tag stream with inline Add button */}
              <div className="mt-3 flex flex-wrap items-center gap-1.5">
                {(takeover.excludedSites || []).map((site) => (
                  <span
                    key={site}
                    className="group inline-flex items-center gap-1 rounded-md border border-[var(--border-subtle)] bg-[var(--bg-subtle)] px-1.5 py-0.5 font-mono text-[11px] text-[var(--text-secondary)] transition-colors select-none hover:border-[var(--border-hover)] hover:text-[var(--text-primary)]"
                  >
                    <span>{site}</span>
                    <button
                      type="button"
                      onClick={() => {
                        void handleRemoveExcludedSite(site);
                      }}
                      className="opacity-50 hover:text-red-500 hover:opacity-100"
                      title={`移除 ${site}`}
                    >
                      <X className="h-3 w-3" />
                    </button>
                  </span>
                ))}

                {isAddingExcludedSite ? (
                  <span className="inline-flex items-center rounded-md border border-[var(--accent)] bg-[var(--accent-muted)]/20 px-1.5 py-0.5">
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
                      className="w-28 bg-transparent font-mono text-[11px] text-[var(--text-primary)] outline-none"
                    />
                  </span>
                ) : (
                  <button
                    type="button"
                    onClick={() => {
                      setIsAddingExcludedSite(true);
                      setNewExcludedSiteVal('');
                    }}
                    className="inline-flex items-center gap-0.5 rounded-md border border-dashed border-[var(--border-subtle)] px-1.5 py-0.5 text-[11px] text-[var(--text-muted)] transition-colors hover:border-[var(--accent)] hover:text-[var(--accent)]"
                  >
                    <Plus className="h-3 w-3" />
                    <span>添加</span>
                  </button>
                )}
              </div>
            </div>

            {/* Temporary Shortcuts */}
            <div className="rounded-xl border border-[var(--border-subtle)] bg-[var(--bg-surface)] p-4">
              <label className="text-xs font-semibold text-[var(--text-primary)]">
                临时控制快捷键
              </label>
              <p className="mt-0.5 text-[11px] text-[var(--text-muted)]">
                在浏览器点击链接时按住对应按键临时改变接管策略，松开立即恢复默认规则
              </p>

              <div className="mt-4 grid grid-cols-1 gap-4 sm:grid-cols-2">
                <div>
                  <label className="text-[11px] font-medium text-[var(--text-secondary)]">
                    临时暂停接管快捷键
                  </label>
                  <p className="mt-0.5 text-[10px] text-[var(--text-muted)]">
                    按住时本次下载交由浏览器处理
                  </p>
                  <div className="mt-1.5 max-w-[180px]">
                    <Select
                      value={takeover.pauseShortcut || 'Alt'}
                      onChange={(val) => {
                        void handlePauseShortcutChange(val);
                      }}
                      options={[
                        { value: 'Alt', label: 'Alt 键' },
                        { value: 'Ctrl', label: 'Ctrl 键' },
                        { value: 'Shift', label: 'Shift 键' },
                      ]}
                    />
                  </div>
                </div>
                <div>
                  <label className="text-[11px] font-medium text-[var(--text-secondary)]">
                    强制接管下载快捷键
                  </label>
                  <p className="mt-0.5 text-[10px] text-[var(--text-muted)]">
                    按住时忽略文件后缀与站点排除规则强制接管
                  </p>
                  <div className="mt-1.5 max-w-[180px]">
                    <Select
                      value={takeover.forceShortcut || 'Ctrl'}
                      onChange={(val) => {
                        void handleForceShortcutChange(val);
                      }}
                      options={[
                        { value: 'Ctrl', label: 'Ctrl 键' },
                        { value: 'Alt', label: 'Alt 键' },
                        { value: 'Shift', label: 'Shift 键' },
                      ]}
                    />
                  </div>
                </div>
              </div>
            </div>
          </div>
        )}

        {/* Network Tab */}
        {activeTab === 'network' && (
          <div className="space-y-6">
            {/* Proxy Mode */}
            <div className="rounded-xl border border-[var(--border-subtle)] bg-[var(--bg-surface)] p-4">
              <label className="text-xs font-semibold text-[var(--text-primary)]">代理模式</label>
              <p className="mt-0.5 text-[11px] text-[var(--text-muted)]">
                配置 SheepGet 下载与元数据探测的网络代理模式
              </p>

              <div className="mt-3 max-w-sm">
                <Select
                  value={selectedProxyMode}
                  onChange={(val) => {
                    void handleProxyModeChange(val);
                  }}
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

              {/* Custom Proxy Address input */}
              {selectedProxyMode === configModels.ProxyMode.ProxyModeCustom && (
                <div className="mt-4 border-t border-[var(--border-subtle)] pt-3">
                  <div className="flex items-center justify-between">
                    <div>
                      <label className="text-xs font-semibold text-[var(--text-primary)]">
                        自定义代理服务器地址
                      </label>
                      <p className="mt-0.5 text-[11px] text-[var(--text-muted)]">
                        支持 HTTP/HTTPS 代理及 SOCKS5 代理
                      </p>
                    </div>
                    {proxy.mode !== configModels.ProxyMode.ProxyModeCustom && (
                      <span className="rounded-md bg-amber-500/10 px-2 py-0.5 text-[10px] font-medium text-amber-500">
                        未生效（填写并保存后启用）
                      </span>
                    )}
                  </div>
                  <div className="mt-2 flex max-w-md items-center gap-2">
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
                      placeholder="http://127.0.0.1:7890 或 socks5://127.0.0.1:1080"
                      className="h-8 font-mono text-xs"
                    />
                    <Button
                      size="sm"
                      onClick={() => {
                        void handleCustomProxyCommit();
                      }}
                      className="h-8 shrink-0 text-xs"
                    >
                      {proxy.mode === configModels.ProxyMode.ProxyModeCustom
                        ? '保存'
                        : '保存并启用'}
                    </Button>
                  </div>
                  {proxyError && (
                    <p className="mt-1 text-[11px] font-medium text-rose-500">{proxyError}</p>
                  )}
                </div>
              )}
            </div>

            {/* Scope explanation card */}
            <div className="rounded-xl border border-[var(--border-subtle)] bg-[var(--bg-surface)] p-4">
              <div className="flex items-start gap-3">
                <div className="mt-0.5 flex h-6 w-6 shrink-0 items-center justify-center rounded-lg bg-[var(--accent-muted)] text-[var(--accent)]">
                  <Info className="h-3.5 w-3.5" />
                </div>
                <div className="space-y-1 text-xs">
                  <div className="font-semibold text-[var(--text-primary)]">代理生效规则</div>
                  <p className="text-[11px] leading-relaxed text-[var(--text-muted)]">
                    代理配置仅作用于 SheepGet
                    自身联网。进行中的下载任务沿用开始时的代理，新任务及继续任务将应用最新配置。
                  </p>
                </div>
              </div>
            </div>
          </div>
        )}
      </div>
    </div>
  );
}
