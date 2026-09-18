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

export interface HandoverResponse {
  accepted: boolean;
  queueItemId?: string;
  reason?: string;
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
  tabId: number;
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

export type ExtensionMessage =
  KeyStateMessage | ResetKeysMessage | GetTabMediaMessage | HandoverMediaMessage;
