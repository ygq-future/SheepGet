import {
  DEFAULT_TAKEOVER_CONFIG,
  getStoredTakeoverConfig,
  setStoredTakeoverConfig,
} from './storage';
import type { TakeoverConfigSync } from './types';

/**
 * 接管规则的「就绪」接缝：规则没读回来之前，判定方拿不到规则。
 *
 * Service Worker 每次被唤醒都重新求值脚本，模块级变量回到初值，而下载事件随时可能
 * 就是这次唤醒的原因。规则若在读回来之前被拿去判定，后缀清单是空的，判定必然是
 * 「不接管」——下载留在浏览器里完成，界面上没有任何失败痕迹。因此判定方一律先
 * await readyConfig()，拿到的是已恢复的规则，而不是模块初值。
 *
 * 只有持久化过的状态才有「就绪」可言，所以会话与媒体池不在这里：会话在用到它的地方
 * （ensureDesktop → reverifyLink）自己等存储，媒体池没有持久化来源、无从加载。
 */

let current: TakeoverConfigSync = DEFAULT_TAKEOVER_CONFIG;
let hydrated = false;
let hydration: Promise<TakeoverConfigSync> | null = null;

/**
 * 就绪后给出当前规则；整个 Service Worker 生命周期只读一次存储。
 *
 * 必定有结论：存储读不回来时 `getStoredTakeoverConfig` 回退到空清单（见 lib/storage.ts），
 * 扩展的失败模式是「不接管」而不是判定被挂住。
 */
export function readyConfig(): Promise<TakeoverConfigSync> {
  if (hydrated) return Promise.resolve(current);
  hydration ??= hydrate();
  return hydration;
}

async function hydrate(): Promise<TakeoverConfigSync> {
  const stored = await getStoredTakeoverConfig();
  // 这次读取开始之后可能已有更新的规则落进来（桌面端广播）：那时以它为准。
  if (!hydrated) {
    current = stored;
    hydrated = true;
  }
  return current;
}

/** 采用桌面端下发的规则并落盘，下次冷启动直接可用。 */
export async function applyConfig(config: TakeoverConfigSync): Promise<void> {
  current = config;
  hydrated = true;
  await setStoredTakeoverConfig(config);
}
