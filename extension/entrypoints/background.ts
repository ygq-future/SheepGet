import { DesktopClient } from '../lib/client';
import { isMediaResponse, parseContentDispositionFilename, type MediaResource } from '../lib/media';
import { decideTakeover } from '../lib/rules';
import { updateKeyMask } from '../lib/shortcuts';
import {
  DEFAULT_TAKEOVER_CONFIG,
  getStoredSession,
  getStoredTakeoverConfig,
  setStoredTakeoverConfig,
} from '../lib/storage';
import type {
  ExtensionMessage,
  HandoverRequest,
  SessionMetadata,
  TakeoverConfigSync,
} from '../lib/types';

let currentConfig: TakeoverConfigSync = DEFAULT_TAKEOVER_CONFIG;
let currentKeyMask = 0;
let desktopClient: DesktopClient | null = null;
let disconnectEvents: (() => void) | null = null;

// Track downloads being processed to prevent duplicate interception loops
const inFlightDownloads = new Set<number>();

// In-memory media resource pool indexed by tabId
const tabMediaPool = new Map<number, MediaResource[]>();

export default defineBackground(() => {
  console.log('[SheepGet] Background Service Worker starting...');

  // 1. Initialize configuration and session
  void init();

  // 2. Listen for messages from content scripts & popup (shortcuts, blur, media requests)
  chrome.runtime.onMessage.addListener((msg: ExtensionMessage, _sender, sendResponse) => {
    if (msg?.type === 'KEY_STATE_CHANGED') {
      currentKeyMask = updateKeyMask(currentKeyMask, msg.key, msg.isDown);
    } else if (msg?.type === 'RESET_KEYS') {
      currentKeyMask = 0;
    } else if (msg?.type === 'GET_TAB_MEDIA') {
      sendResponse(tabMediaPool.get(msg.tabId) || []);
      return true;
    } else if (msg?.type === 'HANDOVER_MEDIA') {
      void (async () => {
        const result = await handleMediaHandover(msg.resource as MediaResource);
        sendResponse(result);
      })();
      return true;
    }
  });

  // 3. Reset keys on tab switch or window blur (prevent stuck key states)
  chrome.tabs.onActivated.addListener(() => {
    currentKeyMask = 0;
  });
  if (chrome.windows?.onFocusChanged) {
    chrome.windows.onFocusChanged.addListener(() => {
      currentKeyMask = 0;
    });
  }

  // 4. Listen for storage changes (e.g. config or session updated from popup or sync)
  chrome.storage.onChanged.addListener((changes, areaName) => {
    if (areaName === 'local') {
      if (changes['sheepget_takeover_config']?.newValue) {
        currentConfig = changes['sheepget_takeover_config'].newValue as TakeoverConfigSync;
      }
      if (changes['sheepget_session']) {
        const session = changes['sheepget_session'].newValue as SessionMetadata | null;
        updateSession(session);
      }
    }
  });

  // 5. Intercept downloads via onDeterminingFilename / onCreated
  chrome.downloads.onCreated.addListener((item) => {
    void handleDownloadIntercept(item);
  });

  // 6. Network-level media sniffing (onHeadersReceived)
  chrome.webRequest.onHeadersReceived.addListener(
    (details) => {
      if (details.tabId < 0 || !details.url) return;
      let contentType = '';
      let contentLength = -1;
      let contentDisposition = '';

      if (details.responseHeaders) {
        for (const header of details.responseHeaders) {
          const name = header.name.toLowerCase();
          if (name === 'content-type') contentType = header.value || '';
          if (name === 'content-length') contentLength = parseInt(header.value || '-1', 10);
          if (name === 'content-disposition') contentDisposition = header.value || '';
        }
      }

      const detected = isMediaResponse(details.url, contentType);
      if (!detected.isMedia) return;

      let list = tabMediaPool.get(details.tabId);
      if (!list) {
        list = [];
        tabMediaPool.set(details.tabId, list);
      }

      // Avoid duplicate entries for identical URLs
      if (list.some((r) => r.url === details.url)) return;

      const filename =
        parseContentDispositionFilename(contentDisposition) ||
        details.url.split('?')[0]?.split('/').pop() ||
        (detected.isHls ? 'stream.m3u8' : 'media.mp4');

      const resource: MediaResource = {
        id: `${details.tabId}_${Date.now()}_${Math.random().toString(36).slice(2, 7)}`,
        url: details.url,
        tabId: details.tabId,
        filename,
        mimeType: detected.mime,
        totalBytes: contentLength > 0 ? contentLength : undefined,
        isHls: detected.isHls,
        foundAt: Date.now(),
      };
      list.push(resource);
      void updateBadge(details.tabId);
      return undefined;
    },
    { urls: ['<all_urls>'] },
    ['responseHeaders'],
  );

  // 7. Clean up media resources on tab close and navigation
  chrome.tabs.onRemoved.addListener((tabId) => {
    tabMediaPool.delete(tabId);
  });
  chrome.tabs.onUpdated.addListener((tabId, changeInfo) => {
    if (changeInfo.status === 'loading' && changeInfo.url) {
      tabMediaPool.delete(tabId);
      void updateBadge(tabId);
    }
  });
});

async function updateBadge(tabId: number) {
  const count = tabMediaPool.get(tabId)?.length || 0;
  try {
    if (count > 0) {
      await chrome.action.setBadgeText({ tabId, text: String(count) });
      await chrome.action.setBadgeBackgroundColor({ tabId, color: '#10b981' });
    } else {
      await chrome.action.setBadgeText({ tabId, text: '' });
    }
  } catch {
    // Tab may have closed
  }
}

async function handleMediaHandover(resource: MediaResource) {
  if (!desktopClient) {
    return { accepted: false, reason: '桌面端未连接' };
  }

  let cookiesStr = '';
  try {
    const cookies = await chrome.cookies.getAll({ url: resource.url });
    cookiesStr = cookies.map((c) => `${c.name}=${c.value}`).join('; ');
  } catch {
    // Ignore cookie error
  }

  const handoverReq: HandoverRequest = {
    sourceType: 'resource_list',
    url: resource.url,
    filenameSuggestion: resource.filename,
    totalBytes: resource.totalBytes,
    mimeType: resource.mimeType,
    pageContext: {
      pageUrl: resource.pageUrl || resource.url,
      pageTitle: resource.pageTitle,
    },
    credentials: {
      cookies: cookiesStr,
      headers: {
        'User-Agent': navigator.userAgent,
        ...(resource.pageUrl ? { Referer: resource.pageUrl } : {}),
      },
    },
    mediaMeta: resource.isHls ? { isHls: true } : undefined,
  };

  return await desktopClient.sendHandover(handoverReq, 3000);
}

async function init() {
  // Read local cache immediately to ensure millisecond responsiveness on wake-up
  currentConfig = await getStoredTakeoverConfig();

  const session = await getStoredSession();
  updateSession(session);
}

function updateSession(session: SessionMetadata | null) {
  if (disconnectEvents) {
    disconnectEvents();
    disconnectEvents = null;
  }

  if (!session) {
    desktopClient = null;
    return;
  }

  desktopClient = new DesktopClient(session);

  // Reconcile configuration version asynchronously
  void (async () => {
    if (!desktopClient) return;
    const remoteConfig = await desktopClient.fetchTakeoverConfig(currentConfig.version);
    if (remoteConfig && remoteConfig.version !== currentConfig.version) {
      currentConfig = remoteConfig;
      await setStoredTakeoverConfig(remoteConfig);
    }
  })();

  // Subscribe to live WebSocket broadcasts from desktop
  disconnectEvents = desktopClient.connectEvents((newConfig) => {
    currentConfig = newConfig;
    void setStoredTakeoverConfig(newConfig);
  });
}

async function handleDownloadIntercept(item: chrome.downloads.DownloadItem) {
  if (!item || !item.id || inFlightDownloads.has(item.id)) {
    return;
  }

  // Only intercept HTTP/HTTPS downloads
  const url = item.finalUrl || item.url;
  if (!url || (!url.startsWith('http://') && !url.startsWith('https://'))) {
    return;
  }

  // Check takeover rules
  const decision = decideTakeover(url, item.filename, item.referrer, currentConfig, currentKeyMask);

  if (!decision.takeover) {
    return;
  }

  inFlightDownloads.add(item.id);

  // Synchronous blocking suspension: pause download immediately in place
  try {
    await chrome.downloads.pause(item.id);
  } catch (err) {
    console.warn('[SheepGet] Failed to pause download:', item.id, err);
    inFlightDownloads.delete(item.id);
    return;
  }

  // If no client is available, fallback and resume immediately
  if (!desktopClient) {
    console.log('[SheepGet] Desktop loopback client not available, resuming download');
    await resumeDownload(item.id);
    return;
  }

  // Gather cookies if available
  let cookiesStr = '';
  try {
    const cookies = await chrome.cookies.getAll({ url });
    cookiesStr = cookies.map((c) => `${c.name}=${c.value}`).join('; ');
  } catch {
    // Cookie access may fail or be restricted
  }

  const handoverReq: HandoverRequest = {
    sourceType: 'browser_takeover',
    url,
    filenameSuggestion: item.filename,
    totalBytes: item.totalBytes && item.totalBytes > 0 ? item.totalBytes : undefined,
    mimeType: item.mime,
    pageContext: {
      pageUrl: item.referrer || url,
      referrer: item.referrer,
    },
    credentials: {
      cookies: cookiesStr,
      headers: {
        'User-Agent': navigator.userAgent,
        ...(item.referrer ? { Referer: item.referrer } : {}),
      },
    },
  };

  // Handover to desktop with 2.5 second timeout
  const resp = await desktopClient.sendHandover(handoverReq, 2500);

  if (resp.accepted) {
    // Desktop accepted: cancel native download and clean up traces
    try {
      await chrome.downloads.cancel(item.id);
      await chrome.downloads.erase({ id: item.id });
    } catch {
      // Ignore cleanup error
    }
  } else {
    // Desktop rejected or timed out: resume native browser download smoothly
    console.warn('[SheepGet] Handover rejected or timed out, resuming:', resp.reason);
    await resumeDownload(item.id);
  }

  inFlightDownloads.delete(item.id);
}

async function resumeDownload(downloadId: number) {
  try {
    await chrome.downloads.resume(downloadId);
  } catch (err) {
    console.warn('[SheepGet] Failed to resume download:', downloadId, err);
  }
}
