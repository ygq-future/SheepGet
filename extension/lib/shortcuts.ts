import type { KeyName, KeyStateReport } from './types';

export const KEY_MASKS: Record<KeyName, number> = {
  Shift: 1 << 0,
  Control: 1 << 1,
  Alt: 1 << 2,
  Insert: 1 << 3,
  Delete: 1 << 4,
};

export function normalizeKeyName(name: string): KeyName | null {
  const lower = name.trim().toLowerCase();
  switch (lower) {
    case 'ctrl':
    case 'control':
      return 'Control';
    case 'shift':
      return 'Shift';
    case 'alt':
      return 'Alt';
    case 'ins':
    case 'insert':
      return 'Insert';
    case 'del':
    case 'delete':
      return 'Delete';
    default:
      return null;
  }
}

export function updateKeyMask(currentMask: number, keyStr: string, isDown: boolean): number {
  const norm = normalizeKeyName(keyStr);
  if (!norm) return currentMask;
  const bit = KEY_MASKS[norm];
  if (isDown) {
    return currentMask | bit;
  }
  return currentMask & ~bit;
}

export function isKeyPressed(mask: number, targetShortcutName: string): boolean {
  if (!targetShortcutName) return false;
  const norm = normalizeKeyName(targetShortcutName);
  if (!norm) return false;
  const bit = KEY_MASKS[norm];
  return (mask & bit) !== 0;
}

/** 快捷键在用户松开或失焦后的宽限保留时间（毫秒），平滑网络延迟与切标签页导致的按键状态丢失 */
export const SHORTCUT_GRACE_PERIOD_MS = 3000;

/**
 * 页面对「现在按住了哪些键」的回答：只在真的按住键时应答。
 *
 * 没按住必须表现为不应答，而不是回一个 0：后台据此区分「用户没按住」与「这个页面没有
 * 内容脚本（特权页）/ 忙得没来得及回」，回 0 会把后两者伪装成前者的答案。
 */
export function keyStateReply(mask: number): KeyStateReport | undefined {
  return mask === 0 ? undefined : { keyMask: mask };
}

export interface ShortcutClickIntent {
  keyMask: number;
  url?: string;
  timestamp: number;
}

/**
 * 比对点击意图记录的目标链接与下载项真实 URL。
 * 协议与域名相同时，忽略 hash 与末尾斜杠；若点击意图未记录具体 URL（如脚本触发下载），直接视为匹配。
 */
export function urlsMatch(clickUrl: string | undefined, downloadUrl: string | undefined): boolean {
  if (!clickUrl || !downloadUrl) return true;
  if (clickUrl === downloadUrl) return true;
  try {
    const c = new URL(clickUrl);
    const d = new URL(downloadUrl);
    if (c.origin === d.origin && c.pathname.replace(/\/$/, '') === d.pathname.replace(/\/$/, '')) {
      return true;
    }
  } catch {
    // 非标准 URL 容错
  }
  return false;
}

/**
 * 综合即时按键掩码、按键释放宽限掩码与近期点击意图，计算出对当前下载有效的按键掩码。
 */
export function resolveEffectiveKeyMask(
  currentMask: number,
  recentReleaseMask: number,
  recentReleaseTime: number,
  recentClicks: readonly ShortcutClickIntent[],
  downloadUrl?: string,
  now = Date.now(),
): number {
  let effective = currentMask;

  // 1. 最近按键释放宽限期（若在宽限期内，保留被释放的按键位）
  if (recentReleaseMask > 0 && now - recentReleaseTime <= SHORTCUT_GRACE_PERIOD_MS) {
    effective |= recentReleaseMask;
  }

  // 2. 检查是否有最近的快捷键点击意图（3 秒内）
  for (const click of recentClicks) {
    if (now - click.timestamp <= SHORTCUT_GRACE_PERIOD_MS) {
      if (urlsMatch(click.url, downloadUrl)) {
        effective |= click.keyMask;
      }
    }
  }

  return effective;
}
