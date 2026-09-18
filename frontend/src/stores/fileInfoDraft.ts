import { create } from 'zustand';
import { CheckURLFilesExist } from '../../bindings/sheep-get/app';
import type * as configModels from '../../bindings/sheep-get/internal/config/models';
import type * as windowModels from '../../bindings/sheep-get/internal/window/models';
import {
  draftFromItem,
  emptyFileInfoDraft,
  reuseFields,
  type FileInfoDraft,
} from '../lib/fileInfoDraft';

/**
 * 文件信息窗口的表单草稿：每个队列项一份，切换项时整体换掉。
 *
 * 显示中的草稿与快照里的是同一个对象引用，所有改动都只经由 patch 一次写入，
 * 因此不存在「某个字段忘了同步」的可能。
 */
interface FileInfoDraftState {
  /** 当前显示的草稿。 */
  draft: FileInfoDraft;
  /** 各队列项的草稿快照，用户在队列里来回切换时据此恢复。 */
  drafts: Record<string, FileInfoDraft>;
  activeItemId: string | null;
  /** 在途指示属于发起它的那一项，切换项时清零，不随草稿恢复。 */
  probing: boolean;
  loading: boolean;

  open: (item: windowModels.FileInfoItem, settings: configModels.Settings | null) => Promise<void>;
  /**
   * owner 用于异步回填：窗口在等待期间切到了别的项时，这次结果直接丢弃，不写进新项的草稿。
   * 同步改动不传，直接写当前项。
   */
  patch: (partial: Partial<FileInfoDraft>, owner?: string | null) => void;
  /** 丢弃某一项的草稿；提交成功与取消时使用，显示中的内容不受影响。 */
  discard: (itemId: string) => void;
  setProbing: (probing: boolean) => void;
  setLoading: (loading: boolean) => void;
}

export const useFileInfoDraftStore = create<FileInfoDraftState>((set, get) => ({
  draft: emptyFileInfoDraft(),
  drafts: {},
  activeItemId: null,
  probing: false,
  loading: false,

  open: async (item, settings) => {
    const existing = get().drafts[item.id];
    const draft = existing ?? draftFromItem(item, settings);
    set({
      draft,
      drafts: { ...get().drafts, [item.id]: draft },
      activeItemId: item.id,
      probing: false,
      loading: false,
    });
    if (existing) return;

    // 可复用文件属于另一条独立规则（其他目录已有同一份成品），仍由后端查询后给出。
    if (item.duplicateTask && draft.directory && draft.filename) {
      const conf = await CheckURLFilesExist(item.url || '', draft.directory, draft.filename);
      get().patch(reuseFields(conf), item.id);
    }
  },

  patch: (partial, owner) => {
    const { activeItemId, draft, drafts } = get();
    if (owner !== undefined && owner !== activeItemId) return;
    const next = { ...draft, ...partial };
    if (!activeItemId) {
      set({ draft: next });
      return;
    }
    set({ draft: next, drafts: { ...drafts, [activeItemId]: next } });
  },

  discard: (itemId) => {
    const { drafts } = get();
    if (!(itemId in drafts)) return;
    const next = { ...drafts };
    delete next[itemId];
    set({ drafts: next });
  },

  setProbing: (probing) => set({ probing }),

  setLoading: (loading) => set({ loading }),
}));
