import type { MediaResource } from './media';

export interface PageContext {
  pageUrl: string;
  referrer?: string;
  pageTitle?: string;
}

export interface CredentialsPayload {
  cookies?: string;
  headers?: Record<string, string>;
}

export interface HandoverRequest {
  sourceType: 'browser_takeover' | 'media_bar' | 'resource_list';
  url: string;
  filenameSuggestion?: string;
  totalBytes?: number;
  mimeType?: string;
  pageContext: PageContext;
  credentials?: CredentialsPayload;
  mediaMeta?: unknown;
}

/**
 * 交接失败的种类。桌面端重启会换 loopback 端口，因此「失败」并不都等价：
 * 连不上和令牌失效可以重新发现会话后重试，超时和明确拒绝则不能（见 lib/handover.ts）。
 */
export type HandoverFailureKind =
  /** 连接没能建立（连接被拒/网络错误）：请求一定没到达桌面端。 */
  | 'not_delivered'
  /** 到达了但令牌不被接受（401/403）：桌面端换了会话。 */
  | 'unauthorized'
  /** 超时：无法确认桌面端是否已受理。 */
  | 'no_response'
  /** 桌面端明确拒绝（如重复链接策略）。 */
  | 'rejected';

export interface HandoverResponse {
  accepted: boolean;
  queueItemId?: string;
  reason?: string;
  /** 仅当 accepted 为 false 时有意义，决定「重新连接后重试」还是「还给浏览器」。 */
  failure?: HandoverFailureKind;
}

/** 扩展对「与桌面端链路是否真的可用」的观测结果。界面读它，不再读「是否存过会话」。 */
export interface DesktopStatus {
  online: boolean;
  port: number | null;
  lastVerifiedAt: number | null;
  /** 离线原因，用于把「桌面端没启动」和「令牌失效」区分开。 */
  reason: string | null;
}

export interface TakeoverConfigSync {
  version: number;
  extensions: string[];
  excludedSites: string[];
  pauseShortcut: string;
  forceShortcut: string;
}

export interface SessionMetadata {
  port: number;
  sessionToken: string;
  pid?: number;
  startedAt?: number;
}

export type KeyName = 'Shift' | 'Control' | 'Alt' | 'Insert' | 'Delete';

export interface KeyStateMessage {
  type: 'KEY_STATE_CHANGED';
  key: KeyName;
  isDown: boolean;
}

export interface ResetKeysMessage {
  type: 'RESET_KEYS';
}

export interface GetTabMediaMessage {
  type: 'GET_TAB_MEDIA';
  // content script 不知道自己所在标签页的编号，只有 popup 会带上
  tabId?: number;
}

export interface TabMediaUpdatedMessage {
  type: 'TAB_MEDIA_UPDATED';
  resources: MediaResource[];
}

export interface HandoverMediaMessage {
  type: 'HANDOVER_MEDIA';
  resource: {
    url: string;
    filename: string;
    totalBytes?: number;
    mimeType: string;
    isHls: boolean;
    pageUrl?: string;
    pageTitle?: string;
  };
}

/** 读当前链路状态（不发网络请求，读的是后台维护的观测结果）。 */
export interface GetStatusMessage {
  type: 'GET_STATUS';
}

/** 强制重新验证链路：ping 现会话，失效就问原生消息宿主拿最新端口与令牌。 */
export interface ReconnectMessage {
  type: 'RECONNECT';
}

export type ExtensionMessage =
  | KeyStateMessage
  | ResetKeysMessage
  | GetTabMediaMessage
  | TabMediaUpdatedMessage
  | HandoverMediaMessage
  | GetStatusMessage
  | ReconnectMessage;
