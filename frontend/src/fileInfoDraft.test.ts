import { describe, it, expect, beforeEach } from 'vitest';
import * as configModels from '../bindings/sheep-get/internal/config/models';
import * as duplicateModels from '../bindings/sheep-get/internal/duplicate/models';
import * as windowModels from '../bindings/sheep-get/internal/window/models';
import {
  canKeepCategoryPathOf,
  draftFromItem,
  effectiveCategoryIdOf,
  emptyFileInfoDraft,
  overwriteOptionOf,
  resolveAction,
  reuseFields,
  shouldBlockForDiskConflict,
} from './lib/fileInfoDraft';
import { useFileInfoDraftStore } from './stores/fileInfoDraft';

function makeItem(overrides: Partial<windowModels.FileInfoItem> = {}): windowModels.FileInfoItem {
  return new windowModels.FileInfoItem({
    id: 'item-1',
    url: 'https://host/a.mp4',
    filename: 'a.mp4',
    ...overrides,
  });
}

function makeSettings(overrides: Partial<configModels.DownloadConfig> = {}) {
  return new configModels.Settings({
    download: new configModels.DownloadConfig({
      defaultDirectory: '/downloads',
      defaultConnectionsPerTask: 6,
      preDownload: true,
      ...overrides,
    }),
  });
}

beforeEach(() => {
  useFileInfoDraftStore.setState({
    draft: emptyFileInfoDraft(),
    drafts: {},
    activeItemId: null,
    probing: false,
    loading: false,
  });
});

describe('draftFromItem', () => {
  it('以后端给出的项与当前设置生成草稿', () => {
    const draft = draftFromItem(makeItem(), makeSettings());
    expect(draft.url).toBe('https://host/a.mp4');
    expect(draft.filename).toBe('a.mp4');
    expect(draft.directory).toBe('/downloads');
    expect(draft.maxConn).toBe(6);
    expect(draft.originalFilename).toBe('a.mp4');
    expect(draft.error).toBeNull();
    expect(draft.nameEdited).toBe(false);
    expect(draft.dirEdited).toBe(false);
  });

  it('目录与并发数取项上的值，缺失时才回落到设置', () => {
    const fromSettings = draftFromItem(makeItem({ directory: '', maxConn: 0 }), makeSettings());
    expect(fromSettings.directory).toBe('/downloads');
    expect(fromSettings.maxConn).toBe(6);

    const fromItem = draftFromItem(
      makeItem({ directory: '/elsewhere', maxConn: 3 }),
      makeSettings(),
    );
    expect(fromItem.directory).toBe('/elsewhere');
    expect(fromItem.maxConn).toBe(3);
  });

  it('提前下载开关以队列项上的值为准', () => {
    expect(
      draftFromItem(makeItem({ preDownload: true }), makeSettings({ preDownload: false }))
        .preDownload,
    ).toBe(true);
    expect(
      draftFromItem(makeItem({ preDownload: false }), makeSettings({ preDownload: true }))
        .preDownload,
    ).toBe(false);
  });

  it('没有链接且文件名只是占位值时不预填文件名', () => {
    const draft = draftFromItem(makeItem({ url: '', filename: 'download.bin' }), null);
    expect(draft.filename).toBe('');
    expect(draft.originalFilename).toBe('');
  });

  it('目标位置已有成品文件时不沿用同名文件冲突标记', () => {
    const occupied = draftFromItem(
      makeItem({
        fileConflict: true,
        duplicateDecision: new duplicateModels.Decision({
          case: duplicateModels.Case.CaseDestinationOccupied,
          options: [
            duplicateModels.Action.ActionShowCompleted,
            duplicateModels.Action.ActionContinue,
            duplicateModels.Action.ActionCopy,
          ],
        }),
      }),
      null,
    );
    expect(occupied.fileConflict).toBe(false);
  });

  it('命中分类同时作为选中分类与自动分类的起点', () => {
    const draft = draftFromItem(makeItem({ categoryId: 'custom-video' }), null);
    expect(draft.selectedCategoryId).toBe('custom-video');
    expect(draft.autoCategoryId).toBe('custom-video');
    expect(effectiveCategoryIdOf(draft)).toBe('custom-video');
  });
});

describe('resolveAction', () => {
  const options = [duplicateModels.Action.ActionContinue, duplicateModels.Action.ActionCopy];

  it('落点变化时保留用户已选且仍然可选的动作', () => {
    const decision = new duplicateModels.Decision({
      case: duplicateModels.Case.CaseDestinationOccupied,
      options,
      default: duplicateModels.Action.ActionContinue,
    });
    expect(
      resolveAction(decision, duplicateModels.Action.ActionCopy, { keepSelection: true }),
    ).toBe(duplicateModels.Action.ActionCopy);
  });

  it('用户已选的动作不再可选时回落到裁决给出的默认项', () => {
    const decision = new duplicateModels.Decision({
      case: duplicateModels.Case.CaseDestinationOccupied,
      options,
      default: duplicateModels.Action.ActionContinue,
    });
    expect(
      resolveAction(decision, duplicateModels.Action.ActionShowCompleted, { keepSelection: true }),
    ).toBe(duplicateModels.Action.ActionContinue);
  });

  it('换链接重新裁决时直接采用默认项', () => {
    const decision = new duplicateModels.Decision({
      case: duplicateModels.Case.CaseHistoryUnfinished,
      default: duplicateModels.Action.ActionContinue,
    });
    expect(resolveAction(decision, duplicateModels.Action.ActionCopy)).toBe(
      duplicateModels.Action.ActionContinue,
    );
  });

  it('裁决没有默认项时不给选中项，等用户自己选', () => {
    const decision = new duplicateModels.Decision({
      case: duplicateModels.Case.CaseDestinationOccupied,
      options,
    });
    expect(resolveAction(decision, undefined, { keepSelection: true })).toBeUndefined();
  });
});

describe('overwriteOptionOf', () => {
  it('历史任务未完成时「继续覆盖」解析为续传', () => {
    const decision = new duplicateModels.Decision({
      options: [duplicateModels.Action.ActionContinue, duplicateModels.Action.ActionCopy],
    });
    expect(overwriteOptionOf(decision)).toBe(duplicateModels.Action.ActionContinue);
  });

  it('历史任务已完成且成品在磁盘上时「继续覆盖」解析为覆盖重下', () => {
    const decision = new duplicateModels.Decision({
      options: [duplicateModels.Action.ActionRedownload, duplicateModels.Action.ActionCopy],
    });
    expect(overwriteOptionOf(decision)).toBe(duplicateModels.Action.ActionRedownload);
  });

  it('选项里没有续传或覆盖时返回空', () => {
    const decision = new duplicateModels.Decision({
      options: [duplicateModels.Action.ActionShowCompleted],
    });
    expect(overwriteOptionOf(decision)).toBeUndefined();
  });
});

describe('reuseFields', () => {
  it('三项齐备时才标记可复用', () => {
    expect(
      reuseFields({
        canReuseExistingFile: true,
        existingPath: '/other/a.mp4',
        existingTaskID: 't1',
      }),
    ).toEqual({ canReuseExistingFile: true, existingPath: '/other/a.mp4', existingTaskID: 't1' });
  });

  it('缺任一项就清空复用状态', () => {
    expect(reuseFields({ canReuseExistingFile: true, existingPath: '/other/a.mp4' })).toEqual({
      canReuseExistingFile: false,
      existingPath: '',
      existingTaskID: '',
    });
  });
});

describe('canKeepCategoryPathOf', () => {
  const settings = makeSettings({
    defaultDirectory: '/downloads',
    builtinCategories: [
      new configModels.CategoryConfig({ id: 'builtin-video', name: '视频', directory: '/videos' }),
    ],
  });

  it('选了分类且路径被改过时可用', () => {
    const draft = {
      ...emptyFileInfoDraft(),
      categoryEdited: true,
      selectedCategoryId: 'builtin-video',
      directory: '/custom',
    };
    expect(canKeepCategoryPathOf(draft, settings)).toBe(true);
  });

  it('路径仍等于该分类默认值时不可用', () => {
    const draft = {
      ...emptyFileInfoDraft(),
      categoryEdited: true,
      selectedCategoryId: 'builtin-video',
      directory: '/videos',
    };
    expect(canKeepCategoryPathOf(draft, settings)).toBe(false);
  });

  it('没有填目录时不可用', () => {
    const draft = {
      ...emptyFileInfoDraft(),
      categoryEdited: true,
      selectedCategoryId: 'builtin-video',
      directory: '   ',
    };
    expect(canKeepCategoryPathOf(draft, settings)).toBe(false);
  });
});

describe('文件信息草稿 store', () => {
  const first = makeItem({ id: 'item-1', url: 'https://host/a.mp4', filename: 'a.mp4' });
  const second = makeItem({ id: 'item-2', url: 'https://host/b.mp4', filename: 'b.mp4' });

  it('切换队列项时错误提示不串到别的项', () => {
    const store = useFileInfoDraftStore.getState();
    store.open(first, null);
    useFileInfoDraftStore.getState().patch({ error: '资源探测失败: boom' });

    useFileInfoDraftStore.getState().open(second, null);
    expect(useFileInfoDraftStore.getState().draft.error).toBeNull();
    expect(useFileInfoDraftStore.getState().draft.filename).toBe('b.mp4');
  });

  it('切回已经访问过的项会恢复那一项自己的提示与编辑', () => {
    const store = useFileInfoDraftStore.getState();
    store.open(first, null);
    useFileInfoDraftStore.getState().patch({
      url: 'https://host/typo.mp4',
      filename: 'typed.mp4',
      error: '资源探测失败: boom',
      originalFilename: 'a.mp4',
      rememberCategory: true,
    });

    useFileInfoDraftStore.getState().open(second, null);
    useFileInfoDraftStore.getState().open(first, null);

    const restored = useFileInfoDraftStore.getState().draft;
    expect(restored.url).toBe('https://host/typo.mp4');
    expect(restored.filename).toBe('typed.mp4');
    expect(restored.error).toBe('资源探测失败: boom');
    expect(restored.originalFilename).toBe('a.mp4');
    expect(restored.rememberCategory).toBe(true);
  });
  it('切换项时清零在途指示，不把上一项的探测或提交状态带过来', () => {
    const store = useFileInfoDraftStore.getState();
    store.open(first, null);
    useFileInfoDraftStore.getState().setProbing(true);
    useFileInfoDraftStore.getState().setLoading(true);

    useFileInfoDraftStore.getState().open(second, null);
    expect(useFileInfoDraftStore.getState().probing).toBe(false);
    expect(useFileInfoDraftStore.getState().loading).toBe(false);
  });

  it('打开项时继承项上的 probing 状态', () => {
    const probingItem = makeItem({ id: 'item-probing', probing: true });
    useFileInfoDraftStore.getState().open(probingItem, null);
    expect(useFileInfoDraftStore.getState().probing).toBe(true);

    const doneItem = makeItem({ id: 'item-done', probing: false });
    useFileInfoDraftStore.getState().open(doneItem, null);
    expect(useFileInfoDraftStore.getState().probing).toBe(false);
  });

  it('提交或取消后丢弃的草稿不会在再次打开时复活', () => {
    const store = useFileInfoDraftStore.getState();
    store.open(first, null);
    useFileInfoDraftStore.getState().patch({ filename: 'typed.mp4' });

    useFileInfoDraftStore.getState().discard(first.id);
    expect(useFileInfoDraftStore.getState().draft.filename).toBe('typed.mp4');

    useFileInfoDraftStore.getState().open(second, null);
    useFileInfoDraftStore.getState().open(first, null);
    expect(useFileInfoDraftStore.getState().draft.filename).toBe('a.mp4');
  });

  it('异步回填仍停在发起项时照常写入', () => {
    const store = useFileInfoDraftStore.getState();
    store.open(first, null);
    const owner = useFileInfoDraftStore.getState().activeItemId;

    useFileInfoDraftStore.getState().patch({ fileConflict: true }, owner);
    expect(useFileInfoDraftStore.getState().draft.fileConflict).toBe(true);
  });

  it('异步回填在等待期间切走时丢弃，不写进新项', () => {
    const store = useFileInfoDraftStore.getState();
    store.open(first, null);
    const owner = useFileInfoDraftStore.getState().activeItemId;

    useFileInfoDraftStore.getState().open(second, null);
    useFileInfoDraftStore.getState().patch({ fileConflict: true }, owner);
    expect(useFileInfoDraftStore.getState().draft.fileConflict).toBe(false);
  });
  it('没有活动项时改动只更新显示，不写进任何一项的快照', () => {
    useFileInfoDraftStore.getState().patch({ filename: 'typed.mp4' });
    expect(useFileInfoDraftStore.getState().draft.filename).toBe('typed.mp4');
    expect(useFileInfoDraftStore.getState().drafts).toEqual({});
  });
});

describe('shouldBlockForDiskConflict', () => {
  it('无磁盘文件冲突时不阻断', () => {
    expect(shouldBlockForDiskConflict(false, false)).toBe(false);
  });

  it('有冲突且用户在同名弹窗中明确勾选覆盖时不阻断', () => {
    expect(shouldBlockForDiskConflict(true, true)).toBe(false);
  });

  it('有冲突且用户选择了覆盖动作（Continue/Redownload）时不阻断', () => {
    expect(shouldBlockForDiskConflict(true, false, duplicateModels.Action.ActionContinue)).toBe(
      false,
    );
    expect(shouldBlockForDiskConflict(true, false, duplicateModels.Action.ActionRedownload)).toBe(
      false,
    );
  });

  it('有冲突且用户明确选择了序号副本动作（ActionCopy）时绝不阻断', () => {
    expect(shouldBlockForDiskConflict(true, false, duplicateModels.Action.ActionCopy)).toBe(false);
  });

  it('有冲突且未选择任何覆盖或副本策略时予以阻断', () => {
    expect(shouldBlockForDiskConflict(true, false)).toBe(true);
    expect(shouldBlockForDiskConflict(true, false, undefined)).toBe(true);
    expect(
      shouldBlockForDiskConflict(true, false, duplicateModels.Action.ActionShowCompleted),
    ).toBe(true);
  });
});
