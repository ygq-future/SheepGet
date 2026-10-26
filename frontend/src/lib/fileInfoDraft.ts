import * as duplicateModels from '../../bindings/sheep-get/internal/duplicate/models';
import * as windowModels from '../../bindings/sheep-get/internal/window/models';
import type * as configModels from '../../bindings/sheep-get/internal/config/models';

/**
 * FileInfoDraft 是文件信息窗口在单个队列项上的一份表单草稿。
 *
 * 窗口同一时刻只显示一项，但用户可以在队列里来回切换。草稿保存的是「用户在这一项上填过什么」，
 * 与后端给出的队列项数据是两回事。凡是随项切换必须整体替换的内容都放在这里，包括错误提示，
 * 这样任何一项的提示都不会显示在别的项上。
 */
export interface FileInfoDraft {
  url: string;
  filename: string;
  directory: string;
  maxConn: number;
  preDownload: boolean;
  // 裁决结果与用户选中的动作都由后端给出/校验，草稿只做原样保存与恢复。
  duplicateDecision: duplicateModels.Decision;
  selectedAction?: duplicateModels.Action;
  fileConflict: boolean;
  suggestedFilename: string;
  overwriteConflict: boolean;
  error: string | null;
  nameEdited: boolean;
  dirEdited: boolean;
  originalFilename: string;
  selectedCategoryId: string;
  autoCategoryId: string;
  rememberCategory: boolean;
  categoryEdited: boolean;
  keepCategoryPath: boolean;
  canReuseExistingFile: boolean;
  existingPath: string;
  existingTaskID: string;
}

// 后端未命中任何分类时使用的分类标识；界面不自行推导分类规则，只保留这个兜底。
const FALLBACK_CATEGORY_ID = 'builtin-file';

export function emptyFileInfoDraft(): FileInfoDraft {
  return {
    url: '',
    filename: '',
    directory: '',
    maxConn: 32,
    preDownload: false,
    duplicateDecision: new duplicateModels.Decision(),
    selectedAction: undefined,
    fileConflict: false,
    suggestedFilename: '',
    overwriteConflict: false,
    error: null,
    nameEdited: false,
    dirEdited: false,
    originalFilename: '',
    selectedCategoryId: FALLBACK_CATEGORY_ID,
    autoCategoryId: FALLBACK_CATEGORY_ID,
    rememberCategory: false,
    categoryEdited: false,
    keepCategoryPath: false,
    canReuseExistingFile: false,
    existingPath: '',
    existingTaskID: '',
  };
}

/** 由队列项与当前设置生成一份全新草稿；用户还没有在任何字段上改动过。 */
export function draftFromItem(
  item: windowModels.FileInfoItem,
  settings: configModels.Settings | null,
): FileInfoDraft {
  const directory = item.directory || settings?.download?.defaultDirectory || '';
  // 没有链接、文件名又只是占位值时，界面上留空让用户自己填。
  const initialName = !item.url && item.filename === 'download.bin' ? '' : item.filename || '';
  const decision = item.duplicateDecision || new duplicateModels.Decision();
  const destinationOccupied = decision.case === duplicateModels.Case.CaseDestinationOccupied;
  const categoryId = item.categoryId || FALLBACK_CATEGORY_ID;

  return {
    ...emptyFileInfoDraft(),
    url: item.url || '',
    filename: initialName,
    directory,
    maxConn: item.maxConn || settings?.download?.defaultConnectionsPerTask || 32,
    // 提前下载在登记请求时就按「设置默认值或请求覆盖」定好并随项下发，窗口只展示它。
    preDownload: item.preDownload,
    duplicateDecision: decision,
    selectedAction: decision.default || undefined,
    fileConflict: destinationOccupied ? false : !!item.fileConflict,
    suggestedFilename: item.suggestedFilename || '',
    originalFilename: initialName,
    selectedCategoryId: categoryId,
    autoCategoryId: categoryId,
  };
}

/** 「其他目录已有同一份成品」这条规则的字段归一化；三项齐备才算可复用。 */
export function reuseFields(conf: {
  canReuseExistingFile?: boolean;
  existingPath?: string;
  existingTaskID?: string;
}): Pick<FileInfoDraft, 'canReuseExistingFile' | 'existingPath' | 'existingTaskID'> {
  if (conf.canReuseExistingFile && conf.existingPath && conf.existingTaskID) {
    return {
      canReuseExistingFile: true,
      existingPath: conf.existingPath,
      existingTaskID: conf.existingTaskID,
    };
  }
  return { canReuseExistingFile: false, existingPath: '', existingTaskID: '' };
}

/**
 * 落点变化后重新裁决时，用户已经选过的动作只要仍然可选就保留，否则回落到裁决给出的默认项。
 * keepSelection 由落点变化的场景使用；换链接重新裁决时不传，直接采用默认项。
 */
export function resolveAction(
  decision: duplicateModels.Decision,
  previous: duplicateModels.Action | undefined,
  options?: { keepSelection?: boolean },
): duplicateModels.Action | undefined {
  if (options?.keepSelection && previous && decision.options?.includes(previous)) {
    return previous;
  }
  return decision.default || undefined;
}

/**
 * 「继续覆盖」是唯一需要后端解析的一项：历史任务还活着就续传，已完成且成品在磁盘上就覆盖重下。
 * 其余两项语义唯一，直接用动作标识即可。
 */
export function overwriteOptionOf(
  decision: duplicateModels.Decision,
): duplicateModels.Action | undefined {
  return decision.options?.find(
    (action) =>
      action === duplicateModels.Action.ActionContinue ||
      action === duplicateModels.Action.ActionRedownload,
  );
}

export function isOverwriteAction(action: duplicateModels.Action | undefined): boolean {
  return (
    action === duplicateModels.Action.ActionContinue ||
    action === duplicateModels.Action.ActionRedownload
  );
}

/**
 * 校验是否需要因本地磁盘同名文件冲突而阻断提交。
 * 当用户已明确选择覆盖（ActionContinue/ActionRedownload）、序号副本（ActionCopy），
 * 或在单文件冲突弹窗中明确勾选了覆盖（overwriteConflict）时，不予阻断。
 */
export function shouldBlockForDiskConflict(
  fileConflict: boolean,
  overwriteConflict: boolean,
  action?: duplicateModels.Action,
): boolean {
  if (!fileConflict) return false;
  if (overwriteConflict) return false;
  if (isOverwriteAction(action)) return false;
  if (action === duplicateModels.Action.ActionCopy) return false;
  return true;
}

/** 用户手动选过分类就用选中的，否则用后端给出的命中分类。 */
export function effectiveCategoryIdOf(draft: FileInfoDraft): string {
  return draft.categoryEdited && draft.selectedCategoryId
    ? draft.selectedCategoryId
    : draft.autoCategoryId;
}

/** 选了分类、且路径相对该分类的默认值被改过时，才允许把这条路径写回分类设置。 */
export function canKeepCategoryPathOf(
  draft: FileInfoDraft,
  settings: configModels.Settings | null,
): boolean {
  const categoryId = effectiveCategoryIdOf(draft);
  if (!categoryId) return false;
  const categories = [
    ...(settings?.download?.customCategories || []),
    ...(settings?.download?.builtinCategories || []),
  ];
  const categoryDir =
    categories.find((c) => c.id === categoryId)?.directory ||
    settings?.download?.defaultDirectory ||
    '';
  const directory = draft.directory.trim();
  return directory.length > 0 && directory !== categoryDir.trim();
}

/**
 * 判断当前项是否属于 HLS 流媒体资源（或待选清晰度候选）。
 * 只有具备已选清晰度、可选变体或明确为播放列表链接时，界面才展示清晰度相关信息及加载态。
 */
export function isHLSResource(item: windowModels.FileInfoItem | null, draftUrl?: string): boolean {
  if (!item && !draftUrl) return false;
  if (item?.qualityLabel) return true;
  if (item?.variants && item.variants.length > 0) return true;
  const targetUrl = (draftUrl || item?.url || '').split('?')[0].split('#')[0].toLowerCase();
  return targetUrl.endsWith('.m3u8') || targetUrl.endsWith('.m3u');
}

/**
 * 后端后台探测完成（或兜底轮询取回已完成项）时，把探测补齐的真实文件名、命中分类、目录、
 * 同名冲突及重复裁决合并到当前草稿中，同时保留用户在等待期间已手动修改过的字段。
 */
export function patchFromProbedItem(
  item: windowModels.FileInfoItem,
  current: FileInfoDraft,
): Partial<FileInfoDraft> {
  const result: Partial<FileInfoDraft> = {};
  if (!current.nameEdited && item.filename && item.filename !== current.filename) {
    result.filename = item.filename;
    result.originalFilename = item.filename;
  }
  const isDestinationOccupied =
    item.duplicateDecision?.case === duplicateModels.Case.CaseDestinationOccupied ||
    current.duplicateDecision?.case === duplicateModels.Case.CaseDestinationOccupied;
  if (item.fileConflict !== undefined) {
    result.fileConflict = isDestinationOccupied ? false : item.fileConflict;
  }
  if (item.suggestedFilename !== undefined) {
    result.suggestedFilename = item.suggestedFilename;
  }
  if (item.categoryId) {
    result.autoCategoryId = item.categoryId;
    if (!current.categoryEdited) {
      result.selectedCategoryId = item.categoryId;
    }
  }
  if (item.directory && !current.dirEdited) {
    result.directory = item.directory;
  }
  if (item.duplicateDecision) {
    result.duplicateDecision = item.duplicateDecision;
    result.selectedAction = resolveAction(item.duplicateDecision, current.selectedAction, {
      keepSelection: true,
    });
  }
  return result;
}
