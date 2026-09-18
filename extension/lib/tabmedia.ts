import type { MediaResource } from './media';

// 资源池按标签页归属。content script 发消息时不知道自己所在标签页的编号，
// 因此以发送者标签页兜底；popup 会显式带上 tabId，优先采用它。
export function resolveTabId(messageTabId?: number, senderTabId?: number): number | null {
  if (typeof messageTabId === 'number') return messageTabId;
  if (typeof senderTabId === 'number') return senderTabId;
  return null;
}

// 同一 URL 只入池一次，避免预检、分片或重复请求把资源列表刷屏。
export function addResourceOnce(list: MediaResource[], resource: MediaResource): boolean {
  if (list.some((r) => r.url === resource.url)) return false;
  list.push(resource);
  return true;
}
