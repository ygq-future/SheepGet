import type { SessionMetadata, TakeoverConfigSync } from './types';

export const DEFAULT_TAKEOVER_CONFIG: TakeoverConfigSync = {
  version: 0,
  extensions: [
    'zip',
    'rar',
    '7z',
    'tar',
    'gz',
    'bz2',
    'iso',
    'exe',
    'msi',
    'dmg',
    'pkg',
    'apk',
    'mp4',
    'mkv',
    'flv',
  ],
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
