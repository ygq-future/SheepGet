import type { SessionMetadata, TakeoverConfigSync } from './types';

/**
 * 接管清单的唯一来源是桌面端：内置分类的类型与用户在「接管」页加的后缀都由它下发。
 * 扩展这边不持有自己的预置清单——两份清单必然漂移，而漂移的表现是「设置页里明明接管了，
 * 浏览器却没接管」。没同步到桌面端配置时（桌面端没运行、首次安装）空清单意味着不接管，
 * 下载留在浏览器里正常完成，这比按一份过期清单抢下载要好。
 */
export const DEFAULT_TAKEOVER_CONFIG: TakeoverConfigSync = {
  version: 0,
  extensions: [],
  excludedSites: [],
  pauseShortcut: 'Delete',
  forceShortcut: 'Insert',
};

const STORAGE_KEYS = {
  TAKEOVER_CONFIG: 'sheepget_takeover_config',
  SESSION: 'sheepget_session',
} as const;

export async function getStoredTakeoverConfig(): Promise<TakeoverConfigSync> {
  try {
    const result = await chrome.storage.local.get(STORAGE_KEYS.TAKEOVER_CONFIG);
    if (result[STORAGE_KEYS.TAKEOVER_CONFIG]) {
      return result[STORAGE_KEYS.TAKEOVER_CONFIG] as TakeoverConfigSync;
    }
  } catch (err) {
    console.warn('[SheepGet] Failed to read takeover config from storage:', err);
  }
  return DEFAULT_TAKEOVER_CONFIG;
}

export async function setStoredTakeoverConfig(config: TakeoverConfigSync): Promise<void> {
  try {
    await chrome.storage.local.set({
      [STORAGE_KEYS.TAKEOVER_CONFIG]: config,
    });
  } catch (err) {
    console.warn('[SheepGet] Failed to write takeover config to storage:', err);
  }
}

export async function getStoredSession(): Promise<SessionMetadata | null> {
  try {
    const result = await chrome.storage.local.get(STORAGE_KEYS.SESSION);
    if (result[STORAGE_KEYS.SESSION]) {
      return result[STORAGE_KEYS.SESSION] as SessionMetadata;
    }
  } catch (err) {
    console.warn('[SheepGet] Failed to read session from storage:', err);
  }
  return null;
}

export async function setStoredSession(session: SessionMetadata | null): Promise<void> {
  try {
    if (session) {
      await chrome.storage.local.set({ [STORAGE_KEYS.SESSION]: session });
    } else {
      await chrome.storage.local.remove(STORAGE_KEYS.SESSION);
    }
  } catch (err) {
    console.warn('[SheepGet] Failed to write session to storage:', err);
  }
}
