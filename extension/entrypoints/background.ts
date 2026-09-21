import {
  DesktopClient,
  type DesktopEventLink,
  DEFAULT_LOOPBACK_PORT,
  discoverSessionViaHttp,
} from '../lib/client';
import { ResponseFilenameCache } from '../lib/filenames';
import { LINK_VERIFY_TTL_MS, planHandoverFailure, shouldReverifyLink } from '../lib/handover';
import {
  isMediaResponse,
  isHlsSegment,
  parseContentDispositionFilename,
  type MediaResource,
} from '../lib/media';
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
  HLSVariantsResponse,
  MediaProbeInfo,
  SessionMetadata,
  TakeoverConfigSync,
} from '../lib/types';

let currentConfig: TakeoverConfigSync = DEFAULT_TAKEOVER_CONFIG;
let currentKeyMask = 0;

// 与桌面端的链路。桌面端每次启动都换 loopback 端口，所以「连过」不等于「还连着」：
// linkOnline 只在真实 ping 成功或事件长连接打开时置位，在 ping 失败、长连接关闭、
// 交接失败时置否。界面状态读它，不再读「是否存过会话」。
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

// 页面标题/真实 URL 的缓存。content script 上报标题通常在资源请求之后，这里缓存起来，
// 后到的资源在构建时直接取用，不必等下一次上报。
const tabPageContext = new Map<number, { title: string; url: string }>();

// 响应头里的真实文件名，供 onCreated 的接管判定与交接使用（见 lib/filenames.ts）。
const responseFilenames = new ResponseFilenameCache();

// 探测展示信息（时长/大小）的 URL 级缓存：同一地址的结果不会变，面板反复打开、
// 页面反复刷新都只算一次。service worker 被挂起重启后缓存随内存消失，那只是
// 「多探一次」，不值得为此引入持久化。
const mediaProbeCache = new Map<string, MediaProbeInfo>();
const MEDIA_PROBE_CACHE_LIMIT = 200;
// 同一 URL 正在进行的探测：并发请求共享同一个 promise，不打两遍桌面端。
const mediaProbeInFlight = new Map<string, Promise<MediaProbeInfo | null>>();
// 最近一次探测失败的 URL 与时刻。离线时每条资源入池都会排队探测，冷却避免它们
// 各自白跑一趟 ensureDesktop；成功的结果进缓存，不经过这里。
const mediaProbeFailedAt = new Map<string, number>();
const MEDIA_PROBE_RETRY_COOLDOWN_MS = 60_000;

function cacheMediaProbe(url: string, info: MediaProbeInfo) {
  if (mediaProbeCache.size >= MEDIA_PROBE_CACHE_LIMIT) {
    // Map 迭代按插入序：淘汰最早进入的一条。
    const oldest = mediaProbeCache.keys().next().value;
    if (oldest !== undefined) mediaProbeCache.delete(oldest);
  }
  mediaProbeCache.set(url, info);
}

/**
 * 探测一条资源的展示信息：命中缓存立即返回，进行中的请求共享同一个 promise，
 * 未命中才打桌面端。面板的懒探测与嗅探时的后台预探测走的是这一个入口，
 * 因此「打开面板时重新算一遍」这件事在结构上不会发生。
 */
function probeMediaCached(req: {
  url: string;
  filename?: string;
  mimeType?: string;
  isHls?: boolean;
  totalBytes?: number;
  pageUrl?: string;
}): Promise<MediaProbeInfo | null> {
  const cached = mediaProbeCache.get(req.url);
  if (cached) return Promise.resolve(cached);
  const pending = mediaProbeInFlight.get(req.url);
  if (pending) return pending;

  const promise = (async () => {
    try {
      const info = await fetchMediaProbe(req);
      if (info && (info.durationSeconds || info.totalBytes || info.variants)) {
        cacheMediaProbe(req.url, info);
        mediaProbeFailedAt.delete(req.url);
        return info;
      }
      mediaProbeFailedAt.set(req.url, Date.now());
      return info;
    } finally {
      mediaProbeInFlight.delete(req.url);
    }
  })();
  mediaProbeInFlight.set(req.url, promise);
  return promise;
}

// 后台预探测队列。资源一入池就排队，一次跑一条：HLS 探测要读清单、可能逐分片询问，
// 并发多份互相抢带宽，串行让先到的资源先出结果。
const mediaProbeQueue: MediaResource[] = [];
let mediaProbeQueueRunning = false;

function scheduleMediaProbe(resource: MediaResource) {
  if (mediaProbeCache.has(resource.url)) return;
  if (mediaProbeInFlight.has(resource.url)) return;
  const failedAt = mediaProbeFailedAt.get(resource.url);
  if (failedAt !== undefined && Date.now() - failedAt < MEDIA_PROBE_RETRY_COOLDOWN_MS) return;
  if (mediaProbeQueue.some((r) => r.url === resource.url)) return;
  mediaProbeQueue.push(resource);
  void runMediaProbeQueue();
}

async function runMediaProbeQueue() {
  if (mediaProbeQueueRunning) return;
  mediaProbeQueueRunning = true;
  try {
    while (mediaProbeQueue.length > 0) {
      const resource = mediaProbeQueue.shift()!;
      const info = await probeMediaCached({
        url: resource.url,
        filename: resource.filename,
        mimeType: resource.mimeType,
        isHls: resource.isHls,
        totalBytes: resource.totalBytes,
        pageUrl: resource.pageUrl,
      });
      if (!info) {
        // 桌面端离线或这条探测失败：整队清空，失败的 URL 有冷却。桌面端上线后
        // 新入池的资源会重新触发预探测，不必在这里轮询重试。
        mediaProbeQueue.length = 0;
        return;
      }
      applyProbeToPool(resource.tabId, resource.url, info);
    }
  } finally {
    mediaProbeQueueRunning = false;
  }
}

/** 把预探测结果回填到该标签页里同地址的所有资源上，并推给页面（面板重开时直接可见）。 */
function applyProbeToPool(tabId: number, url: string, info: MediaProbeInfo) {
  const list = tabMediaPool.get(tabId);
  if (!list) return;
  let changed = false;
  for (const r of list) {
    if (r.url !== url) continue;
    r.probeDuration = info.durationSeconds;
    r.probeTotalBytes = info.totalBytes;
    r.probeVariants = info.variants;
    changed = true;
  }
  if (changed) void publishTabResources(tabId);
}

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
      // 面板打开意味着用户正看着这个标签页：顺手校准一次角标。
      // per-tab 角标会在导航等时机被浏览器重置，这里是低成本的自我修复点。
      if (tabId !== null) void updateBadge(tabId);
      sendResponse(tabId === null ? [] : tabMediaPool.get(tabId) || []);
      return true;
    } else if (msg?.type === 'GET_MEDIA_PROBE') {
      void (async () => {
        sendResponse(
          await fetchMediaProbe({
            url: msg.url,
            filename: msg.filename,
            mimeType: msg.mimeType,
            isHls: msg.isHls,
            totalBytes: msg.totalBytes,
            pageUrl: msg.pageUrl,
          }),
        );
      })();
      return true;
    } else if (msg?.type === 'REPORT_PAGE_CONTEXT') {
      // content script 上报的页面标题/URL。iframe 也会上报，但主框架的标题才是视频名
      // 的可靠来源——只接受顶层框架的，避免把嵌入框架的空标题/重复标题盖掉真名。
      if (sender.frameId === 0) {
        applyPageContext(sender.tab?.id, msg.pageTitle, msg.pageUrl);
      }
      sendResponse(undefined);
    } else if (msg?.type === 'HANDOVER_MEDIA') {
      void (async () => {
        const result = await handleMediaHandover(
          msg.resource as MediaResource,
          msg.resource.variantUri,
        );
        sendResponse(result);
      })();
      return true;
    } else if (msg?.type === 'GET_HLS_VARIANTS') {
      void (async () => {
        sendResponse(await fetchHLSVariants(msg.url, msg.pageUrl));
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

      // HLS 分片（.ts / audio|video/mp2t）不进入资源池：它们是清单的内部实现细节，
      // 真正要下载的是 m3u8 清单（或其最终视频）。放进来会把面板刷成几百个 seg*.ts，
      // 还会让悬浮条的兜底关联误命中第一个分片而不是清单。
      if (isHlsSegment(details.url, contentType)) return;

      let list = tabMediaPool.get(details.tabId);
      if (!list) {
        list = [];
        tabMediaPool.set(details.tabId, list);
      }

      const filename =
        headerFilename ||
        details.url.split('?')[0]?.split('/').pop() ||
        (detected.isHls ? 'stream.m3u8' : 'media.mp4');

      // HLS 清单的 URL 末段往往是 index.m3u8，真正的视频名在页面标题里。
      // 命名优先级：Content-Disposition 头 → 页面标题（补上媒体后缀）→ URL 末段。
      // 标题可能尚未上报（媒体请求先于 content script 就绪），拿到时后补。
      const pageCtx = tabPageContext.get(details.tabId);
      const suggestedName = suggestFilename(filename, detected, pageCtx?.title);

      const resource: MediaResource = {
        id: `${details.tabId}_${Date.now()}_${Math.random().toString(36).slice(2, 7)}`,
        url: details.url,
        tabId: details.tabId,
        filename: suggestedName,
        mimeType: detected.mime,
        totalBytes: contentLength > 0 ? contentLength : undefined,
        isHls: detected.isHls,
        pageTitle: pageCtx?.title,
        pageUrl: pageCtx?.url,
        foundAt: Date.now(),
      };
      if (!addResourceOnce(list, resource)) return undefined;
      void updateBadge(details.tabId);
      // 预探测：时长与大小现在就在后台算好并缓存，面板打开时直接有结果，
      // 而不是每次打开都从头问一遍桌面端。
      scheduleMediaProbe(resource);
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
    tabPageContext.delete(tabId);
  });
  chrome.tabs.onUpdated.addListener((tabId, changeInfo) => {
    if (changeInfo.status === 'loading' && changeInfo.url) {
      tabMediaPool.delete(tabId);
      tabPageContext.delete(tabId);
      void updateBadge(tabId);
    }
    // 导航完成时校准一次角标：per-tab 角标可能在导航过程中被浏览器重置，
    // 资源池里已有的媒体数量就是此刻该显示的数字。
    if (changeInfo.status === 'complete') {
      void updateBadge(tabId);
    }
  });
});

// publishTabResources 把某个标签页的最新嗅探结果推给页面内的悬浮条。
// 播放器可能在 iframe 里，而 tabs.sendMessage 默认只到主框架；遍历所有 frame 逐个投递，
// 才能让 iframe 内的悬浮条也拿到新嗅探到的资源（否则只有启动那次 GET_TAB_MEDIA 拉到的旧结果）。
async function publishTabResources(tabId: number) {
  const resources = tabMediaPool.get(tabId) || [];
  const payload = { type: 'TAB_MEDIA_UPDATED' as const, resources };

  let frameIds: number[] = [0];
  try {
    const frames = await chrome.webNavigation.getAllFrames({ tabId });
    frameIds = (frames ?? []).filter((f) => f.frameId !== undefined).map((f) => f.frameId);
  } catch {
    // 拿不到 frame 列表（如特权页）就只投主框架
  }

  for (const frameId of frameIds) {
    chrome.tabs.sendMessage(tabId, payload, { frameId }, () => {
      void chrome.runtime.lastError;
    });
  }
}

/**
 * 把页面标题与真实 URL 回填到该标签页已嗅探到的所有资源上。
 * 资源池里的条目是在页面标题可读之前（onHeadersReceived）写进去的，标题只能后补；
 * 后到的资源在构建时也会读到已经缓存的标题（见 tabPageContext）。
 */
function applyPageContext(tabId: number | undefined, pageTitle: string, pageUrl: string) {
  if (tabId === undefined || tabId < 0) return;
  tabPageContext.set(tabId, { title: pageTitle, url: pageUrl });

  const list = tabMediaPool.get(tabId);
  if (!list) return;
  for (const r of list) {
    if (!r.pageTitle && pageTitle) r.pageTitle = pageTitle;
    // pageUrl 始终用真实页面地址，不能用 m3u8 的清单地址冒充。
    r.pageUrl = pageUrl;
    // 标题补到之后，之前按 index.m3u8 命名的 HLS 资源要重新用真名命名。
    r.filename = suggestFilename(r.filename, { isHls: r.isHls, mime: r.mimeType }, pageTitle);
  }
}

/**
 * 生成建议文件名。命名来源优先级与 IDM 一致：
 *   1. 已经落定的文件名（Content-Disposition 头给出的真名）原样保留；
 *   2. 否则若 URL 末段是 index.m3u8 / stream.m3u8 这类占位名，且拿到了页面标题，
 *      用标题 + 媒体后缀命名；
 *   3. 否则保留 URL 末段。
 */
function suggestFilename(
  fallback: string,
  detected: { isHls: boolean; mime: string },
  pageTitle?: string,
): string {
  const urlBasename = fallback.split('?')[0]?.split('/').pop() || '';
  const isPlaceholder =
    !urlBasename || /^(index|stream|playlist|master|chunklist)\.m3u8?$/i.test(urlBasename);
  if (!isPlaceholder || !pageTitle) return fallback;

  const suffix = detected.isHls ? '.mp4' : `.${detected.mime.split('/').pop() || 'mp4'}`;
  return sanitizeFilename(pageTitle + suffix);
}

/** 去掉文件名里的非法字符，避免把换行/路径分隔符/控制符带进文件名。 */
function sanitizeFilename(name: string): string {
  const cleaned = name
    .replace(/[\\/:*?"<>|\u0000-\u001f]/g, ' ')
    .replace(/[ \t]+/g, ' ')
    .trim();
  return cleaned || 'media';
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

async function handleMediaHandover(
  resource: MediaResource,
  variantUri?: string,
): Promise<HandoverResponse> {
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
    variantUri,
  };

  return await handOverWithRetry(handoverReq, 3000, 'media-handover');
}

/** 拉取一份清单的可选清晰度：带 cookies/referer 转发给桌面端解析。 */
async function fetchHLSVariants(url: string, pageUrl?: string): Promise<HLSVariantsResponse> {
  let cookiesStr = '';
  try {
    const cookies = await chrome.cookies.getAll({ url });
    cookiesStr = cookies.map((c) => `${c.name}=${c.value}`).join('; ');
  } catch {
    // Ignore cookie error
  }

  const client = await ensureDesktop();
  if (!client) {
    return { variants: [] };
  }
  const resp = await client.fetchHLSVariants(url, {
    cookies: cookiesStr,
    headers: {
      'User-Agent': navigator.userAgent,
      ...(pageUrl ? { Referer: pageUrl } : {}),
    },
  });
  return resp ?? { variants: [] };
}

/**
 * 面板资源项的展示信息探测（时长与大小），经桌面端完成。请求带着嗅探时已有的
 * 上下文（Cookie/Referer），防盗链的清单与媒体不带它们只会得到 403。桌面端离线或
 * 探测失败时返回 null——面板保持「未知大小」的现状，不为此阻塞任何交互。
 */
async function fetchMediaProbe(req: {
  url: string;
  filename?: string;
  mimeType?: string;
  isHls?: boolean;
  totalBytes?: number;
  pageUrl?: string;
}): Promise<MediaProbeInfo | null> {
  let cookiesStr = '';
  try {
    const cookies = await chrome.cookies.getAll({ url: req.url });
    cookiesStr = cookies.map((c) => `${c.name}=${c.value}`).join('; ');
  } catch {
    // Ignore cookie error
  }

  const client = await ensureDesktop();
  if (!client) return null;
  return await client.fetchMediaProbe(
    {
      url: req.url,
      filename: req.filename,
      mimeType: req.mimeType,
      isHls: req.isHls,
      totalBytes: req.totalBytes,
    },
    {
      cookies: cookiesStr,
      headers: {
        'User-Agent': navigator.userAgent,
        ...(req.pageUrl ? { Referer: req.pageUrl } : {}),
      },
    },
  );
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
  const stored = await getStoredSession();
  if (!desktopClient && stored) {
    const client = new DesktopClient(stored);
    if (await client.ping(PING_TIMEOUT_MS)) {
      adoptLink(stored, client);
      console.info(`[SheepGet] Desktop link re-established on stored port ${stored.port}`);
      return desktopStatus();
    }
  }

  // 优先通过本地 HTTP 直接探测（为便携版与免 Host 模式提供纯净 HTTP 通信链路）
  // 按优先级探测：上次成功连接的端口、默认端口 9248
  const candidatePorts = Array.from(
    new Set(
      [stored?.port, DEFAULT_LOOPBACK_PORT].filter(
        (p): p is number => typeof p === 'number' && p > 0,
      ),
    ),
  );
  for (const port of candidatePorts) {
    const httpDiscovered = await discoverSessionViaHttp(port);
    if (httpDiscovered) {
      const client = new DesktopClient(httpDiscovered);
      if (await client.ping(PING_TIMEOUT_MS)) {
        adoptLink(httpDiscovered, client);
        void setStoredSession(httpDiscovered);
        console.info(
          `[SheepGet] Desktop link established via direct HTTP on port ${httpDiscovered.port} (${trigger})`,
        );
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
 * 采用一个已被证实可用的会话：替换长连接、刷新存活时刻、对齐接管配置。
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
}

/**
 * 标记链路不可用。不改动 desktopClient：下一次 reverifyLink 会重新验证并替换它。
 * 浏览器下载界面始终归浏览器所有：链路断掉时它本来就在正常反馈，这里不需要动任何东西。
 */
function markLinkOffline(reason: string) {
  if (!linkOnline && linkOfflineReason === reason) return;
  console.warn(`[SheepGet] Desktop link offline: ${reason}`);
  linkOnline = false;
  linkOfflineReason = reason;
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

/**
 * 一次下载该不该接管，同时决定这次下载的浏览器反馈归谁。
 *
 * 接管按次进行：判定要接管就地 pause 这一次下载（下一行就发出来，不等任何网络），
 * 交接成功 cancel、失败 resume；判定不接管则完全不插手——浏览器该有的气泡、动画、
 * downloads 页记录一个不少。
 *
 * 注意 downloads.ui 的 setUiOptions：它作用于整个 profile，一旦压下，连「不接管」的下载
 * 也没有任何可见反馈，用户看到的就是文件静默丢失。接管只动被接管的那一次。
 */
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
  // `item.mime` 是这次响应真实的 Content-Type：后缀命中但内容其实是页面/脚本时
  // （`.ts` 的 TypeScript 源码、签名过期后返回 HTML 错误页的 `.mp4`），规则会否决接管。
  const decision = decideTakeover(
    url,
    filename,
    item.referrer,
    currentConfig,
    currentKeyMask,
    item.mime,
  );
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
      // 桌面端确实接不了：把这次下载还给浏览器。我们只挂起了这一次，resume 之后
      // 它是浏览器的一个普通下载，气泡、进度与下载页记录都照常。
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
