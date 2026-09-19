import { DesktopClient, type DesktopEventLink } from '../lib/client';
import { ResponseFilenameCache } from '../lib/filenames';
import { LINK_VERIFY_TTL_MS, planHandoverFailure, shouldReverifyLink } from '../lib/handover';
import { isMediaResponse, parseContentDispositionFilename, type MediaResource } from '../lib/media';
import { decideTakeover, type TakeoverDecision } from '../lib/rules';
import { updateKeyMask } from '../lib/shortcuts';
import { addResourceOnce, resolveTabId } from '../lib/tabmedia';
import {
  DEFAULT_TAKEOVER_CONFIG,
  getStoredSession,
  getStoredTakeoverConfig,
  setStoredSession,
  setStoredTakeoverConfig,
} from '../lib/storage';
import type {
  DesktopStatus,
  ExtensionMessage,
  HandoverRequest,
  HandoverResponse,
  SessionMetadata,
  TakeoverConfigSync,
} from '../lib/types';

let currentConfig: TakeoverConfigSync = DEFAULT_TAKEOVER_CONFIG;
let currentKeyMask = 0;

// 与桌面端的链路。桌面端每次启动都换 loopback 端口，所以「连过」不等于「还连着」：
// linkOnline 只在真实 ping 成功或事件长连接打开时置位，在 ping 失败、长连接关闭、
// 交接失败时置否。界面状态与 Chrome 下载界面都读它，不再读「是否存过会话」。
let desktopClient: DesktopClient | null = null;
let eventLink: DesktopEventLink | null = null;
let linkOnline = false;
let linkSession: SessionMetadata | null = null;
let linkLastVerifiedAt: number | null = null;
let linkOfflineReason: string | null = null;

const PING_TIMEOUT_MS = 1200;
const HANDOVER_TIMEOUT_MS = 2500;

// 发现会话（起一次原生消息宿主）的等待上限。宿主读到一条消息、回一条、自己就退出，
// 实测一次十来毫秒；超过这个数说明它卡住了，按「没发现」处理。
const NATIVE_DISCOVER_TIMEOUT_MS = 2000;

// 保活定时器。0.5 分钟是 Chrome 允许的最小周期（更小会被夹到 0.5 并告警）。
// 它的作用不是「心跳好看」，而是给链路状态一个陈旧上界：无论 service worker 因为什么
// 原因握着一个失效会话不放（长连接被静默掐断、SW 被挂起后重建、桌面端重启），
// 最多 30 秒就会重算一次，界面不会一直显示「已就绪」。
const HEALTH_ALARM = 'sheepget_link_health';
const HEALTH_ALARM_PERIOD_MINUTES = 0.5;

// Track downloads being processed to prevent duplicate interception loops
const inFlightDownloads = new Set<number>();

// In-memory media resource pool indexed by tabId
const tabMediaPool = new Map<number, MediaResource[]>();

// 响应头里的真实文件名，供 onCreated 的接管判定与交接使用（见 lib/filenames.ts）。
const responseFilenames = new ResponseFilenameCache();

// Chrome 自身下载 UI 当前是否已被我们压掉；null 表示还没同步过，首次必须真的写一次。
let chromeDownloadUiHidden: boolean | null = null;

export default defineBackground(() => {
  console.log('[SheepGet] Background Service Worker starting...');

  // 1. Initialize configuration, the link to the desktop, and the keepalive alarm
  void init();

  // 2. Listen for messages from content scripts & popup (shortcuts, blur, media requests)
  chrome.runtime.onMessage.addListener((msg: ExtensionMessage, sender, sendResponse) => {
    if (msg?.type === 'KEY_STATE_CHANGED') {
      currentKeyMask = updateKeyMask(currentKeyMask, msg.key, msg.isDown);
    } else if (msg?.type === 'RESET_KEYS') {
      currentKeyMask = 0;
    } else if (msg?.type === 'GET_TAB_MEDIA') {
      // content script 不知道自己所在标签页的编号，退回发送者标签页；
      // popup 会显式带上 tabId，优先采用它。
      const tabId = resolveTabId(msg.tabId, sender.tab?.id);
      sendResponse(tabId === null ? [] : tabMediaPool.get(tabId) || []);
      return true;
    } else if (msg?.type === 'HANDOVER_MEDIA') {
      void (async () => {
        const result = await handleMediaHandover(msg.resource as MediaResource);
        sendResponse(result);
      })();
      return true;
    } else if (msg?.type === 'GET_STATUS') {
      sendResponse(desktopStatus());
    } else if (msg?.type === 'RECONNECT') {
      void (async () => {
        sendResponse(await reverifyLink('popup'));
      })();
      return true;
    }
  });

  // 3. Keepalive probe. Fires on its own even when the service worker has been suspended,
  //    which is exactly the state in which a stale session would otherwise go unnoticed.
  chrome.alarms.onAlarm.addListener((alarm) => {
    if (alarm.name !== HEALTH_ALARM) return;
    void reverifyLink('health-alarm');
  });

  // 4. Reset keys on tab switch or window blur (prevent stuck key states)
  chrome.tabs.onActivated.addListener(() => {
    currentKeyMask = 0;
  });
  if (chrome.windows?.onFocusChanged) {
    chrome.windows.onFocusChanged.addListener(() => {
      currentKeyMask = 0;
    });
  }

  // 5. Listen for storage changes (e.g. config or session updated from popup or sync)
  chrome.storage.onChanged.addListener((changes, areaName) => {
    if (areaName === 'local') {
      if (changes['sheepget_takeover_config']?.newValue) {
        currentConfig = changes['sheepget_takeover_config'].newValue as TakeoverConfigSync;
      }
    }
  });

  // 6. Intercept downloads via onCreated
  chrome.downloads.onCreated.addListener((item) => {
    void handleDownloadIntercept(item);
  });

  // 7. Network-level media sniffing (onHeadersReceived)
  chrome.webRequest.onHeadersReceived.addListener(
    (details) => {
      if (!details.url) return;
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

      // 下载的真实文件名往往只出现在响应头里（GitHub 资产链接的路径末段是一个 GUID），
      // 而 Chrome 在 onCreated 给出的只是从 URL 推出来的名字。先记下来，供接管判定与交接使用。
      const headerFilename = parseContentDispositionFilename(contentDisposition);
      if (headerFilename) {
        responseFilenames.remember(details.url, headerFilename);
      }

      if (details.tabId < 0) return;

      const detected = isMediaResponse(details.url, contentType);
      if (!detected.isMedia) return;

      let list = tabMediaPool.get(details.tabId);
      if (!list) {
        list = [];
        tabMediaPool.set(details.tabId, list);
      }

      const filename =
        headerFilename ||
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
      if (!addResourceOnce(list, resource)) return undefined;
      void updateBadge(details.tabId);
      // 播放器可能晚于资源请求出现，也可能页面先就绪再触发媒体请求，
      // 因此每次新增资源都推给该标签页，让悬浮条无需重新加载页面即可关联。
      publishTabResources(details.tabId);
      return undefined;
    },
    { urls: ['<all_urls>'] },
    ['responseHeaders'],
  );

  // 8. Clean up media resources on tab close and navigation
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

// publishTabResources 把某个标签页的最新嗅探结果推给页面内的悬浮条。
function publishTabResources(tabId: number) {
  const resources = tabMediaPool.get(tabId) || [];
  // 特权页与尚未注入 content script 的页面没有接收方，投递失败属正常情况
  chrome.tabs.sendMessage(tabId, { type: 'TAB_MEDIA_UPDATED', resources }, () => {
    void chrome.runtime.lastError;
  });
}

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

async function handleMediaHandover(resource: MediaResource): Promise<HandoverResponse> {
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

  return await handOverWithRetry(handoverReq, 3000, 'media-handover');
}

async function init() {
  // Read local cache immediately to ensure millisecond responsiveness on wake-up
  currentConfig = await getStoredTakeoverConfig();

  // The keepalive alarm is what keeps the link state honest across service worker
  // suspensions. Creating it on every start is fine: same name replaces the old one.
  await chrome.alarms
    .create(HEALTH_ALARM, { periodInMinutes: HEALTH_ALARM_PERIOD_MINUTES })
    .catch((err: unknown) => {
      console.warn('[SheepGet] Failed to create the link health alarm:', err);
    });

  await reverifyLink('startup');
}

/** 链路状态快照，供 popup 显示真实连接情况。 */
function desktopStatus(): DesktopStatus {
  return {
    online: linkOnline,
    port: linkSession?.port ?? null,
    lastVerifiedAt: linkLastVerifiedAt,
    reason: linkOfflineReason,
  };
}

/**
 * 确认当前链路可用，必要时重新发现桌面端。
 *
 * 顺序刻意如此：先用手上已有的会话确认（桌面端没重启时这步就结束，不会反复重建长连接），
 * 失败再问原生消息宿主拿最新端口与令牌——那是唯一能把「扩展不知道桌面端换了端口」
 * 变成「扩展知道」的途径。
 */
async function reverifyLink(trigger: string): Promise<DesktopStatus> {
  if (desktopClient && (await desktopClient.ping(PING_TIMEOUT_MS))) {
    if (linkSession && !eventLink?.isOpen()) {
      // HTTP 通但事件长连接不在了：重新采用同一会话，把配置广播这条通道接回来，
      // 否则接管规则不会再随桌面端设置变化。
      adoptLink(linkSession, desktopClient);
      console.info(`[SheepGet] Desktop event link re-established on port ${linkSession.port}`);
    } else {
      noteLinkVerified();
    }
    return desktopStatus();
  }

  // 没有 client 时也先试一次存下来的会话：原生消息宿主若没注册成功，这条路径还能救回来。
  if (!desktopClient) {
    const stored = await getStoredSession();
    if (stored) {
      const client = new DesktopClient(stored);
      if (await client.ping(PING_TIMEOUT_MS)) {
        adoptLink(stored, client);
        console.info(`[SheepGet] Desktop link re-established on stored port ${stored.port}`);
        return desktopStatus();
      }
    }
  }

  const discovered = await discoverSessionViaNativeHost();
  if (!discovered) {
    markLinkOffline('未发现运行中的桌面端');
    return desktopStatus();
  }

  const client = new DesktopClient(discovered);
  if (!(await client.ping(PING_TIMEOUT_MS))) {
    markLinkOffline('原生消息宿主返回的会话无法连通');
    return desktopStatus();
  }

  adoptLink(discovered, client);
  console.info(`[SheepGet] Desktop link re-established on port ${discovered.port} (${trigger})`);
  return desktopStatus();
}

/**
 * 采用一个已被证实可用的会话：替换长连接、刷新存活时刻、对齐配置、重算 Chrome 下载界面。
 */
function adoptLink(session: SessionMetadata, client: DesktopClient) {
  // 先摘掉旧的 client 再关它的长连接：否则旧连接的 onclose 会把自己当成「桌面端掉线」上报。
  teardownEvents();
  desktopClient = client;
  linkSession = session;
  linkOnline = true;
  linkLastVerifiedAt = Date.now();
  linkOfflineReason = null;

  eventLink = client.connectEvents(
    (cfg) => {
      currentConfig = cfg;
      void setStoredTakeoverConfig(cfg);
    },
    (open) => {
      // 主动断开时 desktopClient 已换人或已清空，只有当前连接才代表链路状态。
      if (!open && desktopClient === client) {
        markLinkOffline('桌面端事件连接已断开');
      }
    },
  );

  void reconcileTakeoverConfig(client);
  void syncChromeDownloadUi();
}

function teardownEvents() {
  eventLink?.close();
  eventLink = null;
  desktopClient = null;
}

/** 记录一次「链路被证实可用」。一次成功的 ping 或一次成功的交接都算。 */
function noteLinkVerified() {
  linkOnline = true;
  linkLastVerifiedAt = Date.now();
  linkOfflineReason = null;
  void syncChromeDownloadUi();
}

/**
 * 标记链路不可用。不改动 desktopClient：下一次 reverifyLink 会重新验证并替换它。
 * Chrome 下载界面在这里被恢复——这是「桌面端不在时，用户至少还能看见浏览器在下载」的保证。
 */
function markLinkOffline(reason: string) {
  if (!linkOnline && linkOfflineReason === reason) return;
  console.warn(`[SheepGet] Desktop link offline: ${reason}`);
  linkOnline = false;
  linkOfflineReason = reason;
  void syncChromeDownloadUi();
}

async function reconcileTakeoverConfig(client: DesktopClient) {
  const remoteConfig = await client.fetchTakeoverConfig(currentConfig.version);
  if (remoteConfig && remoteConfig.version !== currentConfig.version) {
    currentConfig = remoteConfig;
    await setStoredTakeoverConfig(remoteConfig);
  }
}

/** 问原生消息宿主当前端口与令牌；桌面端没在跑或宿主没注册都返回 null。 */
async function discoverSessionViaNativeHost(): Promise<SessionMetadata | null> {
  const { promise, resolve } = Promise.withResolvers<SessionMetadata | null>();
  let settled = false;
  // Chrome 的 sendNativeMessage 没有超时：宿主进程要是卡住（杀软拦下、磁盘无响应、进程没退干净），
  // 回调就永远不会来。发现会话因此必须自己兜底——判成「没发现」而不是把调用方吊在这里，
  // 否则面板会一直停在「检查中」，交接也会一直等一个不会到来的答复。
  const timer = setTimeout(() => finish(null), NATIVE_DISCOVER_TIMEOUT_MS);
  function finish(value: SessionMetadata | null) {
    if (settled) return;
    settled = true;
    clearTimeout(timer);
    resolve(value);
  }
  try {
    chrome.runtime.sendNativeMessage(
      'com.sheepget.host',
      { action: 'query' },
      (response?: { status?: string; port?: number; sessionToken?: string }) => {
        if (chrome.runtime.lastError) {
          finish(null);
          return;
        }
        if (response?.status === 'ok' && response.port && response.sessionToken) {
          const session: SessionMetadata = {
            port: response.port,
            sessionToken: response.sessionToken,
          };
          void setStoredSession(session);
          finish(session);
          return;
        }
        finish(null);
      },
    );
  } catch {
    finish(null);
  }
  return promise;
}

/**
 * 在需要时确认链路，返回可用的 client。连接刚被证实过就不再重复 ping，
 * 否则每次下载都要多一个来回。
 */
async function ensureDesktop(): Promise<DesktopClient | null> {
  const liveness = { online: linkOnline, lastVerifiedAt: linkLastVerifiedAt };
  if (shouldReverifyLink(liveness, Date.now(), LINK_VERIFY_TTL_MS)) {
    await reverifyLink('pre-handover');
  }
  return linkOnline ? desktopClient : null;
}

/**
 * 交接一次请求，失败时按种类决定是否重新发现会话再试一次。
 * 下载接管与媒体移交共用，避免只有一条路径能自愈。
 */
async function handOverWithRetry(
  req: HandoverRequest,
  timeoutMs: number,
  context: string,
): Promise<HandoverResponse> {
  const client = await ensureDesktop();
  if (!client) {
    return { accepted: false, failure: 'not_delivered', reason: '桌面端未连接' };
  }

  let resp = await client.sendHandover(req, timeoutMs);
  if (resp.accepted) {
    noteLinkVerified();
    return resp;
  }

  const failure = resp.failure ?? 'rejected';
  if (planHandoverFailure(failure) !== 'retry_after_rediscover') {
    return resp;
  }

  // 连接没能建立或令牌失效：桌面端多半刚重启过，重新发现会话再试一次即可自愈。
  console.warn(`[SheepGet] ${context}: handover failed, reconnecting and retrying once`, {
    failure,
    reason: resp.reason,
  });
  markLinkOffline(`交接失败（${failure}）`);
  const status = await reverifyLink(`${context}-retry`);
  if (!status.online || !desktopClient) {
    return resp;
  }

  resp = await desktopClient.sendHandover(req, timeoutMs);
  if (resp.accepted) {
    noteLinkVerified();
  }
  return resp;
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

  // 判定用的文件名优先取响应头里的真名：Chrome 在 onCreated 给出的名字是从 URL 推出来的，
  // 路径里没有后缀时（GitHub 资产链接的末段是 GUID）按它判定必然漏接。
  const responseFilename = responseFilenames.lookup(item.finalUrl, item.url);
  const filename = responseFilename || item.filename;

  // Check takeover rules
  const decision = decideTakeover(url, filename, item.referrer, currentConfig, currentKeyMask);
  logDownloadDecision(item.id, url, filename, responseFilename !== undefined, decision);

  if (!decision.takeover) {
    return;
  }

  inFlightDownloads.add(item.id);

  // 原位挂起能让交接失败时把原连接还给浏览器（ADR-0005 选项 A），但它只是手段不是前提：
  // 挂不上也要继续交接，否则这次下载会被静默放回浏览器，用户既看不到窗口也看不到原因。
  const paused = await pauseDownload(item.id);

  try {
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
      // 交给桌面端的也是真名：桌面端据此命中分类，不能让它再按 URL 猜一次。
      filenameSuggestion: filename,
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

    const resp = await handOverWithRetry(handoverReq, HANDOVER_TIMEOUT_MS, 'download-takeover');

    if (resp.accepted) {
      await cancelDownload(item.id);
    } else {
      // 桌面端确实接不了：把下载还给浏览器。链路若已断，Chrome 下载界面在
      // handOverWithRetry 标记离线时就恢复了，这里不必再动它。
      console.warn('[SheepGet] Handing the download back to the browser:', {
        failure: resp.failure ?? 'unknown',
        reason: resp.reason,
      });
      await resumeDownload(item.id, paused);
    }
  } catch (err) {
    console.warn('[SheepGet] Unexpected interception failure, resuming download:', item.id, err);
    await resumeDownload(item.id, paused);
  } finally {
    inFlightDownloads.delete(item.id);
  }
}

/**
 * 接管判定留一条可读日志：接管与否、用的是哪个文件名、规则版本、桌面端是否在线。
 * 判定以外的分支（挂起失败、交接被拒、超时）各有自己的 warn，组合起来能定位一次失败的下载。
 */
function logDownloadDecision(
  downloadId: number,
  url: string,
  filename: string,
  fromResponseHeader: boolean,
  decision: TakeoverDecision,
): void {
  console.info(
    '[SheepGet] download decision',
    JSON.stringify({
      downloadId,
      // 只记录来源与路径：链接里的令牌与一次性参数属于敏感请求上下文，不写进日志。
      url: stripQuery(url),
      filename,
      filenameSource: fromResponseHeader ? 'response-header' : 'download-item',
      takeover: decision.takeover,
      reason: decision.reason,
      configVersion: currentConfig.version,
      excludedSites: currentConfig.excludedSites,
      linkOnline,
      linkCheckedAt: linkLastVerifiedAt,
      clientConnected: desktopClient !== null,
    }),
  );
}

function stripQuery(rawUrl: string): string {
  try {
    const parsed = new URL(rawUrl);
    return `${parsed.origin}${parsed.pathname}`;
  } catch {
    return rawUrl;
  }
}

/** 挂起下载并返回是否真的挂上了；调用方据此决定失败时是否还有东西需要还给浏览器。 */
async function pauseDownload(downloadId: number): Promise<boolean> {
  try {
    await chrome.downloads.pause(downloadId);
    return true;
  } catch (err) {
    console.warn('[SheepGet] Failed to pause download, continuing with handover:', downloadId, err);
    return false;
  }
}

/** 交接被接受：取消浏览器这一份，并擦掉下载条上的痕迹。 */
async function cancelDownload(downloadId: number) {
  try {
    await chrome.downloads.cancel(downloadId);
    await chrome.downloads.erase({ id: downloadId });
  } catch (err) {
    console.warn('[SheepGet] Failed to cancel the browser download:', downloadId, err);
  }
}

/** 把下载还给浏览器。没挂起过的下载不能再调 resume，否则只会多抛一次错、掩盖真实原因。 */
async function resumeDownload(downloadId: number, paused: boolean) {
  if (!paused) {
    return;
  }
  try {
    await chrome.downloads.resume(downloadId);
  } catch (err) {
    console.warn(
      '[SheepGet] Failed to resume download, it stays paused in the browser:',
      downloadId,
      err,
    );
  }
}

/**
 * 按「链路是否真的可用」切换 Chrome 自己的下载界面。
 *
 * Chrome 在 onCreated 之后就建好了下载条目，「文件飞向下载按钮」的动画在那时已经触发，
 * 事后再取消只能让残留变短，消不掉。downloads.ui 权限提供的 setUiOptions 是唯一能在
 * 下载开始前就关掉这套界面的接口，因此按链路状态提前切换：在线时关掉，断开或接管不可用时
 * 立刻恢复，浏览器接管下载时的正常反馈不受影响。
 *
 * 这里判据是 linkOnline 而不是「有没有 client」：桌面端一重启，旧 client 对象还在但已经
 * 连不上，用它当判据会让界面一直压着、失败也无声。每次 service worker 启动、每次保活
 * 探测、每次链路状态变化都会重算，不存在「关着没人管」的状态。
 */
async function syncChromeDownloadUi() {
  if (typeof chrome.downloads.setUiOptions !== 'function') {
    return;
  }
  const hide = linkOnline;
  if (hide === chromeDownloadUiHidden) {
    return;
  }
  try {
    await chrome.downloads.setUiOptions({ enabled: !hide });
    chromeDownloadUiHidden = hide;
    console.info(`[SheepGet] Chrome download UI ${hide ? 'hidden' : 'restored'}`);
  } catch (err) {
    // 另一个扩展（例如 IDM）已经把它关掉时，设回 true 会失败；保留标志以便下次再试。
    console.warn('[SheepGet] Failed to change Chrome download UI:', err);
  }
}
