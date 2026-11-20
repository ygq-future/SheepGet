import { after, describe, it } from 'node:test';
import assert from 'node:assert/strict';
import type * as backgroundModule from '../entrypoints/background';
import type { MediaResource } from './media';

const CONFIG_KEY = 'sheepget_takeover_config';
const TARGET_KEY = 'sheepget_download_target_config';

interface CapturedHandover {
  url: string;
  body: string;
}

type HeadersReceivedDetails = {
  url: string;
  tabId: number;
  statusCode?: number;
  responseHeaders?: { name: string; value?: string }[];
};

interface SniffHarness {
  chrome: unknown;
  handovers: CapturedHandover[];
  hlsRequests: CapturedHandover[];
  fetch: typeof fetch;
  send(message: unknown, sender: unknown): Promise<unknown>;
  emitHeaders(details: HeadersReceivedDetails): void;
}

function jsonResponse(body: unknown, init: ResponseInit = {}): Response {
  const headers = new Headers(init.headers);
  headers.set('Content-Type', 'application/json');
  return new Response(JSON.stringify(body), { ...init, headers });
}

class FakeWebSocket {
  static readonly CONNECTING = 0;
  static readonly OPEN = 1;
  static readonly CLOSING = 2;
  static readonly CLOSED = 3;

  readyState = FakeWebSocket.OPEN;
  onopen: ((ev: Event) => void) | null = null;
  onmessage: ((ev: MessageEvent) => void) | null = null;
  onclose: ((ev: CloseEvent) => void) | null = null;
  onerror: ((ev: Event) => void) | null = null;

  constructor(_url: string) {
    queueMicrotask(() => {
      this.onopen?.(new Event('open'));
    });
  }

  close() {
    this.readyState = FakeWebSocket.CLOSED;
  }
}

function makeHarness(pageUrl: string): SniffHarness {
  const stored: Record<string, unknown> = {
    [CONFIG_KEY]: {
      version: 9,
      extensions: ['zip'],
      excludedSites: [],
      pauseShortcut: 'Delete',
      forceShortcut: 'Insert',
    },
    [TARGET_KEY]: { type: 'sheepget', aria2: { rpcUrl: '', secret: '' }, webhook: { url: '' } },
  };
  const handovers: CapturedHandover[] = [];
  const hlsRequests: CapturedHandover[] = [];

  let messageListener:
    ((msg: unknown, sender: unknown, sendResponse: (r: unknown) => void) => unknown) | null = null;
  let headersListener: ((details: HeadersReceivedDetails) => unknown) | null = null;

  const chrome = {
    runtime: {
      id: 'test-extension',
      lastError: undefined,
      onMessage: {
        addListener: (listener: typeof messageListener) => {
          messageListener = listener;
        },
      },
      onInstalled: { addListener() {} },
      onStartup: { addListener() {} },
      sendMessage: async () => undefined,
    },
    storage: {
      local: {
        get: async (keys: string | string[]) => {
          const wanted = Array.isArray(keys) ? keys : [keys];
          const result: Record<string, unknown> = {};
          for (const key of wanted) {
            if (key in stored) result[key] = stored[key];
          }
          return result;
        },
        set: async (values: Record<string, unknown>) => {
          Object.assign(stored, values);
        },
        remove: async () => {},
      },
      session: { get: async () => ({}), set: async () => {}, remove: async () => {} },
      onChanged: { addListener() {} },
    },
    downloads: {
      onCreated: { addListener() {} },
      onChanged: { addListener() {} },
      cancel: async () => {},
      erase: async () => {},
      download: async () => 1,
      pause: async () => {},
      resume: async () => {},
      search: async () => [],
    },
    tabs: {
      onActivated: { addListener() {} },
      onRemoved: { addListener() {} },
      onUpdated: { addListener() {} },
      sendMessage: async () => undefined,
      query: async () => [{ id: 7, url: pageUrl }],
      get: async (id: number) => ({ id, url: pageUrl }),
    },
    windows: { onRemoved: { addListener() {} }, getCurrent: async () => ({ id: 1 }) },
    webNavigation: {
      onCommitted: { addListener() {} },
      onHistoryStateUpdated: { addListener() {} },
      getAllFrames: async () => [{ frameId: 0 }],
    },
    webRequest: {
      onHeadersReceived: {
        addListener: (listener: typeof headersListener) => {
          headersListener = listener;
        },
      },
      onBeforeRequest: { addListener() {} },
      onSendHeaders: { addListener() {} },
    },
    action: {
      setBadgeText: async () => {},
      setBadgeBackgroundColor: async () => {},
      onClicked: { addListener() {} },
    },
    alarms: { onAlarm: { addListener() {} }, create: async () => {}, clear: async () => {} },
    cookies: { getAll: async () => [{ name: 'age_verified', value: '1' }] },
  };

  const fetchStub: typeof fetch = async (input, init) => {
    const url =
      typeof input === 'string' ? input : input instanceof URL ? input.toString() : input.url;

    if (url.endsWith('/api/v1/discover')) {
      return jsonResponse({ status: 'ok', port: 9248, sessionToken: 'tok' });
    }
    if (url.endsWith('/api/v1/ping')) {
      return jsonResponse({});
    }
    if (url.endsWith('/api/v1/config/takeover')) {
      if (init?.method === 'HEAD') {
        return new Response(null, { status: 200, headers: { 'X-Config-Version': '9' } });
      }
      return jsonResponse({
        version: 9,
        extensions: ['zip'],
        excludedSites: [],
        pauseShortcut: 'Delete',
        forceShortcut: 'Insert',
      });
    }
    if (url.endsWith('/api/v1/media/probe')) {
      return jsonResponse({});
    }
    if (url.endsWith('/api/v1/hls/variants')) {
      hlsRequests.push({ url, body: String(init?.body ?? '') });
      return jsonResponse({
        variants: [{ uri: 'https://cdn.phncdn.com/1080p.m3u8', label: '1080P' }],
      });
    }
    if (url.endsWith('/api/v1/handover')) {
      handovers.push({ url, body: String(init?.body ?? '') });
      return jsonResponse({ accepted: true, queueItemId: 'queue-item-1' });
    }
    return new Response('not found', { status: 404 });
  };

  return {
    chrome,
    handovers,
    hlsRequests,
    fetch: fetchStub,
    send(message: unknown, sender: unknown): Promise<unknown> {
      const { promise, resolve } = Promise.withResolvers<unknown>();
      const handled = messageListener?.(message, sender, resolve);
      if (handled !== true) resolve(undefined);
      return promise;
    },
    emitHeaders(details: HeadersReceivedDetails) {
      headersListener?.(details);
    },
  };
}

interface TestGlobals {
  chrome?: unknown;
  defineBackground?: unknown;
  fetch: typeof fetch;
  WebSocket: typeof WebSocket;
}

const testGlobals: TestGlobals = globalThis;
const realFetch = testGlobals.fetch;
const realWebSocket = testGlobals.WebSocket;
const realChrome = testGlobals.chrome;
const realDefineBackground = testGlobals.defineBackground;

let instances = 0;

async function startServiceWorker(harness: SniffHarness): Promise<void> {
  testGlobals.chrome = harness.chrome;
  testGlobals.defineBackground = (main: () => void) => main;
  testGlobals.fetch = harness.fetch;
  testGlobals.WebSocket = FakeWebSocket as unknown as typeof WebSocket;

  // 入口在模块求值时注册监听器；必须先注入 mock chrome 再按查询串隔离加载（模块加载边界）。
  const definition = (await import(
    `../entrypoints/background?sniff=${++instances}`
  )) as typeof backgroundModule;
  (definition.default as unknown as () => void)();
}

after(() => {
  testGlobals.fetch = realFetch;
  testGlobals.WebSocket = realWebSocket;
  testGlobals.chrome = realChrome;
  testGlobals.defineBackground = realDefineBackground;
});

describe('media sniffing hygiene and Origin forwarding', () => {
  it('filters .php DOM media reports, ignores non-2xx responses, normalizes .php media responses, and forwards Origin', async () => {
    const pageUrl = 'https://cn.pornhub.com/view_video.php?viewkey=696f01bad407a';
    const harness = makeHarness(pageUrl);
    await startServiceWorker(harness);

    // 1. Report real page context first
    await harness.send(
      { type: 'REPORT_PAGE_CONTEXT', pageTitle: 'Test PH Video', pageUrl },
      { tab: { id: 7 }, frameId: 0 },
    );

    // 2. DOM fallback reporting .php page URL must NOT enter media pool
    await harness.send(
      {
        type: 'REPORT_DOM_MEDIA',
        resources: [{ url: pageUrl }, { url: 'https://cn.pornhub.com/get_media.php?s=abc' }],
      },
      { tab: { id: 7 }, frameId: 0 },
    );

    let mediaList = (await harness.send(
      { type: 'GET_TAB_MEDIA', tabId: 7 },
      { frameId: 0 },
    )) as MediaResource[];
    assert.equal(mediaList.length, 0);

    // 3. Non-2xx network responses (302/403/412) must NOT enter media pool
    harness.emitHeaders({
      url: 'https://cn.pornhub.com/redirect.php',
      tabId: 7,
      statusCode: 302,
      responseHeaders: [{ name: 'Content-Type', value: 'video/mp4' }],
    });
    harness.emitHeaders({
      url: 'https://cdn.phncdn.com/blocked.m3u8',
      tabId: 7,
      statusCode: 412,
      responseHeaders: [{ name: 'Content-Type', value: 'application/vnd.apple.mpegurl' }],
    });

    mediaList = (await harness.send(
      { type: 'GET_TAB_MEDIA', tabId: 7 },
      { frameId: 0 },
    )) as MediaResource[];
    assert.equal(mediaList.length, 0);

    // 4. Genuine 200 video/mp4 served from a .php endpoint gets normalized to pageTitle.mp4
    harness.emitHeaders({
      url: 'https://example.com/stream.php?id=99',
      tabId: 7,
      statusCode: 200,
      responseHeaders: [
        { name: 'Content-Type', value: 'video/mp4' },
        { name: 'Content-Length', value: '1048576' },
      ],
    });

    // 5. Genuine 200 HLS manifest: playlist text Content-Length must not be reported as media totalBytes
    harness.emitHeaders({
      url: 'https://cv-h.phncdn.com/videos/master.m3u8?ttl=123',
      tabId: 7,
      statusCode: 200,
      responseHeaders: [
        { name: 'Content-Type', value: 'application/vnd.apple.mpegurl' },
        { name: 'Content-Length', value: '212' },
      ],
    });

    mediaList = (await harness.send(
      { type: 'GET_TAB_MEDIA', tabId: 7 },
      { frameId: 0 },
    )) as MediaResource[];
    assert.equal(mediaList.length, 2);
    assert.equal(mediaList[0]?.filename, 'Test PH Video.mp4');
    assert.equal(mediaList[0]?.totalBytes, 1048576);
    assert.equal(mediaList[1]?.filename, 'Test PH Video.mp4');
    assert.equal(mediaList[1]?.totalBytes, undefined);
    // 6. Fetching HLS variants & handing over media forward both Origin and Referer
    await harness.send(
      { type: 'GET_HLS_VARIANTS', url: mediaList[1]!.url, pageUrl },
      { tab: { id: 7 }, frameId: 0 },
    );
    assert.equal(harness.hlsRequests.length, 1);
    const hlsBody = JSON.parse(harness.hlsRequests[0]!.body) as {
      credentials?: { headers?: Record<string, string> };
    };
    assert.equal(hlsBody.credentials?.headers?.Origin, 'https://cn.pornhub.com');
    assert.equal(hlsBody.credentials?.headers?.Referer, pageUrl);

    const handoverResp = await harness.send(
      { type: 'HANDOVER_MEDIA', resource: mediaList[1], tabId: 7 },
      { tab: { id: 7 }, frameId: 0 },
    );
    assert.deepEqual(handoverResp, { accepted: true, queueItemId: 'queue-item-1' });
    assert.equal(harness.handovers.length, 1);
    const handoverBody = JSON.parse(harness.handovers[0]!.body) as {
      credentials?: { headers?: Record<string, string> };
    };
    assert.equal(handoverBody.credentials?.headers?.Origin, 'https://cn.pornhub.com');
    assert.equal(handoverBody.credentials?.headers?.Referer, pageUrl);
  });
});
