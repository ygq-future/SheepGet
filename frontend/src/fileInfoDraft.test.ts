import { describe, it, expect, beforeEach } from 'vitest';
import * as configModels from '../bindings/sheep-get/internal/config/models';
import * as duplicateModels from '../bindings/sheep-get/internal/duplicate/models';
import * as windowModels from '../bindings/sheep-get/internal/window/models';
import {
  canKeepCategoryPathOf,
  draftFromItem,
  effectiveCategoryIdOf,
  emptyFileInfoDraft,
  isHLSResource,
  overwriteOptionOf,
  patchFromProbedItem,
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

  it('分类默认目录取后端给出的值，不按分类配置重算', () => {
    // 内置分类在配置里存的是相对子目录，落到目录时已经由后端拼成绝对路径。
    const draft = draftFromItem(
      makeItem({ directory: '/manual', categoryDirectory: '/downloads/Videos' }),
      makeSettings(),
    );
    expect(draft.directory).toBe('/manual');
    expect(draft.categoryDefaultDir).toBe('/downloads/Videos');
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
  it('路径与分类默认值不同时可用', () => {
    const draft = {
      ...emptyFileInfoDraft(),
      categoryEdited: true,
      selectedCategoryId: 'builtin-video',
      directory: '/custom',
      categoryDefaultDir: '/downloads/Videos',
    };
    expect(canKeepCategoryPathOf(draft)).toBe(true);
  });

  it('路径仍等于分类默认值时不可用', () => {
    // 回归：分类配置里存的是相对子目录 "Videos"，与绝对路径直接比较会永远判成“改过”。
    const draft = {
      ...emptyFileInfoDraft(),
      categoryEdited: true,
      selectedCategoryId: 'builtin-video',
      directory: '/downloads/Videos',
      categoryDefaultDir: '/downloads/Videos',
    };
    expect(canKeepCategoryPathOf(draft)).toBe(false);
  });

  it('没有填目录时不可用', () => {
    const draft = {
      ...emptyFileInfoDraft(),
      categoryEdited: true,
      selectedCategoryId: 'builtin-video',
      directory: '   ',
      categoryDefaultDir: '/downloads/Videos',
    };
    expect(canKeepCategoryPathOf(draft)).toBe(false);
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

describe('isHLSResource', () => {
  it('常规文件（.7z, .zip, .exe 等）不作为 HLS 资源，不展示清晰度', () => {
    const item = makeItem({
      url: 'https://github.com/mifi/lossless-cut/releases/download/v3.69.0/LosslessCut-win-x64.7z',
      filename: 'LosslessCut-win-x64.7z',
    });
    expect(isHLSResource(item)).toBe(false);
    expect(isHLSResource(null, item.url)).toBe(false);
  });

  it('具有已选清晰度标签的项识别为 HLS 资源', () => {
    const item = makeItem({
      url: 'https://example.com/video',
      qualityLabel: '1080p',
    });
    expect(isHLSResource(item)).toBe(true);
  });

  it('具有清晰度变体选项的项识别为 HLS 资源', () => {
    const item = makeItem({
      url: 'https://example.com/video',
      variants: [{ uri: 'https://example.com/1080p.m3u8', label: '1080p' }],
    });
    expect(isHLSResource(item)).toBe(true);
  });

  it('以 .m3u8 或 .m3u 结尾的链接（含参数/哈希）识别为 HLS 候选', () => {
    expect(isHLSResource(null, 'https://example.com/live/index.m3u8')).toBe(true);
    expect(isHLSResource(null, 'https://example.com/live/index.m3u8?token=abc#section')).toBe(true);
    expect(isHLSResource(null, 'https://example.com/playlist.m3u')).toBe(true);
    expect(isHLSResource(null, 'https://example.com/file.m3u80')).toBe(false);
  });
});

describe('patchFromProbedItem', () => {
  it('后台探测补齐真实文件名与分类目录时更新未手动编辑的字段', () => {
    const initial = draftFromItem(
      makeItem({
        id: 'item-1',
        url: 'https://host/download?id=123',
        filename: 'download.bin',
        directory: '/downloads/Files',
        categoryId: 'builtin-file',
        probing: true,
      }),
      null,
    );
    const probed = makeItem({
      id: 'item-1',
      url: 'https://host/download?id=123',
      filename: 'setup.msi',
      directory: '/downloads/Software',
      categoryId: 'builtin-software',
      categoryDirectory: '/downloads/Software',
      totalBytes: 0,
      probing: false,
    });

    const patch = patchFromProbedItem(probed, initial);
    expect(patch.filename).toBe('setup.msi');
    expect(patch.originalFilename).toBe('setup.msi');
    expect(patch.autoCategoryId).toBe('builtin-software');
    expect(patch.selectedCategoryId).toBe('builtin-software');
    expect(patch.directory).toBe('/downloads/Software');
    expect(patch.categoryDefaultDir).toBe('/downloads/Software');
  });

  it('用户在探测等待期间已手动改过文件名或目录时保留用户的修改', () => {
    const initial = {
      ...draftFromItem(makeItem({ filename: 'raw.bin', directory: '/downloads' }), null),
      filename: 'custom.bin',
      nameEdited: true,
      directory: '/custom-dir',
      dirEdited: true,
      selectedCategoryId: 'custom-cat',
      categoryEdited: true,
    };
    const probed = makeItem({
      filename: 'server-name.msi',
      directory: '/downloads/Software',
      categoryId: 'builtin-software',
      categoryDirectory: '/downloads/Software',
      totalBytes: 2048,
      probing: false,
    });

    const patch = patchFromProbedItem(probed, initial);
    expect(patch.filename).toBeUndefined();
    expect(patch.directory).toBeUndefined();
    expect(patch.autoCategoryId).toBe('builtin-software');
    expect(patch.selectedCategoryId).toBeUndefined();
    // 用户选过的分类保持自己的默认目录，不被后台探测算出的另一个分类覆盖。
    expect(patch.categoryDefaultDir).toBeUndefined();
  });
});
