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
  ResolveDuplicateDecision,
  AssignExtensionToCategory,
  SetCategoryDirectory,
} from '../../bindings/sheep-get/app';
import type * as windowModels from '../../bindings/sheep-get/internal/window/models';
import * as duplicateModels from '../../bindings/sheep-get/internal/duplicate/models';
import { Events } from '@wailsio/runtime';
import { unwrapEventData } from '../lib/utils';
import { Event } from '../lib/events';
import { useSettingsStore } from '../stores/settings';
import { useFileInfoDraftStore } from '../stores/fileInfoDraft';
import { Select } from '../components/ui/Select';
import { Checkbox } from '../components/ui/Checkbox';
import { extractExtension } from '../lib/category';
import {
  canKeepCategoryPathOf,
  effectiveCategoryIdOf,
  isOverwriteAction,
  overwriteOptionOf,
  resolveAction,
  reuseFields,
  type FileInfoDraft,
} from '../lib/fileInfoDraft';

export function FileInfoView() {
  const { loadSettings } = useSettingsStore();
  const [activeItem, setActiveItem] = useState<windowModels.FileInfoItem | null>(null);
  const [slideDirection, setSlideDirection] = useState(0);

  // 表单内容全部来自当前项的草稿：切换项时整体替换，任何一项的输入与提示都不会串到别的项。
  const draft = useFileInfoDraftStore((s) => s.draft);
  const openDraft = useFileInfoDraftStore((s) => s.open);
  const patch = useFileInfoDraftStore((s) => s.patch);
  const discardDraft = useFileInfoDraftStore((s) => s.discard);
  const probing = useFileInfoDraftStore((s) => s.probing);
  const loading = useFileInfoDraftStore((s) => s.loading);
  const setProbing = useFileInfoDraftStore((s) => s.setProbing);
  const setLoading = useFileInfoDraftStore((s) => s.setLoading);

  const {
    url,
    filename,
    directory,
    maxConn,
    duplicateDecision,
    selectedAction,
    fileConflict,
    suggestedFilename,
    overwriteConflict,
    error,
    originalFilename,
    rememberCategory,
    keepCategoryPath,
    canReuseExistingFile,
    existingPath,
  } = draft;

  const probeSeqRef = useRef(0);
  const filenameSeqRef = useRef(0);
  const containerRef = useRef<HTMLDivElement>(null);

  // 切换项时作废在途请求：上一项的探测与文件名解析结果不得落到下一项上。
  const invalidateInFlight = useCallback(() => {
    probeSeqRef.current += 1;
    filenameSeqRef.current += 1;
  }, []);

  // 裁决结果与选中项一起写回：用户已经选过的动作只要仍然可选就保留，否则回落到裁决给出的默认项。
  const applyDecision = useCallback(
    (decision: duplicateModels.Decision, options?: { keepSelection?: boolean }) => {
      patch({
        duplicateDecision: decision,
        selectedAction: resolveAction(
          decision,
          useFileInfoDraftStore.getState().draft.selectedAction,
          options,
        ),
      });
    },
    [patch],
  );

  // 落点变化后向后端重新要一次裁决：给哪些选项、默认哪一项、目标位置是否已有成品文件，
  // 全部由后端依据策略与磁盘事实算出，界面只负责渲染。
  // owner 是发起这次请求时的队列项；等待期间切走了就把结果丢掉，不写进新项。
  const refreshDuplicateDecision = async (
    owner: string | null,
    nextUrl: string,
    nextDir: string,
    nextName: string,
    options?: { keepSelection?: boolean },
  ): Promise<duplicateModels.Decision> => {
    let next = new duplicateModels.Decision();
    if (nextUrl && nextDir && nextName) {
      next = await ResolveDuplicateDecision(nextUrl, nextDir, nextName);
    }
    if (owner !== useFileInfoDraftStore.getState().activeItemId) {
      return next;
    }
    applyDecision(next, options);
    return next;
  };

  const initItem = useCallback(
    async (item: windowModels.FileInfoItem) => {
      invalidateInFlight();
      setActiveItem((prev) => ({
        ...item,
        queueIndex: item.queueIndex || prev?.queueIndex || 1,
        queueTotal: item.queueTotal || prev?.queueTotal || 1,
      }));
      await openDraft(item, useSettingsStore.getState().settings);
    },
    [openDraft, invalidateInFlight],
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
    const unlistenNext = Events.On(Event.FileInfoNext, (ev: unknown) => {
      const item = unwrapEventData<windowModels.FileInfoItem>(ev);
      if (item) {
        void initItem(item);
      } else {
        setActiveItem(null);
      }
    });

    // Listen for queue updates
    const unlistenQueue = Events.On(Event.FileInfoQueueUpdated, (ev: unknown) => {
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
    const unlistenUpdated = Events.On(Event.FileInfoUpdated, (ev: unknown) => {
      const item = unwrapEventData<windowModels.FileInfoItem>(ev);
      if (item && item.id === useFileInfoDraftStore.getState().activeItemId) {
        setProbing(false);
        setActiveItem((prev) => (prev ? { ...prev, ...item } : item));
        const current = useFileInfoDraftStore.getState().draft;
        if (item.totalBytes !== undefined && item.totalBytes > 0) {
          if (!current.nameEdited && item.filename) {
            patch({ filename: item.filename, originalFilename: item.filename });
          }
        }
        if (item.fileConflict !== undefined) {
          patch({ fileConflict: item.fileConflict });
        }
        if (item.suggestedFilename) {
          patch({ suggestedFilename: item.suggestedFilename });
        }
        // 后端探测完毕后会带着重新裁决的结果回来，界面照它更新选项与选中项。
        if (item.duplicateDecision) {
          applyDecision(item.duplicateDecision, { keepSelection: true });
        }
        if (item.url && (item.filename || current.filename)) {
          void (async () => {
            const checkName = item.filename || current.filename;
            const checkDir = item.directory || current.directory;
            if (checkDir && checkName) {
              const conf = await CheckURLFilesExist(item.url, checkDir, checkName);
              patch(reuseFields(conf), item.id);
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
  }, [loadSettings, initItem, applyDecision, patch, setProbing]);

  // Handle URL probe when URL is changed manually
  // 手动改链接后的探测。每一步 await 之后都要重新确认请求还有效，否则上一项的结果会写进下一项。
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
      const current = useFileInfoDraftStore.getState().draft;
      const rawName =
        (!current.nameEdited && result.filename
          ? result.filename
          : current.filename || result.filename) || '';
      const originalName = result.filename || current.filename;

      // 命中分类只取决于文件名，即使目录被用户手动改过也要更新；目录只在用户没改过时跟随。
      let effectiveDir = current.directory;
      let autoCategoryId = current.autoCategoryId;
      let destinationPatch: Partial<FileInfoDraft> = {};
      if (rawName) {
        try {
          const dest = await ResolveDestination(rawName);
          if (seq !== probeSeqRef.current) return;
          autoCategoryId = dest.categoryId || 'builtin-file';
          if (!current.dirEdited && dest.directory) {
            effectiveDir = dest.directory;
            if (dest.directory !== current.directory) {
              destinationPatch = { directory: dest.directory };
            }
          }
        } catch {
          // fallback to currentDir
        }
      }

      let suggestedName = '';
      let conflictPatch: Partial<FileInfoDraft> = {};
      if (effectiveDir && rawName) {
        if (result.duplicateTask) {
          const conf = await CheckURLFilesExist(trimmed, effectiveDir, rawName);
          if (seq !== probeSeqRef.current) return;
          conflictPatch = reuseFields(conf);
          suggestedName = conf.suggestedFilename;
        } else {
          const conf = await CheckFileConflict(effectiveDir, rawName);
          if (seq !== probeSeqRef.current) return;
          conflictPatch = { fileConflict: conf.exists };
          suggestedName = conf.suggestedFilename;
        }
      }

      // 链接换了就要重新裁决：选项与默认动作取决于这个链接和最终落点。
      const decision =
        effectiveDir && rawName
          ? await ResolveDuplicateDecision(trimmed, effectiveDir, rawName)
          : new duplicateModels.Decision();
      if (seq !== probeSeqRef.current) return;

      // 默认动作是「序号副本」时预置后端算出的建议名称，这正是该动作的含义。
      const chosenName =
        decision.default === duplicateModels.Action.ActionCopy && suggestedName
          ? suggestedName
          : rawName;
      patch({
        ...destinationPatch,
        autoCategoryId,
        ...conflictPatch,
        ...(effectiveDir && rawName ? { suggestedFilename: suggestedName } : {}),
        filename: chosenName,
        originalFilename: originalName,
        duplicateDecision: decision,
        selectedAction: resolveAction(decision, current.selectedAction),
        error: null,
      });
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
    } catch (err: unknown) {
      if (seq !== probeSeqRef.current) return;
      const msg = err instanceof Error ? err.message : String(err);
      patch({ error: `资源探测失败: ${msg}` });
    } finally {
      if (seq === probeSeqRef.current) {
        setProbing(false);
      }
    }
  };

  const currentSettings = useSettingsStore((s) => s.settings);
  const effectiveCategoryId = effectiveCategoryIdOf(draft);

  const overwriteOption = overwriteOptionOf(duplicateDecision);
  const isOverwriteSelected = overwriteOption !== undefined && selectedAction === overwriteOption;
  const destinationOccupied =
    duplicateDecision.case === duplicateModels.Case.CaseDestinationOccupied;
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
    patch({ selectedCategoryId: catId, categoryEdited: true, dirEdited: true });

    const allCats = [
      ...(currentSettings?.download?.customCategories || []),
      ...(currentSettings?.download?.builtinCategories || []),
    ];
    const targetCat = allCats.find((c) => c.id === catId);
    const catDir = targetCat?.directory || currentSettings?.download?.defaultDirectory || '';

    patch({ directory: catDir });
    if (filename) {
      const owner = useFileInfoDraftStore.getState().activeItemId;
      void (async () => {
        const conf = await CheckFileConflict(catDir, filename);
        patch({ fileConflict: conf.exists, suggestedFilename: conf.suggestedFilename }, owner);
        await refreshDuplicateDecision(owner, url, catDir, filename, { keepSelection: true });
      })();
    }
  };

  const canKeepCategoryPath = canKeepCategoryPathOf(draft, currentSettings);

  const handleSelectDir = async () => {
    try {
      const owner = useFileInfoDraftStore.getState().activeItemId;
      const selected = await SelectDirectory();
      if (selected) {
        patch({ directory: selected, dirEdited: true }, owner);
        if (filename) {
          if (activeItem?.duplicateTask) {
            const conf = await CheckURLFilesExist(url, selected, filename);
            patch({ ...reuseFields(conf), suggestedFilename: conf.suggestedFilename }, owner);
          } else {
            const conf = await CheckFileConflict(selected, filename);
            patch({ fileConflict: conf.exists, suggestedFilename: conf.suggestedFilename }, owner);
          }
          await refreshDuplicateDecision(owner, url, selected, filename, { keepSelection: true });
        }
      }
    } catch (err) {
      console.error('Failed to select directory:', err);
    }
  };

  const handleConfirm = useCallback(
    async (actionOverride?: duplicateModels.Action, e?: SyntheticEvent) => {
      if (e) e.preventDefault();
      // 提交以草稿的最新值为准，不依赖渲染时捕获的那一份。
      const current = useFileInfoDraftStore.getState().draft;
      const {
        url,
        filename,
        directory,
        maxConn,
        preDownload,
        duplicateDecision,
        selectedAction,
        fileConflict,
        overwriteConflict,
        rememberCategory,
        keepCategoryPath,
        existingTaskID,
      } = current;
      const categoryId = effectiveCategoryIdOf(current);

      if (!url.trim()) {
        patch({ error: '请输入下载链接' });
        return;
      }
      if (!filename.trim()) {
        patch({ error: '请输入文件名' });
        return;
      }
      if (!directory.trim()) {
        patch({ error: '请选择保存目录' });
        return;
      }

      // 动作只来自后端给出的选项或裁决的默认项；界面不替用户决定，也不自行兜底。
      const action = actionOverride ?? selectedAction;
      if (duplicateDecision.case === duplicateModels.Case.CaseDestinationOccupied && !action) {
        patch({ error: '已有相同的下载链接和文件，请确认处理方式' });
        return;
      }

      // Disk conflict check: only block if not an explicit overwrite!
      if (fileConflict && !overwriteConflict && !isOverwriteAction(action)) {
        patch({ error: '目标目录存在同名文件，请确认是否覆盖或使用建议名称' });
        return;
      }

      if (rememberCategory && categoryId) {
        const ext = extractExtension(filename);
        if (ext) {
          try {
            await AssignExtensionToCategory(ext, categoryId);
            await loadSettings();
          } catch (assignErr) {
            console.error('Failed to assign extension to category:', assignErr);
          }
        }
      }

      if (
        keepCategoryPath &&
        categoryId &&
        canKeepCategoryPathOf(current, useSettingsStore.getState().settings)
      ) {
        try {
          await SetCategoryDirectory(categoryId, directory.trim());
          await loadSettings();
        } catch (catDirErr) {
          console.error('Failed to set category directory:', catDirErr);
        }
      }
      patch({ error: null });
      // 提交期间禁用按钮：提交会让后端把这一项移出队列并推进到下一项，
      // 连点两次会让第二次带着这一项的落点作用到下一项上。
      setLoading(true);
      const parsedConn = Math.min(32, Math.max(1, Number(maxConn) || 8));

      try {
        await SubmitFileInfo({
          requestId: activeItem?.id || '',
          url: url.trim(),
          filename: filename.trim(),
          directory: directory.trim(),
          maxConn: parsedConn,
          action,
          preDownload,
          reuseTaskId: action === duplicateModels.Action.ActionReuse ? existingTaskID : undefined,
        });
        if (activeItem?.id) {
          discardDraft(activeItem.id);
        }
      } catch (err: unknown) {
        const msg = err instanceof Error ? err.message : String(err);
        patch({ error: `提交失败: ${msg}` });
      } finally {
        setLoading(false);
      }
    },
    [activeItem, discardDraft, loadSettings, patch, setLoading],
  );
  const handleCancel = useCallback(async () => {
    try {
      const itemId = useFileInfoDraftStore.getState().activeItemId;
      if (itemId) {
        discardDraft(itemId);
      }
      await CancelCurrentFileInfo();
    } catch (err) {
      console.error('Failed to cancel file info:', err);
    }
  }, [discardDraft]);
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
                    patch({ url: e.target.value, error: null });
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
                {destinationOccupied ? (
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
                          patch({ selectedAction: duplicateModels.Action.ActionShowCompleted });
                          if (activeItem.duplicateTask?.id) {
                            void ShowProgressWindow(activeItem.duplicateTask.id);
                          }
                          // Keep window open as requested by user
                        }}
                        className={`flex items-center justify-center gap-1.5 rounded-md py-1.5 text-[11px] font-medium transition-all ${
                          selectedAction === duplicateModels.Action.ActionShowCompleted
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
                          patch({
                            selectedAction: overwriteOption,
                            error: null,
                            ...(originalFilename ? { filename: originalFilename } : {}),
                          });
                        }}
                        className={`flex items-center justify-center gap-1.5 rounded-md py-1.5 text-[11px] font-medium transition-all ${
                          isOverwriteSelected
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
                          patch({ selectedAction: duplicateModels.Action.ActionCopy, error: null });
                          const owner = useFileInfoDraftStore.getState().activeItemId;
                          void (async () => {
                            const baseName = originalFilename || activeItem?.filename || filename;
                            if (directory && baseName) {
                              const conf = await CheckURLFilesExist(url, directory, baseName);
                              if (conf.suggestedFilename) {
                                patch({ filename: conf.suggestedFilename }, owner);
                              }
                            }
                          })();
                        }}
                        className={`flex items-center justify-center gap-1.5 rounded-md py-1.5 text-[11px] font-medium transition-all ${
                          selectedAction === duplicateModels.Action.ActionCopy
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
                          {duplicateDecision.case === duplicateModels.Case.CaseHistoryUnfinished
                            ? '此链接上次未下载完成，将继续下载已有进度'
                            : '此前已下载过此链接，当前目录下无同名文件'}
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
                                onClick={() =>
                                  void handleConfirm(duplicateModels.Action.ActionReuse)
                                }
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
            {fileConflict && !activeItem?.duplicateTask && !isOverwriteSelected && (
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
                    onClick={() => patch({ overwriteConflict: true })}
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
                        patch({
                          filename: suggestedFilename,
                          fileConflict: false,
                          overwriteConflict: false,
                        });
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
                    patch({ filename: newName, nameEdited: true });

                    const seq = ++filenameSeqRef.current;
                    if (newName) {
                      void (async () => {
                        try {
                          const dest = await ResolveDestination(newName);
                          if (seq === filenameSeqRef.current) {
                            // 命中分类跟随文件名变化；目录仅在用户未手动改过时跟随。
                            patch({ autoCategoryId: dest.categoryId || 'builtin-file' });
                            if (
                              !useFileInfoDraftStore.getState().draft.dirEdited &&
                              dest.directory
                            ) {
                              patch({ directory: dest.directory });
                            }
                          }
                        } catch {
                          // ignore
                        }
                      })();
                    }

                    if (directory) {
                      const owner = useFileInfoDraftStore.getState().activeItemId;
                      void (async () => {
                        const conf = await CheckFileConflict(directory, newName);
                        patch(
                          { fileConflict: conf.exists, suggestedFilename: conf.suggestedFilename },
                          owner,
                        );
                        await refreshDuplicateDecision(owner, url, directory, newName, {
                          keepSelection: true,
                        });
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
                      patch({ maxConn: 0 });
                    } else {
                      const val = parseInt(cleaned, 10);
                      patch({ maxConn: Math.min(32, Math.max(1, val)) });
                    }
                  }}
                  onBlur={() => {
                    if (!maxConn || Number(maxConn) < 1) {
                      patch({ maxConn: 1 });
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
                    const newDir = e.target.value;
                    patch({ directory: newDir, dirEdited: true });
                    if (filename) {
                      const owner = useFileInfoDraftStore.getState().activeItemId;
                      void (async () => {
                        const conf = await CheckFileConflict(newDir, filename);
                        patch(
                          { fileConflict: conf.exists, suggestedFilename: conf.suggestedFilename },
                          owner,
                        );
                        await refreshDuplicateDecision(owner, url, newDir, filename, {
                          keepSelection: true,
                        });
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
                        onCheckedChange={(checked) => patch({ rememberCategory: Boolean(checked) })}
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
                        onCheckedChange={(checked) => patch({ keepCategoryPath: Boolean(checked) })}
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
