import type { KeyName } from './types';

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
