import { useState, useRef } from 'react';
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
  X,
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
      </div>
    </div>
  );
}
