import { useState } from 'react';
import { useSettingsStore } from '../stores/settings';
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
} from 'lucide-react';
import { Badge } from './ui/Badge';
import { normalizeExtensions } from '../lib/category';
import { motion } from 'motion/react';

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

export function SettingsPanel() {
  const { settings, updateSettings } = useSettingsStore();
  const [activeTab, setActiveTab] = useState<'appearance' | 'download' | 'categories'>(
    'appearance',
  );
  const [customColor, setCustomColor] = useState('');
  const appearance = settings?.appearance || new configModels.AppearanceConfig();
  const download = settings?.download || new configModels.DownloadConfig();

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
  const [extDrafts, setExtDrafts] = useState<Record<string, string>>({});
  const [catDirDrafts, setCatDirDrafts] = useState<Record<string, string>>({});
  const [catNameDrafts, setCatNameDrafts] = useState<Record<string, string>>({});
  const [draggedIdx, setDraggedIdx] = useState<number | null>(null);
  if (!settings) {
    return (
      <div className="flex h-full items-center justify-center p-8 text-xs text-[var(--text-muted)]">
        正在加载配置...
      </div>
    );
  }

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

  const handleAddCustomCategory = async () => {
    const defaultDir = download.defaultDirectory || '';
    const newCat = new configModels.CategoryConfig({
      id: `custom_${Date.now()}`,
      name: '新建分类',
      directory: defaultDir,
      extensions: ['ext'],
      isBuiltin: false,
    });
    const updatedList = [newCat, ...(download.customCategories || [])];
    await updateSettings({
      download: new configModels.DownloadConfig({
        ...download,
        customCategories: updatedList,
      }),
    });
    showToast('已新建自定义分类（已置顶）', 'success');
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

  const handleDeleteCustomCategory = async (index: number) => {
    const list = [...(download.customCategories || [])];
    list.splice(index, 1);
    await updateSettings({
      download: new configModels.DownloadConfig({
        ...download,
        customCategories: list,
      }),
    });
    showToast('已删除自定义分类，匹配规则已回退', 'info');
  };

  const handleDragStart = (e: React.DragEvent, index: number) => {
    setDraggedIdx(index);
    e.dataTransfer.effectAllowed = 'move';
  };

  const handleDragOver = (e: React.DragEvent) => {
    e.preventDefault();
    e.dataTransfer.dropEffect = 'move';
  };

  const handleDrop = async (e: React.DragEvent, targetIndex: number) => {
    e.preventDefault();
    if (draggedIdx === null || draggedIdx === targetIndex) {
      setDraggedIdx(null);
      return;
    }
    const list = [...(download.customCategories || [])];
    const [moved] = list.splice(draggedIdx, 1);
    list.splice(targetIndex, 0, moved);
    setDraggedIdx(null);
    await updateSettings({
      download: new configModels.DownloadConfig({
        ...download,
        customCategories: list,
      }),
    });
    showToast('已更新分类排序', 'success');
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

  return (
    <div className="flex h-full w-full min-w-0 flex-col font-sans">
      <div className="mb-6 flex items-center justify-between border-b border-[var(--border-subtle)] pb-4">
        <div>
          <h2 className="text-base font-semibold text-[var(--text-primary)]">偏好设置</h2>
          <p className="mt-0.5 text-xs text-[var(--text-muted)]">管理应用外观与下载规则配置</p>
        </div>

        {/* Tab Navigation with smooth motion pill and accent color */}
        <div className="flex rounded-xl border border-[var(--border-subtle)] bg-[var(--bg-surface)] p-1 shadow-xs">
          <button
            onClick={() => setActiveTab('appearance')}
            className="group relative flex items-center gap-1.5 rounded-lg px-3 py-1.5 text-xs font-medium transition-colors select-none"
          >
            {activeTab === 'appearance' && (
              <motion.div
                layoutId="settings-active-tab"
                className="absolute inset-0 rounded-lg border border-[var(--border-focus)] bg-[var(--accent-muted)] shadow-xs"
                transition={{ type: 'spring', stiffness: 400, damping: 32 }}
              />
            )}
            <span
              className={`relative z-10 flex items-center gap-1.5 transition-colors ${
                activeTab === 'appearance'
                  ? 'font-semibold text-[var(--accent)]'
                  : 'text-[var(--text-secondary)] group-hover:text-[var(--text-primary)]'
              }`}
            >
              <Palette className="h-3.5 w-3.5" />
              外观
            </span>
          </button>

          <button
            onClick={() => setActiveTab('download')}
            className="group relative flex items-center gap-1.5 rounded-lg px-3 py-1.5 text-xs font-medium transition-colors select-none"
          >
            {activeTab === 'download' && (
              <motion.div
                layoutId="settings-active-tab"
                className="absolute inset-0 rounded-lg border border-[var(--border-focus)] bg-[var(--accent-muted)] shadow-xs"
                transition={{ type: 'spring', stiffness: 400, damping: 32 }}
              />
            )}
            <span
              className={`relative z-10 flex items-center gap-1.5 transition-colors ${
                activeTab === 'download'
                  ? 'font-semibold text-[var(--accent)]'
                  : 'text-[var(--text-secondary)] group-hover:text-[var(--text-primary)]'
              }`}
            >
              <Zap className="h-3.5 w-3.5" />
              下载
            </span>
          </button>

          <button
            onClick={() => setActiveTab('categories')}
            className="group relative flex items-center gap-1.5 rounded-lg px-3 py-1.5 text-xs font-medium transition-colors select-none"
          >
            {activeTab === 'categories' && (
              <motion.div
                layoutId="settings-active-tab"
                className="absolute inset-0 rounded-lg border border-[var(--border-focus)] bg-[var(--accent-muted)] shadow-xs"
                transition={{ type: 'spring', stiffness: 400, damping: 32 }}
              />
            )}
            <span
              className={`relative z-10 flex items-center gap-1.5 transition-colors ${
                activeTab === 'categories'
                  ? 'font-semibold text-[var(--accent)]'
                  : 'text-[var(--text-secondary)] group-hover:text-[var(--text-primary)]'
              }`}
            >
              <FolderTree className="h-3.5 w-3.5" />
              分类
            </span>
          </button>
        </div>
      </div>

      <div className="flex-1 overflow-y-auto pr-1">
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
                        label: '询问我 (默认)',
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
                      使用服务器文件修改时间 (Last-Modified)
                    </label>
                    <p className="mt-0.5 text-[11px] text-[var(--text-muted)]">
                      下载完成后将本地文件修改时间设置为服务器返回的 Last-Modified。注意：HTTP
                      仅提供修改时间（非创建时间），支持跨平台保留资源原始修改时间；未提供则保留完成时间。
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
          <div className="space-y-6">
            {/* Header info */}
            <div className="rounded-xl border border-[var(--border-subtle)] bg-[var(--bg-surface)] p-4">
              <div className="flex items-center gap-2">
                <FolderTree className="h-4 w-4 text-[var(--accent)]" />
                <span className="text-xs font-semibold text-[var(--text-primary)]">
                  分类保存规则
                </span>
              </div>
              <p className="mt-1 text-[11px] text-[var(--text-muted)]">
                配置不同文件类型的归档目录与后缀规则。新建下载时根据后缀自动选择保存目录；新建下载对话框中的手动选择优先。
              </p>
            </div>

            {/* Custom Categories */}
            <div className="space-y-3">
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
                  className="gap-1.5"
                >
                  <Plus className="h-3.5 w-3.5" />
                  <span>新建分类</span>
                </Button>
              </div>
              <p className="px-1 text-[11px] text-[var(--text-muted)]">
                自定义分类规则从上至下优先匹配，命中首个分类即停止；新建分类自动置顶，支持调整优先级。
              </p>

              {(!download.customCategories || download.customCategories.length === 0) && (
                <div className="rounded-xl border border-dashed border-[var(--border-subtle)] bg-[var(--bg-surface)] py-8 text-center text-xs text-[var(--text-muted)]">
                  暂无自定义分类。点击右上角“新建分类”添加，自定义规则优先级高于内置分类。
                </div>
              )}

              {download.customCategories?.map((cat, idx) => (
                <div
                  key={cat.id || idx}
                  draggable
                  onDragStart={(e) => handleDragStart(e, idx)}
                  onDragOver={handleDragOver}
                  onDrop={(e) => void handleDrop(e, idx)}
                  className={`space-y-3 rounded-xl border border-[var(--border-subtle)] bg-[var(--bg-surface)] p-4 transition-all hover:border-[var(--border-focus)] ${
                    draggedIdx === idx ? 'opacity-50' : ''
                  }`}
                >
                  <div className="flex items-center justify-between gap-3">
                    <div className="flex flex-1 items-center gap-2">
                      <div
                        className="cursor-grab p-1 text-[var(--text-muted)] hover:text-[var(--text-primary)] active:cursor-grabbing"
                        title="拖动排序"
                      >
                        <GripVertical className="h-4 w-4" />
                      </div>
                      <div className="flex flex-col gap-0.5">
                        <button
                          type="button"
                          disabled={idx === 0}
                          onClick={() => {
                            void handleMoveCustomCategory(idx, 'up');
                          }}
                          className="rounded p-0.5 text-[var(--text-muted)] transition-colors hover:bg-[var(--bg-subtle)] hover:text-[var(--text-primary)] disabled:opacity-30"
                          title="上移（提高优先级）"
                        >
                          <ArrowUp className="h-3 w-3" />
                        </button>
                        <button
                          type="button"
                          disabled={idx === (download.customCategories?.length || 0) - 1}
                          onClick={() => {
                            void handleMoveCustomCategory(idx, 'down');
                          }}
                          className="rounded p-0.5 text-[var(--text-muted)] transition-colors hover:bg-[var(--bg-subtle)] hover:text-[var(--text-primary)] disabled:opacity-30"
                          title="下移（降低优先级）"
                        >
                          <ArrowDown className="h-3 w-3" />
                        </button>
                      </div>
                      <Input
                        value={cat.id in catNameDrafts ? catNameDrafts[cat.id] : cat.name}
                        onChange={(e) => {
                          setCatNameDrafts({ ...catNameDrafts, [cat.id]: e.target.value });
                        }}
                        onBlur={() => {
                          if (cat.id in catNameDrafts) {
                            const trimmed = catNameDrafts[cat.id].trim();
                            if (trimmed && trimmed !== cat.name) {
                              void handleUpdateCustomCategory(idx, { name: trimmed });
                            }
                            const next = { ...catNameDrafts };
                            delete next[cat.id];
                            setCatNameDrafts(next);
                          }
                        }}
                        onKeyDown={(e) => {
                          if (e.key === 'Enter') {
                            e.preventDefault();
                            if (cat.id in catNameDrafts) {
                              const trimmed = catNameDrafts[cat.id].trim();
                              if (trimmed && trimmed !== cat.name) {
                                void handleUpdateCustomCategory(idx, { name: trimmed });
                              }
                              const next = { ...catNameDrafts };
                              delete next[cat.id];
                              setCatNameDrafts(next);
                            }
                          }
                        }}
                        placeholder="分类名称"
                        className="w-40 text-xs font-semibold"
                      />
                      <Badge variant="outline" className="text-[10px]">
                        优先级 #{idx + 1}
                      </Badge>
                    </div>

                    <Button
                      variant="ghost"
                      size="sm"
                      onClick={() => {
                        void handleDeleteCustomCategory(idx);
                      }}
                      className="text-red-500 hover:bg-red-500/10 hover:text-red-600"
                      title="删除此分类"
                    >
                      <Trash2 className="h-3.5 w-3.5" />
                    </Button>
                  </div>

                  <div className="grid grid-cols-1 gap-3 sm:grid-cols-2">
                    <div className="space-y-1">
                      <label className="text-[11px] font-medium text-[var(--text-secondary)]">
                        匹配后缀（逗号或空格分隔）
                      </label>
                      <Input
                        value={
                          cat.id in extDrafts
                            ? extDrafts[cat.id]
                            : (cat.extensions || []).join(', ')
                        }
                        onChange={(e) => {
                          setExtDrafts({ ...extDrafts, [cat.id]: e.target.value });
                        }}
                        onBlur={() => {
                          if (cat.id in extDrafts) {
                            const parsed = normalizeExtensions(extDrafts[cat.id]);
                            void handleUpdateCustomCategory(idx, { extensions: parsed });
                            const next = { ...extDrafts };
                            delete next[cat.id];
                            setExtDrafts(next);
                          }
                        }}
                        onKeyDown={(e) => {
                          if (e.key === 'Enter') {
                            e.preventDefault();
                            if (cat.id in extDrafts) {
                              const parsed = normalizeExtensions(extDrafts[cat.id]);
                              void handleUpdateCustomCategory(idx, { extensions: parsed });
                              const next = { ...extDrafts };
                              delete next[cat.id];
                              setExtDrafts(next);
                            }
                          }
                        }}
                        placeholder="例如: rar, 7z, tar"
                        className="font-mono text-xs"
                      />
                      <div className="flex flex-wrap gap-1 pt-1">
                        {(cat.extensions || []).map((ext) => (
                          <Badge key={ext} variant="accent">
                            .{ext}
                          </Badge>
                        ))}
                      </div>
                    </div>

                    <div className="space-y-1">
                      <label className="text-[11px] font-medium text-[var(--text-secondary)]">
                        保存目录
                      </label>
                      <div className="flex items-center gap-1.5">
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
                            void handleCommitCategoryDirectory(true, idx, val);
                            const next = { ...catDirDrafts };
                            delete next[cat.id];
                            setCatDirDrafts(next);
                          }}
                          onKeyDown={(e) => {
                            if (e.key === 'Enter') {
                              e.preventDefault();
                              const val =
                                cat.id in catDirDrafts ? catDirDrafts[cat.id] : cat.directory || '';
                              void handleCommitCategoryDirectory(true, idx, val);
                              const next = { ...catDirDrafts };
                              delete next[cat.id];
                              setCatDirDrafts(next);
                            }
                          }}
                          placeholder="留空则使用默认保存位置"
                          className="flex-1 font-mono text-xs"
                        />
                        <Button
                          variant="secondary"
                          size="md"
                          onClick={() => {
                            void handleSelectCategoryDirectory(true, idx);
                          }}
                          className="shrink-0 gap-1"
                        >
                          <Folder className="h-3 w-3" />
                          <span>浏览</span>
                        </Button>
                      </div>
                    </div>
                  </div>
                </div>
              ))}
            </div>

            {/* Builtin Categories */}
            <div className="space-y-3 border-t border-[var(--border-subtle)] pt-4">
              <div className="flex items-center justify-between px-1">
                <div className="flex items-center gap-2">
                  <h3 className="text-xs font-semibold text-[var(--text-primary)]">内置分类</h3>
                  <Badge variant="outline">{download.builtinCategories?.length || 6}</Badge>
                </div>
              </div>
              <p className="px-1 text-[11px] text-[var(--text-muted)]">
                预置分类不可删除；自定义分类未命中时，依次尝试匹配；未命中任何规则的文件统一归入【文件】分类。
              </p>

              {download.builtinCategories?.map((cat, idx) => {
                const isFallback = cat.id === 'builtin-file' || cat.name === '文件';
                return (
                  <div
                    key={cat.id || idx}
                    className="space-y-3 rounded-xl border border-[var(--border-subtle)] bg-[var(--bg-surface)] p-4"
                  >
                    <div className="flex items-center justify-between">
                      <div className="flex items-center gap-2">
                        <span className="text-xs font-semibold text-[var(--text-primary)]">
                          {cat.name}
                        </span>
                        <Badge variant={isFallback ? 'warning' : 'outline'}>
                          {isFallback ? '兜底分类' : '内置'}
                        </Badge>
                      </div>
                      {isFallback && (
                        <span className="text-[11px] text-[var(--text-muted)]">
                          未匹配任何后缀的文件默认保存至此目录
                        </span>
                      )}
                    </div>

                    <div className="grid grid-cols-1 gap-3 sm:grid-cols-2">
                      <div className="space-y-1">
                        <label className="text-[11px] font-medium text-[var(--text-secondary)]">
                          匹配后缀
                        </label>
                        <Input
                          value={
                            cat.id in extDrafts
                              ? extDrafts[cat.id]
                              : (cat.extensions || []).join(', ')
                          }
                          onChange={(e) => {
                            setExtDrafts({ ...extDrafts, [cat.id]: e.target.value });
                          }}
                          onBlur={() => {
                            if (cat.id in extDrafts) {
                              const parsed = normalizeExtensions(extDrafts[cat.id]);
                              void handleUpdateBuiltinCategory(idx, { extensions: parsed });
                              const next = { ...extDrafts };
                              delete next[cat.id];
                              setExtDrafts(next);
                            }
                          }}
                          onKeyDown={(e) => {
                            if (e.key === 'Enter') {
                              e.preventDefault();
                              if (cat.id in extDrafts) {
                                const parsed = normalizeExtensions(extDrafts[cat.id]);
                                void handleUpdateBuiltinCategory(idx, { extensions: parsed });
                                const next = { ...extDrafts };
                                delete next[cat.id];
                                setExtDrafts(next);
                              }
                            }
                          }}
                          className="font-mono text-xs"
                        />
                        <div className="flex flex-wrap gap-1 pt-1">
                          {(cat.extensions || []).map((ext) => (
                            <Badge key={ext} variant="outline">
                              .{ext}
                            </Badge>
                          ))}
                        </div>
                      </div>

                      <div className="space-y-1">
                        <label className="text-[11px] font-medium text-[var(--text-secondary)]">
                          保存目录
                        </label>
                        <div className="flex items-center gap-1.5">
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
                            className="flex-1 font-mono text-xs"
                          />
                          <Button
                            variant="secondary"
                            size="md"
                            onClick={() => {
                              void handleSelectCategoryDirectory(false, idx);
                            }}
                            className="shrink-0 gap-1"
                          >
                            <Folder className="h-3 w-3" />
                            <span>浏览</span>
                          </Button>
                        </div>
                      </div>
                    </div>
                  </div>
                );
              })}
            </div>
          </div>
        )}
      </div>
    </div>
  );
}
