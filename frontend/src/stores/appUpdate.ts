import { create } from 'zustand';
import { Events } from '@wailsio/runtime';
import { GetCachedAppUpdate } from '../../bindings/sheep-get/app';
import type { AppUpdateResult } from '../../bindings/sheep-get/internal/update/models';
import { Event } from '../lib/protocol.generated';
import { unwrapEventData } from '../lib/utils';
import { showToast } from '../components/ui/Toast';

interface AppUpdateState {
  availableUpdate: AppUpdateResult | null;
  hasNotified: boolean;
  setAvailableUpdate: (update: AppUpdateResult | null) => void;
  markNotified: () => void;
  fetchCachedUpdate: () => Promise<AppUpdateResult | null>;
}

export const useAppUpdateStore = create<AppUpdateState>((set, get) => ({
  availableUpdate: null,
  hasNotified: false,

  setAvailableUpdate: (update) => set({ availableUpdate: update }),
  markNotified: () => set({ hasNotified: true }),

  fetchCachedUpdate: async () => {
    try {
      const cached = await GetCachedAppUpdate();
      if (cached && cached.hasUpdate) {
        set({ availableUpdate: cached });
        if (!get().hasNotified) {
          get().markNotified();
          showToast(`发现应用新版本 v${cached.latestVersion}`, 'info', '检查更新');
        }
        return cached;
      }
    } catch {
      // 忽略缓存读取异常
    }
    return null;
  },
}));

/**
 * 监听后端推送的更新事件（Event.UpdateAvailable）。
 * 单次启动生命周期内只弹一次 Toast，避免打扰用户。
 */
export function initAppUpdateListener(): () => void {
  // 首次挂载时尝试从后端拉取预检缓存
  void useAppUpdateStore.getState().fetchCachedUpdate();

  const unlisten = Events.On(Event.UpdateAvailable, (ev: unknown) => {
    const rawData = (ev as { data?: unknown })?.data ?? ev;
    const res = unwrapEventData<AppUpdateResult>(rawData);
    if (res && res.hasUpdate) {
      useAppUpdateStore.getState().setAvailableUpdate(res);
      if (!useAppUpdateStore.getState().hasNotified) {
        useAppUpdateStore.getState().markNotified();
        showToast(`发现应用新版本 v${res.latestVersion}`, 'info', '检查更新');
      }
    }
  });

  return unlisten;
}
