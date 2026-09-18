import { DesktopClient } from '../lib/client';
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

export default defineBackground(() => {
  console.log('[SheepGet] Background Service Worker starting...');

  // 1. Initialize configuration and session
  void init();

  // 2. Listen for messages from content scripts (shortcuts & blur)
  chrome.runtime.onMessage.addListener((msg: ExtensionMessage, _sender, _sendResponse) => {
    if (msg && msg.type === 'KEY_STATE_CHANGED') {
      currentKeyMask = updateKeyMask(currentKeyMask, msg.key, msg.isDown);
    } else if (msg && msg.type === 'RESET_KEYS') {
      currentKeyMask = 0;
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
});

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
