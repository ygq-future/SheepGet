import { useState, useEffect, useRef, useCallback, useMemo, type SyntheticEvent } from 'react';
import { AnimatePresence, motion } from 'motion/react';
import {
  FolderOpen,
  DownloadCloud,
  AlertCircle,
  Copy,
  RotateCcw,
  Minus,
  X,
  Loader2,
  AlertTriangle,
  CheckCircle2,
  ChevronLeft,
  ChevronRight,
  FileCheck,
  FolderInput,
} from 'lucide-react';
import { Input } from '../components/ui/Input';
import { Button } from '../components/ui/Button';
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
  CheckURLFilesExist,
  ShowProgressWindow,
  SwitchFileInfoActive,
  ResolveDestination,
  AssignExtensionToCategory,
  SetCategoryDirectory,
} from '../../bindings/sheep-get/app';
import type * as windowModels from '../../bindings/sheep-get/internal/window/models';
import * as configModels from '../../bindings/sheep-get/internal/config/models';
import { Events } from '@wailsio/runtime';
import { unwrapEventData } from '../lib/utils';
import { useSettingsStore } from '../stores/settings';
import { Select } from '../components/ui/Select';
import { Checkbox } from '../components/ui/Checkbox';
import { extractExtension } from '../lib/category';
interface ItemDraftState {
  filename: string;
  directory: string;
  maxConn: number;
  preDownload: boolean;
  duplicateStrategy?: string;
  dupFileExists: boolean;
  fileConflict: boolean;
  suggestedFilename: string;
  overwriteConflict: boolean;
  nameEdited: boolean;
  dirEdited: boolean;
  originalFilename: string;
  selectedCategoryId: string;
  autoCategoryId: string;
  rememberCategory: boolean;
  categoryEdited: boolean;
  keepCategoryPath?: boolean;
  canReuseExistingFile?: boolean;
  existingPath?: string;
  existingTaskID?: string;
}

export function FileInfoView() {
  const { loadSettings } = useSettingsStore();
  const [activeItem, setActiveItem] = useState<windowModels.FileInfoItem | null>(null);

  // Form states
  const [url, setUrl] = useState('');
  const [filename, setFilename] = useState('');
  const [directory, setDirectory] = useState('');
  const [maxConn, setMaxConn] = useState(8);
  const [preDownload, setPreDownload] = useState(false);
  const [duplicateStrategy, setDuplicateStrategy] = useState<string | undefined>(undefined);
  const [dupFileExists, setDupFileExists] = useState(false);
  const [slideDirection, setSlideDirection] = useState(0);

  // Conflict & probe states
  const [probing, setProbing] = useState(false);
  const [fileConflict, setFileConflict] = useState(false);
  const [suggestedFilename, setSuggestedFilename] = useState('');
  const [overwriteConflict, setOverwriteConflict] = useState(false);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [selectedCategoryId, setSelectedCategoryId] = useState('');
  // 后端给出的命中分类，随文件名变化更新；分类规则只在后端实现。
  const [autoCategoryId, setAutoCategoryId] = useState('builtin-file');
  const [rememberCategory, setRememberCategory] = useState(false);
  const [categoryEdited, setCategoryEdited] = useState(false);
  const [keepCategoryPath, setKeepCategoryPath] = useState(false);
  const [canReuseExistingFile, setCanReuseExistingFile] = useState(false);
  const [existingPath, setExistingPath] = useState('');
  const [existingTaskID, setExistingTaskID] = useState('');

  const applyReuseState = useCallback(
    (conf: { canReuseExistingFile?: boolean; existingPath?: string; existingTaskID?: string }) => {
      if (conf.canReuseExistingFile && conf.existingPath && conf.existingTaskID) {
        setCanReuseExistingFile(true);
        setExistingPath(conf.existingPath);
        setExistingTaskID(conf.existingTaskID);
      } else {
        setCanReuseExistingFile(false);
        setExistingPath('');
        setExistingTaskID('');
      }
    },
    [],
  );
  const nameEditedRef = useRef(false);
  const dirEditedRef = useRef(false);
  const originalFilenameRef = useRef('');
  const probeSeqRef = useRef(0);
  const filenameSeqRef = useRef(0);
  const containerRef = useRef<HTMLDivElement>(null);
  const itemDraftsRef = useRef<Record<string, ItemDraftState>>({});
  const activeItemIdRef = useRef<string | null>(null);
  const filenameRef = useRef(filename);
  const directoryRef = useRef(directory);

  // Keep active item draft in sync with current form inputs
  useEffect(() => {
    const id = activeItemIdRef.current;
    if (!id) return;
    filenameRef.current = filename;
    directoryRef.current = directory;
    itemDraftsRef.current[id] = {
      filename,
      directory,
      maxConn,
      preDownload,
      duplicateStrategy,
      dupFileExists,
      fileConflict,
      suggestedFilename,
      overwriteConflict,
      nameEdited: nameEditedRef.current,
      dirEdited: dirEditedRef.current,
      originalFilename: originalFilenameRef.current,
      selectedCategoryId,
      autoCategoryId,
      rememberCategory,
      categoryEdited,
      keepCategoryPath,
      canReuseExistingFile,
      existingPath,
      existingTaskID,
    };
  }, [
    filename,
    directory,
    maxConn,
    preDownload,
    duplicateStrategy,
    dupFileExists,
    fileConflict,
    suggestedFilename,
    overwriteConflict,
    selectedCategoryId,
    autoCategoryId,
    rememberCategory,
    categoryEdited,
    keepCategoryPath,
    canReuseExistingFile,
    existingPath,
    existingTaskID,
  ]);

  const initItem = useCallback(
    async (item: windowModels.FileInfoItem) => {
      activeItemIdRef.current = item.id;
      const existingDraft = itemDraftsRef.current[item.id];
      if (existingDraft) {
        setActiveItem((prev) => ({
          ...item,
          queueIndex: item.queueIndex || prev?.queueIndex || 1,
          queueTotal: item.queueTotal || prev?.queueTotal || 1,
        }));
        setUrl(item.url || '');
        setFilename(existingDraft.filename);
        setDirectory(existingDraft.directory);
        setMaxConn(existingDraft.maxConn);
        setPreDownload(existingDraft.preDownload);
        setDuplicateStrategy(existingDraft.duplicateStrategy);
        setDupFileExists(existingDraft.dupFileExists);
        setFileConflict(existingDraft.fileConflict);
        setSuggestedFilename(existingDraft.suggestedFilename);
        setOverwriteConflict(existingDraft.overwriteConflict);
        nameEditedRef.current = existingDraft.nameEdited;
        dirEditedRef.current = existingDraft.dirEdited;
        setCategoryEdited(existingDraft.categoryEdited || false);
        setSelectedCategoryId(existingDraft.selectedCategoryId || '');
        setAutoCategoryId(existingDraft.autoCategoryId || 'builtin-file');
        setKeepCategoryPath(existingDraft.keepCategoryPath || false);
        setCanReuseExistingFile(existingDraft.canReuseExistingFile || false);
        setExistingPath(existingDraft.existingPath || '');
        setExistingTaskID(existingDraft.existingTaskID || '');
        setLoading(false);
        return;
      }

      const currentSettings = useSettingsStore.getState().settings;
      const currentPolicy =
        item.duplicatePolicy ||
        currentSettings?.download?.duplicateUrlPolicy ||
        configModels.DuplicateURLPolicy.DuplicatePolicyPrompt;

      const dir = item.directory || currentSettings?.download?.defaultDirectory || '';
      const initialName = !item.url && item.filename === 'download.bin' ? '' : item.filename || '';
      originalFilenameRef.current = initialName;

      let initialStrategy: string | undefined = undefined;
      let dupExistsOnDisk = false;
      let chosenName = initialName;
      // Check duplicate URL and disk existence
      if (item.duplicateTask && dir && initialName) {
        const conf = await CheckURLFilesExist(item.url || '', dir, initialName);
        dupExistsOnDisk = conf.exists;
        applyReuseState(conf);
        // Handle duplicate policies
        const policyStr = String(currentPolicy);
        if (policyStr === 'skip_show_completed' || policyStr === 'skip_show_done') {
          // Automatically show completed progress window, keep current file info open
          if (item.duplicateTask.id) {
            void ShowProgressWindow(item.duplicateTask.id);
          }
        } else if (dupExistsOnDisk) {
          if (policyStr === 'continue_overwrite' || policyStr === 'overwrite') {
            initialStrategy = 'continue_overwrite';
          } else if (policyStr === 'numbered_copy') {
            initialStrategy = 'copy';
            chosenName = conf.suggestedFilename;
          }
        }
      }
      setActiveItem((prev) => ({
        ...item,
        queueIndex: item.queueIndex || prev?.queueIndex || 1,
        queueTotal: item.queueTotal || prev?.queueTotal || 1,
      }));
      setUrl(item.url || '');
      setFilename(chosenName);
      setDirectory(dir);
      setMaxConn(item.maxConn || currentSettings?.download?.defaultConnectionsPerTask || 8);
      setPreDownload(item.preDownload ?? !!currentSettings?.download?.preDownload);
      setDupFileExists(dupExistsOnDisk);
      setFileConflict(dupExistsOnDisk ? false : !!item.fileConflict);
      setSuggestedFilename(item.suggestedFilename || '');
      setDuplicateStrategy(initialStrategy);
      setOverwriteConflict(false);
      setError(null);
      setLoading(false);
      nameEditedRef.current = false;
      dirEditedRef.current = false;
      setCategoryEdited(false);
      // 命中分类由后端在登记请求时判定，界面不重复实现分类规则。
      const initialCatId = item.categoryId || 'builtin-file';
      setSelectedCategoryId(initialCatId);
      setAutoCategoryId(initialCatId);
      setRememberCategory(false);
      setKeepCategoryPath(false);
      itemDraftsRef.current[item.id] = {
        filename: chosenName,
        directory: dir,
        maxConn: item.maxConn || currentSettings?.download?.defaultConnectionsPerTask || 8,
        preDownload: item.preDownload ?? !!currentSettings?.download?.preDownload,
        duplicateStrategy: initialStrategy,
        dupFileExists: dupExistsOnDisk,
        fileConflict: dupExistsOnDisk ? false : !!item.fileConflict,
        suggestedFilename: item.suggestedFilename || '',
        overwriteConflict: false,
        nameEdited: false,
        dirEdited: false,
        categoryEdited: false,
        selectedCategoryId: initialCatId,
        autoCategoryId: initialCatId,
        rememberCategory: false,
        keepCategoryPath: false,
        canReuseExistingFile: false,
        existingPath: '',
        existingTaskID: '',
        originalFilename: initialName,
      };
    },
    [applyReuseState],
  );

  useEffect(() => {
    void loadSettings();

    // Fetch initial active request
    void (async () => {
      try {
        const item = await GetActiveFileInfo();
        if (item) {
          await initItem(item);
        }
      } catch (err) {
        console.error('Failed to get active file info:', err);
      }
    })();

    // Listen for next item in queue
    const unlistenNext = Events.On('fileinfo:next', (ev: unknown) => {
      const item = unwrapEventData<windowModels.FileInfoItem>(ev);
      if (item) {
        void initItem(item);
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

    // Listen for probed updates on the active item
    const unlistenUpdated = Events.On('fileinfo:updated', (ev: unknown) => {
      const item = unwrapEventData<windowModels.FileInfoItem>(ev);
      if (item && item.id === activeItemIdRef.current) {
        setProbing(false);
        setActiveItem((prev) => (prev ? { ...prev, ...item } : item));
        if (item.totalBytes !== undefined && item.totalBytes > 0) {
          if (!nameEditedRef.current && item.filename) {
            setFilename(item.filename);
            originalFilenameRef.current = item.filename;
          }
        }
        if (item.fileConflict !== undefined) {
          setFileConflict(item.fileConflict);
        }
        if (item.suggestedFilename) {
          setSuggestedFilename(item.suggestedFilename);
        }
        if (item.url && (item.filename || filenameRef.current)) {
          void (async () => {
            const checkName = item.filename || filenameRef.current;
            const checkDir = item.directory || directoryRef.current;
            if (checkDir && checkName) {
              const conf = await CheckURLFilesExist(item.url, checkDir, checkName);
              setDupFileExists(conf.exists);
              if (conf.suggestedFilename) {
                setSuggestedFilename(conf.suggestedFilename);
              }
              applyReuseState(conf);
            }
          })();
        }
      }
    });

    return () => {
      unlistenNext();
      unlistenQueue();
      unlistenUpdated();
    };
  }, [loadSettings, initItem, applyReuseState]);

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
      const rawName =
        (!nameEditedRef.current && result.filename
          ? result.filename
          : filenameRef.current || result.filename) || '';
      originalFilenameRef.current = result.filename || filenameRef.current;

      const currentDir = directoryRef.current;
      let effectiveDir = currentDir;
      if (rawName) {
        try {
          const dest = await ResolveDestination(rawName);
          // 命中分类只取决于文件名，即使目录被用户手动改过也要更新。
          setAutoCategoryId(dest.categoryId || 'builtin-file');
          if (!dirEditedRef.current && dest.directory) {
            effectiveDir = dest.directory;
            if (dest.directory !== currentDir) {
              setDirectory(dest.directory);
            }
          }
        } catch {
          // fallback to currentDir
        }
      }

      let chosenName = rawName;
      let initStrategy: string | undefined = undefined;
      let dupExistsOnDisk = false;
      if (effectiveDir && rawName) {
        if (result.duplicateTask) {
          const conf = await CheckURLFilesExist(trimmed, effectiveDir, rawName);
          dupExistsOnDisk = conf.exists;
          applyReuseState(conf);
          const policyStr = String(currentPolicy);
          if (policyStr === 'skip_show_completed' || policyStr === 'skip_show_done') {
            // Auto open completed progress window, do NOT close file info dialog
            if (result.duplicateTask.id) {
              void ShowProgressWindow(result.duplicateTask.id);
            }
          } else if (dupExistsOnDisk) {
            if (policyStr === 'continue_overwrite' || policyStr === 'overwrite') {
              initStrategy = 'continue_overwrite';
            } else if (policyStr === 'numbered_copy') {
              initStrategy = 'copy';
              chosenName = conf.suggestedFilename;
            }
          }
          setSuggestedFilename(conf.suggestedFilename);
        } else {
          const conf = await CheckFileConflict(effectiveDir, rawName);
          setFileConflict(conf.exists);
          setSuggestedFilename(conf.suggestedFilename);
        }
      }
      setFilename(chosenName);
      setDupFileExists(dupExistsOnDisk);
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

  const currentSettings = useSettingsStore((s) => s.settings);
  const effectiveCategoryId =
    categoryEdited && selectedCategoryId ? selectedCategoryId : autoCategoryId;
  const categoryOptions = useMemo(() => {
    const opts: { value: string; label: string }[] = [];
    for (const c of currentSettings?.download?.customCategories || []) {
      opts.push({ value: c.id, label: `${c.name} (自定义)` });
    }
    for (const c of currentSettings?.download?.builtinCategories || []) {
      opts.push({ value: c.id, label: `${c.name} (内置)` });
    }
    return opts;
  }, [currentSettings?.download?.customCategories, currentSettings?.download?.builtinCategories]);

  const handleCategoryDropdownChange = (catId: string) => {
    setSelectedCategoryId(catId);
    setCategoryEdited(true);
    dirEditedRef.current = true;

    const allCats = [
      ...(currentSettings?.download?.customCategories || []),
      ...(currentSettings?.download?.builtinCategories || []),
    ];
    const targetCat = allCats.find((c) => c.id === catId);
    const catDir = targetCat?.directory || currentSettings?.download?.defaultDirectory || '';

    setDirectory(catDir);
    if (filename) {
      void (async () => {
        const conf = await CheckFileConflict(catDir, filename);
        setFileConflict(conf.exists);
        setSuggestedFilename(conf.suggestedFilename);
      })();
    }
  };

  const selectedCategory = useMemo(() => {
    const allCats = [
      ...(currentSettings?.download?.customCategories || []),
      ...(currentSettings?.download?.builtinCategories || []),
    ];
    return allCats.find((c) => c.id === effectiveCategoryId);
  }, [currentSettings, effectiveCategoryId]);

  const originalCategoryDir =
    selectedCategory?.directory || currentSettings?.download?.defaultDirectory || '';
  const canKeepCategoryPath =
    Boolean(effectiveCategoryId) &&
    directory.trim().length > 0 &&
    directory.trim() !== originalCategoryDir.trim();

  const handleSelectDir = async () => {
    try {
      const selected = await SelectDirectory();
      if (selected) {
        dirEditedRef.current = true;
        setDirectory(selected);
        if (filename) {
          if (activeItem?.duplicateTask) {
            const conf = await CheckURLFilesExist(url, selected, filename);
            setDupFileExists(conf.exists);
            setSuggestedFilename(conf.suggestedFilename);
            applyReuseState(conf);
          } else {
            const conf = await CheckFileConflict(selected, filename);
            setFileConflict(conf.exists);
            setSuggestedFilename(conf.suggestedFilename);
          }
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

      let effectiveStrategy = strategyOverride ?? duplicateStrategy;
      if (!dupFileExists && activeItem?.duplicateTask && !effectiveStrategy) {
        effectiveStrategy = 'redownload';
      }
      if (activeItem?.duplicateTask && dupFileExists && !effectiveStrategy) {
        setError('已有相同的下载链接和文件，请确认处理方式');
        return;
      }

      const isOverwrite =
        effectiveStrategy === 'continue_overwrite' || effectiveStrategy === 'redownload';

      // Disk conflict check: only block if not an explicit overwrite!
      if (fileConflict && !overwriteConflict && !isOverwrite) {
        setError('目标目录存在同名文件，请确认是否覆盖或使用建议名称');
        return;
      }

      if (rememberCategory && effectiveCategoryId) {
        const ext = extractExtension(filename);
        if (ext) {
          try {
            await AssignExtensionToCategory(ext, effectiveCategoryId);
            await loadSettings();
          } catch (assignErr) {
            console.error('Failed to assign extension to category:', assignErr);
          }
        }
      }

      if (keepCategoryPath && canKeepCategoryPath && effectiveCategoryId) {
        try {
          await SetCategoryDirectory(effectiveCategoryId, directory.trim());
          await loadSettings();
        } catch (catDirErr) {
          console.error('Failed to set category directory:', catDirErr);
        }
      }
      setError(null);
      const parsedConn = Math.min(32, Math.max(1, Number(maxConn) || 8));

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
          reuseTaskId: effectiveStrategy === 'reuse' ? existingTaskID : undefined,
        });
        if (activeItem?.id) {
          delete itemDraftsRef.current[activeItem.id];
        }
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
      dupFileExists,
      duplicateStrategy,
      effectiveCategoryId,
      fileConflict,
      filename,
      loadSettings,
      maxConn,
      overwriteConflict,
      preDownload,
      rememberCategory,
      url,
      canKeepCategoryPath,
      existingTaskID,
      keepCategoryPath,
    ],
  );
  const handleCancel = useCallback(async () => {
    try {
      if (activeItem?.id) {
        delete itemDraftsRef.current[activeItem.id];
      }
      await CancelCurrentFileInfo();
    } catch (err) {
      console.error('Failed to cancel file info:', err);
    }
  }, [activeItem]);
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
  }, [handleConfirm, handleCancel]);

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
      className="flex flex-col overflow-hidden rounded-lg border border-[var(--border-subtle)] bg-[var(--bg-base)] font-sans text-[var(--text-primary)] shadow-2xl select-none [&::-webkit-scrollbar]:hidden"
      style={{ scrollbarWidth: 'none', msOverflowStyle: 'none' }}
    >
      <header
        className="relative flex h-8 shrink-0 cursor-default items-center justify-between border-b border-[var(--border-subtle)] bg-[var(--bg-surface)] px-3 select-none"
        style={{ ['--wails-draggable' as string]: 'drag' }}
      >
        <div className="pointer-events-none flex items-center gap-1.5">
          <DownloadCloud className="h-3.5 w-3.5 text-[var(--accent)]" />
          <span className="text-[11px] font-semibold tracking-wide text-[var(--text-primary)]">
            新建下载
          </span>
        </div>

        {/* Centered Floating Queue Switcher Pill */}
        {activeItem && activeItem.queueTotal > 1 && (
          <div
            className="absolute top-1/2 left-1/2 flex -translate-x-1/2 -translate-y-1/2 items-center gap-1.5 rounded-full border border-[var(--border-subtle)] bg-[var(--bg-subtle)]/90 px-1.5 py-0.5 shadow-xs backdrop-blur-md"
            style={{ ['--wails-draggable' as string]: 'no-drag' }}
          >
            <button
              type="button"
              disabled={activeItem.queueIndex <= 1}
              onClick={() => {
                setSlideDirection(-1);
                void SwitchFileInfoActive(activeItem.queueIndex - 2);
              }}
              className="flex h-5 w-5 cursor-pointer items-center justify-center rounded-full text-[var(--text-muted)] transition-all hover:bg-[var(--bg-surface)] hover:text-[var(--text-primary)] hover:shadow-xs active:scale-95 disabled:pointer-events-none disabled:opacity-20"
              title="上一条待下载任务"
            >
              <ChevronLeft className="h-3.5 w-3.5 stroke-[2.5]" />
            </button>
            <span className="font-mono text-[10px] font-bold tracking-tight text-[var(--accent)] select-none">
              {activeItem.queueIndex} / {activeItem.queueTotal}
            </span>
            <button
              type="button"
              disabled={activeItem.queueIndex >= activeItem.queueTotal}
              onClick={() => {
                setSlideDirection(1);
                void SwitchFileInfoActive(activeItem.queueIndex);
              }}
              className="flex h-5 w-5 cursor-pointer items-center justify-center rounded-full text-[var(--text-muted)] transition-all hover:bg-[var(--bg-surface)] hover:text-[var(--text-primary)] hover:shadow-xs active:scale-95 disabled:pointer-events-none disabled:opacity-20"
              title="下一条待下载任务"
            >
              <ChevronRight className="h-3.5 w-3.5 stroke-[2.5]" />
            </button>
          </div>
        )}
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

      {/* Main Content (strictly localized scrollbar suppression and smooth item transition) */}
      <main
        className="flex-1 overflow-x-hidden overflow-y-hidden p-3 [&::-webkit-scrollbar]:hidden"
        style={{ scrollbarWidth: 'none', msOverflowStyle: 'none' }}
      >
        <AnimatePresence mode="popLayout" custom={slideDirection} initial={false}>
          <motion.div
            key={activeItem?.id || 'default'}
            custom={slideDirection}
            initial={{ opacity: 0, x: slideDirection * 24 }}
            animate={{ opacity: 1, x: 0 }}
            exit={{ opacity: 0, x: slideDirection * -24 }}
            transition={{ duration: 0.16, ease: [0.16, 1, 0.3, 1] }}
            className="space-y-2 overflow-hidden"
          >
            <div className="space-y-0.5">
              <label className="text-[11px] font-medium text-[var(--text-secondary)]">
                下载链接
              </label>
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
            {/* Duplicate Task Alert - Linear/Raycast refined segmented banner */}
            {activeItem?.duplicateTask && (
              <div className="space-y-2.5 rounded-xl border border-[var(--border-subtle)] bg-[var(--bg-surface)] p-3 shadow-xs">
                {dupFileExists ? (
                  <>
                    <div className="flex items-center justify-between gap-2">
                      <div className="flex items-center gap-2">
                        <div className="flex h-5 w-5 shrink-0 items-center justify-center rounded-md bg-amber-500/15 text-amber-600 dark:text-amber-400">
                          <AlertTriangle className="h-3.5 w-3.5" />
                        </div>
                        <span className="text-[11px] font-semibold tracking-tight text-[var(--text-primary)]">
                          已有相同下载链接与同名文件
                        </span>
                      </div>
                      <span className="text-[10px] text-[var(--text-muted)]">请选择处理方式</span>
                    </div>

                    {/* Refined Segmented Control */}
                    <div className="grid grid-cols-3 gap-1 rounded-lg border border-[var(--border-subtle)] bg-[var(--bg-subtle)]/70 p-1 select-none">
                      <button
                        type="button"
                        onClick={() => {
                          setDuplicateStrategy('show_completed');
                          if (activeItem.duplicateTask?.id) {
                            void ShowProgressWindow(activeItem.duplicateTask.id);
                          }
                          // Keep window open as requested by user
                        }}
                        className={`flex items-center justify-center gap-1.5 rounded-md py-1.5 text-[11px] font-medium transition-all ${
                          duplicateStrategy === 'show_completed'
                            ? 'bg-[var(--bg-surface)] text-[var(--text-primary)] shadow-xs ring-1 ring-black/5 dark:ring-white/10'
                            : 'text-[var(--text-secondary)] hover:text-[var(--text-primary)]'
                        }`}
                      >
                        <CheckCircle2 className="h-3 w-3 shrink-0 text-emerald-500" />
                        <span>查看已完成</span>
                      </button>

                      <button
                        type="button"
                        onClick={() => {
                          setDuplicateStrategy('continue_overwrite');
                          if (error) setError(null);
                          if (originalFilenameRef.current) {
                            setFilename(originalFilenameRef.current);
                          }
                        }}
                        className={`flex items-center justify-center gap-1.5 rounded-md py-1.5 text-[11px] font-medium transition-all ${
                          duplicateStrategy === 'continue_overwrite'
                            ? 'bg-[var(--bg-surface)] text-[var(--accent)] shadow-xs ring-1 ring-black/5 dark:ring-white/10'
                            : 'text-[var(--text-secondary)] hover:text-[var(--text-primary)]'
                        }`}
                      >
                        <RotateCcw className="h-3 w-3 shrink-0" />
                        <span>继续覆盖</span>
                      </button>

                      <button
                        type="button"
                        onClick={() => {
                          setDuplicateStrategy('copy');
                          if (error) setError(null);
                          void (async () => {
                            const baseName =
                              originalFilenameRef.current || activeItem?.filename || filename;
                            if (directory && baseName) {
                              const conf = await CheckURLFilesExist(url, directory, baseName);
                              if (conf.suggestedFilename) {
                                setFilename(conf.suggestedFilename);
                              }
                            }
                          })();
                        }}
                        className={`flex items-center justify-center gap-1.5 rounded-md py-1.5 text-[11px] font-medium transition-all ${
                          duplicateStrategy === 'copy'
                            ? 'bg-[var(--bg-surface)] text-[var(--accent)] shadow-xs ring-1 ring-black/5 dark:ring-white/10'
                            : 'text-[var(--text-secondary)] hover:text-[var(--text-primary)]'
                        }`}
                      >
                        <Copy className="h-3 w-3 shrink-0" />
                        <span>序号副本</span>
                      </button>
                    </div>
                  </>
                ) : (
                  <div className="space-y-2">
                    <div className="flex items-center justify-between gap-2">
                      <div className="flex items-center gap-2 text-amber-600 dark:text-amber-400">
                        <AlertTriangle className="h-3.5 w-3.5 shrink-0" />
                        <span className="text-[11px] font-medium text-[var(--text-secondary)]">
                          此前已下载过此链接，当前目录下无同名文件
                        </span>
                      </div>
                      <button
                        type="button"
                        onClick={() => {
                          if (activeItem.duplicateTask?.id) {
                            void ShowProgressWindow(activeItem.duplicateTask.id);
                          }
                        }}
                        className="flex items-center gap-1.5 rounded-md border border-[var(--border-subtle)] bg-[var(--bg-subtle)] px-2.5 py-1 text-[11px] font-medium text-[var(--text-primary)] transition-colors hover:bg-[var(--bg-surface-hover)]"
                      >
                        <CheckCircle2 className="h-3 w-3 text-emerald-500" />
                        <span>查看完成记录</span>
                      </button>
                    </div>

                    {canReuseExistingFile && existingPath && (
                      <div className="rounded-lg border border-blue-500/30 bg-blue-500/10 p-2.5 text-[11px] text-blue-900 dark:text-blue-200">
                        <div className="flex items-start gap-2">
                          <FileCheck className="mt-0.5 h-4 w-4 shrink-0 text-blue-600 dark:text-blue-400" />
                          <div className="flex-1 space-y-1">
                            <p className="font-medium text-[var(--text-primary)]">
                              在其他历史目录中发现相同文件
                            </p>
                            <p className="font-mono text-[10px] break-all text-[var(--text-muted)]">
                              {existingPath}
                            </p>
                            <div className="flex items-center gap-2 pt-1">
                              <Button
                                size="sm"
                                variant="primary"
                                className="h-6 gap-1 px-2 text-[11px]"
                                onClick={() => void handleConfirm('reuse')}
                              >
                                <FolderInput className="h-3 w-3" />
                                <span>复用并移动到当前目录 (免下载)</span>
                              </Button>
                              <span className="text-[10px] text-[var(--text-muted)]">
                                或点击下方“开始下载”重新下载
                              </span>
                            </div>
                          </div>
                        </div>
                      </div>
                    )}
                  </div>
                )}
              </div>
            )}

            {/* File Conflict Alert */}
            {fileConflict &&
              !activeItem?.duplicateTask &&
              duplicateStrategy !== 'continue_overwrite' &&
              duplicateStrategy !== 'redownload' && (
                <div className="space-y-1 rounded-md border border-amber-500/40 bg-amber-500/10 p-2.5 text-[11px] font-medium text-amber-950 dark:text-amber-100">
                  <div className="flex items-center gap-1.5 text-amber-900 dark:text-amber-200">
                    <AlertCircle className="h-3.5 w-3.5 shrink-0 text-amber-600 dark:text-amber-400" />
                    <span className="font-semibold text-amber-950 dark:text-amber-100">
                      目标目录存在同名文件: {filename}
                    </span>
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
                <label className="text-[11px] font-medium text-[var(--text-secondary)]">
                  文件名
                </label>
                <Input
                  value={filename}
                  placeholder="请输入文件名"
                  onChange={(e) => {
                    const newName = e.target.value;
                    nameEditedRef.current = true;
                    setFilename(newName);

                    const seq = ++filenameSeqRef.current;
                    if (newName) {
                      void (async () => {
                        try {
                          const dest = await ResolveDestination(newName);
                          if (seq === filenameSeqRef.current) {
                            // 命中分类跟随文件名变化；目录仅在用户未手动改过时跟随。
                            setAutoCategoryId(dest.categoryId || 'builtin-file');
                            if (!dirEditedRef.current && dest.directory) {
                              setDirectory(dest.directory);
                            }
                          }
                        } catch {
                          // ignore
                        }
                      })();
                    }

                    if (directory) {
                      void (async () => {
                        const conf = await CheckFileConflict(directory, newName);
                        setFileConflict(conf.exists);
                        setSuggestedFilename(conf.suggestedFilename);
                      })();
                    }
                  }}
                />
              </div>

              <div className="w-20 shrink-0 space-y-0.5">
                <label className="text-[11px] font-medium text-[var(--text-secondary)]">
                  并发数
                </label>
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

            {/* Save Directory with IDM-style Category Select and Remember Checkbox */}
            <div className="space-y-1.5">
              <label className="text-[11px] font-medium text-[var(--text-secondary)]">
                保存目录
              </label>
              <div className="flex items-center gap-1.5">
                <div className="w-28 shrink-0">
                  <Select
                    value={effectiveCategoryId}
                    onChange={(catId) => handleCategoryDropdownChange(String(catId))}
                    options={categoryOptions}
                    className="h-8 py-1.5 text-xs"
                  />
                </div>
                <Input
                  value={directory}
                  onChange={(e) => {
                    dirEditedRef.current = true;
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
                  className="h-8 flex-1 font-mono text-xs"
                />
                <Button
                  size="icon"
                  variant="secondary"
                  onClick={() => void handleSelectDir()}
                  title="浏览选择保存目录"
                >
                  <FolderOpen className="h-4 w-4 text-[var(--text-secondary)]" />
                </Button>
              </div>

              {/* Checkboxes: 保持此类型文件为该分类 & 保持此分类为此路径 */}
              {(() => {
                const currentExt = extractExtension(filename);
                return (
                  <div className="flex flex-wrap items-center gap-4 px-0.5 py-0.5">
                    <div className="flex items-center gap-2">
                      <Checkbox
                        id="remember-category-checkbox"
                        checked={rememberCategory}
                        onCheckedChange={(checked) => setRememberCategory(Boolean(checked))}
                        disabled={!currentExt}
                      />
                      <label
                        htmlFor="remember-category-checkbox"
                        className={`cursor-pointer text-[11px] select-none ${
                          currentExt
                            ? 'text-[var(--text-secondary)] hover:text-[var(--text-primary)]'
                            : 'cursor-not-allowed text-[var(--text-muted)]'
                        }`}
                      >
                        记住此后缀分类{currentExt ? ` (.${currentExt})` : ''}
                      </label>
                    </div>

                    <div className="flex items-center gap-2">
                      <Checkbox
                        id="keep-category-path-checkbox"
                        checked={keepCategoryPath}
                        onCheckedChange={(checked) => setKeepCategoryPath(Boolean(checked))}
                        disabled={!canKeepCategoryPath}
                      />
                      <label
                        htmlFor="keep-category-path-checkbox"
                        className={`cursor-pointer text-[11px] select-none ${
                          canKeepCategoryPath
                            ? 'text-[var(--text-secondary)] hover:text-[var(--text-primary)]'
                            : 'cursor-not-allowed text-[var(--text-muted)]'
                        }`}
                        title={
                          canKeepCategoryPath
                            ? '下载时将此分类的默认保存路径更新为当前路径'
                            : '仅在选择分类并修改路径后可用'
                        }
                      >
                        更新此分类默认路径
                      </label>
                    </div>
                  </div>
                );
              })()}
            </div>
          </motion.div>
        </AnimatePresence>
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
